## Simple Architecture Breakdown (Core Layers)

1. **Runtime entrypoints**
   This repo ships one `or3-intern` binary with multiple entrypoints rather than a separate `openclaw gateway`.
   - `or3-intern chat` runs the interactive CLI.
   - `or3-intern serve` runs enabled channels and automation.
   - `or3-intern service` runs the authenticated HTTP API for OR3 Net.
   - All entrypoints feed into the same agent runtime and persistence layer.

2. **Agent runtime / reasoning loop**
   When a turn starts from chat, a channel message, a webhook, cron, heartbeat, or file watch:
   - Session history is loaded from SQLite.
   - Bootstrap docs, indexed workspace docs, and retrieved memory are assembled into prompt context.
   - The model can answer directly or call tools and skills.
   - Tool results are fed back into the loop until the turn completes.
   - The final response is written back to history and returned through the originating surface.

3. **Persistence and memory**
   This system is not file-only.
   - Conversation history, pinned memory, retrieved notes, artifacts, approvals, and other runtime state live in SQLite.
   - Bootstrap files like `SOUL.md`, `IDENTITY.md`, `MEMORY.md`, `AGENTS.md`, `TOOLS.md`, and optional workspace docs provide additional context.
   - Retrieval combines pinned context, vector search, and FTS keyword search.
   - Session scopes can link multiple session keys to shared history and memory.

4. **Tools and skills**
   The runtime exposes built-in tools plus optional installed skills.
   - Built-in tools cover file access, guarded command execution, web fetch/search, memory, MCP, and other runtime actions.
   - Skills can be loaded from local bundles or managed through ClawHub-compatible flows.
   - Hardening, approvals, quotas, and network policy constrain what tools and skills are allowed to do.

5. **Channels, triggers, and automation**
   The same runtime is reused across interactive and autonomous entrypoints.
   - Channels include CLI, Telegram, Slack, Discord, Email, and a WhatsApp bridge.
   - Triggers include webhook, file watch, cron, and heartbeat.
   - Subagents and structured task execution run through the same shared runtime and persistence model.

### Quick summary flow
- A message or trigger arrives through CLI, a channel, or automation.
- `or3-intern` loads session state from SQLite and gathers extra context from docs and memory.
- The model runs, may call tools or skills, and loops until it has a final result.
- The runtime persists the outcome and sends the response back through the original interface.
