# Requirements: Token-Efficient Context Packets

## Overview

This plan defines a token-efficient context assembly system for or3-intern that preserves rich agent behavior while reducing average context size per model call. The implementation should replace the current mostly character-capped prompt assembly path with a budgeted Context Packet Builder that keeps soul, identity, tool policy, pinned memory, skills, project context, and safety rules intact while making memory, history, tools, workspace context, and artifacts more selective.

Scope assumptions:

- or3-intern remains a Go 1.22, CLI-first AI assistant with optional external chat channels.
- SQLite remains the persistence layer, using the existing primary database plus sqlite-vec/sqlvec-style vector search support.
- Existing prompt inputs such as `SOUL.md`, `IDENTITY.md`, `AGENTS.md`, `TOOLS.md`, `MEMORY.md`, skills, pinned memory, consolidation output, workspace context, and tool safety rules must not be removed to save tokens.
- The first implementation should improve assembly, budgets, task state, retrieval packing, and artifact references before adding optional cheap-model context management.
- Token estimates can initially be approximate and deterministic; exact tokenizer integration can be added later if needed.

## Requirements

1. The system must build every primary model call through a central Context Packet Builder.
   - Acceptance criteria:
     - Agent prompt construction has a single package-level path that assembles sections for system core, soul/identity/behavior, tool policy, active task card, pinned memories, memory digest, recent rolling chat, retrieved snippets, workspace context, tool schemas, artifact references, and output reserve.
     - Existing `internal/agent.Builder.BuildWithOptions` can consume the packet without losing existing history/tool-call compatibility.
     - The packet exposes section usage and pruning metadata for diagnostics.

2. The system must preserve identity, soul, safety, tool policy, pinned memory, and core behavior under all budget pressure levels.
   - Acceptance criteria:
     - Tests prove `SOUL.md`/default soul, `IDENTITY.md`, `AGENTS.md`, `TOOLS.md`, pinned memory, and safety/tool-use instructions are retained even when optional sections are pruned.
     - Emergency compaction never drops safety rules, tool restrictions, identity, or pinned memory; it can only shrink or summarize them within configured minimums.
     - Cheap context-manager output cannot remove or rewrite protected sections.

3. The system must provide configurable context budget modes.
   - Acceptance criteria:
     - Config supports at least `poor`, `balanced`, and `quality` modes with defaults for each context section.
     - Every cap is user-adjustable through persisted config and can be overridden without code changes.
     - Existing configs that do not contain context-packet settings load successfully with backward-compatible defaults.

4. The default budget modes must be cost-effective but quality-preserving.
   - Acceptance criteria:
     - Poor mode targets roughly 4k-6k input tokens before output reserve for very low-cost operation.
     - Balanced mode targets roughly 6k-10k input tokens and becomes the recommended default for normal users.
     - Quality mode targets roughly 12k-20k input tokens while still using selective retrieval and artifact references instead of indiscriminate stuffing.
     - All modes reserve output tokens and maintain a safety margin.

5. The system must add an active task card that survives rolling-history pruning.
   - Acceptance criteria:
     - SQLite stores active task state per session/scope without polluting durable long-term memory.
     - The task card tracks current user goal, current plan, hard constraints, decisions, open questions, relevant message IDs, memory IDs, artifact IDs, files inspected/edited, and last known status.
     - The task card is included in every packet for an active session, within a configurable budget.
     - The task card is updated after each assistant turn and after significant tool runs.

6. The memory schema must support richer lifecycle and source metadata without breaking existing memory rows.
   - Acceptance criteria:
     - Existing `memory_notes` rows remain readable and retrievable after migration.
     - New metadata supports kinds including pinned, fact, preference, goal, procedure, decision, episode, warning, task_state, artifact_summary, and file_summary where applicable.
     - New or extended fields include summary, scope, source artifact ID, importance, confidence, status, updated time, last used time, use count, expiration time, and supersedes ID where appropriate.
     - Migrations are additive and safe for existing SQLite databases.

7. The RAG pipeline must retrieve candidates broadly but pack snippets narrowly.
   - Acceptance criteria:
     - Retrieval can gather more candidates than it injects into the prompt.
     - The packer includes only top relevant, non-stale, non-redundant snippets within the retrieved-memory budget.
     - Each injected snippet is concise and includes source references such as memory ID, message ID, artifact ID, source kind, and score.
     - Full historical messages or full tool outputs are not injected by default.

8. The SQLite + FTS + vector retrieval strategy must support message/span-level retrieval.
   - Acceptance criteria:
     - Chat messages can be chunked into bounded spans with role, message ID, chunk index, text, summary, entities/tags, token estimate, embedding, and timestamps.
     - FTS and vector search can retrieve memory notes, durable summaries, and message spans using session/scope isolation.
     - Full source messages are fetched only when the packet contains a reference and the agent/tool explicitly needs details.

9. The final RAG score must consider task overlap, importance, lifecycle status, and novelty, not only vector/FTS similarity.
   - Acceptance criteria:
     - Ranking combines semantic/vector score, FTS/keyword score, task-card overlap, importance, recency, source quality, and novelty.
     - Stale, superseded, expired, low-confidence, and redundant candidates are demoted or excluded according to config.
     - Tests cover relevant memory inclusion, irrelevant RAG pruning, stale memory exclusion, and dedupe behavior.

10. The system must support semantic compression that preserves model behavior.
    - Acceptance criteria:
      - Prompt rendering uses readable natural text, stable labels, short structured bullets, and source references.
      - Storage may use compact JSON where useful, but rendered prompt text must remain legible.
      - CamelCase/no-whitespace compression is explicitly not used as a default strategy.
      - Summaries retain decisions, constraints, unresolved questions, and source references.

11. Tool schema exposure must be dynamic and safety-aware.
    - Acceptance criteria:
      - The runtime can expose only tools likely to be needed for the current turn, while preserving safety policy and permission checks.
      - Tool groups can distinguish default read/search/list tools, write/edit tools, exec tools, web tools, cron tools, memory tools, channel/service tools, and MCP tools.
      - Full schemas are sent only for exposed callable tools; hidden tools are not callable for that model turn.
      - Safety guards still enforce restrictions even if a tool schema is exposed.

12. Large tool outputs must be artifact-backed and context-light.
    - Acceptance criteria:
      - Tool outputs above configured thresholds store full content in artifacts.
      - Chat history keeps a bounded preview plus artifact ID, summary, MIME/type, size, and key lines where available.
      - Context packets include artifact references and summaries, not huge logs.
      - Fetch-on-demand tools can retrieve full artifacts when needed and authorized.

13. The system must include a context pressure meter.
    - Acceptance criteria:
      - Each packet records estimated input tokens, output reserve, per-section usage, total budget percent, largest sections, dropped/pruned items, rejected retrieval candidates, and pruning reasons.
      - Pressure states include normal, warning, high pressure, and emergency compaction.
      - The pressure policy deterministically prunes optional sections before invoking any cheap context manager.

14. The system may use a cheap context-manager model only behind deterministic guardrails.
    - Acceptance criteria:
      - Cheap context-manager use is optional and configurable with its own provider/model settings.
      - It is triggered only when configured events occur: packet over budget, task shift, long conversation interval, large tool output, low-confidence RAG, stale-memory review, or explicit maintenance.
      - It returns validated structured JSON for task-card updates, memory extraction, stale proposals, retrieval filtering, history summaries, tool-output summaries, and compaction proposals.
      - Invalid JSON, unsafe edits, protected-section removals, or policy weakening are rejected.

15. The cheap context-manager model must not make high-stakes or destructive decisions.
    - Acceptance criteria:
      - It cannot produce final user-facing answers for high-stakes work.
      - It cannot permanently delete memories; it can only propose lifecycle changes such as mark stale/superseded/archive.
      - It cannot silently remove identity, soul, pinned memory, tool policy, or safety rules.
      - Security-sensitive approvals remain deterministic or user-approved through existing approval/safety systems.

16. The implementation must keep channel, session, and scope isolation intact.
    - Acceptance criteria:
      - Task cards, message spans, artifact summaries, and memory retrieval respect existing session keys and resolved scope keys.
      - Channel peers remain isolated when configured by hardening settings.
      - Global memory and session-scoped memory are merged only through existing scope rules.

17. The implementation must maintain bounded resource usage.
    - Acceptance criteria:
      - Candidate retrieval, snippet packing, span chunking, artifact summarization, and task-card updates have configured limits.
      - SQLite queries are bounded and indexed.
      - Model calls for consolidation/context management have input caps and timeouts.
      - No implementation path loads unbounded chat history, artifact output, or workspace content into RAM.

18. The implementation must preserve feature parity with current OpenClaw-style agent behavior.
    - Acceptance criteria:
      - Skills, structured autonomy, heartbeats/cron, channel integrations, tool loops, memory consolidation, pinned memory, workspace context, and artifacts remain usable.
      - Token efficiency comes from hierarchy, references, retrieval filtering, summaries, and dynamic exposure rather than removing behavioral systems.

## Non-functional constraints

- Deterministic first: deterministic pruning, dedupe, scoring, and budgets should run before any model-assisted compaction.
- Low memory: all per-turn assembly must be bounded by config; avoid loading full conversations, full artifacts, or full workspaces unless explicitly fetched.
- SQLite safety: schema migrations must be additive, single-process compatible, and safe for existing user databases.
- Backward compatibility: existing configs, sessions, messages, pinned memory, memory notes, vector metadata, channels, and artifacts must continue to work.
- Secure by default: file, exec, network, web, MCP, and channel tools must keep existing sandbox, approval, allowlist, and quota behavior.
- No secret leakage: prompt packets, task cards, memory summaries, artifact previews, and diagnostic budget reports must not include secrets from config or environment.
- Bounded outputs: tool outputs, provider responses, context-manager responses, and diagnostics must be truncated or artifacted according to config.
- Inspectable behavior: budget decisions and pruning reasons should be observable enough to debug quality regressions without dumping sensitive content.
