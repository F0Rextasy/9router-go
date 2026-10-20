### fix(translator): rewrite every Hermes identity variant in the antigravity lane

The `antigravity` lane rewrote exactly one Hermes identity phrasing — the
legacy sentence `You are Hermes Agent, an intelligent AI assistant created by
Nous Research.` — and left every other variant in place. A caller whose
system prompt identified itself as Hermes in any other wording reached the
Antigravity backend with the fingerprint intact, and the upstream gateway
answers that with a fake 429 RESOURCE_EXHAUSTED.

`internal/translator/antigravity.go` now matches the whole family of variants
and replaces the match wholesale with a neutral identity:

```
You are Hermes Agent, an intelligent AI assistant.          -> You are an AI assistant.
You are Hermes.                                             -> You are an AI assistant.
You are Hermes Agent, an intelligent AI assistant, created by Nous Research.
                                                            -> You are an AI assistant.
You are Hermes Agent, built by Nous Research.               -> You are an AI assistant.
You are Hermes, an AI assistant.                            -> You are an AI assistant.
You are Hermes, an AI agent.                                -> You are an AI assistant.
```

Upstream `decolua/9router` reached the same behaviour in commit `a61fc6a0`
(*"fix(antigravity): rewrite all Hermes identity variants, not just the legacy
sentence"*), whose message states the reason the rule exists: the old rule only
matched the one sentence, the current Hermes versions use `You are Hermes
Agent, built by Nous Research.` and similar, and those *slipped through and got
a fake 429 RESOURCE_EXHAUSTED*. The Go port carried upstream's pre-`a61fc6a0`
rule.

The change is confined to the `hermesIdentityRegex` pattern and its
replacement inside `rewriteBrandingText`. Which slots the rule runs in, and the
separate `opencode` row of the same upstream rule table, are untouched here —
both are open questions tracked in #439.

Verified with a table-driven unit test over the eleven identity phrasings,
including the case-insensitive ones and the negative case (a bare `You are
Hermes` with no trailing period is left alone, as upstream leaves it). The test
fails on the pre-fix code with the expected signature, and each invariant it
names was mutation-checked: restoring the old pattern, restoring the old
replacement, and dropping the trailing-period requirement each turn it red.
