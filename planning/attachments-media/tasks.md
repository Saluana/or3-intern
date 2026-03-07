## 1. Define the v1 scope and shared attachment model

- [ ] Write down the supported v1 matrix in code comments/docs: Telegram, Discord, Slack, and WhatsApp media support are in scope, with channel-specific transport notes. Requirements: 1, 2, 5, 6
- [x] Introduce a small shared `Attachment` descriptor type and helpers for kind detection (`image`, `audio`, `file`, `video`). Requirements: 1, 3, 4
- [x] Decide whether the shared type belongs in `internal/artifacts` or a tiny new `internal/media` package; keep the package surface minimal. Requirements: 3, 6

## 2. Add bounded config for media support

- [x] Extend `internal/config/config.go` with `MaxMediaBytes` and `Provider.EnableVision`. Requirements: 2, 4, 6
- [x] Set safe defaults and env/load behavior so older configs still load cleanly. Requirements: 2, 6
- [x] Add config tests covering default values and backwards-compatible loading. Requirements: 6

## 3. Extend artifact storage with attachment-oriented helpers

- [x] Add an artifact helper that saves named media and returns an attachment descriptor without changing the existing oversized-tool artifact flow. Requirements: 1, 3, 6
- [x] Add an artifact lookup helper for prompt rebuilding from stored `artifact_id`. Requirements: 3, 4
- [x] Add tests for named media save/lookup and failure handling. Requirements: 1, 3, 6

## 4. Capture inbound attachments in supported channels

- [x] Update `internal/channels/telegram/telegram.go` to detect photo, voice, audio, and document payloads, enforce size limits, download binaries, persist them as artifacts, and add textual markers plus `Meta["attachments"]`. Requirements: 1, 2, 3
- [x] Decide and implement the v1 behavior for Telegram media groups: aggregate or process individually; document and test the choice. Requirements: 1, 6
- [x] Update `internal/channels/discord/discord.go` to parse attachment metadata, download allowed files, persist them as artifacts, and add markers plus `Meta["attachments"]`. Requirements: 1, 2, 3
- [x] Update `internal/channels/slack/slack.go` to detect file shares or attachment-bearing events, download files with bot-authenticated requests, persist them as artifacts, and add markers plus `Meta["attachments"]`. Requirements: 1, 2, 3
- [x] Extend `internal/channels/whatsapp/whatsapp.go` and the bridge contract it depends on so inbound media descriptors can be received, validated, persisted, and converted into markers plus `Meta["attachments"]`. Requirements: 1, 2, 3, 6
- [ ] Add channel tests/fixtures for accepted media, oversize media, bridge/download failure markers, and media-group aggregation behavior where applicable. Requirements: 1, 2, 6

## 5. Persist attachment metadata with user messages

- [x] Update `internal/agent/runtime.go` so inbound user message payloads preserve structured attachment metadata from `bus.Event.Meta`. Requirements: 3, 6
- [x] Keep message `Content` as human-readable text with markers so history remains understandable even when multimodal is disabled. Requirements: 1, 3
- [x] Add runtime tests proving attachment metadata is persisted and text-only behavior is unchanged. Requirements: 3, 6

## 6. Rebuild recent image context for provider requests

- [x] Update `internal/agent/prompt.go` to inspect `PayloadJSON` for user-message attachments. Requirements: 3, 4
- [x] Add a helper that converts local image artifacts into OpenAI-compatible `content` parts only when `Provider.EnableVision` is enabled. Requirements: 4
- [x] Keep non-image attachments as text markers in prompt history for v1. Requirements: 1, 4
- [x] Define and implement the fallback path for missing artifact files or disabled vision. Requirements: 3, 4
- [x] Add prompt/provider tests for text-only, image+text, disabled-vision, and missing-artifact cases. Requirements: 3, 4, 6

## 7. Extend `send_message` for outbound media

- [x] Extend `internal/tools/message.go` schema and execution logic to accept `media: []string`. Requirements: 5
- [x] Validate outbound media paths against workspace restrictions and artifact storage before dispatch. Requirements: 2, 5
- [x] Pass validated media paths through channel delivery metadata without breaking current text-only sends. Requirements: 5, 6
- [ ] Add tool tests for valid media, invalid paths, unsupported channels, and text-only backward compatibility. Requirements: 2, 5, 6

## 8. Implement outbound media delivery in supported channels

- [x] Update Telegram delivery to send image/audio/document files with caption or follow-up text as needed by the Bot API. Requirements: 5
- [x] Update Discord delivery to use multipart upload for attachments while preserving text content. Requirements: 5
- [x] Update Slack delivery to use the external upload flow and preserve thread/channel context when posting attachments with message text. Requirements: 5
- [x] Extend the WhatsApp bridge send contract and Go channel delivery path to transmit outbound media requests safely. Requirements: 5, 6
- [ ] Add outbound channel tests/fixtures for Telegram, Discord, Slack, and WhatsApp media sends, including channel-specific failure paths. Requirements: 5, 6

## 9. Document the behavior and limits

- [ ] Update `README.md` with supported media channels, size limits, and the opt-in vision setting. Requirements: 2, 4, 5, 6
- [ ] Update any bootstrap/tool notes if the agent should be taught that `send_message` can carry local media paths. Requirements: 5, 6

## 10. Out of scope for this plan

- [ ] Do not implement OCR, audio transcription, or full document parsing in v1.
- [ ] Do not allow outbound media by remote URL.
