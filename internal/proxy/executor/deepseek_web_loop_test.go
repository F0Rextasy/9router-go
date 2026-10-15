package executor

import (
	"strings"

	json "encoding/json/v2"
	"testing"
)

// The agent loop: without the call rendered into the transcript, the replay
// shows a tool result whose cause is invisible, and the model concludes it never
// called anything and calls again on every turn.
func TestDeepSeekPromptRendersPriorToolCall(t *testing.T) {
	body := `{"model":"deepseek-v4-pro","messages":[
	  {"role":"user","content":"weather in Jakarta?"},
	  {"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function",
	    "function":{"name":"get_weather","arguments":"{\"city\": \"Jakarta\"}"}}]},
	  {"role":"tool","tool_call_id":"call_1","name":"get_weather","content":"Sunny, 31C"}
	]}`

	var env deepSeekRequestEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("parse: %v", err)
	}
	prompt := deepSeekMessagesToPrompt(env.Messages, 0)

	if !strings.Contains(prompt, "get_weather") {
		t.Fatalf("prompt drops the tool call entirely:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[called get_weather") {
		t.Errorf("prompt does not record that the call was already made:\n%s", prompt)
	}
	// The result must come after the call, or the transcript reads backwards.
	call := strings.Index(prompt, "[called get_weather")
	result := strings.Index(prompt, "Sunny, 31C")
	if call < 0 || result < 0 || call > result {
		t.Errorf("call at %d must precede result at %d:\n%s", call, result, prompt)
	}
}

// The tool instructions have to tell the model the call is already made,
// because the transcript alone does not stop a model that reads it as context
// rather than as history.
func TestDeepSeekToolInstructionsForbidRepeating(t *testing.T) {
	var tool deepSeekToolSpec
	tool.Function.Name = "get_weather"
	tool.Function.Description = "weather"

	out := deepSeekRenderTools([]deepSeekToolSpec{tool})
	for _, want := range []string{"Do not repeat it", "Calling a tool again", "already"} {
		if !strings.Contains(out, want) {
			t.Errorf("tool instructions missing %q:\n%s", want, out)
		}
	}
}

// Prose written around the object must reach the client: dropping it produced
// content:null, which an agent reads as an empty stop and retries.
func TestDeepSeekToolCallKeepsSurroundingText(t *testing.T) {
	tools := []deepSeekToolSpec{func() deepSeekToolSpec {
		var t deepSeekToolSpec
		t.Function.Name = "get_weather"
		return t
	}()}

	answer := `Let me check that for you.` + "\n" +
		`{"name":"get_weather","arguments":{"city":"Jakarta"}}`

	call, ok := deepSeekExtractToolCall(answer, tools)
	if !ok {
		t.Fatal("a call with prose around it must still be recognised")
	}
	if got := deepSeekSurroundingText(answer, call.Raw); got != "Let me check that for you." {
		t.Errorf("surrounding text = %q, want the prose the model wrote", got)
	}
}

// A bare object with nothing around it yields no preamble, so the client still
// gets content:null — which is correct, because there was nothing else said.
func TestDeepSeekToolCallWithoutProseHasNoPreamble(t *testing.T) {
	tools := []deepSeekToolSpec{func() deepSeekToolSpec {
		var t deepSeekToolSpec
		t.Function.Name = "get_weather"
		return t
	}()}

	answer := `{"name":"get_weather","arguments":{"city":"Jakarta"}}`
	call, ok := deepSeekExtractToolCall(answer, tools)
	if !ok {
		t.Fatal("call not recognised")
	}
	if got := deepSeekSurroundingText(answer, call.Raw); got != "" {
		t.Errorf("preamble = %q, want empty when the model said nothing else", got)
	}
}

// An undeclared tool name must never be forwarded: the client would execute a
// call it never authorised.
func TestDeepSeekUndeclaredToolIsRejected(t *testing.T) {
	var declared deepSeekToolSpec
	declared.Function.Name = "get_weather"
	declared.Function.Description = "weather"

	answer := `{"name":"delete_everything","arguments":{"path":"/"}}`
	if _, ok := deepSeekExtractToolCall(answer, []deepSeekToolSpec{declared}); ok {
		t.Error("a tool the client never declared must not be returned as a call")
	}
}
