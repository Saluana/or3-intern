# Running service mode

Service mode runs OR3 Intern as an HTTP API server. This is how the OR3 App and OR3 Net connect to your agent.

## Start the service

```bash
or3-intern service
```

The server starts on port 9100 by default. You can change this in your config.

## Authentication

You need to set a service secret. You can do this in your config or with an env var:

```bash
export OR3_SERVICE_SECRET=your-secret
```

The OR3 App uses this secret to authenticate. Without it, the API will reject requests.

## What the API supports

- Chat turns — send messages and get responses
- File uploads — send files for the agent to work with
- Terminal access — the app can access a terminal through the service
- Job management — track background tasks
- Approval requests — approve or deny tool calls from your paired device

## Paired devices

For the best experience, pair your phone or tablet with the service. The setup wizard can help with this. Paired devices can receive approval requests and approve tool calls remotely.

## Next step

Run [serve mode](running-serve-mode.md) for connected apps and automation.
