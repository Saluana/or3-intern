# Runner Memory Bridge and Global Skill Tasks

## 1. Native Memory Refresh

- [x] (Req 1, 2) Add `MemoryRefresh string` to `RunnerChatCommandRequest` in
  `internal/runners/runners.go`.
- [x] (Req 1) Update `ChatExecutionInput` in `internal/runners/chat_execution.go`: for
  `ContinuationNative` with a non-empty `NativeSessionRef`, return `MemoryRefresh + "\n\n" +
  UserMessage` when `MemoryRefresh` is non-empty, else the raw user text; keep first-turn and
  replay behavior unchanged.
- [x] (Req 1) Thread `runner_chat_memory_refresh` through `internal/runners/manager.go` and
  `internal/runners/native_runtime.go` into `RunnerChatCommandRequest.MemoryRefresh`.
- [x] (Req 1, 2) Add `MemoryRefresh` to `StartTurnRequest` and set
  `agentMeta["runner_chat_memory_refresh"]` in `internal/runners/chat_manager.go`.

## 2. Centralized Refresh Assembly

- [x] (Req 2, 8) Add a bounded native refresh builder to `internal/app/runner_context.go` that
  reuses the existing pinned/retrieved/digest sources and caps (memory blocks only, no docs).
- [x] (Req 2) Compile the native refresh in `internal/app/turn_orchestrator.go` /
  `runner_prompt_compiler.go` for native follow-up turns and set `StartTurnRequest.MemoryRefresh`.
- [x] (Req 2) Confirm `RunnerContextBuilder` remains the only retrieval path; no duplicate memory
  logic added to the runners package.

## 3. Memory CLI Bridge

- [x] (Req 3) Create `cmd/or3-intern/memory_cmd.go` with `runMemoryCommandWithDeps` and a
  `memoryCommandDeps` struct (service factory, stdout, stderr).
- [x] (Req 3) Implement subcommands `search`, `add-note`, `pinned get`, `pinned set` backed by
  `memorysvc`, with `--session`, `--global`, `--top-k`, `--tags`, `--key` flags.
- [x] (Req 3) Emit JSON by default; add `--format text` for human output.
- [x] (Req 3) Build the CLI memory service from `cfg`/db/embed provider, mirroring
  `service_runner_memory.go`'s `memoryService()`.
- [x] (Req 3) Register `case "memory"` in `cmd/or3-intern/main.go`.
- [x] (Req 4) Validate required inputs and `--global` scoping; non-zero exit and no partial write
  on missing session/query/key/content; surface embedding-unavailable warnings.

## 4. Bundled Memory Skill And Discoverability

- [x] (Req 5) Add `builtin_skills/memory/SKILL.md` with `name: memory` frontmatter and body
  covering recall vs injected context, session vs global scope, durable-note rules, pinned rules,
  and command/JSON examples.
- [x] (Req 6) Embed `builtin_skills/**` via `go:embed` and resolve a stable bundled-skills dir so
  bundled skills load without workspace-local files.
- [x] (Req 6) Materialize embedded bundled skills during `setup`/`init`, with an embedded fallback
  when the on-disk bundled dir is absent; auto-install missing `memory` skill from partial trees
  and register it in `skills.policy.approved` when absent.
- [x] (Req 6) Ensure the `memory` skill resolves as source `bundled` (not `workspace`) and stays
  eligible under default policy.

## 5. Bootstrap Text

- [x] (Req 7) Rewrite the memory section of `DefaultRunnerNotes` in
  `internal/runnercontext/bootstrap.go` to prefer `or3-intern memory ...`.
- [x] (Req 7) Demote `/internal/v1/runner-memory/*` to a short service-API-internals note while
  keeping the endpoints available.

## 6. Debug Metadata

- [x] (Req 8) Add a `RunnerMemoryDebug` struct returned from context assembly and carried on
  `RunnerPromptCompileResult`.
- [x] (Req 8) Record `passive_compiled`, `native_refresh`, and per-source non-empty flags into
  runner-chat turn meta (extend `runnerChatTurnMetaJSON`/`agentMeta`) with no raw memory content.

## 7. Tests

- [x] (Req 1) `internal/runners` table tests: first native turn uses compiled prompt; follow-up
  native turns include refresh when available and fall back to raw text otherwise; replay
  unchanged.
- [x] (Req 3, 4) `cmd/or3-intern` tests: search, add-note, pinned get/set, global-only,
  missing-input validation, and JSON output via injected service.
- [x] (Req 6) Skill inventory tests: `memory` skill present, eligible, source `bundled`, and not
  workspace-only.
- [x] (Req 1-8) Run focused suites:
  `go test ./internal/app ./internal/runners ./cmd/or3-intern -run
  'TestRunnerPromptCompiler|TestChatExecutionInput|TestRunnerMemory|TestSkills'`.
- [ ] (Req 1, 5, 7) Smoke: save a pinned fact, ask an OpenCode/Codex native runner about it on a
  follow-up turn, and confirm it is visible without switching to replay mode.

## Out of Scope

- [x] Reading or importing ChatGPT/Codex app memory folders (OR3 memory DB + configured
  `MEMORY.md` remain the source of truth).
- [x] Exposing runner-visible bearer tokens for memory access (the CLI bridge replaces that).
- [x] New memory schema/tables or changes to memory consolidation behavior.
- [x] Making the memory skill a third-party managed/Clawhub skill.
