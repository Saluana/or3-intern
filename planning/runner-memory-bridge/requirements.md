# Runner Memory Bridge and Global Skill Requirements

## Overview

Runners (OpenCode, Codex, Claude Code, Gemini CLI) lose access to OR3 memory on follow-up
turns and have no simple, cross-platform way to read or write durable memory. This feature
closes that gap with three coordinated changes:

1. Native continuation turns refresh a bounded memory block instead of sending only the raw
   user delta once a native session exists.
2. A first-party `or3-intern memory` CLI gives any runner a stable, authenticated-by-process
   bridge to OR3 memory on macOS/Linux/Windows.
3. A bundled `memory` skill teaches runners when and how to use that CLI.

`RunnerContextBuilder` stays the single passive memory assembly path. OR3's own memory DB and
configured `MEMORY.md` remain the source of truth. ChatGPT/Codex app memories are out of scope
unless explicitly imported into OR3.

## Requirements

1. Native continuation includes fresh memory on follow-up turns.
   - Acceptance: The first native turn (no `NativeSessionRef`) still sends the compiled OR3
     prompt envelope from `Run.Task`.
   - Acceptance: A follow-up native turn (with `NativeSessionRef`) sends a bounded fresh memory
     refresh block followed by the user task when compiled memory is non-empty.
   - Acceptance: A follow-up native turn falls back to the raw user text when no memory exists.
   - Acceptance: Replay-mode behavior is unchanged.

2. Passive memory assembly stays centralized.
   - Acceptance: `RunnerContextBuilder` remains the only bounded memory/doc/bootstrap assembly
     path for runner turns, including the native refresh block.
   - Acceptance: The native refresh reuses the same pinned/retrieved/digest sources and bounds
     as the compiled prompt, without introducing a second retrieval path.

3. A first-party `or3-intern memory` CLI bridges runners to OR3 memory.
   - Acceptance: `or3-intern memory search` recalls durable facts for a session, with optional
     global-only scope.
   - Acceptance: `or3-intern memory add-note` stores a durable note (not scratch work).
   - Acceptance: `or3-intern memory pinned get` reads pinned memory for a session (or a single
     key), and `or3-intern memory pinned set` upserts a pinned key/value.
   - Acceptance: All commands emit JSON by default so any runner can parse output consistently.
   - Acceptance: Commands are backed by `memorysvc` and reuse its validation, scoping, and bounds.

4. The CLI is robust and validated.
   - Acceptance: Missing required inputs (session, query, key, content) produce a clear,
     non-zero-exit error and no partial write.
   - Acceptance: `global-only` reads/writes target global scope without leaking other sessions.
   - Acceptance: When embeddings are unavailable, search/add-note still work with a surfaced
     warning rather than failing.

5. A bundled `memory` skill documents the bridge.
   - Acceptance: `builtin_skills/memory/SKILL.md` exists with valid frontmatter (`name: memory`,
     a description) and teaches when/how to call `or3-intern memory ...`.
   - Acceptance: The skill explains session scope vs global memory, durable-note rules (what to
     persist vs not), and pinned-memory rules (only durable facts that must always appear).

6. The memory skill ships as discoverable first-party content.
   - Acceptance: The `memory` skill is bundled (source `bundled`), not workspace-only, and is
     first-party rather than a third-party managed/Clawhub skill.
   - Acceptance: Setup/install ensures bundled skills are discoverable without requiring
     workspace-local files, regardless of the process working directory.
   - Acceptance: The skill appears in the inventory and remains eligible under default policy.

7. Runner bootstrap text prefers the CLI bridge.
   - Acceptance: `DefaultRunnerNotes` (and any seeded `TOOLS.md`) instruct runners to use
     `or3-intern memory ...` for recall/notes/pinned operations.
   - Acceptance: The HTTP `/internal/v1/runner-memory/*` endpoints are described as service API
     internals, not the primary runner-facing interface, but remain available.

8. Runner turns expose memory debug metadata.
   - Acceptance: Each runner turn records whether passive memory was compiled, whether a native
     memory refresh was included, and which memory sources (pinned, retrieved, digest, docs)
     were non-empty.
   - Acceptance: The metadata never includes raw memory contents or secrets, only booleans/flags.

## Non-functional Constraints

- Keep execution local-first, single-process, and compatible with current SQLite storage.
- Bound the native refresh block to the same limits already used by `RunnerContextBuilder`
  (pinned/retrieved/digest character and hit caps); never send unbounded transcripts.
- The CLI must work cross-platform through the installed `or3-intern` binary and must not require
  the runner to hold a bearer token.
- Do not weaken OR3 approval, workspace, sandbox, secret-handling, or memory-scope boundaries.
- Add no schema changes; reuse existing memory tables and turn metadata JSON.
- Preserve existing replay-mode behavior, runner-chat sessions, and CLI fallback paths.
