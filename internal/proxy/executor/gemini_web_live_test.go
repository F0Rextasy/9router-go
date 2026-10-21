package executor

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"9router/proxy/internal/providers"
)

// A live smoke against the real gemini.google.com.
//
// The site is a third party, so the test is opt-in: it is skipped unless
// 9ROUTER_LIVE_TESTS=1 is set, and CI does not set it. The session is
// anonymous, so no credential is involved.
//
// Run it with:
//
//	9ROUTER_LIVE_TESTS=1 go test ./internal/proxy/executor/ -run TestGeminiWebLive -v
func TestGeminiWebLiveAnonymousTurn(t *testing.T) {
	if os.Getenv("9ROUTER_LIVE_TESTS") == "" {
		t.Skip("live upstream test: set 9ROUTER_LIVE_TESTS=1 to run it against gemini.google.com")
	}
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

// TestGeminiWebLiveSignedInTurn exercises the provider with a real signed-in
// credential, which is the only way to see the account's paid models.
//
// The credential is read from the environment so it never reaches the
// repository, and the test is skipped when it is absent. A credential without
// SAPISID is refused rather than tested: the site answers it as a visitor's,
// and a run that passed would prove nothing about a signed-in session.
//
// Run it with:
//
//	9ROUTER_LIVE_TESTS=1 \
//	9ROUTER_LIVE_GEMINI_WEB_COOKIE='__Secure-1PSID=…; SAPISID=…; __Secure-1PSIDTS=…' \
//	  go test ./internal/proxy/executor/ -run TestGeminiWebLiveSignedIn -v
func TestGeminiWebLiveSignedInTurn(t *testing.T) {
	if os.Getenv("9ROUTER_LIVE_TESTS") == "" {
		t.Skip("live upstream test: set 9ROUTER_LIVE_TESTS=1 to run it against gemini.google.com")
	}
	cookie := strings.TrimSpace(os.Getenv("9ROUTER_LIVE_GEMINI_WEB_COOKIE"))
	if cookie == "" {
		t.Skip("live upstream test: set 9ROUTER_LIVE_GEMINI_WEB_COOKIE to a signed-in cookie header")
	}
	if !GeminiWebHasSAPISID(cookie) {
		t.Fatal("the credential carries no SAPISID, so the site answers it as an anonymous visitor")
	}

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
