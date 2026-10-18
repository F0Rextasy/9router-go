### fix(gemini-web): pass transcript flag to deepSeekMessagesToPrompt

PR #352 updated `deepSeekMessagesToPrompt` to accept a third argument
(`transcript bool`), which controls whether single-turn prompts are rendered as
role-tagged transcripts for session persistence or collapsed to raw user prompts
for plain/self-contained requests.

When PR #352 and PR #353 were merged concurrently into `main`, the call site in
`internal/proxy/executor/gemini_web_payload.go` was left with 2 arguments,
causing compilation on `main` to fail (`not enough arguments in call to
deepSeekMessagesToPrompt`).

Passed `false` as `gemini-web` turns are self-contained, preserving single-turn
collapsing behavior. Added `TestGeminiWebMessagesToPrompt` to verify prompt
generation formatting.

Also upgraded `github.com/cloudflare/circl` to `v1.6.3` (dependency of
`bogdanfinn/tls-client`), clearing vulnerability `GO-2026-4550` detected by
`govulncheck`.

**Verified.** `go build ./...`, `go vet ./...`, `go test ./...` all pass clean;
`govulncheck ./...` reports 0 vulnerabilities; `TestGeminiWebMessagesToPrompt` passes.
