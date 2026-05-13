# Monitoring

OR3 Intern provides health and readiness endpoints for monitoring.

## Health check

```
GET /health
```

Returns the current service status. A 200 response means the service is running.

Response example:

```json
{
  "status": "ok",
  "uptime": 3600
}
```

## Readiness check

```
GET /ready
```

Returns 200 when the service is fully initialized and ready to handle requests. This includes checking provider connectivity and channel status.

## Uptime monitoring

Use these endpoints with monitoring tools like:

- UptimeRobot
- Better Uptime
- Prometheus + Grafana
- Custom scripts

A simple script to check every minute:

```bash
curl -s http://localhost:9100/health | grep -q '"status":"ok"'
```
