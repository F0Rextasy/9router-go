### fix(gemini-web): carry the session to the push host and sign the registry probe

A signed-in account could not be told apart from an anonymous one, and a
signed-in account's image was uploaded anonymously.

- **The image upload carried no session.** The upload goes to Google's
  content-push service — a different host from the site — and a cookie jar is
  per-host, so the site's cookies were not on the request. The upload belonged
  to no session, and the model cannot resolve an attachment that has none. The
  session's cookies are now copied to the push host, the way the site's own
  composer carries them, and only the Google session cookies are copied.
- **The dashboard's "Check" asked the model registry without a signature.**
  The site answers that call as a visitor unless it carries a `SAPISIDHASH`
  signature, and the signature needs the `SAPISID` cookie — a bare
  `__Secure-1PSID` reaches a page and not the registry. A working Pro account
  was therefore reported as reaching one model, and a user who pasted a
  correct cookie was told to paste it again. The probe is signed the way the
  executor signs its own call.
- **A credential without `SAPISID` was blamed on the wrong cause.** It cannot
  be signed at all, and an unsigned registry call is answered as a visitor's
  however valid the cookie is — a third cause, distinct from a stale cookie.
  The probe result, the quota panel and the refusal of an unreachable model
  now name that cause, and the credential hint asks for the whole cookie
  header instead of the one cookie that was not enough.

The upload is served by a second server in the test, so the test fails if the
copy to the push host is dropped, and the probe is checked for the signature
it sends. A live signed-in test was added for a credential supplied through
the environment; it is skipped without one.

Verified with `go build ./...`, `go test ./...`,
`go test -tags=integration ./internal/integration/...`,
`golangci-lint run --new-from-rev` (0 issues), `make vet-svelte` (0
unresolved identifiers, 81 errors against a baseline of 81) and the
portability gate over all five release targets. The anonymous turn was
exercised live against `gemini.google.com`.

The signed-in path was not exercised against the real site, because no Pro
credential was available: the two fixes are covered by tests that assert the
observable behaviour — the cookie on the upload request to a second host, and
the signature on the registry probe.
