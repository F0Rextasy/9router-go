package executor

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	json "encoding/json/v2"

	"9router/proxy/internal/providers"
)

// Live tests reach the real gemini.google.com.
//
// The site is a third party, so the suite is opt-in: it is skipped unless
// 9ROUTER_LIVE_TESTS=1 is set, and CI does not set it. The anonymous turn
// needs no credential; the signed-in ones read one from the environment so it
// never reaches the repository.
//
// Run them with:
//
//	9ROUTER_LIVE_TESTS=1 go test ./internal/proxy/executor/ -run TestGeminiWebLive -v
//
// The signed-in ones also need:
//
//	9ROUTER_LIVE_GEMINI_WEB_COOKIE='__Secure-1PSID=…; SAPISID=…; __Secure-1PSIDTS=…'

// liveUpstreamEnv is the opt-in variable. The 9ROUTER_ prefix keeps it from
// colliding with a real runtime setting and makes it greppable.
const liveUpstreamEnv = "9ROUTER_LIVE_TESTS"

// liveCookieEnv names the signed-in credential the live tests use.
const liveCookieEnv = "9ROUTER_LIVE_GEMINI_WEB_COOKIE"

// liveUpstreamEnabled reports whether live upstream tests were opted into.
//
// Only an explicit opt-in counts, and "1" is the spelling the Makefile uses.
// A developer who writes 9ROUTER_LIVE_TESTS=true must not silently get a
// skipped run, so the accepted spellings are exactly the ones the chat
// package's own live guard accepts.
func liveUpstreamEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(liveUpstreamEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// requireLiveUpstream skips the calling test unless live tests were enabled.
func requireLiveUpstream(t *testing.T) {
	t.Helper()
	if !liveUpstreamEnabled() {
		t.Skipf("live upstream test: set %s=1 to run it against gemini.google.com", liveUpstreamEnv)
	}
}

// requireLiveCookie returns the signed-in credential the live tests use.
//
// A credential without __Secure-1PSIDTS is refused rather than used: that
// cookie is what carries the session into the model registry, so a header
// without it is answered as an anonymous visitor's and a run that passed
// would prove nothing about a signed-in session.
func requireLiveCookie(t *testing.T) string {
	t.Helper()
	cookie := strings.TrimSpace(os.Getenv(liveCookieEnv))
	if cookie == "" {
		t.Skipf("live upstream test: set %s to a signed-in cookie header", liveCookieEnv)
	}
	if !GeminiWebHasSessionTimestamp(cookie) {
		t.Fatalf("the credential carries no __Secure-1PSIDTS, so gemini.google.com answers it as an anonymous visitor")
	}
	return cookie
}

// TestGeminiWebLiveAnonymousTurn exercises a turn with no credential.
func TestGeminiWebLiveAnonymousTurn(t *testing.T) {
	requireLiveUpstream(t)
	rec := httptest.NewRecorder()
	err := ForwardGeminiWeb(rec, &Request{
		Ctx:      context.Background(),
		Config:   &providers.ProviderConfig{},
		Body:     geminiWebTestBody(t, "gemini-3.1-flash-lite", "Reply with the word pong and nothing else.", nil),
		IsStream: true,
	})
	if err != nil {
		t.Fatalf("ForwardGeminiWeb() error = %v", err)
	}
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "pong") {
		t.Errorf("the answer carries no pong:\n%s", body)
	}
	if !strings.Contains(body, "\"usage\"") {
		t.Errorf("the stream carries no usage frame:\n%s", body)
	}
}

// TestGeminiWebLiveSignedInTurn asks the site which models the account may be
// served.
//
// This is the check that proves the credential reaches the signed-in models
// rather than one model's worth of anonymous access: the model registry is
// answered as a visitor unless the call is signed.
func TestGeminiWebLiveSignedInTurn(t *testing.T) {
	requireLiveUpstream(t)
	cookie := requireLiveCookie(t)

	session := geminiWebSessionForCredential(cookie)
	models, err := session.availableModels(context.Background())
	if err != nil {
		t.Fatalf("availableModels() error = %v", err)
	}
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, model.Category)
	}
	t.Logf("the session may be served: %s", strings.Join(names, ", "))
	if len(models) < 2 {
		t.Errorf("the session reaches %d model(s); a signed-in account is expected to reach more than one",
			len(models))
	}
}

// TestGeminiWebLiveSignedInTurnWithAnImage exercises a turn that carries an
// image.
//
// The image is the part that needed the session cookie: the upload goes to
// Google's content-push service on another host, and without the cookie the
// upload belongs to no session. The picture is a solid red square and the
// answer has to name the colour, so a turn that never saw the picture cannot
// pass — an answer about the colour is the proof that the attachment was
// resolved and handed to the model.
func TestGeminiWebLiveSignedInTurnWithAnImage(t *testing.T) {
	requireLiveUpstream(t)
	cookie := requireLiveCookie(t)

	// A 32x32 solid red PNG, so the test needs no file and no network of its
	// own. The pixels are one colour so the answer is not a matter of taste.
	png := solidPNGBase64(32, 32, 0xff, 0x00, 0x00)

	rec := httptest.NewRecorder()
	err := ForwardGeminiWeb(rec, &Request{
		Ctx:    context.Background(),
		Config: &providers.ProviderConfig{},
		APIKey: cookie,
		Body: geminiWebTestBody(t, "gemini-3.1-flash-lite", "", map[string]any{
			"messages": []map[string]any{{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "What colour fills this image? Answer with one word."},
					{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + png}},
				},
			}},
		}),
		IsStream: true,
	})
	if err != nil {
		t.Fatalf("ForwardGeminiWeb() error = %v", err)
	}
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	answer := geminiWebAnswerText(body)
	if answer == "" {
		t.Fatalf("the stream carries no answer:\n%s", body)
	}
	t.Logf("the answer: %s", answer)
	if !strings.Contains(strings.ToLower(answer), "red") {
		t.Errorf("the answer = %q, want it to name the colour of the picture", answer)
	}
}

// geminiWebAnswerText reads the answer out of a streamed chat completion.
func geminiWebAnswerText(body string) string {
	var out strings.Builder
	for line := range strings.Lines(body) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(trimmed, "data: ")
		if payload == "[DONE]" {
			continue
		}
		var chunk streamedChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if text, ok := choice.Delta["content"].(string); ok {
				out.WriteString(text)
			}
		}
	}
	return strings.TrimSpace(out.String())
}

// solidPNGBase64 renders a solid-colour PNG.
func solidPNGBase64(width, height int, r, g, b byte) string {
	var raw bytes.Buffer
	for range height {
		raw.WriteByte(0) // filter: none
		for range width {
			raw.Write([]byte{r, g, b})
		}
	}
	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	chunk := func(kind string, payload []byte) {
		var length bytes.Buffer
		_ = binary.Write(&length, binary.BigEndian, uint32(len(payload)))
		out.Write(length.Bytes())
		body := append([]byte(kind), payload...)
		sum := crc32.ChecksumIEEE(body)
		var checksum bytes.Buffer
		_ = binary.Write(&checksum, binary.BigEndian, sum)
		out.Write(body)
		out.Write(checksum.Bytes())
	}
	var header bytes.Buffer
	_ = binary.Write(&header, binary.BigEndian, uint32(width))
	_ = binary.Write(&header, binary.BigEndian, uint32(height))
	header.Write([]byte{8, 2, 0, 0, 0}) // 8-bit, truecolour
	chunk("IHDR", header.Bytes())
	chunk("IDAT", compress(raw.Bytes()))
	chunk("IEND", nil)
	return base64.StdEncoding.EncodeToString(out.Bytes())
}

// compress deflates the PNG's pixel data.
func compress(data []byte) []byte {
	var out bytes.Buffer
	writer, err := zlib.NewWriterLevel(&out, zlib.BestSpeed)
	if err != nil {
		return data
	}
	_, _ = writer.Write(data)
	_ = writer.Close()
	return out.Bytes()
}
