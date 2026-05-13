# Webhook Triggers

The webhook server listens for HTTP requests and publishes them as events on the internal bus.

## Configuration

- `enabled` - whether the webhook server runs
- `secret` - shared secret for HMAC authentication (required to start)
- `addr` - listen address (default: `127.0.0.1:8765`)
- `maxBodyKB` - max request body size in KB (default: 64KB)

Source: `internal/triggers/webhook.go:20-25` (WebhookServer struct), `internal/triggers/webhook.go:33-60` (Start)

## Authentication

Two authentication methods are supported:

1. **HMAC-SHA256** - checks the `X-Hub-Signature-256` header. Signature format: `sha256=<hex-hmac>`. Computes HMAC-SHA256 of the request body with the shared secret.
2. **Simple shared secret** - checks the `X-Webhook-Secret` header against the configured secret.

Source: `internal/triggers/webhook.go:132-148` (authenticate)

## Request handling

1. Read the request body (bounded by `maxBodyKB`)
2. Authenticate the request
3. Extract the route path (everything after `/webhook`)
4. Create a body preview (first 512 characters)
5. Build a `bus.Event` with type `EventWebhook`:
   - Route, content type, X-Request-ID in metadata
   - StructuredEvent with untrusted flag, body preview, and details
   - StructuredTasks parsed from the body if applicable
6. Publish to the event bus

Source: `internal/triggers/webhook.go:69-131` (handle)

## Server lifecycle

- `Start()` creates an HTTP server, binds to the address, and starts serving in a goroutine
- `Stop()` gracefully shuts down the server

Source: `internal/triggers/webhook.go:33-67`

## Routes

The server handles both `/webhook` and `/webhook/` paths. Everything after `/webhook/` becomes the route metadata.

Source: `internal/triggers/webhook.go:42-43`
