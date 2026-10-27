### Inject OpenCode v2 environment marker into agent system prompts

- Port `applyFingerprintSystemPrompt` from serenhope/9router PR #17.
- OpenCode v2 free tier checks for the `<env>` working directory/workspace block
  in the system prompt, instructions, or first system message. Non-agent or
  incomplete payloads trigger synthetic `429 FreeUsageLimitError` responses.
- Injects `<env>\n  Working directory: /workspace\n  Workspace root folder: /workspace\n</env>`
  when absent, and upgrades incomplete `<env>` blocks idempotently.
