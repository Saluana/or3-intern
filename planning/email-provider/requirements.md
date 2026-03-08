# Overview

This plan interprets "email provider" as an email channel integration for `or3-intern`, because this repo uses "provider" to mean LLM backend while nanobot's email functionality is an inbound/outbound chat channel.

Scope covers a bounded, opt-in email channel that:

- polls an IMAP mailbox for inbound email
- routes inbound email into the existing bus/runtime/session flow
- sends outbound replies or proactive messages through SMTP
- preserves session isolation, reply threading, and safe-by-default behavior

Assumptions:

- v1 is text-first and focuses on plain-text / HTML body extraction, not rich attachment sync
- the feature runs inside the existing single-process `serve` runtime
- existing `send_message` and channel delivery paths remain the only outbound integration surface

# Requirements

## 1. Add an opt-in email channel configuration

The system shall support an `email` entry inside `channels` config with disabled-by-default behavior.

### Acceptance criteria

- `internal/config/config.go` defines an `EmailChannelConfig` nested under `ChannelsConfig`
- defaults keep the channel disabled and do not start any network activity unless explicitly enabled
- config loading preserves backward compatibility for existing `config.json` files
- env overrides are available for the critical connection secrets and hosts
- startup validation fails with actionable errors when the channel is enabled but required settings are missing

## 2. Receive inbound email through IMAP polling

The system shall poll a configured mailbox and publish inbound email as normal agent events.

### Acceptance criteria

- when `channels.email.enabled=true` and consent is granted, `serve` starts an email poll loop
- polling reads only bounded batches of matching messages from a configured mailbox
- each accepted email becomes a `bus.Event` with `Channel="email"`
- the event message includes sender, subject, date, and bounded body text in a readable format
- duplicate delivery is prevented through IMAP state and local UID/message-id dedupe safeguards
- polling failures are logged and retried without crashing the process

## 3. Enforce safe sender access controls

The system shall require explicit consent and support sender allowlisting similar to other external channels.

### Acceptance criteria

- the email channel does not start unless a consent flag is enabled
- config supports either `openAccess=true` or a non-empty sender allowlist
- inbound messages from disallowed senders are ignored with bounded logging
- secrets are never included in log lines or user-visible error text
- the feature does not auto-reply to arbitrary email unless explicitly permitted by config

## 4. Deliver outbound email through SMTP

The system shall send email using the existing channel manager and `send_message` tool flow.

### Acceptance criteria

- `send_message` with `channel="email"` routes through the channel manager without a new notifier abstraction
- outbound delivery supports explicit recipient override and configured defaults
- replies to previously seen senders reuse the most recent subject and message-id threading metadata when available
- proactive sends are possible when a destination email address is supplied explicitly
- delivery failures are returned as normal tool/runtime errors and do not crash the channel loop

## 5. Preserve stable email session behavior

The system shall map email conversations into the existing session, history, and memory model.

### Acceptance criteria

- inbound emails map to deterministic session keys derived from sender identity, such as `email:<normalized-address>`
- message history and long-term memory for email traffic are isolated by session key unless the user explicitly links scopes
- persisted message payloads include bounded metadata needed for reply threading and audits (for example sender, subject, message-id)
- the feature works with existing scope linking and retrieval logic without schema-breaking changes

## 6. Extract readable bodies with bounded parsing

The system shall extract a useful text body from plain text and HTML email while remaining low-memory.

### Acceptance criteria

- plain-text email bodies are preferred when present
- HTML-only email is converted to readable text with lightweight sanitization
- body extraction enforces a configured character limit before event publication
- attachment bytes are not loaded or persisted in v1
- malformed MIME messages degrade gracefully instead of breaking the polling loop

## 7. Integrate with docs and operational UX

The system shall document how to configure and operate the email channel inside the existing CLI-first workflow.

### Acceptance criteria

- README documents config shape, required env vars, sender access rules, and any consent requirement
- startup output and error messages clearly indicate whether the email channel is enabled and valid
- the feature is only started in `serve`, not as an always-on background daemon outside the existing process model

# Non-functional constraints

- Keep the design single-process and SQLite-compatible; no external queue or service is introduced
- Bound IMAP polling frequency, message body size, and any in-memory dedupe state
- Reuse existing bus, runtime, and channel abstractions rather than adding parallel infrastructure
- Keep secrets in config/env only; never echo usernames, passwords, or auth tokens into prompts or logs
- Maintain deterministic startup and shutdown behavior through the existing `cmd/or3-intern` lifecycle
- Avoid attachment sync, calendar-style workflows, or full mailbox indexing in the first pass
