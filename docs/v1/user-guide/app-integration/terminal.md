# Terminal

The terminal API is exposed under `/internal/v1/terminal/sessions/*`.

## Main routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/terminal/sessions` | List running terminal sessions |
| `POST /internal/v1/terminal/sessions` | Create a terminal session |
| `GET /internal/v1/terminal/sessions/{sessionId}` | Read session metadata and status |
| `GET /internal/v1/terminal/sessions/{sessionId}/stream` | Attach to SSE output/lifecycle events |
| `POST /internal/v1/terminal/sessions/{sessionId}/ws-ticket` | Issue a short-lived WebSocket ticket |
| `GET /internal/v1/terminal/sessions/{sessionId}/ws` | Attach over WebSocket |
| `POST /internal/v1/terminal/sessions/{sessionId}/input` | Send stdin |
| `POST /internal/v1/terminal/sessions/{sessionId}/resize` | Update terminal size metadata |
| `POST /internal/v1/terminal/sessions/{sessionId}/close` | Close the session |

## Behavior notes

- terminal mode can be unavailable if runtime hardening forbids it
- terminal creation and input are operator-only actions
- sessions are bounded and cleaned up when idle or closed

## Recommended client flow

1. create a session
2. attach to `.../stream` for SSE output or request a `ws-ticket` and open the WebSocket path
3. send keystrokes or command input with `.../input`
4. optionally send `.../resize`
5. close with `.../close`

Apps should treat terminal creation as a privileged capability that may be unavailable or approval-gated depending on host posture.
