# Overview

Close the net-new architecture gaps identified in `planning/openclaw-architecture-gaps/findings.md` with the smallest secure design that still moves `or3-intern` toward OpenClaw-style behavior. The implementation must remain single-process, Go/SQLite based, and practical on a low-cost Raspberry Pi.

Scope assumptions:
- This plan covers the gaps from `planning/openclaw-architecture-gaps/findings.md`, not the already-tracked items in `missing.md`.
- All new capabilities are opt-in and safe by default.
- The repo remains CLI-first; no frontend, broker, or external worker service is introduced.
- Cross-channel continuity is explicit and operator-controlled, not inferred automatically.

# Requirements

1. **Bootstrap prompt files must cover identity, static memory, and autonomous guidance**
   The runtime must support `IDENTITY.md`, `MEMORY.md`, and `HEARTBEAT.md` alongside the existing `SOUL.md`, `AGENTS.md`, and `TOOLS.md`.
   Acceptance criteria:
   - Config loading supports file paths for `IDENTITY.md` and `MEMORY.md` without breaking existing configs.
   - `HEARTBEAT.md` is only injected for non-user-triggered turns such as cron, webhook, and file-change events.
   - Missing files fall back cleanly to empty/default content and do not abort startup.

2. **File-backed memory and context documents must participate in bounded retrieval**
   The runtime must index an opt-in set of memory/context files from disk into SQLite so relevant file content can be retrieved before the first model call.
   Acceptance criteria:
   - Config can enable one or more small document roots, with caps for file count, bytes per file, and indexed chunk count.
   - Indexed document retrieval works through FTS for all enabled docs and can add embeddings for small docs when configured.
   - Unconfigured repos pay effectively zero indexing cost.

3. **The prompt builder must preload relevant file context before the first LLM call**
   Prompt assembly must include a bounded section of relevant indexed file excerpts, not just chat history and DB memory.
   Acceptance criteria:
   - A turn with matching indexed docs includes those excerpts in the initial prompt snapshot.
   - Prompt inclusion stays within configured character budgets and never loads raw whole files by default.
   - Turns with no relevant docs behave like the current runtime.

4. **Skills must become lightweight local extensions with usable metadata**
   Skills must support structured discovery metadata and optional executable entrypoints while preserving the existing markdown-only skill behavior.
   Acceptance criteria:
   - The prompt sees each skill’s name plus a short summary/capability description.
   - A local skill bundle can expose a declared entrypoint that runs under existing file/exec restrictions.
   - Markdown-only skills continue to load and remain readable through `read_skill`.

5. **Executable skills must stay narrow, bounded, and explicit**
   Running a skill must be manifest-driven and must not become arbitrary command execution by another name.
   Acceptance criteria:
   - A new `run_skill` tool only executes entrypoints declared inside a skill manifest from trusted skill directories.
   - Skill execution uses direct argv execution or stdin piping, not shell interpolation of model-generated strings.
   - Timeouts, output limits, allowed-root checks, and artifact spill behavior match the existing tool safety model.

6. **Cross-channel continuity must use a logical conversation scope without merging raw history rows**
   Multiple physical session keys must be linkable into one logical conversation scope for history and memory lookup while keeping per-channel message storage intact.
   Acceptance criteria:
   - An operator can explicitly link two or more session keys into one conversation scope.
   - Prompt history and memory lookups for a linked session use the shared scope, while message appends still record the original physical session key.
   - Unlinked sessions behave exactly as they do today.

7. **Scheduled and autonomous work must target the correct conversation scope**
   Cron jobs and other autonomous triggers must be able to wake a specific linked conversation/session instead of always using the default CLI session.
   Acceptance criteria:
   - Cron jobs support an optional target session or conversation scope key.
   - Older cron job records without the new field still run and keep their current default-session behavior.
   - Non-user-triggered turns resolve the same shared scope rules as inbound channel messages.

8. **Webhook and file-change triggers must be supported with low-overhead, secure defaults**
   The runtime must accept opt-in external triggers without introducing a heavyweight control plane.
   Acceptance criteria:
   - `serve` can start a small webhook listener bound to a configured address, defaulting to loopback-only.
   - Webhook requests require a configured shared secret or HMAC validation and bounded request size.
   - File-change triggers use configured watch roots with bounded polling, dedupe, and debounce; they do not recursively monitor the whole workspace by default.

9. **Assistant output must be streamable when the channel supports it**
   The provider/runtime path must support incremental output delivery while preserving the current final-message fallback.
   Acceptance criteria:
   - CLI turns can stream assistant text incrementally.
   - Channels that do not implement streaming still receive the final response exactly once.
   - Tool loops, provider failures, and cancellations do not leave partially streamed text in an inconsistent terminal state.

# Non-functional constraints

- **Deterministic behavior**
  - Keep one main process and the current SQLite WAL + single-connection model.
  - Prefer additive tables/indexes and simple restart logic over background daemons or eventually consistent caches.
- **Low memory usage**
  - Index only configured document roots.
  - Keep prompt hydration bounded by file count, excerpt count, and character limits.
  - Prefer FTS-first retrieval and optional embeddings for small docs to keep Pi resource use predictable.
- **Bounded loops, output, and history**
  - Reuse current tool-loop, artifact spill, exec timeout, and history limits.
  - Stream deltas in bounded chunks with flush-rate caps.
- **SQLite safety and migration compatibility**
  - All schema changes must be additive and backward compatible.
  - Existing messages, memory notes, cron files, and channel session keys must remain valid.
- **Secure handling of files, network access, and secrets**
  - Webhooks bind to loopback by default and enforce request authentication.
  - File watchers must canonicalize and constrain watched paths.
  - Session linking must be explicit; no automatic cross-channel identity matching.
  - Skill execution must be manifest-bound, timeout-bound, and limited to trusted local directories.
