**Big Gaps**
- **MCP tool integration**: nanobot can connect to external MCP servers and expose their tools dynamically; `or3-intern` still has only native built-in tools, with no MCP client layer.
- **Background subagents / spawn tool**: nanobot can offload long jobs to subagents and report back later; `or3-intern` processes each session inline and has no background task agent model.
- **Automatic memory consolidation**: nanobot summarizes old conversation into durable memory/history windows; `or3-intern` has retrieval and pinned memory, but not automatic rolling consolidation/archival.
- **Media + attachment handling**: nanobot handles inbound attachments and image/audio-aware flows; `or3-intern` is still mostly text-only across channels.
- **Broader provider layer**: nanobot supports more provider backends and transcription; `or3-intern` appears centered on OpenAI only.

**Channel-Adjacent Gaps**
- **Email and Matrix**: nanobot includes these mainstream channels; `or3-intern` currently documents only Telegram, Slack, Discord, and WhatsApp bridge in README.md.
- **First-party WhatsApp bridge implementation**: nanobot ships the bridge server itself; `or3-intern` only supports connecting to a compatible external bridge, not hosting one.
- **Attachment-aware outbound messaging**: nanobot’s message tool can send files/media; `or3-intern`’s current `send_message` path is text-focused.

**Operational Gaps**
- **Real heartbeat service**: nanobot has a dedicated heartbeat module/service; `or3-intern` currently exposes heartbeat config in config.go, but there’s no corresponding service package wired into startup like cron/channels in main.go.
- **Richer session management controls**: nanobot has an explicit session manager with cancellation/consolidation lifecycle; `or3-intern` has session keys and DB persistence, but less active session orchestration.

**What I’d prioritize next**
- 1) MCP support - TODO
- 2) background subagents - PLANNED
- 3) automatic memory consolidation - DONE
- 4) attachments/media - PLANNED
- 5) Email + Matrix - TODO
