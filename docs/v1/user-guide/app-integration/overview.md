# App Integration Overview

OR3 Intern exposes an authenticated machine-facing API under `/internal/v1/*`. OR3 App is the main consumer, but the same surface can be used by other browser, mobile, or local-network clients.

## Start the service

```bash
or3-intern service
```

The default listen address is `127.0.0.1:9100` unless your config overrides it.

## Core contract rules

- request and response bodies are JSON unless the route explicitly streams SSE or returns raw file content
- snake_case is the canonical request shape
- some camelCase aliases are still accepted for compatibility
- conflicting canonical + alias fields keep the snake_case value and can emit `X-Or3-Request-Warning`
- non-2xx JSON errors include `error`, `code`, and `request_id`

## Authentication model

Most routes require `Authorization: Bearer <token>`.

The main auth modes are:

- shared-secret bearer token
- paired-device bearer token
- auth-session / passkey-based session flows for app UX

Useful discovery routes:

- `GET /internal/v1/auth/capabilities` — public auth-capability discovery
- `GET /internal/v1/app/bootstrap` — authenticated app-shaped host overview

## Main route families

| Area | Route family |
| --- | --- |
| Foreground turns | `/internal/v1/turns` |
| Background jobs | `/internal/v1/subagents`, `/internal/v1/jobs/{jobId}` |
| Pairing and devices | `/internal/v1/pairing/*`, `/internal/v1/devices/*` |
| Approvals | `/internal/v1/approvals/*` |
| Config editing | `/internal/v1/configure/*` |
| Files | `/internal/v1/files/*` |
| Terminal sessions | `/internal/v1/terminal/sessions/*` |
| Chat session metadata | `/internal/v1/chat-sessions/*` |
| Runner chat | `/internal/v1/runner-chat/sessions/*` |
| Cron | `/internal/v1/cron/*` |

Use the pages in this folder for route-family details.
