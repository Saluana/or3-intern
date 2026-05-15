# Agent CLI — Manual Verification Notes

Before enabling real structured parsers beyond best-effort JSON/JSONL, verify the following commands produce the expected output formats and exit codes. Run each command from a terminal on the same host that will run the service.

## OpenCode

```bash
# Version probe
opencode --version

# Help (future reference)
opencode run --help

# Non-interactive run with JSON output
opencode run --format json "respond with PING only"
# Expected: JSON output, exit 0
```

## Codex

```bash
# Help/version probe
codex --help

# Auth status
codex login status

# Non-interactive run with JSON output
codex exec --json --color never --sandbox workspace-write --ask-for-approval never "respond with PING only"
# Expected: JSONL lines on stdout, exit 0
```

## Claude Code

```bash
# Version probe
claude --version

# Auth status
claude auth status
# Expected: exit 0 if logged in, exit 1 if not

# Non-interactive run with streaming JSON
claude --bare -p "respond with PING only" --output-format stream-json --verbose --include-partial-messages --permission-mode acceptEdits
# Expected: JSONL lines on stdout, exit 0
```

## Gemini CLI

```bash
# Version probe (may fail; fallback is --help)
gemini --version

# Help (used as version fallback)
gemini --help

# Non-interactive run with JSON output
gemini --prompt "respond with PING only" --output-format json --approval-mode default
# Expected: JSON output, exit 0
```

## When to revisit parsers

The current best-effort parser reads each line of stdout as a standalone JSON object and emits a `structured` event if `json.Unmarshal` succeeds. This works for JSONL streams (Codex, Claude) and single-JSON-object output (OpenCode, Gemini). It is **not safe to enable per-runner structured-only modes** until manual verification confirms:

1. Every runner emits parseable JSON/JSONL for its documented output format.
2. Malformed output (e.g. ANSI escape codes in JSON fields) does not cause the parser to swallow valid output.
3. Partial lines (incomplete JSON objects at stream end) are handled gracefully (dropped, not broken-parsed).

After verification, consider enhancing the `structured` event emitter to:
- Buffer incomplete lines across scanner boundaries.
- Strip ANSI escapes before JSON parsing where appropriate.
- Recognise runner-specific structured output schemas for richer event payloads.
