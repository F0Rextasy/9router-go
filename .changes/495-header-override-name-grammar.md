### fix(web): let the dashboard apply the server's header-override grammar

- The per-provider outbound-header override modal now accepts a header name
  built from the full RFC 7230 §3.2.6 token alphabet (`A-Z a-z 0-9` and
  `! # $ % & ' * + - . ^ _ ` | ~`), the same grammar the server applies in
  `internal/db/provider_overrides.go`. It used to test `/^[A-Za-z0-9-]+$/` and
  told the operator "Letters, digits and dashes only." — so a legal header name
  containing a tchar punctuation character could not be saved from the UI even
  though the server has accepted it since #458.
- The modal's value check now rejects NUL, DEL and every C0 control except
  HTAB, matching the server's `validateOverrideHeaderValue`. It used to reject
  only CR and LF, so it let the operator submit a value the server refuses.
- The rules moved out of the modal into
  `web/src/components/connections/headerOverrideValidation.ts` so the modal and
  the test share one copy of the grammar, and the comment above the check now
  describes what the modal actually does.
- Deliberate divergences from upstream's validation, which upstream
  `providerOverrides` settings entry still applies:
  - **name** — upstream tests `/^[A-Za-z0-9-]+$/`; the port uses the RFC 7230
    tchar set, so the port accepts *more* names.
  - **value** — upstream rejects CR/LF only; the port rejects NUL, DEL and the
    C0 controls, so the port accepts *fewer* values.
  - **case-variant spellings** — upstream has no conflict rule; the port
    treats two spellings of one header with different values as an error, a
    rule upstream does not have.
  The blocked-header list and the 8192 value-length cap are identical to
  upstream's.
- Verified with `web/src/components/connections/headerOverrideValidation.test.ts`:
  a name with tchar punctuation is accepted; a name with a space, a colon, a
  parenthesis or a non-ASCII letter is rejected; an empty name is accepted (a
  blank row is a row being typed, not a header); a value carrying CR, LF, NUL
  or DEL is rejected; a value carrying TAB is accepted; a value longer than
  8192 is rejected.
