# App-Controlled Computer Workflow

This is the high-level OR3 App → OR3 Intern flow.

## 1. Start the host service

```bash
or3-intern service
```

## 2. Pair and authenticate the app

The app uses pairing bootstrap first, then may need a session/passkey flow depending on the current auth posture.

Useful routes and commands:

- `POST /internal/v1/pairing/requests`
- `POST /internal/v1/pairing/exchange`
- `or3-intern pairing approve-code <code>`
- `or3-intern connect-device`
- `GET /internal/v1/auth/capabilities`

The app supports both pairing directions: app-created code approval with `pairing approve-code`, and CLI-created requests from `connect-device` entered into the app's **Connect with a CLI code** section. See [OR3 App Connection Guide](../app-integration/or3-app-connection-guide.md) for the complete operator guide.

## 3. Load host overview

After auth, the app can load:

```http
GET /internal/v1/app/bootstrap
```

That gives the app a summary of warnings, actions, and current host state.

## 4. Create or browse chat state

Use `chat-sessions` for metadata/history and `turns` for execution.

## 5. Resolve approvals and monitor jobs

As the app drives the host, it may need to:

- show approval requests
- monitor background jobs
- browse files
- attach to terminal sessions

The mobile UX is best when those surfaces are treated as separate route families, not one opaque “session” object.
