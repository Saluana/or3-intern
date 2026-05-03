# External Agent CLI Delegation — Requirements

## Overview

Implement external agent CLI delegation for `or3-intern` so the service can queue and supervise non-interactive OpenCode, Codex, Claude Code, and Gemini CLI runs from the existing internal service API and job viewer.

Scope assumptions:

- The first implementation is backend-first in `or3-intern`; `or3-app` changes are planned only as API consumers and are not implemented in this planning slice.
- External CLIs run as child processes managed by `or3-intern`, not by the terminal session API.
- V1 supports typed runner options only; it must not expose raw arbitrary CLI flags.
- Host-machine runs support review and safe workspace edit modes. Dangerous/yolo behavior is an explicit sandbox-only policy unless a future dev-only override is deliberately added.

## Requirements

1. **Runner discovery is layered and safe.**
   - As an operator, I can list supported external runners and see whether each runner is installed separately from whether it is authenticated.
   - Acceptance criteria:
     - `GET /internal/v1/agent-runners` returns one entry for `or3-intern`, `opencode`, `codex`, `claude`, and `gemini` unless disabled by config.
     - Binary discovery uses `exec.LookPath` and reports `missing` without invoking the binary.
     - Version/help probing uses a short timeout and reports `error` or `unsupported_version` without conflating that result with auth.
     - Auth probing runs only documented non-mutating commands: `opencode auth list`, `codex login status`, and `claude auth status`.
     - Gemini reports `auth_unknown` when `gemini --help` works because no stable auth status command is assumed.

2. **All external runs are non-interactive.**
   - As a user submitting background delegation, I never get a job that blocks forever on an external CLI approval prompt.
   - Acceptance criteria:
     - Every adapter builds commands using documented non-interactive modes: `opencode run`, `codex exec`, `claude -p`/`--print`, and `gemini --prompt`.
     - Codex uses `--ask-for-approval never` in non-dangerous modes.
     - Claude uses a non-interactive `--permission-mode` and never relies on interactive prompts.
     - Gemini uses `--approval-mode` explicitly.
     - Process stdin is closed or deliberately controlled; V1 does not attach users to an interactive approval session.

3. **Commands are built from typed adapters, never raw shell strings.**
   - As a maintainer, I can prove prompt text and options cannot become shell syntax.
   - Acceptance criteria:
     - All process launches use `exec.CommandContext(ctx, binary, args...)` or an equivalent sandbox wrapper that preserves argv boundaries.
     - No implementation path uses `sh -c`, shell interpolation, or string-concatenated commands.
     - Prompt text containing `;`, backticks, `$()`, quotes, or newlines remains a single argv element or controlled stdin payload.
     - Runner-specific flags are emitted only by adapter code after validating typed options.

4. **Safe defaults avoid yolo/bypass behavior.**
   - As an operator, I can enable useful background work without silently granting full host-machine autonomy.
   - Acceptance criteria:
     - Default external mode is `safe_edit` unless product/UI chooses `review`; neither maps to yolo or dangerous bypass flags.
     - OpenCode dangerous mode is the only mode that emits `--dangerously-skip-permissions`.
     - Codex dangerous mode is the only mode that emits `--dangerously-bypass-approvals-and-sandbox` or `--yolo`, and `--full-auto` is never emitted.
     - Claude dangerous mode is the only mode that emits `--permission-mode bypassPermissions` or equivalent dangerous flag.
     - Gemini dangerous mode is the only mode that emits `--approval-mode yolo`, and the adapter never emits both `--yolo` and `--approval-mode yolo`.

5. **Dangerous autonomy requires OR3 sandbox isolation.**
   - As a security-conscious user, I can choose full autonomy only when the blast radius is controlled.
   - Acceptance criteria:
     - Requests separate user-facing `mode` from execution `isolation`.
     - `sandbox_auto`/dangerous mode is rejected unless isolation is `sandbox_dangerous` and sandbox readiness is true.
     - Host isolation accepts only `review` and `safe_edit` in v1.
     - Rejection messages explain that full autonomy requires an OR3 sandbox or copied workspace.

6. **Runner options are validated per runner.**
   - As an API consumer, I can provide common options without knowing unsafe runner-specific flags.
   - Acceptance criteria:
     - `POST /internal/v1/agent-runs` accepts typed fields: `parent_session_key`, `runner_id`, `task`, `timeout_seconds`, `cwd`, `model`, `mode`, `isolation`, `max_turns`, and `meta`.
     - Unknown JSON fields are rejected consistently with existing service request decoding.
     - Unsupported runner/mode/option combinations return `400` with a stable public error.
     - `max_turns` is accepted only by adapters that support it, initially Claude.
     - `cwd` must resolve inside configured service file roots/workspace boundaries unless future sandbox materialization explicitly expands it.

7. **Structured output is preferred but raw logs are always available.**
   - As a user watching a job, I can see reliable stdout/stderr even if a runner’s JSON format differs from docs.
   - Acceptance criteria:
     - OpenCode is invoked with `--format json` by default and falls back to raw display if parsing fails.
     - Codex is invoked with `--json` and parsed as JSONL best-effort.
     - Claude is invoked with `--output-format stream-json --verbose --include-partial-messages` and parsed as JSONL best-effort.
     - Gemini is invoked with `--output-format json` and parsed as best-effort JSON/raw text.
     - Raw `output` events are stored and streamed independent of structured parser success.

8. **External CLI runs use the existing job model without losing durable history.**
   - As a user, external jobs appear alongside existing turn/subagent jobs and can be read after the in-memory job registry expires.
   - Acceptance criteria:
     - Each run registers an in-memory job kind such as `agent_cli:codex` for SSE and cancellation.
     - Each run also persists an `agent_cli_runs` row and `agent_cli_events` rows in SQLite.
     - `GET /internal/v1/jobs/:job_id` can normalize an external CLI run if the in-memory `JobRegistry` no longer has it.
     - Existing `subagent_jobs` schema and behavior remain backward compatible.

9. **Process lifecycle is bounded and cancellable.**
   - As a user, I can cancel a running external CLI job and trust timeouts to stop runaway processes.
   - Acceptance criteria:
     - A dedicated process manager starts external CLI jobs in goroutines separate from terminal sessions.
     - Default timeout is 900 seconds, request minimum is 30 seconds, and server-side maximum is 7200 seconds.
     - Exit code `0` marks `succeeded`; nonzero marks `failed`; user cancel marks `aborted`; deadline marks `timed_out`.
     - On Unix, the process manager starts a process group and sends SIGTERM followed by SIGKILL after a short grace period.
     - V1 Windows behavior may kill only the direct process, with Job Objects documented as future hardening.

10. **Output storage and streaming are bounded.**
    - As an operator, a noisy CLI cannot exhaust memory, SQLite storage, or SSE consumers.
    - Acceptance criteria:
      - Event chunks are split to at most 16 KiB before publication/storage.
      - Stdout and stderr previews retain at most 64 KiB each.
      - Each event has a monotonic sequence number and timestamp.
      - Truncation emits a structured event that records dropped bytes when a configured storage cap is reached.
      - The existing in-memory `JobRegistry` event cap remains acceptable for live updates because durable `agent_cli_events` keeps complete bounded history.

11. **Child process environments are sanitized.**
    - As an operator, OR3 internal secrets are not leaked into external CLIs by default.
    - Acceptance criteria:
      - Child environments are built through a helper compatible with the existing `tools.BuildChildEnv` allowlist pattern.
      - The environment excludes OR3 internal service/pairing/node secrets even if the broader allowlist would include them.
      - `NO_COLOR=1` and `TERM=dumb` are set unless an adapter has a documented reason not to.
      - `HOME` remains available by default so CLIs can find their auth/config files.

12. **Service APIs preserve current auth, audit, and rate-limit behavior.**
    - As an operator, external runner endpoints are not a command-execution backdoor.
    - Acceptance criteria:
      - New endpoints are registered under `/internal/v1` and pass through existing service auth middleware, browser boundary checks, mutation rate limits, and audit logging.
      - Starting or canceling external jobs requires the same operator-level capability expected for subagent/job mutations.
      - Request and response payloads avoid returning secrets, raw environment values, or full command lines containing sensitive data.
      - `argv_preview` redacts or omits sensitive values while preserving enough detail for debugging.

13. **Configuration is explicit and backwards compatible.**
    - As a maintainer, I can enable or disable external CLI delegation without changing existing subagent defaults.
    - Acceptance criteria:
      - A new config section controls external agent CLI delegation independently of `subagents.enabled`.
      - Defaults are safe: feature disabled or no dangerous mode on host, conservative concurrency, bounded queue, and bounded timeout.
      - Env overrides follow existing `OR3_*` naming and config normalization patterns.
      - Existing config files load unchanged.

14. **`or3-app` can consume the feature through stable contracts.**
    - As the app implementer, I can add a runner dropdown, output panel, and cancel/retry behavior without depending on backend internals.
    - Acceptance criteria:
      - Backend responses include stable snake_case fields for runner status, job IDs, job status, stream event payloads, final previews, and stderr previews.
      - External job snapshots include `kind`, `runner_id`, `title`/task preview, `status`, `output_preview`, and `error_preview` at API boundaries.
      - Existing `/internal/v1/jobs/:job_id/stream` can stream CLI events, or a dedicated events endpoint supports reconnect with `after_seq`.

## Non-functional constraints

- **Deterministic behavior:** adapters must build predictable argv arrays from typed request fields and tests must use fake binaries instead of real external CLIs.
- **Low memory usage:** process output is streamed line/chunk-wise with ring buffers; full output is not held in memory.
- **Bounded loops/output/history:** worker concurrency, queue size, timeouts, chunk sizes, preview sizes, and persisted event retention/storage caps are bounded by config.
- **SQLite safety and migration compatibility:** migrations are additive; existing `subagent_jobs`, sessions, messages, memory, and auth tables must remain compatible.
- **Secure files/network/secrets:** cwd must be workspace-bounded, dangerous modes require sandbox isolation, and child env sanitization must avoid OR3 service secrets.
- **No frontend assumptions in backend:** backend contracts should support `or3-app`, CLI clients, and future channels without requiring a React/TypeScript runtime in `or3-intern`.
