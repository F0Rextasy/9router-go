package executor

import (
	json "encoding/json/v2"
	"strings"
	"testing"
)

// liveDSMLAnswer is a DSML tool call copied out of a real answer, byte for byte,
// newlines and all. It is not reconstructed from a reading of the format.
//
// That distinction is the whole reason this file exists twice over. The first
// version of it wrote the closing tags without a slash — `<｜｜DSML｜｜ invoke>` —
// because that is what the notation looks like when it is described in prose. The
// live form closes with a slash, `</｜｜DSML｜｜ invoke>`, and a parser keyed on the
// invented spelling matched nothing at all: every DSML call stayed unparsed and
// reached the client as literal markup. A test that had used the real bytes would
// have failed immediately.
const liveDSMLAnswer = `<｜｜DSML｜｜ calls>
<｜｜DSML｜｜ invoke name="read">
<｜｜DSML｜｜ parameter name="path" string="true">https://wttr.in/Jakarta?format=j1</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="i" string="true">Fetching Jakarta weather</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
<｜｜DSML｜｜ invoke name="read">
<｜｜DSML｜｜ parameter name="path" string="true">https://wttr.in/Tokyo?format=j1</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="i" string="true">Fetching Tokyo weather</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
</｜｜DSML｜｜ calls>`

func readTool() deepSeekToolSpec {
	var spec deepSeekToolSpec
	spec.Type = "function"
	spec.Function.Name = "read"
	spec.Function.Description = "read a file or url"
	return spec
}

// The whole point: a DSML answer reached the client as literal markup, because the
// parser never matched the real closing tags. The client then had no call to run
// and read the whole turn as a refusal.
func TestDeepSeekExtractsDSMLToolCall(t *testing.T) {
	call, ok := deepSeekExtractToolCall(liveDSMLAnswer, []deepSeekToolSpec{readTool()})
	if !ok {
		t.Fatal("a DSML call was not recognised")
	}
	if call.Name != "read" {
		t.Errorf("name = %q, want read", call.Name)
	}

	var args struct {
		Path string `json:"path"`
		Info string `json:"i"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		t.Fatalf("arguments are not a JSON object: %v\n%s", err, call.Arguments)
	}
	if args.Path != "https://wttr.in/Jakarta?format=j1" {
		t.Errorf("path = %q, want the first invoke's url", args.Path)
	}
	if args.Info != "Fetching Jakarta weather" {
		t.Errorf("i = %q, want the first invoke's info", args.Info)
	}
}

// The close tags are recognised in both spellings. A harness that omits the slash
// has to keep working, and a parser that only accepts one of them fails silently.
func TestDeepSeekDSMLAcceptsBothCloseSpellings(t *testing.T) {
	tests := []struct {
		name  string
		close string
	}{
		{name: "with slash", close: "</｜｜DSML｜｜ invoke>"},
		{name: "without slash", close: "<｜｜DSML｜｜ invoke>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answer := "<｜｜DSML｜｜ invoke name=\"read\">" +
				"<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">/tmp/x</｜｜DSML｜｜ parameter>" +
				tt.close

			call, ok := deepSeekExtractToolCall(answer, []deepSeekToolSpec{readTool()})
			if !ok {
				t.Fatalf("close tag %q was not recognised", tt.close)
			}
			if !strings.Contains(call.Arguments, "/tmp/x") {
				t.Errorf("arguments = %s, want the path", call.Arguments)
			}
		})
	}
}

// An ASCII pipe is the same marker: the model emits either, and a build that
// normalises the text upstream may hand over the ASCII spelling.
func TestDeepSeekDSMLAcceptsASCIIPipe(t *testing.T) {
	answer := "<||DSML|| invoke name=\"read\">" +
		"<||DSML|| parameter name=\"path\" string=\"true\">/tmp/x</||DSML|| parameter>" +
		"</||DSML|| invoke>"

	call, ok := deepSeekExtractToolCall(answer, []deepSeekToolSpec{readTool()})
	if !ok {
		t.Fatal("an ASCII-pipe block was not recognised")
	}
	if !strings.Contains(call.Arguments, "/tmp/x") {
		t.Errorf("arguments = %s, want the path", call.Arguments)
	}
}

// A parameter value is element text and must survive verbatim: trimming or
// re-quoting it changes what the tool receives.
func TestDeepSeekDSMLKeepsParameterTextVerbatim(t *testing.T) {
	answer := "<｜｜DSML｜｜ invoke name=\"read\">" +
		"<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">a \"quoted\" value</｜｜DSML｜｜ parameter>" +
		"</｜｜DSML｜｜ invoke>"

	call, ok := deepSeekExtractToolCall(answer, []deepSeekToolSpec{readTool()})
	if !ok {
		t.Fatal("a single un-wrapped invoke was not recognised")
	}

	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		t.Fatalf("arguments do not parse: %v", err)
	}
	if args.Path != `a "quoted" value` {
		t.Errorf("path = %q, want the quotes preserved", args.Path)
	}
}

// An undeclared tool must not be forwarded, in this dialect either.
func TestDeepSeekDSMLUndeclaredToolIsRejected(t *testing.T) {
	answer := "<｜｜DSML｜｜ invoke name=\"delete_everything\">" +
		"<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">/</｜｜DSML｜｜ parameter>" +
		"</｜｜DSML｜｜ invoke>"

	if _, ok := deepSeekExtractToolCall(answer, []deepSeekToolSpec{readTool()}); ok {
		t.Error("a tool the client never declared must not be returned as a call")
	}
}

// The same conservative rule as the JSON path: markup that names no tool is not
// an answer, and showing it to the user reads as corruption.
func TestDeepSeekStripsUnparsedDSML(t *testing.T) {
	got := deepSeekStripDSML("I'll check that.\n" + liveDSMLAnswer)
	if strings.Contains(got, "DSML") {
		t.Errorf("DSML survived into user-visible content:\n%s", got)
	}
	if got != "I'll check that." {
		t.Errorf("prose around the block was lost:\n%s", got)
	}
}

// An answer with no DSML is returned untouched — the strip must not touch prose.
func TestDeepSeekStripDSMLLeavesPlainText(t *testing.T) {
	const answer = "Halo! Ada yang bisa dibantu?"
	if got := deepSeekStripDSML(answer); got != answer {
		t.Errorf("plain text was altered: %q", got)
	}
}

// Attributes are matched whole: a longer attribute name ending in `name` must not
// be read as the tool name.
func TestDeepSeekDSMLAttributeMatchesWholeName(t *testing.T) {
	attrs := ` toolname="wrong" name="right"`
	if got := deepSeekDSMLAttribute(attrs, "name"); got != "right" {
		t.Errorf("name = %q, want right", got)
	}
}

// A block the model was cut off partway through writing has no closing marker.
// It must not be mistaken for something to parse, and must not panic.
func TestDeepSeekDSMLTruncatedBlockIsSafe(t *testing.T) {
	truncated := "<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"read\">\n" +
		"<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">half"
	if _, ok := deepSeekExtractToolCall(truncated, []deepSeekToolSpec{readTool()}); ok {
		t.Error("a truncated block must not be parsed as a call")
	}
	if got := deepSeekStripDSML(truncated); got != "" {
		t.Errorf("a truncated block left content behind: %q", got)
	}
}

// DSML must not shadow the JSON path: an answer with no markers is still read as
// JSON, and an answer with markers is read as DSML.
func TestDeepSeekJSONPathUnaffectedByDSML(t *testing.T) {
	call, ok := deepSeekExtractToolCall(
		`{"name":"read","arguments":{"path":"/tmp/x"}}`,
		[]deepSeekToolSpec{readTool()})
	if !ok {
		t.Fatal("the JSON path stopped working")
	}
	if !strings.Contains(call.Arguments, "/tmp/x") {
		t.Errorf("arguments = %s, want the path", call.Arguments)
	}
}

// Both calls in the live answer are recoverable, in order. The second one is
// reachable only after the first is consumed, so the splitter must not stop at
// the first match.
func TestDeepSeekDSMLReadsEveryInvoke(t *testing.T) {
	invokes := deepSeekDSMLInvokes(liveDSMLAnswer)
	if len(invokes) != 2 {
		t.Fatalf("found %d invokes, want 2", len(invokes))
	}
	if !strings.Contains(invokes[0], "Jakarta") {
		t.Errorf("first invoke is not the Jakarta one:\n%s", invokes[0])
	}
	if !strings.Contains(invokes[1], "Tokyo") {
		t.Errorf("second invoke is not the Tokyo one:\n%s", invokes[1])
	}
}
