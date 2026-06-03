# Message Tool

Name: `send_message` | Capability: `guarded` | Group: `channels`

Sends a message or attachment through a configured delivery channel (Telegram, Slack, Discord, WhatsApp, or email).

## Parameters

- `channel` - optional channel override
- `to` - optional recipient override
- `text` - message body
- `reply_in_thread` - reuse current channel's reply metadata
- `media` - array of local file paths to send as attachments

Source: `internal/tools/message.go:31-49`

## Channel resolution

The channel and recipient are resolved in order:
1. Explicit parameter
2. Tool default (`DefaultChannel`, `DefaultTo`)
3. Current delivery context (inherited from the inbound message being replied to)

Source: `internal/tools/message.go:50-69`

## Reply-in-thread restrictions

When `reply_in_thread=true`:
- Cannot change recipient (`to` must not be explicitly set)
- Cannot change channel
- Inherits reply metadata from the current delivery context

Source: `internal/tools/message.go:84-93`

## Media attachments

Media paths must:
- Be absolute and canonical (symlinks resolved)
- Not be directories
- Fit within the configured `MaxMediaBytes` limit
- Be inside the allowed root or artifacts directory

Source: `internal/tools/message.go:123-180`

## Delivery function

The actual delivery is handled by a `DeliverFunc` injected into the tool. The function receives channel, recipient, text, and metadata.

Source: `internal/tools/message.go:13-14` (DeliverFunc)
