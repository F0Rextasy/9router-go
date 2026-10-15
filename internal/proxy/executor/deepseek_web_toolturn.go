package executor

import (
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/proxy"
)

// deepSeekToolTurnToClient serves a request that declared tools.
//
// This path buffers the whole upstream answer before answering, which is the
// only way tool calling can work here. The model emits its tool call as text
// typed one character at a time, so a streamed answer could end at
// `{"name":"get_weather","parameters":{"` with no way to tell a truncated call
// from prose. Buffering restores the guarantee the native API gives: whatever is
// parsed is whole. The native API does not stream tool arguments either, so
// nothing is lost by not streaming here — only the text preamble a model might
// emit before the call, which is not useful once a call is returned.
//
// The isolation is deliberate. Nothing in this file is reached unless the
// request carried a `tools[]` array, and an answer that is not a declared tool
// call is passed through as ordinary content rather than being coerced into one.

// deepSeekToolTurnToClient reads the turn and writes either a tool call or the
// model's plain answer.
func deepSeekToolTurnToClient(w http.ResponseWriter, req *Request, upstream io.Reader, envelope deepSeekRequestEnvelope) error {
	accumulator, err := deepSeekAccumulate(upstream)
	if err != nil {
		return err
	}
	if !accumulator.sawFinished {
		// The same rule as the streaming path: a turn the upstream never signed
		// off is a cut-off session, not an answer. Reporting it as an empty tool
		// result would have the client retry a call that may have been made.
		return deepSeekReportStreamAbort(nil, nil,
			fmt.Errorf("deepseek-web stream ended before completion — the upstream session was cut off"))
	}

	text := deepSeekToolPlainText(accumulator)
	if call, ok := deepSeekExtractToolCall(text, envelope.Tools); ok {
		return deepSeekWriteToolCall(w, req, envelope.Model, call)
	}
	return deepSeekWritePlainAnswer(w, req, envelope.Model, accumulator, text)
}

// deepSeekAccumulate drains the upstream body into one accumulator.
func deepSeekAccumulate(upstream io.Reader) (*deepSeekStreamAccumulator, error) {
	accumulator := newDeepSeekStreamAccumulator("")
	scanErr := proxy.ScanStream(upstream, func(payload []byte) {
		var event deepSeekStreamEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return
		}
		accumulator.apply(event)
	})
	if scanErr != nil {
		return nil, deepSeekReportStreamAbort(nil, nil, scanErr)
	}
	return accumulator, nil
}

// deepSeekToolPlainText returns the answer with the tool-call scaffolding
// removed when there is one, so the fallback prose path does not show the user a
// raw JSON object the extractor merely rejected for a trivial reason.
func deepSeekToolPlainText(accumulator *deepSeekStreamAccumulator) string {
	return deepSeekStripToolScaffold(accumulator.contentText())
}

// deepSeekWriteToolCall answers with an OpenAI tool_calls message.
func deepSeekWriteToolCall(w http.ResponseWriter, req *Request, model string, call deepSeekToolRequest) error {
	id := deepSeekToolCallID(call.Name)
	message := map[string]any{
		"role":    "assistant",
		"content": nil,
		"tool_calls": []any{map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		}},
	}
	return deepSeekWriteJSON(w, req, model, message)
}

// deepSeekWritePlainAnswer answers with the model's text when no declared tool
// was called.
func deepSeekWritePlainAnswer(w http.ResponseWriter, req *Request, model string, accumulator *deepSeekStreamAccumulator, text string) error {
	message := map[string]any{"role": "assistant", "content": text}
	if thinking := accumulator.thinkingText(); thinking != "" {
		message["reasoning_content"] = thinking
	}
	return deepSeekWriteJSON(w, req, model, message)
}

// deepSeekWriteJSON writes one chat.completion.
func deepSeekWriteJSON(w http.ResponseWriter, req *Request, model string, message map[string]any) error {
	content, _ := message["content"].(string)
	if content != "" && req.ResponseBuf != nil {
		req.ResponseBuf.Write([]byte(content))
	}

	payload := map[string]any{
		"id":      deepSeekResponseID(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": deepSeekFinishReason(message),
		}},
		"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("deepseek-web: marshal completion: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(encoded); err != nil {
		return fmt.Errorf("deepseek-web: write completion: %w", err)
	}
	return nil
}

// deepSeekFinishReason reports "tool_calls" only when a call is present, which
// is what tells a client to run the tool rather than print the text.
func deepSeekFinishReason(message map[string]any) string {
	if calls, ok := message["tool_calls"]; ok {
		if list, ok := calls.([]any); ok && len(list) > 0 {
			return "tool_calls"
		}
	}
	return "stop"
}

// deepSeekStripToolScaffold removes a leading instruction echo.
//
// The prompt tells the model to reply with a single JSON object, and models
// sometimes echo that instruction back before complying. When the answer is not
// a declared tool call, the echoed scaffolding is noise the user would read, so
// it is dropped — but only when what follows still has content, so an answer
// that is *only* the echo is never emptied.
func deepSeekStripToolScaffold(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	end := strings.LastIndex(trimmed, "}")
	if end > 0 && end < len(trimmed)-1 {
		tail := strings.TrimSpace(trimmed[end+1:])
		if tail != "" && !strings.HasPrefix(tail, "{") {
			return trimmed[:end+1] + "\n" + tail
		}
		return trimmed[:end+1]
	}
	return trimmed
}
