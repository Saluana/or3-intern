# OpenClaw Breakdown Gap Review

This note captures gaps from `breakdown.md` that are still missing in `or3-intern` and are not duplicated from `missing.md`.

## Net-New Gaps

- **Bootstrap memory files are still incomplete.**
  The runtime bootstraps `SOUL.md`, `AGENTS.md`, and `TOOLS.md`, but there is no equivalent prompt-loaded `IDENTITY.md`, `MEMORY.md`, or `HEARTBEAT.md` layer. `heartbeat.tasksFile` exists in config, but it is not wired into prompt assembly.
  Evidence: `internal/config/config.go`, `cmd/or3-intern/main.go`, `internal/agent/prompt.go`

- **There is no file-backed memory/workspace retrieval layer.**
  Retrieval is limited to SQLite-backed pinned notes and vector/FTS memory notes. There is no index/search path for a `memory/` directory, daily logs, project notes, or other workspace documents on disk.
  Evidence: `internal/memory/retrieve.go`, `internal/db/store.go`, `internal/db/db.go`

- **Workspace files are not gathered before the first LLM call.**
  The initial prompt is built from history, pinned memory, retrieved DB notes, and bootstrap text. Workspace files are only available if the model later decides to call `read_file` or `list_dir`, which is weaker than the breakdown's "gather context from workspace files" step.
  Evidence: `internal/agent/prompt.go`, `cmd/or3-intern/main.go`, `internal/tools/files.go`

- **Skills are markdown references, not executable/installable extensions.**
  The current skill system scans `.md` and `.txt` files, summarizes only skill names, and exposes a single `read_skill` tool to open the body. Skills do not register new tools, ship runnable code, or act like installable extensions.
  Evidence: `internal/skills/skills.go`, `internal/tools/skill.go`, `cmd/or3-intern/main.go`

- **Skill discovery is name-only, not description-driven.**
  The prompt builder gives the model a list of skill names, not capability summaries. That means the model cannot really choose skills "based on descriptions" without opening them one by one.
  Evidence: `internal/skills/skills.go`, `internal/agent/prompt.go`

- **Cross-channel session continuity is missing.**
  Sessions are intentionally namespaced per transport, such as `telegram:<chat-id>` and `slack:<channel-id>`, which keeps channels isolated instead of coherent across WhatsApp, Telegram, and Discord.
  Evidence: `README.md`, `internal/channels/telegram/telegram.go`, `internal/channels/slack/slack.go`, `internal/channels/discord/discord.go`, `internal/channels/whatsapp/whatsapp.go`

- **Scheduled work does not target a specific ongoing session.**
  Cron is wired through `agent.CronRunner(b, cfg.DefaultSessionKey)`, so scheduled jobs wake the configured default session rather than a job-specific session or agent context.
  Evidence: `cmd/or3-intern/main.go`, `internal/agent/runtime.go`, `internal/cron/cron.go`

- **Trigger coverage is limited to channel messages and cron.**
  The bus only models `user_message`, `cron`, and `system` events. There is no generic webhook ingress or file-watch trigger path to wake the agent from external events or workspace changes.
  Evidence: `internal/bus/bus.go`, `cmd/or3-intern/main.go`

- **Responses are returned only after the full loop completes.**
  The provider client uses blocking chat completions, and runtime delivers the final assistant message only after tool execution finishes. There is no streaming path back through the gateway/channels.
  Evidence: `internal/providers/openai.go`, `internal/agent/runtime.go`

## Not Repeated Here

I intentionally did not repeat items already tracked in `missing.md`, including MCP integration, broader provider support, background subagents, media/attachments, email/matrix, first-party WhatsApp bridge hosting, heartbeat service, and session-manager lifecycle work.
