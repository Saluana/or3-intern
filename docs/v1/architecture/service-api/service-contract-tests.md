# Service Contract Tests

Service contract tests verify that the internal HTTP API (`/internal/v1/`) produces stable responses. These tests use frozen fixture files to catch accidental changes to the service contract.

## Test location

Tests are in `cmd/or3-intern/service_contract_test.go` with fixture files in `cmd/or3-intern/testdata/service_contract/`.

## Fixture files

The testdata directory contains frozen fixtures:

| Fixture | Purpose |
|---------|---------|
| `turn-request.json` | Raw turn request body |
| `turn-request.decoded.json` | Decoded turn request fields |
| `intern-turn-request.json` | Frozen intern (or3-app) turn request with expected correlation metadata |
| `turn-response.json` | Expected turn response shape |
| `intern-turn-response.json` | Frozen intern turn response for compatibility |
| `subagent-request.json` | Raw subagent request body |
| `subagent-request.decoded.json` | Decoded subagent request fields |
| `subagent-response.json` | Expected subagent response shape |
| `job-stream.sse` | Expected SSE stream format for job events |
| `intern-stream-events.jsonl` | Frozen intern stream events with correlation fields |
| `job-abort-response.json` | Expected abort response shape |
| `health-response.json` | Expected health endpoint response |
| `embeddings-status-response.json` | Expected embeddings status response |
| `audit-status-response.json` | Expected audit status response |
| `app-usage-routes.json` | Routes consumed by or3-app |

Source: `cmd/or3-intern/testdata/service_contract/` directory

## Test structure

The main test functions are (source: `cmd/or3-intern/service_contract_test.go`):

- `TestOr3NetCompatibilityFixtures_RequestDecoding` - verifies turn and subagent request decoding
- `TestOr3NetCompatibilityFixtures_Responses` - verifies response shapes for turns, subagents, job streams, abort, health, embeddings status
- `TestOr3NetCompatibilityFixtures_ErrorResponses` - verifies error response shapes
- `TestOr3NetCompatibilityFixtures_AuditStatus` - verifies audit status response
- `TestOr3NetCompatibilityFixtures_AppUsageRoutes` - verifies route coverage

## Correlation metadata

The frozen intern fixtures include correlation metadata fields:
- `request_id` - tracks the request across the system
- `workspace_id` - identifies the workspace
- `network_session_id` - correlates with network sessions

These fields are preserved in lifecycle events (queued, started, completion, error) and validated by the test.

Source: `cmd/or3-intern/service_contract_test.go:161-172` (turn response lifecycle event validation)

## CI enforcement

Contract tests run in CI via `.github/workflows/contracts.yml`. The workflow runs "frozen service contract tests" to prevent accidental API changes.

Source: `.github/workflows/contracts.yml:33`

## V1 contract stability

These API endpoints are part of the stable v1 contract:
- `POST /internal/v1/turns` - initiate a turn
- `POST /internal/v1/subagents` - create a subagent
- `GET /internal/v1/jobs/:id/stream` - stream job events via SSE
- `POST /internal/v1/jobs/:id/abort` - abort a job
- `GET /internal/v1/health` - health status
- `GET /internal/v1/embeddings/status` - embedding system status
- `GET /internal/v1/audit` - audit system status

Aliases such as `session_id` for `session_key` are also part of the frozen contract and covered by compatibility tests.

Source: `docs/api-reference.md:1005-1006`
