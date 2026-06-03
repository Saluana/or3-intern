# Service API Middleware

Middleware runs on every request before the handler. It handles cross-cutting concerns.

## Boundary Middleware

`serviceBoundaryMiddleware` assigns or echoes `X-Request-Id`, applies mutation rate limiting, wraps the response writer so status can be logged, records service request audit events, and logs method/path/status.

Mutation rate limiting applies to POST, PUT, PATCH, and DELETE routes except high-frequency terminal input/resize events.

## Auth Layer

Auth validation accepts configured service credentials, paired-device credentials, and auth sessions. The route policy can then require a specific role. Failures return `401` or `403` with a normalized JSON body.

## Error Normalization

Most service errors are normalized to include:

```json
{
  "error": "human-readable summary",
  "code": "validation_failed",
  "request_id": "req_abc123"
}
```

Known codes include `validation_failed`, `method_not_allowed`, `not_found`, `forbidden`, `unauthorized`, `rate_limited`, `capability_unavailable`, `request_too_large`, `conflict`, `timeout`, and `request_failed`.

## Request Validation

Strict JSON decoding is used for core execution and settings routes. Unknown fields or trailing JSON are rejected there so incompatible clients fail loudly. Some app metadata routes use looser decoding to tolerate forward-compatible optional fields.

## Typical Order

1. Boundary middleware assigns request ID and applies mutation rate limit.
2. Auth middleware validates credentials and route role requirements.
3. Handler limits body size, decodes JSON, validates fields, and executes.
4. Response is logged and audited.
