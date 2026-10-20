### fix(test): read the Cloud Code envelope at the level the prompt parts actually live

The integration test added in #435 unmarshalled the upstream request body into
a struct that expects `system_instruction` and `contents` at the top level of
the body. The Cloud Code envelope nests the whole Gemini payload under
`request`, so both fields decode to zero parts and the two per-part assertions
— the ones that check the prompt text carries no renamed form — never execute.
The test still failed on the pre-fix code, but only through the raw-body
substring checks; the envelope assertions it advertises were inert.

Verified against the body the router actually sends:

```
WIRE TOP-LEVEL KEYS: [model project request requestId userAgent]
MERGED-TEST STRUCT -> system_instruction.parts=0 contents=0
NESTED UNDER request -> system_instruction.parts=1 contents=1
```

The parsed struct now reads `request.system_instruction` and
`request.contents`, and the test fails loudly when either list comes back
empty, so a future change to the envelope layout is a hard failure instead of
a silently skipped assertion.

Mutation table on the corrected test — every break is caught, and the baseline
passes:

| Break | Result |
|---|---|
| none (merged code) | pass |
| rename in the system instruction only | fail — `system instruction text incorrectly contains renamed form` |
| rename in the conversation contents only | fail — `content part text incorrectly contains renamed form` |
| the Claude Agent SDK control phrase dropped from the blacklist | fail — `control phrase ... was not stripped from upstream body` |

The pre-fix code still fails the corrected test, so the test keeps its
original value. The failure carries the four raw-body checks it had before
plus the two envelope assertions the corrected struct now reaches, because the
pre-fix code renames in both slots:

```
provider_branding_rewrite_test.go:76: upstream request body missing literal "opencode"
provider_branding_rewrite_test.go:76: upstream request body missing literal "opencode-zen"
provider_branding_rewrite_test.go:76: upstream request body missing literal "opencode-go"
provider_branding_rewrite_test.go:76: upstream request body missing literal "C:/Users/admin/.opencode/db/data.sqlite"
provider_branding_rewrite_test.go:119: system instruction text incorrectly contains renamed form "antigravity"
provider_branding_rewrite_test.go:125: content part text incorrectly contains renamed form "antigravity"
```
