### fix(translator): rewrite competing-client branding in the antigravity system instruction

The `antigravity` lane sanitizes competing-client branding in the system
instruction before a request leaves the gateway. Upstream `decolua/9router`
drives this from a rule table (`ANTIGRAVITY_PROMPT_REWRITES` in
`open-sse/config/appConstants.js:187-195`) with four rows, and its executor
applies all of them to `requestWithoutTools.systemInstruction.parts` only
(`open-sse/executors/antigravity.js:302-309`), with the reason:

> Rewrite competing-client branding in system prompts (e.g. Zed's Claude
> prompt, OpenCode naming) so Antigravity doesn't flag the request with a 429
> Quota Exhausted.

The port had the first three rows and no fourth. #435 removed the `opencode`
→ `antigravity` rename from both slots, so the port applies it nowhere —
including the system instruction, where upstream still applies it. Measured on
the Cloud Code envelope that leaves the gateway:

```
in : "You are Hermes, an AI agent. Working dir C:/Users/admin/.config/opencode/AGENTS.md"
out: unchanged — literal 'opencode' still present
```

Upstream's table over the same input rewrites the branding:

```
"Running opencode, opencode-zen, opencode-go at C:/Users/adima/.config/opencode/AGENTS.md"
  -> "Running antigravity, antigravity-zen, antigravity-go at C:/Users/adima/.config/antigravity/AGENTS.md"
"You are OpenCode, a coding agent."  -> "You are Antigravity, a coding agent."
"YOU ARE OPENCODE."                   -> "YOU ARE ANTIGRAVITY."
"Opencode is fine."                  -> "antigravity is fine."
```

This restores the fourth row at upstream's scope: the system instruction only.
The conversation is untouched, so the caller's own identifiers, config paths
and code identifiers reach the upstream unchanged — the property #435
established and the one its own fragment describes as *"the conversation keeps
the caller's vocabulary"*. Upstream's replacement mapping is reproduced
exactly, including the three capitalisations it names (`OpenCode` →
`Antigravity`, `OPENCODE` → `ANTIGRAVITY`, everything else → `antigravity`)
and its unbounded, case-insensitive match, so `opencode-zen` and
`.config/opencode/` are rewritten as they are upstream.

The integration test that covers this lane parsed the prompt fields from the
top level of the Cloud Code body, but the payload is nested under `request`.
Both part lists therefore came back empty and every assertion below the parse
— the ones that would have caught this regression — was silently skipped, so
the test passed against the unfixed code. The level is now asserted first and
the parsed parts are required to be non-empty, so an empty parse fails loudly.

Verified with a table-driven unit test over the system instruction and the
conversation, and with the integration test, which boots the real router
against a fake upstream and inspects the envelope that goes out. Both fail on
the pre-fix code.

This is upstream commit `dff6484` (*"fix(antigravity): sanitize
competing-client branding in system prompts"*) parity, scoped to the row
upstream still applies. The scope question for the other three rows — whether
the whole table should apply to the system instruction only — is tracked in
#439.
