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
// `parameters` is raw JSON because clients disagree about it: the OpenAI shape
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

// deepSeekToolRequest is one call recovered from the model's answer.
type deepSeekToolRequest struct {
	Name      string
	Arguments string
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
	return b.String()
}

// deepSeekExtractToolCall recovers a tool call from the model's answer.
//
// It is deliberately conservative. The answer must be a single JSON object that
// names one of the tools the client declared. Anything else — prose around it, an
// undeclared name, malformed JSON — returns false and reaches the client as
// ordinary content. Guessing here would turn a failed parse into an invocation
// the client then acts on.
func deepSeekExtractToolCall(text string, tools []deepSeekToolSpec) (deepSeekToolRequest, bool) {
	trimmed := strings.TrimSpace(text)
	// A code fence around the object is common enough to unwrap; nothing looser.
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
	if !strings.HasPrefix(trimmed, "{") || !deepSeekValidJSON(trimmed) {
		return deepSeekToolRequest{}, false
	}

	var call struct {
		Name      string         `json:"name"`
		Arguments jsontext.Value `json:"arguments"`
		// Some models write "parameters" instead of "arguments"; both are read so
		// a correct call is not discarded over a key name.
		Params jsontext.Value `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(trimmed), &call); err != nil {
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
	return deepSeekToolRequest{Name: call.Name, Arguments: encoded}, true
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
