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
| the Claude Agent SDK control phrase left in the blacklist | fail — `control phrase ... was not stripped from upstream body` |

The pre-fix code still fails the corrected test with the same signature it had
before, so the test keeps its original value.
