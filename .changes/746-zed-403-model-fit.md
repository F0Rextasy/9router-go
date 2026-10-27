### fix(combo): recognize Zed's 403 context-window refusal as a model-fit error

Zed rejects an oversized prompt with a `403` carrying
`Context window exceeded: 103326 tokens exceeds the limit of 100000
tokens`. `IsModelFitError` gated on 400/413/422, so the refusal fell to
the generic 403 rule and the combo locked the connection on the model
for two minutes:

```
WRN fallback upstream failed provider=zed model=claude-haiku-5-5 status=403 error=... Context window exceeded ...
WRN combo locked on retryable error provider=zed model=claude-haiku-5-5 status=403 cooldown_s=120
WRN combo skip unhealthy provider=zed model=claude-haiku-5-5
WRN request POST /chat/completions status=429 ...
```

Every combo request for the next two minutes skipped the member as
unhealthy and the combo answered 429, even though the prompt, not the
account, was at fault.

The fix adds 403 to `ModelFitStatusCodes`, so the model-fit failover
(#338) advances to the next combo member instead of locking a healthy
connection. The body-pattern gate is unchanged: a 403 permission
refusal without a context-limit phrase still lands on the generic 403
rule.

Verified: `go test ./internal/providers/ -run ModelFit` covers Zed's
403 context-window body (fit failure) and a 403 permission refusal (not
a fit failure); `go test ./internal/providers/
./internal/handlers/chat/` passes.
