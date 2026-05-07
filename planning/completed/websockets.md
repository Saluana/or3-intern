Based on a thorough analysis of your or3 architecture, here's my assessment:
Should be WebSocket

1. Terminal (/terminal/sessions)
   The textbook case. The current SSE+POST split (SSE for output, POST for input) is awkward — every keystroke is a separate HTTP request. Terminal is bidirectional, latency-sensitive, and involves frequent small messages in both directions. A single WebSocket connection per terminal session eliminates the request-per-keystroke overhead and the artificial input/output split.
2. Agent Turns (chat)
   Currently SSE streaming works, but WebSocket would enable proper bidirectional streaming — the client could cancel/interrupt mid-stream, send mid-turn edits/revisions, or have the server push thinking/progress updates without the client needing to poll. If you ever want the agent to ask clarifying questions mid-stream, SSE won't cut it.
3. Job Streaming + Notifications
   If combined with a general real-time channel, WebSocket could push job lifecycle events, approval notifications, and file change alerts to the app without polling. This would replace the current pattern of the app fetching /bootstrap periodically to check counts.
   Should Remain REST

- File CRUD (/files) — classic REST read/write/list, no streaming needed
- Auth (session, passkeys, pairing, devices) — discrete request/response
- Approval actions (approve/deny/cancel) — one-shot decisions
- Admin/Config (cron, configure, embeddings, audit, scope, skills, agent-runners) — infrequent management operations
- Health/Readiness/Capabilities — point-in-time queries
- Artifacts — one-time reads
  The Real Tradeoff
  SSE is simpler (standard HTTP, browser native reconnect, works through all proxies), and for most of your current use cases it's sufficient. The only service where the SSE+POST split is genuinely painful is terminal — every keystroke being a POST is wasteful and adds latency. Start there if you add WebSocket internally.

---

The Pattern (Server)
You already have the right abstraction — subscribe() returning (history, channel, unsubscribe). The cleanest change is 1:1 replacement of SSE with WebSocket per endpoint. No shared multiplexer, no generic bus. Each handler owns its own upgrade.
// Terminal: replace SSE+POST split with a single WS endpoint
// Route: GET /internal/v1/terminal/sessions/{id}/ws
func (s *serviceServer) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request, sessionID string) {
session, ok := s.getTerminalSessionByID(sessionID)
if !ok { /_ 404 _/ return }
conn, err := upgrader.Upgrade(w, r, nil)
if err != nil { return }
defer conn.Close()
history, events, unsubscribe := session.subscribe()
defer unsubscribe()
// Replay history
for \_, evt := range history {
conn.WriteJSON(evt)
}
// Read goroutine: client->server (input, resize, close)
go func() {
for {
var msg map[string]any
if err := conn.ReadJSON(&msg); err != nil { return }
switch msg["type"] {
case "input": io.WriteString(session.stdin, msg["data"].(string))
case "resize": pty.Setsize(session.ptyFile, ...)
case "close": session.close("closed")
}
}
}()
// Write goroutine: server->client (output, status, etc.)
for evt := range events {
if err := conn.WriteJSON(evt); err != nil { return }
}
}
The key insight: your serviceTerminalEvent{Type, Data} struct is already JSON-serializable. It maps directly to WebSocket JSON frames. The subscriber channel pattern stays exactly as-is — you just swap writeSSEEvent(w, ...) for conn.WriteJSON(...).
The Split
Replace SSE with WebSocket:

- GET /terminal/sessions/{id}/ws — single WebSocket endpoint replaces both GET /stream (SSE output) + POST /input (keystrokes) + POST /resize + POST /close
- GET /jobs/{id}/ws — optional, lower priority since SSE+POST works fine here (unidirectional streaming with occasional abort)
  Keep REST:
- Everything else stays exactly as-is. File CRUD, auth, approvals, config, etc. are classic request/response.
  Client Side
  Surgical change to useTerminalSession.ts:
  // Before: SSE stream + POST per keystroke
  attach() → api.stream(`/stream`) // SSE for output
  sendKeys() → api.request(`/input`, POST) // HTTP for every keystroke
  // After: single WebSocket
  connect() → api.ws(`/ws`) // WS for both directions
  sendKeys() → ws.send({ type: "input", data: bytes })
  The useOr3Api.ts gains a tiny ws() method that simply returns new WebSocket(url), authenticating via a query param or header (WebSocket upgrade doesn't carry custom headers easily, so use ?token= query param or pass auth in the first message).
  Why This Is Cleanest

1. Zero new abstractions. No message router, no pub/sub layer, no multiplexer. Each WS endpoint is a self-contained handler in service.go that upgrades, reads, and writes — same shape as your existing handlers.
2. Reuses existing patterns. The subscribe() channel pattern is already correct. The serviceTerminalEvent struct is already JSON-compatible. You literally change the transport layer without touching the data.
3. gorilla/websocket is already in go.mod. Zero new dependencies.
4. Terminal is the only must-do. The SSE+POST split there is genuinely wasteful (HTTP overhead per keystroke). Job streaming via SSE is fine. Don't over-migrate.
