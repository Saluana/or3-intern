# Service API Lifecycle

For service API requests, the flow is different from CLI requests. Here is what happens:

## Step 1: HTTP Request Arrives

A request comes in on port 9100. The service router matches the URL path to a route handler.

## Step 2: Middleware Checks

Middleware runs on every request. It checks auth (passkey, service secret, or session token). It logs the request and applies rate limits.

## Step 3: Request Validation

The request body is parsed and validated. Missing or invalid fields return a 400 error. Wrong auth returns a 401 or 403.

## Step 4: Runtime Action

The request handler calls the runtime to do something. This could be:
- Process a turn (chat)
- Create or check a job
- Upload or download a file
- Manage approvals
- Run a terminal session

## Step 5: Response Formatting

The response is formatted as JSON. If the request asked for streaming (SSE), the response is streamed chunk by chunk.

## Step 6: Audit Log

The action is written to the audit log. This includes who did what and when. The audit log is append-only and tamper-evident.
