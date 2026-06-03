# Runtime Status

Runtime availability is exposed through control-plane reports rather than a single global `/status` route.

## Service Health

`GET /internal/v1/health` returns a lightweight report:

- `status`
- `runtimeAvailable`
- `jobRegistryAvailable`
- `subagentManagerEnabled`
- `approvalBrokerAvailable`
- process metadata

The status is `degraded` when key runtime pieces such as the runtime or job registry are missing.

## Readiness

`GET /internal/v1/readiness` runs the service-startup doctor mode and returns:

- `ready`
- `summary`
- `findings`

It returns `503` when readiness is not satisfied.

## How Status Is Used

- **CLI startup validation** blocks unsafe `chat`, `serve`, and `service` starts before the runtime runs.
- **Service clients** use `health`, `readiness`, and `capabilities` to decide which UI flows are available.
- **Handlers** return `503` when a required subsystem is unavailable, such as terminal mode, cron, database, agent CLI manager, or artifact storage.

## Capabilities

`GET /internal/v1/capabilities` returns the effective runtime posture: profile, hosted mode, approval broker state, tool availability, sandbox/network policy, MCP status, channel ingress summaries, trigger summaries, heartbeat, and cron availability.

Use `?channel=name` or `?trigger=name` to filter.

## Checking Status

You can check status through:

- CLI: `or3-intern status`
- CLI diagnostics: `or3-intern doctor`
- API health: `GET /internal/v1/health`
- API readiness: `GET /internal/v1/readiness`
- API capabilities: `GET /internal/v1/capabilities`
