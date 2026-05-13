# Service API Routing

Routes are defined in `service_routes.go`. All routes are under the base path `/api/v1`.

## Route Groups

| Group | Path | Purpose |
|---|---|---|
| Turns | `/api/v1/turns` | Chat turns (send messages, get responses) |
| Jobs | `/api/v1/jobs` | Background job management |
| Files | `/api/v1/files` | File upload, download, listing |
| Terminal | `/api/v1/terminal` | Terminal sessions (also WebSocket) |
| Approvals | `/api/v1/approvals` | Approval request management |
| Runner | `/api/v1/runner` | Runner (external CLI) chat |
| Cron | `/api/v1/cron` | Cron job management |
| Configure | `/api/v1/configure` | Config read and update |
| Health | `/api/v1/health` | Simple health ping |
| Ready | `/api/v1/ready` | Full readiness check |
| Status | `/api/v1/status` | Detailed runtime status |

## Route Registration

Each route group has a handler function. The router matches the path and method to the handler. Standard REST methods are used: GET, POST, PATCH, DELETE.

## How Routing Works

1. Request comes in with a path like `/api/v1/turns`
2. Router strips the base path
3. Router matches the remaining path to a handler
4. Middleware runs (auth, logging, rate limiting)
5. Handler processes the request
