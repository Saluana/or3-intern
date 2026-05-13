# Service API Middleware

Middleware runs on every request before the handler. It handles cross-cutting concerns.

## Auth Middleware

Checks credentials on every request. Different endpoints need different auth levels. Returns 401 if auth fails or 403 if the credentials do not have access.

## Logging Middleware

Logs every request: method, path, status code, duration, client IP. Logs go to the application log. Useful for debugging and monitoring.

## Rate Limiting

Prevents a single client from sending too many requests. Limits are configurable. When the limit is hit, the API returns 429 Too Many Requests.

## CORS Middleware

Handles Cross-Origin Resource Sharing for web apps. Allows requests from the OR3 Net web interface and other configured origins.

## Request Validation

Checks that the request body is valid JSON. Validates required fields. Returns 400 for malformed requests.

## Middleware Order

1. Logging
2. CORS
3. Rate limiting
4. Auth
5. Request validation
6. Handler
