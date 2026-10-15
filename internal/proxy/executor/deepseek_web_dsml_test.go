package executor

import (
	json "encoding/json/v2"
	"strings"
	"testing"
)

// The DSML block a live answer actually produced, verbatim, including the
// padding spaces and the fullwidth bar the model wrote.
const liveDSMLAnswer = " <｜｜DSML｜｜ calls>\n" +
	" <｜｜DSML｜｜ invoke name=\"web_search\">\n" +
	" <｜｜DSML｜｜ parameter name=\"query\" string=\"true\">cuaca Jakarta hari ini 15 Oktober 2026<｜｜DSML｜｜ parameter>\n" +
	" <｜｜DSML｜｜ parameter name=\"i\" string=\"true\">Searching Jakarta weather<｜｜DSML｜｜ parameter>\n" +
	" <｜｜DSML｜｜ parameter name=\"recency\" string=\"day\"><｜｜DSML｜｜ parameter>\n" +
	" <｜｜DSML｜｜ invoke>\n" +
	" <｜｜DSML｜｜ invoke name=\"web_search\">\n" +
	" <｜｜DSML｜｜ parameter name=\"query\" string=\"true\">Tokyo weather today October 15 2026<｜｜DSML｜｜ parameter>\n" +
	" <｜｜DSML｜｜ parameter name=\"i\" string=\"true\">Searching Tokyo weather<｜｜DSML｜｜ parameter>\n" +
	" <｜｜DSML｜｜ parameter name=\"recency\" string=\"day\"><｜｜DSML｜｜ parameter>\n" +
	" <｜｜DSML｜｜ invoke>\n" +
	" </｜｜DSML｜｜ calls>"

func webSearchTool() deepSeekToolSpec {
	var spec deepSeekToolSpec
	spec.Type = "function"
	spec.Function.Name = "web_search"
	spec.Function.Description = "search the web"
	return spec
}

// The whole point: a DSML answer used to reach the client as literal markup,
// because the extractor only ever looked for JSON. The client then had no call to
// run and read the whole turn as a refusal.
func TestDeepSeekExtractsDSMLToolCall(t *testing.T) {
	call, ok := deepSeekExtractToolCall(liveDSMLAnswer, []deepSeekToolSpec{webSearchTool()})
	if !ok {
		t.Fatal("a DSML call was not recognised")
	}
	if call.Name != "web_search" {
		t.Errorf("name = %q, want web_search", call.Name)
	}

	var args struct {
		Query   string `json:"query"`
		Info    string `json:"i"`
		Recency string `json:"recency"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		t.Fatalf("arguments are not a JSON object: %v\n%s", err, call.Arguments)
	}
	if args.Query != "cuaca Jakarta hari ini 15 Oktober 2026" {
		t.Errorf("query = %q, want the first invoke's query", args.Query)
	}
	if args.Info != "Searching Jakarta weather" {
		t.Errorf("i = %q, want the first invoke's info", args.Info)
	}
	// `string="day"` in the live answer is an attribute of the element, not its
	// body — the model wrote an empty value for recency. Reading it as content
	// would put a literal `string="day"` where the tool expects a value.
	if args.Recency != "" {
		t.Errorf("recency = %q, want empty: it was an attribute, not a value", args.Recency)
	}
}

// A parameter value is element text and must survive verbatim: trimming or
// re-quoting it changes what the tool receives.
func TestDeepSeekDSMLKeepsParameterTextVerbatim(t *testing.T) {
	answer := "<｜｜DSML｜｜ invoke name=\"web_search\">" +
		"<｜｜DSML｜｜ parameter name=\"query\" string=\"true\">a \"quoted\" value<｜｜DSML｜｜ parameter>" +
		"<｜｜DSML｜｜ invoke>"

	call, ok := deepSeekExtractToolCall(answer, []deepSeekToolSpec{webSearchTool()})
	if !ok {
		t.Fatal("a single un-wrapped invoke was not recognised")
	}

	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		t.Fatalf("arguments do not parse: %v", err)
	}
	if args.Query != `a "quoted" value` {
		t.Errorf("query = %q, want the quotes preserved", args.Query)
	}
}

// An undeclared tool must not be forwarded, in this dialect either.
func TestDeepSeekDSMLUndeclaredToolIsRejected(t *testing.T) {
	answer := "<｜｜DSML｜｜ invoke name=\"delete_everything\">" +
		"<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">/<｜｜DSML｜｜ parameter>" +
		"<｜｜DSML｜｜ invoke>"

	if _, ok := deepSeekExtractToolCall(answer, []deepSeekToolSpec{webSearchTool()}); ok {
		t.Error("a tool the client never declared must not be returned as a call")
	}
}

// The same conservative rule as the JSON path: markup that names no tool is not
// an answer, and showing it to the user reads as corruption.
func TestDeepSeekStripsUnparsedDSML(t *testing.T) {
	got := deepSeekStripDSML("Saya cek dulu.\n" + liveDSMLAnswer)
	if strings.Contains(got, "DSML") {
		t.Errorf("DSML survived into user-visible content:\n%s", got)
	}
	if got != "Saya cek dulu." {
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
	truncated := "<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"web_search\">\n" +
		"<｜｜DSML｜｜ parameter name=\"query\" string=\"true\">half"
	if _, ok := deepSeekExtractToolCall(truncated, []deepSeekToolSpec{webSearchTool()}); ok {
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
		`{"name":"web_search","arguments":{"query":"jakarta"}}`,
		[]deepSeekToolSpec{webSearchTool()})
	if !ok {
		t.Fatal("the JSON path stopped working")
	}
	if !strings.Contains(call.Arguments, "jakarta") {
		t.Errorf("arguments = %s, want the query", call.Arguments)
	}
}
