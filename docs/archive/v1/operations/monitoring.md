# Monitoring

OR3 Intern provides health and readiness endpoints for monitoring.

## Health check

```http
GET /internal/v1/health
```

Returns the current service status. A 200 response means the service is running.

Response example:

```json
{
  "status": "ok",
  "runtimeAvailable": true,
  "jobRegistryAvailable": true,
  "subagentManagerEnabled": true,
  "approvalBrokerAvailable": true,
  "processId": 12345,
  "startedAt": "2026-05-12T10:00:00Z"
}
```

## Readiness check

```http
GET /internal/v1/readiness
```

Returns 200 when startup-service checks pass. Returns 503 with `summary` and `findings` when configuration or runtime posture blocks readiness.

## Capabilities check

```http
GET /internal/v1/capabilities
```

Use this for richer monitoring and app feature gating. It reports effective runtime profile, tool availability, sandbox/network policy, MCP server status, channels, triggers, heartbeat, and cron.

## Authentication

The v1 service contract is under `/internal/v1/*`. Except for public auth discovery, monitoring routes should be called with the same authenticated service token or paired-device token used by trusted clients.

## Uptime monitoring

Use these endpoints with monitoring tools like:

- UptimeRobot
- Better Uptime
- Prometheus + Grafana
- Custom scripts

A simple script to check every minute:

```bash
curl -fsS -H "Authorization: Bearer $OR3_SERVICE_TOKEN" \
  http://localhost:9100/internal/v1/health | grep -q '"status":"ok"'
```
