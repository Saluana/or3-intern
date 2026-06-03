# Service API Routing

Routes are defined in `cmd/or3-intern/service_routes.go`. All routes are under the base path `/internal/v1`.

## Route Groups

| Group | Path | Purpose |
|---|---|---|
| Turns | `/internal/v1/turns` | Foreground agent turns, optionally streamed as SSE |
| Subagents | `/internal/v1/subagents` | Create and list background subagent jobs |
| Jobs | `/internal/v1/jobs/{jobId}` | Read, stream, or abort job snapshots |
| Artifacts | `/internal/v1/artifacts/{artifactId}` | Read capped artifact content for a session |
| Pairing | `/internal/v1/pairing/*` | Create, approve, deny, and exchange device pairing requests |
| Devices | `/internal/v1/devices/*` | List, revoke, and rotate paired device tokens |
| Approvals | `/internal/v1/approvals/*` | List, inspect, approve, deny, cancel, expire, and allowlist approval requests |
| Auth | `/internal/v1/auth/*` | Auth capabilities, sessions, passkeys, login, step-up, and revocation |
| Health | `/internal/v1/health` | Lightweight service/runtime health |
| Readiness | `/internal/v1/readiness` | Startup/readiness report from doctor rules |
| Capabilities | `/internal/v1/capabilities` | Machine-readable runtime posture |
| App bootstrap | `/internal/v1/app/bootstrap` | App-shaped host overview |
| Actions | `/internal/v1/actions/*` | Host actions such as service restart |
| Cron | `/internal/v1/cron/*` | Cron status, CRUD, run, pause, resume, delete |
| Embeddings | `/internal/v1/embeddings/*` | Embedding status and rebuild |
| Audit | `/internal/v1/audit/*` | Audit status and chain verification |
| Scope | `/internal/v1/scope/*` | Link and resolve session scope keys |
| Configure | `/internal/v1/configure/*` | Settings sections, fields, providers, models, tests, and apply |
| MCP | `/internal/v1/mcp/servers/*` | MCP server list, add, delete, and test |
| Skills | `/internal/v1/skills/*` | Skill inventory and per-skill settings |
| Files | `/internal/v1/files/*` | Root-and-path file browsing, read/write, upload, mkdir |
| Terminal | `/internal/v1/terminal/sessions/*` | PTY sessions, SSE, WebSocket, input, resize, close |
| Agent runners | `/internal/v1/agent-runners` | Detect available external-agent CLIs |
| Agent runs | `/internal/v1/agent-runs/*` | Queue/list/read external-agent CLI background runs |
| Chat runners | `/internal/v1/chat-runners` | Chat-selectable runner list |
| Chat sessions | `/internal/v1/chat-sessions/*` | App-side chat metadata and message browsing |
| Runner chat | `/internal/v1/runner-chat/sessions/*` | Interactive external-runner chat sessions and turns |

## Route Registration

Each route group has a `serviceRouteSpec`. Routes with `Subtree: true` register both the exact path and its slash-prefixed subtree. Handler functions own method dispatch inside their family.

## How Routing Works

1. Request comes in with a path like `/internal/v1/turns`.
2. The `http.ServeMux` matches the exact or subtree route.
3. Boundary middleware assigns or echoes `X-Request-Id`, applies mutation rate limiting, records request audit, and logs the response.
4. Auth middleware validates the route requirement and stamps the request identity.
5. Handler processes the request

Unknown service paths return `404` with a normalized service error payload.
