# Channel integrations

## Overview

`or3-intern` supports these non-CLI channels:

- Telegram
- Slack
- Discord
- Email
- WhatsApp via a local bridge

All external channels are disabled by default.

## Running channels

Use local terminal chat first:

```bash
go run ./cmd/or3-intern chat
```

Run configured external channels with:

```bash
go run ./cmd/or3-intern serve
```

`serve` starts the shared runtime plus any enabled channel workers.

## Common behavior

- inbound traffic is mapped to session keys per platform
- outbound sending uses the same shared runtime and tool loop
- `hardening.isolateChannelPeers=true` can isolate senders inside shared channels
- channels can use `inboundPolicy=allowlist`, `pairing`, or `deny`; when omitted, legacy `openAccess` behavior still applies
- most channels support a default outbound destination for `send_message`

## Environment variables

```dotenv
OR3_TELEGRAM_TOKEN=
OR3_SLACK_APP_TOKEN=
OR3_SLACK_BOT_TOKEN=
OR3_DISCORD_TOKEN=
OR3_WHATSAPP_BRIDGE_URL=ws://127.0.0.1:3001/ws
OR3_WHATSAPP_BRIDGE_TOKEN=
OR3_EMAIL_IMAP_HOST=
OR3_EMAIL_IMAP_PORT=993
OR3_EMAIL_IMAP_USERNAME=
OR3_EMAIL_IMAP_PASSWORD=
OR3_EMAIL_SMTP_HOST=
OR3_EMAIL_SMTP_PORT=587
OR3_EMAIL_SMTP_USERNAME=
OR3_EMAIL_SMTP_PASSWORD=
OR3_EMAIL_FROM_ADDRESS=
```

## Per-channel setup

### Telegram

- enable `channels.telegram.enabled`
- set `channels.telegram.token` or `OR3_TELEGRAM_TOKEN`
- optionally set `defaultChatId`
- optionally restrict traffic with `allowedChatIds` or `inboundPolicy=pairing`
- polling is used; no webhook setup is required

### Slack

- enable `channels.slack.enabled`
- set `channels.slack.appToken` and `channels.slack.botToken`
- optionally set `defaultChannelId`
- optionally restrict traffic with `allowedUserIds` or `inboundPolicy=pairing`
- `requireMention=true` is recommended in shared spaces
- uses Socket Mode for inbound traffic and Web API for outbound messages

### Discord

- enable `channels.discord.enabled`
- set `channels.discord.token`
- optionally set `defaultChannelId`
- optionally restrict traffic with `allowedUserIds` or `inboundPolicy=pairing`
- `requireMention=true` is recommended in guild channels
- uses the Gateway for inbound traffic and REST for outbound messages

### WhatsApp bridge

- enable `channels.whatsApp.enabled`
- set `channels.whatsApp.bridgeUrl` or `OR3_WHATSAPP_BRIDGE_URL`
- optionally set `channels.whatsApp.bridgeToken`
- optionally set `defaultTo`, `allowedFrom`, or `inboundPolicy=pairing`
- requires a compatible local bridge websocket service

### Email

- enable `channels.email.enabled`
- set `channels.email.consentGranted=true` only after explicit mailbox access permission
- choose `inboundPolicy=pairing`, `openAccess=true`, or a non-empty `allowedSenders` list
- configure IMAP (`imapHost`, `imapPort`, `imapUsername`, `imapPassword`, optional `imapMailbox`)
- configure SMTP (`smtpHost`, `smtpPort`, `smtpUsername`, `smtpPassword`, optional `fromAddress`)
- `autoReplyEnabled=false` disables automatic replies for normal inbound mail turns
- inbound mail is polled over IMAP and outbound mail reuses thread metadata when available

## Pairing-based ingress

`inboundPolicy=pairing` lets a channel accept inbound traffic only from identities that already exist in the paired-device store. The new `or3-intern pairing request` helper can mint channel-bound identities such as `slack:U123` or `telegram:456`, and `or3-intern capabilities --channel <name>` shows the effective ingress policy and access profile for each channel.

## Session key formats

The README documents these automatic session key patterns:

- `telegram:<chat-id>`
- `slack:<channel-id>`
- `discord:<channel-id>`
- `email:<normalized-address>`
- `whatsapp:<chat-id>`

## Related documentation

- [Configuration reference](configuration-reference.md)
- [Memory and context](memory-and-context.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/channels/telegram/`
- `internal/channels/slack/`
- `internal/channels/discord/`
- `internal/channels/email/`
- `internal/channels/whatsapp/`
