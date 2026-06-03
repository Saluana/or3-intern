# Service API Overview

The service API provides authenticated REST/HTTP endpoints for OR3 App, OR3 Net, and other trusted local or LAN clients. Route registration is in `cmd/or3-intern/service_routes.go`; handler families live in the surrounding `service_*.go` files.

## What the API Provides

- **Turns and jobs** — submit agent work, wait for JSON, attach to SSE, or abort work
- **Subagents and external runners** — background subagent jobs, queued external-agent CLI runs, and interactive runner chat
- **App integration** — bootstrap state, health, readiness, capabilities, auth, pairing, devices, and approvals
- **Host tools for apps** — scoped file browsing/writing, terminal sessions, artifacts, MCP server management, skills, cron, embeddings, audit, and session scope
- **Configuration** — section/field settings, provider helpers, model discovery, and MCP server config

## Who Uses It

The API is designed for:
- **OR3 App** — mobile and tablet app for chatting with the agent
- **OR3 Net** — web interface for managing the agent
- **Trusted local tools** — scripts and local clients that can authenticate and honor the same safety model

## Base Path

All routes are under `/internal/v1`. For example:

```http
POST http://localhost:9100/internal/v1/turns
```

The old `/api/v1` and root `/health` / `/ready` surfaces are not the v1 service contract.

## Port

The service listens on port 9100 by default. You can change this in the config.

## Contract Rules

- JSON is the default request and response format.
- SSE routes use `text/event-stream`; terminal WebSocket routes use a short-lived ticket.
- Snake_case is canonical for request bodies. Some camelCase aliases are accepted for compatibility.
- Error responses contain `error`, `code`, and `request_id` when they pass through the service boundary.
- Mutating routes are role-gated and can be rate-limited by actor and path.

## Main route docs

- [Routing](routing.md)
- [Auth requirements](auth-requirements.md)
- [Request models](request-models.md)
- [Turns](turns-endpoint.md)
- [Subagents](subagents-endpoint.md)
- [Jobs](jobs-endpoints.md)
- [Files](files-endpoints.md)
- [Approvals](approvals-endpoints.md)
- [Runner chat](runner-chat-endpoints.md)
- [MCP endpoints](mcp-endpoints.md)
- [Operational endpoints](operational-endpoints.md)
- [Service contract tests](service-contract-tests.md)
