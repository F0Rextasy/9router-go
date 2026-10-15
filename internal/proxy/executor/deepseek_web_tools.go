package executor

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Tool calling on this upstream is not a protocol feature, it is a convention
// the model happens to follow.
//
// There is no `tools[]` parameter on /api/v0/chat/completion and no tool-call
// topic in the event stream. A model told a tool exists answers by *typing* a
// JSON object, one character at a time, through ordinary APPEND operations on the
// fragment content. Everything here exists because of that: a client treating
// the result as a normal streaming answer would hold
// `{"name":"get_weather","parameters":{"` with no way to tell a truncated tool
// call from prose.
//
// So the two paths are kept strictly apart. An ordinary chat streams as it always
// did and never enters this code. A request carrying `tools[]` is buffered to
// completion first, so what gets parsed is always whole JSON — the same guarantee
// the native API gives, since it does not stream tool arguments either.

// deepSeekToolSpec is one declared tool.
//
// `Parameters` is raw JSON because clients disagree about it: the OpenAI shape
// sends a JSON Schema object, while several SDKs stringify it first. Typing it as
// a string would reject the common form outright, so both are accepted and
// normalised where the schema is rendered.
type deepSeekToolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  jsontext.Value `json:"parameters"`
	} `json:"function"`
}

// UnmarshalJSON accepts both tool declaration spellings and folds them into the
// same shape, so nothing downstream has to know which one arrived.
//
// OpenAI wraps the declaration: {"type":"function","function":{...}}. The
// Claude/Anthropic spelling puts name, description and input_schema at the top
// level with no wrapper, and clients send it just as readily.
//
// The flat shape used to parse into a spec whose Function.Name was empty. The
// prompt builder skips those, so the model was told about no tools at all,
// reached for its native DSML instead, and the markup reached the client as if
// it were the answer — the client had no call to run and no question answered.
func (s *deepSeekToolSpec) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  jsontext.Value `json:"parameters"`
		} `json:"function"`
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema jsontext.Value `json:"input_schema"`
		Parameters  jsontext.Value `json:"parameters"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("deepseek-web: parse tool declaration: %w", err)
	}

	s.Type = raw.Type
	s.Function.Name = raw.Function.Name
	s.Function.Description = raw.Function.Description
	s.Function.Parameters = raw.Function.Parameters

	// The flat fields only fill what the wrapper left empty, so a declaration
	// carrying both spellings resolves to the nested one.
	if s.Function.Name == "" {
		s.Function.Name = raw.Name
	}
	if s.Function.Description == "" {
		s.Function.Description = raw.Description
	}
	if len(s.Function.Parameters) == 0 {
		s.Function.Parameters = raw.InputSchema
	}
	if len(s.Function.Parameters) == 0 {
		s.Function.Parameters = raw.Parameters
	}
	return nil
}

// deepSeekToolRequest is one call recovered from the model's answer.
type deepSeekToolRequest struct {
	Name      string
	Arguments string
	// Raw is the exact object text that was parsed. It lets the caller recover
	// whatever the model wrote around the object instead of discarding it.
	Raw string
}

// deepSeekSchemaText renders a declared schema for the prompt, accepting either
// shape and falling back to a permissive one. A schema that cannot be read is
// replaced rather than dropped: a less constrained call is recoverable, while a
// tool the model was never told about is not.
func deepSeekSchemaText(raw jsontext.Value) string {
	if len(raw) == 0 {
		return `{"type":"object","properties":{}}`
	}
	// A stringified schema arrives as a JSON string, not an object.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if trimmed := strings.TrimSpace(asString); deepSeekValidJSON(trimmed) {
			return trimmed
		}
		return `{"type":"object","properties":{}}`
	}
	if deepSeekValidJSON(string(raw)) {
		return string(raw)
	}
	return `{"type":"object","properties":{}}`
}

// deepSeekRenderTools folds the declared tools into prompt text.
//
// The schema is emitted as JSON rather than prose because a model reproducing a
// schema it has only seen in prose returns a differently-shaped object more often
// than not.
func deepSeekRenderTools(tools []deepSeekToolSpec) string {
	var b strings.Builder
	b.WriteString("You have access to the following tools.\n\n<tools>\n")
	for _, tool := range tools {
		if tool.Function.Name == "" {
			continue
		}
		fmt.Fprintf(&b, "{\"name\": %q, \"description\": %q, \"parameters\": %s}\n",
			tool.Function.Name, tool.Function.Description,
			deepSeekSchemaText(tool.Function.Parameters))
	}
	b.WriteString("</tools>\n\n")
	b.WriteString("To use a tool, reply with a single JSON object and nothing else: " +
		`{"name": "<tool name>", "arguments": {<arguments matching that tool's parameters>}}. ` +
		"Do not wrap it in prose or a code fence.")
	b.WriteString("\n\nRules for using tools:\n")
	b.WriteString("- A line reading \"[called <name> with <arguments>]\" means you already " +
		"made that call and its result is the following \"Tool result\" line. Do not repeat it.\n")
	b.WriteString("- Once you have a tool result for what was asked, answer the user in plain " +
		"text. Calling a tool again for the same information is an error.\n")
	b.WriteString("- Only call a tool when you actually need data you do not already have.\n")
	return b.String()
}

// deepSeekExtractToolCall recovers a tool call from the model's answer.
//
// The prompt asks for JSON and the model usually complies, but these models also
// have a native tool syntax they reach for on their own. Both are read here, and
// DSML is tried first: it is unambiguous, where a JSON-shaped object sitting in
// prose has to be guessed at.
//
// It is deliberately conservative. The answer must name one of the tools the
// client declared, in either dialect. Anything else — prose, an undeclared name,
// malformed JSON — returns false and reaches the client as ordinary content.
// Guessing here would turn a failed parse into an invocation the client then
// acts on.
func deepSeekExtractToolCall(text string, tools []deepSeekToolSpec) (deepSeekToolRequest, bool) {
	raw := strings.TrimSpace(text)
	if strings.Contains(raw, "DSML｜｜") {
		return deepSeekExtractDSML(raw, tools)
	}

	// The object has to be found inside the answer rather than expected at the
	// start. A model told "reply with a single JSON object and nothing else"
	// still prefixes it often enough to matter, and rejecting those turns is
	// what produced content:null and an agent retry loop.
	obj := deepSeekFindJSONObject(raw)
	if obj == "" {
		return deepSeekToolRequest{}, false
	}

	var call struct {
		Name      string         `json:"name"`
		Arguments jsontext.Value `json:"arguments"`
		// Some models write "parameters" instead of "arguments"; both are read so
		// a correct call is not discarded over a key name.
		Params jsontext.Value `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(obj), &call); err != nil {
		return deepSeekToolRequest{}, false
	}

	arguments := call.Arguments
	if len(arguments) == 0 {
		arguments = call.Params
	}
	if call.Name == "" || !deepSeekToolDeclared(call.Name, tools) {
		return deepSeekToolRequest{}, false
	}

	encoded := "{}"
	if len(arguments) > 0 {
		if !deepSeekValidJSON(string(arguments)) {
			return deepSeekToolRequest{}, false
		}
		encoded = string(arguments)
	}
	return deepSeekToolRequest{Name: call.Name, Arguments: encoded, Raw: obj}, true
}

// deepSeekToolDeclared reports whether the client asked for this tool. A model
// inventing a tool name is the most damaging failure mode here, so it is
// rejected rather than forwarded.
func deepSeekToolDeclared(name string, tools []deepSeekToolSpec) bool {
	for _, tool := range tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}

// deepSeekValidJSON reports whether s is one complete JSON value.
//
// There is no exported validity predicate, so this round-trips through a scratch
// value. It matters because a truncated tool call is the failure this file exists
// to prevent: a streaming reader accepts `{"name":"x","arguments":{"` and
// silently yields a half-built call.
func deepSeekValidJSON(s string) bool {
	var scratch jsontext.Value
	return json.Unmarshal([]byte(s), &scratch) == nil
}

// deepSeekToolCallID mints a call id in the shape clients expect.
func deepSeekToolCallID(name string) string {
	return "call_" + name + "_" + strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)
}

// deepSeekSurroundingText returns what the model wrote before and after the tool
// call object, with the object itself removed.
//
// This is what stops an agent from seeing an empty stop. A model that says "Let me
// check that for you." and then emits the object has answered twice over; the
// wrapper used to keep only the second half, so the client received
// `content: null` and concluded the model had said nothing.
func deepSeekSurroundingText(answer, raw string) string {
	if raw == "" {
		return ""
	}
	idx := strings.Index(answer, raw)
	if idx < 0 {
		return ""
	}
	before := strings.TrimSpace(answer[:idx])
	after := strings.TrimSpace(answer[idx+len(raw):])
	after = strings.TrimSpace(strings.Trim(after, "`"))
	after = strings.TrimSpace(strings.TrimPrefix(after, "json"))

	switch {
	case before == "":
		return after
	case after == "":
		return before
	default:
		return before + "\n" + after
	}
}

// deepSeekFindJSONObject returns the first balanced top-level JSON object in s.
//
// Balanced rather than greedy, because a fenced answer holds braces in the prose
// ("use {x} here") and a regex would run from the first one to the last. The
// returned slice is the object alone, so the caller can keep everything outside
// it as the model's prose.
func deepSeekFindJSONObject(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		if end := deepSeekMatchBrace(s, i); end > i {
			candidate := s[i : end+1]
			if deepSeekValidJSON(candidate) {
				return candidate
			}
		}
	}
	return ""
}

// deepSeekMatchBrace returns the index of the '}' closing the '{' at start, or
// -1. Quotes and escapes are tracked so a brace inside a string does not end
// the object early.
func deepSeekMatchBrace(s string, start int) int {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// Braces inside a string are literal text.
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
