### fix(gemini-web): carry the session to the push host, and name the cookie that decides it

A signed-in account could not be told apart from an anonymous one, and a
signed-in account's image was uploaded anonymously.

- **The image upload carried no session.** The upload goes to Google's
  content-push service — a different host from the site — and a cookie jar is
  per-host, so the site's cookies were not on the request. The upload belonged
  to no session, and the model cannot resolve an attachment that has none. The
  session's cookies are now copied to the push host, the way the site's own
  composer carries them, and only the Google session cookies are copied.
- **A credential that cannot reach the signed-in models was blamed on the
  wrong cookie.** The model registry is answered as an anonymous visitor
  unless the request carries `__Secure-1PSIDTS`, and that cookie is the one
  the site rotates every few minutes. A header carrying only
  `__Secure-1PSID` is served one model however fresh it is. The probe result,
  the quota panel and the refusal of an unreachable model now name that
  cookie, and the credential hint asks for the whole header including
  `__Secure-1PSIDTS` — with the note that it expires every few minutes.

Measured live against `gemini.google.com` on 2026-10-21 with a real signed-in
cookie header: a header of `__Secure-1PSID` alone was served Flash-Lite only
and reported no quota, and the same header with `__Secure-1PSIDTS` added was
served Flash-Lite, Flash, Pro and Argon, reported the 5-hour and weekly
windows, and answered an `gemini-4-argon` turn. The site accepted the
unsigned registry call as well, so the signature is not what decides the
served set — the session timestamp is.

The upload is served by a second server in the test, so the test fails if the
copy to the push host is dropped. A live suite was added for a credential
supplied through the environment; it is skipped without one.

Verified with `go build ./...`, `go test ./...`,
`go test -tags=integration ./internal/integration/...`,
`golangci-lint run --new-from-rev` (0 issues), `make vet-svelte` (0
unresolved identifiers, 81 errors against a baseline of 81) and the
portability gate over all five release targets. The anonymous turn and a
signed-in turn with an image were exercised live against `gemini.google.com`.
