# 1. Config and validation

- [x] (R1, R3) Add `EmailChannelConfig` to `internal/config/config.go` with JSON fields, defaults, and conservative disabled-by-default behavior.
- [x] (R1, R3) Add env overrides for IMAP/SMTP hosts, ports, usernames, passwords, and sender address in `internal/config/config.go`.
- [x] (R1, R3) Extend channel access validation to cover email consent plus `openAccess` / `allowedSenders` requirements.
- [x] (R1) Add config tests in `internal/config/config_test.go` covering defaults, env overrides, and invalid enabled-email configurations.

# 2. Email channel package

- [x] (R2, R3, R6) Create `internal/channels/email/email.go` implementing the existing channel interface with a bounded IMAP polling loop.
- [x] (R2, R6) Implement MIME parsing helpers for plain-text preference, HTML fallback, body truncation, and malformed-message handling.
- [x] (R2, R3) Implement sender normalization, allowlist filtering, and bounded UID/message-id dedupe.
- [x] (R4, R5) Implement SMTP delivery helpers with reply subject generation and threading header support.

# 3. Startup wiring

- [x] (R1, R2, R4, R7) Update `cmd/or3-intern/main.go` to register the email channel in `buildChannelManager` when enabled.
- [x] (R7) Ensure the email poll loop only runs under `serve` and stops through the existing manager shutdown path.
- [x] (R5) Confirm email events use deterministic `email:<normalized-address>` session keys and standard bus routing.

# 4. Persistence and reply context

- [x] (R4, R5) Persist inbound email metadata through existing message payload JSON without adding a new SQLite table.
- [x] (R4, R5) Add helper logic in the email channel to recover recent reply metadata from stored messages when live in-memory caches are empty after restart.
- [x] (R5) Verify compatibility with existing scope-linking behavior; no migration or session-key rewrite should be required.

# 5. Tests

- [x] (R2, R3, R4, R6) Add `internal/channels/email/email_test.go` for IMAP parsing, dedupe, allowlist, body extraction, and SMTP message construction.
- [x] (R2, R5) Add integration-style channel tests that publish inbound events and assert session keys, channel name, and payload metadata.
- [x] (R4, R7) Add delivery tests covering reply threading, proactive send behavior, and failure propagation.

# 6. Documentation

- [x] (R7) Update `README.md` with email channel config shape, env vars, consent requirement, sender allowlist behavior, and examples for `send_message` use.
- [x] (R7) Document any deliberate v1 limitations such as no attachment sync and text-only body extraction.

# 7. Out of scope

- [x] No attachment download/upload or mailbox file persistence in the first pass.
- [x] No webhook-based inbound email ingestion or external mail relay service.
- [x] No full-mailbox indexing, search UI, or calendar/workflow automation.
- [x] No automatic replying to unknown senders without explicit user config.
