package executor

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
// A credential without SAPISID is refused rather than used: the site answers
// it as an anonymous visitor, so a run that passed would prove nothing about a
// signed-in session.
func requireLiveCookie(t *testing.T) string {
	t.Helper()
	cookie := strings.TrimSpace(os.Getenv(liveCookieEnv))
	if cookie == "" {
		t.Skipf("live upstream test: set %s to a signed-in cookie header", liveCookieEnv)
	}
	if !GeminiWebHasSAPISID(cookie) {
		t.Fatalf("the credential carries no SAPISID, so gemini.google.com answers it as an anonymous visitor")
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
// upload belongs to no session. A turn that answers about the picture proves
// the upload was attached to the session and resolved by the site.
func TestGeminiWebLiveSignedInTurnWithAnImage(t *testing.T) {
	requireLiveUpstream(t)
	cookie := requireLiveCookie(t)

	// A 1x1 PNG, so the test needs no file and no network of its own.
	png := base64.StdEncoding.EncodeToString([]byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
		0x00, 0x00, 0x00, 0x0d, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00, 0x03, 0x01, 0x01, 0x00,
		0x18, 0xdd, 0x8d, 0xb0,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	})

	rec := httptest.NewRecorder()
	err := ForwardGeminiWeb(rec, &Request{
		Ctx:    context.Background(),
		Config: &providers.ProviderConfig{},
		APIKey: cookie,
		Body: geminiWebTestBody(t, "gemini-3.1-flash-lite", "Answer with one word: what colour is this image?", map[string]any{
			"messages": []map[string]any{{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "Answer with one word: what colour is this image?"},
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
	if !strings.Contains(body, "\"content\"") {
		t.Errorf("the stream carries no content:\n%s", body)
	}
}
