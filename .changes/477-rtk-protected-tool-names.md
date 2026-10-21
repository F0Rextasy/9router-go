### fix(rtk): match the protected-tool list case-insensitively

The protected-tool list existed so a file read survives RTK intact, but it was
matched case-sensitively against the exact spellings `Read`, `Grep`, `Edit`,
`Write`, `patch`, `anchor`. The repo canonicalises tool names to **lowercase**
elsewhere — `internal/translator/fingerprint.go:49` lists
`{"bash", "glob", "grep", "read"}` — so the spellings the gateway itself
produces were exactly the ones the list missed. A file read through a
lowercase-named tool was summarised away: the same 18800-byte body came back as
3765 bytes under `read`, while the identical payload under `Read` survived
whole. That is the harm the list exists to prevent, and the agent then edits
against a summary instead of the file it asked to see.

The lookup is now case-insensitive and keeps the whitespace trim it already
performed, and the map is keyed by the canonical lowercase spelling so the
list documents itself. The vocabulary is extended with the repo's own tool
names:

- **The lowercase quartet** — `read`, `grep`, `bash`, `glob`, from
  `internal/translator/fingerprint.go:49`.
- **The Claude Code file tools** — `edit`, `write`, `notebookedit`, `webfetch`,
  from the `CCDecoyTools` list in
  `internal/proxy/executor/claude_decloak.go:22`.

`bash` is protected deliberately: it is the generic way agents read files and
logs (`cat`, `sed -n`, `grep`, `git`), so a lossy content-matched filter on a
bash result changes what the agent believes about the file. Upstream's RTK has
no protected-tool concept at all — it only preserves `is_error` traces
(`open-sse/rtk/index.js:67`) — so this list is a local hardening and the change
extends it rather than diverging from upstream. The content-detection
behaviour itself is untouched: upstream detects the filter from the output
text alone (`open-sse/rtk/autodetect.js:31`), and the port matching that is
correct.

The cost is measured, not assumed: across a corpus of typical bash results
(git status, git diff, build output, grep output, `ls` output, a file read)
RTK saves 37% of the bytes. None of that saving reaches a `bash` result any
more, which is the deliberate trade — a lossy summary of the command output is
what makes an agent conclude its shell is broken.

`isErrorToolResult` is unchanged. The OpenAI Chat Completions tool-result
shape (`role: "tool"`, `tool_call_id`, string `content`) carries no error
field, so the flag does not survive translation; that is a separate defect,
tracked separately.

Verified with `TestCompressMessages_ProtectedToolPreservesContent` — the
end-to-end case through `CompressMessages`: a tool result named `read` comes
back byte-identical, the same body under `some_other_tool` is still
compressed. On the pre-fix code that test fails (17920 bytes in, 3693 bytes
out under `read`); `TestIsProtectedToolVocabulary` and
`TestIsProtectedToolTrimsWhitespace` pin the case-insensitive match over the
whole vocabulary.
