### fix(rtk): truncate a line-numbered read result the way upstream truncates it

A `read` or `grep` result that is a long line-numbered file dump — the shape
Cursor's and Codex's `read_file` produce — now reaches the model truncated the
way upstream truncates it: the first 120 lines and the last 60 are kept, and
the cut is marked. The head and the tail are byte-for-byte what the tool
returned, so the content stays recognisable.

Upstream selects its filter by the *shape* of the content, not by the tool
name (`open-sse/rtk/autodetect.js`), and has no tool-name protection at all.
A numbered dump is the one shape it truncates safely
(`open-sse/rtk/filters/readNumbered.js`). This port keeps the tool-name
protected list from #454 — a protected tool's result is not rewritten, because
the agent asked for that content and a rewritten copy is a different thing —
so a numbered dump was left intact even where upstream compresses it. On a
read-heavy session that is roughly 1.7x the bytes upstream would send for the
same results.

The thresholds are upstream's, unchanged: at least 250 lines
(`SMART_TRUNCATE_MIN_LINES`), a hit ratio of 0.7 over the first 100 sampled
lines (`READ_NUMBERED_MIN_HIT_RATIO`), and the line shape `/^\s*\d+\|/`. A
result that is not that shape is left exactly as it was, byte for byte.

Measured on the same 300-line dump, the port now gives the same numbers as
upstream's `readNumbered` run under `bun`: 10,691 bytes in, 6,413 bytes out,
and the marker `... +120 lines truncated (file continues)` in both. On the
pre-fix tree the same result came back unchanged at 10,691 bytes.

Two deliberate divergences from upstream, recorded here as AGENTS.md §7.6
asks:

- The port keeps a tool-name protected list (`read`, `grep`, `edit`, `write`,
  `patch`, `anchor`) that upstream does not have. `patch` and `anchor` have no
  counterpart in either repository — no tool by either name exists, and
  upstream's filter set has no patch filter — so their provenance is
  unrecorded. They are left in place here; `patch` is a generic enough name
  that an unrelated third-party tool called `patch` would silently lose its
  compression.
- The port still skips compression for a protected tool whose content is *not*
  a numbered dump, where upstream would fall through to the rest of its
  shape-based selection (grep, find, ls, tree, ...). Upstream's whole
  autodetect ladder is not ported.

Verified with `go build ./...`, `go vet ./...`, `go test -count=1
./internal/tokensaver/...`, `go test -count=1 -race ./internal/tokensaver/...`
and `gofmt -l internal/tokensaver/`. The fix-test fails on the pre-fix parent
("a line-numbered dump under a protected tool was not compressed: 10691 bytes
in, 10691 bytes out") and passes here; the control cases — a result that is
not a numbered dump, a dump below the 250-line threshold, and a dump below the
0.7 hit ratio — are left untouched on both trees.
