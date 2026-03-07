# 1. Config and bootstrap surface

- [ ] (R1, R2, R5, R7, R8) Extend `internal/config/config.go` with additive fields for `identityFile`, `memoryFile`, `docIndex`, `skills.enableExec`, and trigger config; preserve current defaults and env override patterns.
- [ ] (R1, R8) Update `cmd/or3-intern/init.go` and `cmd/or3-intern/init_test.go` so fresh configs can initialize the new paths and disabled-by-default trigger settings without breaking existing guided setup.
- [ ] (R1) Update `cmd/or3-intern/main.go` bootstrap wiring to ensure/load `IDENTITY.md` and `MEMORY.md`, while continuing to use `Heartbeat.TasksFile` for `HEARTBEAT.md`.

# 2. Conversation scopes and cross-channel continuity

- [ ] (R6) Add a `session_links` table and migration in `internal/db/db.go`.
- [ ] (R6) Add DB helpers in `internal/db/store.go` for linking sessions, resolving a scope key, listing scope sessions, and reading scoped history ordered by `messages.id`.
- [ ] (R6) Extend `internal/scope/scope.go` or a small adjacent helper to normalize physical session keys into logical scope keys for memory/history reads.
- [ ] (R6) Add a minimal CLI admin path in `cmd/or3-intern/main.go` for linking and inspecting session scopes, rather than auto-linking channels inside adapters.
- [ ] (R6) Add SQLite-backed tests in `internal/db/db_test.go` for linked and unlinked history behavior.

# 3. Indexed file memory and context retrieval

- [ ] (R2) Add `memory_docs` schema, FTS triggers, and supporting indexes in `internal/db/db.go`.
- [ ] (R2, R3) Implement indexed-doc upsert/query helpers in `internal/db/store.go`, keeping embeddings optional and additive.
- [ ] (R2, R3) Add a bounded doc indexer under `internal/memory`, likely `docs.go` and `docs_test.go`, that scans only configured roots, canonicalizes paths, skips symlinks, enforces caps, and updates SQLite incrementally.
- [ ] (R2) Extend retrieval in `internal/memory/retrieve.go` so prompt building can combine DB memory notes with indexed document hits.
- [ ] (R2, R3) Add tests for FTS retrieval, optional embedding ranking, deleted-file deactivation, and cap enforcement.

# 4. Prompt builder upgrades

- [ ] (R1, R3, R6, R7) Refactor `internal/agent/prompt.go` so prompt building accepts turn context, resolves the logical scope, loads `IDENTITY.md` and `MEMORY.md`, and includes `HEARTBEAT.md` only for autonomous events.
- [ ] (R3) Add a bounded "Indexed File Context" section to the system prompt using top-ranked indexed docs instead of whole-file dumps.
- [ ] (R4) Replace the current skill name-only summary with name + summary/capability text in the prompt.
- [ ] (R1, R3) Add prompt-focused regression tests in `internal/agent` for bootstrap sections, heartbeat gating, and excerpt budgets.

# 5. Skill metadata and executable entrypoints

- [ ] (R4, R5) Extend `internal/skills/skills.go` to parse optional skill metadata from front matter or a small manifest file in each skill directory.
- [ ] (R4) Keep `read_skill` backward compatible while exposing richer metadata in inventory summaries.
- [ ] (R5) Add a new `run_skill` tool in `internal/tools`, implemented on top of existing exec safety rules rather than a new execution subsystem.
- [ ] (R5) Restrict `run_skill` to manifest-declared entrypoints, fixed argv, bounded stdin/input, and the current allowed-root model.
- [ ] (R4, R5) Add tests in `internal/skills/skills_test.go` and new tool tests for manifest parsing, summary generation, execution bounds, and invalid entrypoint handling.

# 6. Targeted cron and autonomous triggers

- [ ] (R7) Extend `internal/cron/cron.go` and `internal/tools/cron.go` to support an optional `sessionKey` on cron jobs while preserving current job-file compatibility.
- [ ] (R7) Update `cmd/or3-intern/main.go` and `internal/agent/runtime.go` so cron-triggered turns resolve through the same scope logic as channel turns.
- [ ] (R8) Add a small `internal/triggers` package for webhook ingress and file polling, publishing bounded events onto the existing bus.
- [ ] (R8) Extend `internal/bus/bus.go` with explicit trigger event types or equivalent metadata helpers so downstream code can distinguish user, cron, webhook, and file-change turns.
- [ ] (R8) Add config-driven webhook route validation, secret/HMAC checking, body-size limits, and loopback-default bind behavior.
- [ ] (R8) Add file-watch polling with path canonicalization, debounce, and per-watch caps instead of recursive whole-workspace watching.
- [ ] (R7, R8) Add tests covering backward-compatible cron routing, accepted/rejected webhooks, and file-watch dedupe behavior.

# 7. Streaming runtime and channel support

- [ ] (R9) Add a streaming-capable provider path in `internal/providers/openai.go`, keeping the current non-streaming `Chat` method as the fallback.
- [ ] (R9) Refactor `internal/agent/runtime.go` so the final assistant answer can stream when the provider and channel support it, while tool-call turns still preserve bounded loop semantics.
- [ ] (R9) Add an optional streaming interface under `internal/channels` and implement it first for the CLI channel.
- [ ] (R9) Keep Slack, Discord, Telegram, and WhatsApp on final-only delivery unless a channel-specific streaming/edit path is explicitly added later.
- [ ] (R9) Add tests for CLI delta streaming, clean close/abort handling, and fallback to final-only delivery.

# 8. Integration and regression coverage

- [ ] (R1, R3, R6, R7, R9) Extend `internal/agent/runtime_test.go` for linked-session prompt hydration, autonomous-turn bootstrap handling, and streaming fallback behavior.
- [ ] (R2, R3) Add SQLite-backed integration tests that sync indexed docs, mutate them on disk, and verify retrieval updates.
- [ ] (R4, R5) Add integration coverage that a skill summary is visible to the model and a declared `run_skill` entrypoint respects safety bounds.
- [ ] (R6, R7) Add end-to-end tests that a message arriving on one linked channel produces context continuity on another linked channel without rewriting stored session keys.
- [ ] (R1-R9) Update `README.md` and any bootstrap docs to describe the new opt-in features, low-RAM defaults, and explicit trust model.

# 9. Out of scope

- [ ] No remote skill marketplace, package downloader, or automatic community skill installation.
- [ ] No automatic identity matching across channels; linking remains explicit and operator-controlled.
- [ ] No full-workspace indexing or recursive watch of arbitrary large trees by default.
- [ ] No multi-process gateway, external queue, or always-on separate control-plane service.
- [ ] No channel-native live-edit streaming implementation for every adapter in the first pass.
