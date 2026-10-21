package tokensaver

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"strings"
	"testing"
)

// The protected-tool list is matched case-insensitively: the repo canonicalises
// tool names to lowercase (internal/translator/fingerprint.go collapses any
// case variant onto "bash"/"glob"/"grep"/"read"), so the spellings the gateway
// itself produces are exactly the ones a case-sensitive list misses. A file
// read through a lowercase-named tool must survive RTK untouched, or the
// agent edits against a summary instead of the file it asked to see.
func TestIsProtectedToolVocabulary(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{name: "lowercase read", tool: "read", want: true},
		{name: "capitalised Read", tool: "Read", want: true},
		{name: "lowercase grep", tool: "grep", want: true},
		{name: "capitalised Grep", tool: "Grep", want: true},
		{name: "lowercase bash", tool: "bash", want: true},
		{name: "capitalised Bash", tool: "Bash", want: true},
		{name: "lowercase glob", tool: "glob", want: true},
		{name: "capitalised Glob", tool: "Glob", want: true},
		{name: "lowercase edit", tool: "edit", want: true},
		{name: "capitalised Edit", tool: "Edit", want: true},
		{name: "lowercase write", tool: "write", want: true},
		{name: "capitalised Write", tool: "Write", want: true},
		{name: "lowercase patch", tool: "patch", want: true},
		{name: "lowercase anchor", tool: "anchor", want: true},
		{name: "capitalised Patch", tool: "Patch", want: true},
		{name: "capitalised Anchor", tool: "Anchor", want: true},
		{name: "NotebookEdit", tool: "NotebookEdit", want: true},
		{name: "lowercase notebookedit", tool: "notebookedit", want: true},
		{name: "WebFetch", tool: "WebFetch", want: true},
		{name: "lowercase webfetch", tool: "webfetch", want: true},
		{name: "unrelated MultiEdit", tool: "MultiEdit", want: false},
		{name: "unrelated run_shell", tool: "run_shell", want: false},
		{name: "unrelated curl", tool: "curl", want: false},
		{name: "unrelated some_other_tool", tool: "some_other_tool", want: false},
		{name: "unrelated shell", tool: "shell", want: false},
		{name: "unrelated terminal", tool: "terminal", want: false},
		{name: "unrelated sh", tool: "sh", want: false},
		{name: "unrelated python", tool: "python", want: false},
		{name: "empty name", tool: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isProtectedTool(tt.tool); got != tt.want {
				t.Errorf("isProtectedTool(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

// The whitespace trim the helper already performed must survive the change.
func TestIsProtectedToolTrimsWhitespace(t *testing.T) {
	for _, tool := range []string{"read", "Read", "bash", "Bash", "glob", "Glob", "NotebookEdit", "patch", "anchor"} {
		padded := "  " + tool + "  "
		if !isProtectedTool(padded) {
			t.Errorf("expected %q to be protected", padded)
		}
	}
}

// End-to-end through the public entry point: a tool result whose name is a
// protected one must come back byte-identical, while the same body under an
// unprotected name is compressed. This is the assertion that fails on the
// pre-fix code: "read" was compressed while "Read" was not.
func TestCompressMessages_ProtectedToolPreservesContent(t *testing.T) {
	content := longFileContent()

	t.Run("protected lowercase name is preserved byte for byte", func(t *testing.T) {
		body := toolBody(t, "read", content)
		out, changed := CompressMessages(body)
		if changed {
			t.Errorf("expected no compression for protected tool %q", "read")
		}
		if !bytes.Equal(out, body) {
			t.Errorf("expected body to be returned byte-identical, got %d bytes (original %d)", len(out), len(body))
		}
	})

	t.Run("protected capitalised name is preserved byte for byte", func(t *testing.T) {
		body := toolBody(t, "Read", content)
		out, changed := CompressMessages(body)
		if changed {
			t.Errorf("expected no compression for protected tool %q", "Read")
		}
		if !bytes.Equal(out, body) {
			t.Errorf("expected body to be returned byte-identical, got %d bytes (original %d)", len(out), len(body))
		}
	})

	t.Run("unprotected name is still compressed", func(t *testing.T) {
		body := toolBody(t, "some_other_tool", content)
		out, changed := CompressMessages(body)
		if !changed {
			t.Fatal("expected compression for an unprotected tool")
		}
		if bytes.Equal(out, body) {
			t.Fatal("expected the body to change under an unprotected tool")
		}
		assertContains(t, string(out), "... (")
	})
}

// toolBody builds an OpenAI Chat Completions body carrying a single tool
// result with the given tool name.
func toolBody(t *testing.T, tool, content string) []byte {
	t.Helper()
	payload := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "read the file"},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "name": tool, "content": content},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	return body
}

// longFileContent is a file-like body well over MinCompressSize, with enough
// distinct lines that RTK's line truncation actually shrinks it.
func longFileContent() string {
	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "line %04d: func example%d() { return %d }\n", i, i, i)
	}
	return b.String()
}
