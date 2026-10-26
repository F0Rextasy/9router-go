### Align OpenCode free lane with upstream OpenCode 2 / zen-proxy endpoint and client fingerprint

- Port the endpoint migration from `zen-proxy` / OpenCode v2: free-tier requests now
  target `/inference/openai/v1` instead of legacy `/inference/v1` (the legacy endpoint
  consistently rejects non-CLI requests with `429 FreeUsageLimitError`).
- Rewrite `opencodeInferenceBase` and `opencodeGoInferenceBase` so legacy `/inference/v1`
  and `/zen/v1` paths map to `/inference/openai/v1`.
- Bump `DefaultOpenCodeUA` to `opencode/latest/2.1.4/cli` and `DefaultOpenCodeV1UA` to
  `opencode/1.19.4`.
- Set default `x-opencode-client` on the free lane to `cli` (matching the User-Agent client identity).
