# Overview

This plan adds attachment and media support to `or3-intern` in a way that fits the current Go/SQLite/CLI architecture. The goal is to let supported channels ingest media into a conversation, preserve attachment context in session history, optionally pass images to vision-capable providers, and allow the agent to send outbound media through channels that can upload files.

Assumptions for v1:

- Inbound attachment handling is implemented for channel code that already lives in this repo, with Telegram and Discord as the primary targets.
- Outbound media delivery is implemented for channels whose APIs can be handled directly in this repo without adding a new service layer, with Telegram and Discord as the primary targets.
- Slack binary upload and WhatsApp media exchange are explicitly deferred because Slack requires a more complex upload flow and WhatsApp media depends on bridge protocol changes outside this repo.
- Image attachments may be sent to the provider when vision input is explicitly enabled; non-image media remains available through textual markers and stored artifact metadata.

## Requirements

### 1. Inbound attachment capture

As a user messaging `or3-intern` through a supported external channel, I want images, audio, and documents to be captured as part of my turn so that the agent can reason about the full message instead of silently dropping media.

Acceptance criteria:

- WHEN a supported inbound channel receives a message with text and one or more accepted attachments, THEN the channel handler SHALL preserve the text and SHALL attach structured attachment metadata to the event.
- WHEN a supported inbound channel receives a media-only message, THEN the channel handler SHALL synthesize a non-empty textual marker so the turn is still persisted and visible in history.
- WHEN an inbound attachment is accepted, THEN the file SHALL be persisted locally using repo-managed storage instead of a remote URL reference.
- IF an attachment download fails, THEN the system SHALL continue processing the message and SHALL include a failure marker rather than dropping the turn or crashing.

### 2. Attachment safety and limits

As an operator, I want attachment processing to stay bounded and safe so that media support does not become an unbounded storage, memory, or network risk.

Acceptance criteria:

- WHEN an inbound or outbound attachment exceeds the configured media size limit, THEN the system SHALL reject or skip that attachment with a clear marker/error.
- IF a media type is unsupported for binary handling in v1, THEN the system SHALL preserve text context and SHALL not attempt unsafe best-effort parsing.
- WHEN the system persists or sends a local attachment, THEN it SHALL only use repo-controlled artifact storage or validated local paths and SHALL not accept arbitrary remote URLs for outbound sends.
- WHEN a channel implementation downloads remote media, THEN it SHALL only fetch provider-issued attachment URLs for that channel flow and SHALL continue to respect existing network safety expectations elsewhere in the codebase.

### 3. Attachment persistence in session history

As a user, I want attachment context to remain visible in session history so that later turns and debugging still show what media was part of the conversation.

Acceptance criteria:

- WHEN a user message with attachments is appended to history, THEN the message payload SHALL store structured attachment metadata sufficient to reconstruct recent prompt context.
- WHEN recent history is rebuilt for the model, THEN attachment markers SHALL still appear in the textual transcript even if multimodal provider input is disabled.
- WHEN stored attachment metadata references a persisted artifact, THEN the system SHALL be able to recover the local artifact path and MIME type needed for prompt building or outbound reuse.

### 4. Optional image-to-model delivery

As a user sending images, I want the provider request to include image inputs when the configured model supports them so that the agent can actually inspect the image instead of only seeing a filename marker.

Acceptance criteria:

- WHEN a user message contains supported image attachments and vision input is enabled in config, THEN the prompt builder SHALL construct provider content that includes both text and image parts for the affected user message.
- WHEN history includes recent image-bearing user messages, THEN the builder SHALL preserve those image parts within the same bounded history window already used by the runtime.
- IF vision input is disabled or image conversion fails, THEN the runtime SHALL fall back to the textual marker form without aborting the turn.
- IF the provider rejects multimodal content at request time, THEN the runtime SHALL surface a bounded error and SHALL have a defined fallback path in the design.

### 5. Outbound media delivery

As the agent using `send_message`, I want to attach local media to outbound messages so that reminders, proactive responses, and channel replies can include images or files when needed.

Acceptance criteria:

- WHEN `send_message` is called with text plus a list of media paths, THEN the tool SHALL validate the paths before dispatching delivery.
- WHEN the target channel supports outbound media in v1, THEN the channel SHALL upload or send the file and SHALL preserve the text body as a caption or companion message where the API requires it.
- IF the requested channel does not support outbound media in v1, THEN the tool SHALL return a clear error instead of silently dropping attachments.
- WHEN outbound media references local files, THEN validation SHALL respect existing workspace and artifact boundaries.

### 6. Compatibility and observability

As a maintainer, I want attachment/media support to fit the current system without breaking text-only behavior, existing configs, or deterministic tests.

Acceptance criteria:

- WHEN the new feature is disabled or unused, THEN existing text-only CLI and channel flows SHALL behave exactly as before.
- WHEN older configs are loaded, THEN sensible media defaults SHALL be applied without breaking startup.
- WHEN attachment handling is exercised in tests, THEN coverage SHALL include success, failure, oversize, unsupported-type, and fallback behavior.
- WHEN a supported channel uses media groups or multi-file inbound messages, THEN the implementation SHALL define whether v1 aggregates them or handles them one-by-one, and tests SHALL match that decision.

## Non-functional constraints

- Keep the design bounded in RAM, disk, and request size.
- Reuse existing artifact storage and message payload patterns where practical instead of introducing a large new persistence layer.
- Preserve SQLite compatibility and keep migrations minimal; prefer no schema change unless a concrete retrieval gap requires one.
- Do not require a frontend or additional always-on service for the core feature.
- Keep tool and file safety intact: no unvalidated outbound file access, no unbounded downloads, and no remote URL passthrough for agent-triggered media sends.
