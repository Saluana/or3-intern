# Brutal Code Review: `service.go` and `config.go`

---

## 1. service.go is a 5,667-line abomination that already has split files sitting right next to it

**File:** `cmd/or3-intern/service.go` (entire file, 5667 lines)

**Why this is bad:**
You already proved you understand the concept of file splitting by creating `service_auth.go` (858 lines) and `service_request.go` (389 lines). Yet you left the remaining 5,667-line primary file rotting with:
- Terminal session management (400+ lines)
- File operations (550+ lines)
- Approval handling (250+ lines)
- Configure UI API (250+ lines)
- Model catalog fetching (200+ lines)
- Cron management (200+ lines)
- Skills inventory (200+ lines)
- Agent runner management (150+ lines)
- Middleware, helpers, bootstrap, embeddings, audit, scope, jobs, turns...

Every editor, `git blame`, and code review tool suffers because of this. Any junior engineer who needs to modify file download behavior now has to wade through terminal WebSocket handshake code to find it. You're one developer rage-quitting a 5667-line file from ever being meaningfully reviewed.

**Real-world consequence:**
Merge conflicts will be a daily bloodbath. Multiple features touching different API endpoints will collide on the same file 100% of the time. Your CI `--diff-filter` scanning for changed packages becomes useless because every change hits the same package/file.

**Fix:**
Split into `service_terminal.go`, `service_files.go`, `service_approvals.go`, `service_configure.go`, `service_skills.go`, `service_cron.go`, `service_agents.go`, `service_models.go`, `service_middleware.go`. You already have the naming convention. Use it.

---

## 2. `serviceServer` struct is a garbage dump of 25 fields

**File:** `cmd/or3-intern/service.go:47-72`

```go
type serviceServer struct {
    config             config.Config
    configPath         string
    runtime            *agent.Runtime
    cronSvc            *cron.Service
    subagentManager    *agent.SubagentManager
    agentCLIManager    *agentcli.Manager
    jobs               *agent.JobRegistry
    broker             *approval.Broker
    unsafeDev          bool
    controlOnce        sync.Once
    controlSvc         *controlplane.Service
    appOnce            sync.Once
    appSvc             *app.ServiceApp
    terminalMu         sync.Mutex
    terminalSessions   map[string]*serviceTerminalSession
    terminalWSTicketMu sync.Mutex
    terminalWSTickets  map[string]serviceTerminalWebSocketTicket
    rateMu             sync.Mutex
    rateWindow         time.Time
    rateCounts         map[string]int
    authFailureMu      sync.Mutex
    authFailures       map[string]serviceAuthFailureState
    modelCatalogMu     sync.Mutex
    modelCatalogCache  map[string]serviceModelCatalogCacheEntry
}
```

**Why this is bad:**
This struct has no cohesion. It's an HTTP server, a terminal manager, a rate limiter, an auth failure tracker, a model catalog cache, a config holder, and a dependency injection container all in one. Every handler method on this struct can touch every field. Good luck writing a unit test for anything—you need to construct the entire universe.

The `sync.Mutex`-field-name-field pattern repeats 5 times (terminal, terminalWS, rate, authFailure, modelCatalog). Each of these should be its own type with its own methods.

**Real-world consequence:**
You cannot test the rate limiter without constructing a terminal session map. You cannot test the auth failure tracker without constructing a model catalog cache. The fields have literally nothing to do with each other except "they're maps with mutexes."

**Fix:**
Extract `terminalManager`, `rateLimiter`, `authFailureTracker`, `modelCatalog` into separate types with their own methods. `serviceServer` should delegate to them.

---

## 3. The `Default()` function is a 317-line configuration vomit

**File:** `internal/config/config.go:736-1053`

**Why this is bad:**
`Default()` returns a `Config` with ~200 explicitly set fields, each one hardcoded. There's no way to tell which defaults matter, which are arbitrary, and which are safety-critical. If you change one field's default (say, `MaxToolLoops` from 6 to 8), you have to re-read 317 lines to make sure you didn't miss a related field. The function is an unmodularized data blob masquerading as code.

**Real-world consequence:**
A new team member changes `DefaultModel` to `gpt-4.1` in the `Provider` field but forgets to update the 6 places in `ModelRouting` that also hardcode `gpt-4.1-mini`. Now you have a silent drift between the legacy provider config and the routing config that only manifests as "the model is wrong" 3 months later.

**Fix:**
Break into `defaultPaths()`, `defaultProviderProfiles()`, `defaultModelRouting()`, `defaultChannels()`, `defaultContext()`, etc., each returning their sub-config. A 20-line `Default()` that composes them.

---

## 4. `Load()` is a 460-line validation monolith

**File:** `internal/config/config.go:1577-2077`

**Why this is bad:**
The `Load` function does ALL of this:
1. Reads a file from disk
2. Unmarshals JSON
3. Applies env var overrides
4. Validates and normalizes ~80+ fields with individual `if <= 0` checks
5. Validates MCP servers
6. Validates channel access
7. Validates access profiles
8. Validates approvals
9. Validates auth config
10. Validates agent CLI config
11. Validates provider routing
12. Validates runtime profile

This is a textbook violation of the Single Responsibility Principle. You're mixing IO, parsing, env overrides, normalization, and validation into one unreviewable block.

**Real-world consequence:**
Adding a new config field requires touching this 460-line function. You will inevitably forget to add the `cfg.X <= 0` default check, and then the field silently uses Go's zero value instead of the intended default. This has almost certainly already happened with one of these 80+ fields.

**Fix:**
Separate `Load()` into `readConfig()`, `parseConfig()`, `applyDefaults()`, `normalizeConfig()`, `validateConfig()`. Each a ~30-50 line function.

---

## 5. `ApplyEnvOverrides` is 235 lines of copy-paste with zero abstraction

**File:** `internal/config/config.go:1062-1300`

```go
if v := os.Getenv("OR3_DB_PATH"); v != "" {
    cfg.DBPath = v
}
if v := os.Getenv("OR3_ARTIFACTS_DIR"); v != "" {
    cfg.ArtifactsDir = v
}
if v := os.Getenv("OR3_API_BASE"); v != "" {
    providerKey := inferProviderKey(v)
    cfg.Provider.APIBase = v
    cfg.ModelRouting.Chat.Primary.Provider = providerKey
    // ... 5 more lines of repetitive routing updates
}
// ... 200 more lines of this
```

**Why this is bad:**
Every env var has the exact same pattern: check, trim, assign. Sometimes parse an int/bool. Yet you wrote the pattern out manually 60+ times. The `OR3_SUBAGENTS_*` and `OR3_AGENT_CLI_*` blocks are nearly identical: `if v := os.Getenv(...); v != "" { if parsed, err := strconv.Atoi(v); err == nil { cfg.X = parsed } }`. That's 12 copy-pastes.

**Real-world consequence:**
Someone will add `OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS` but forget to add `OR3_AGENT_CLI_TASK_TIMEOUT_SECONDS` because they didn't realize there are two parallel systems with the same shape. Or worse, they'll copy one of the numeric env var blocks, forget to change `strconv.Atoi` to `strconv.ParseBool`, and create a silent bug.

**Fix:**
Use a table-driven approach. A slice of `{envKey, targetField, parser}` entries. Or at minimum, helper functions like `applyEnvString`, `applyEnvInt`, `applyEnvBool` that take a `*Config` and a field pointer.

---

## 6. `serviceServer.handleAuth()` is a 240-line switch monster that abuses `default:` for dynamic routing

**File:** `cmd/or3-intern/service.go:3862-4106`

```go
func (s *serviceServer) handleAuth(w http.ResponseWriter, r *http.Request) {
    relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/auth"), "/")
    // ...
    switch relative {
    case "capabilities": // ...
    case "passkeys/registration/begin": // ...
    case "passkeys/registration/finish": // ...
    case "passkeys/login/begin": // ...
    case "passkeys/login/finish": // ...
    case "step-up/begin": // ...
    case "step-up/finish": // ...
    case "session": // ...
    case "session/revoke": // ...
    case "passkeys": // ...
    default: // Nested switch on path parsing for PATCH/rename and POST/revoke
    }
}
```

**Why this is bad:**
The `default` case does ANOTHER level of path splitting with `strings.HasPrefix(relative, "passkeys/")` and then a nested `switch` on method and part count. This is string routing soup. A `passkeys/{id}/revoke` path requires parsing `/internal/v1/auth`, then the `default` case, then another split, then another switch. Three levels of manual URL parsing.

**Real-world consequence:**
Add a new auth route and you have to reason about whether it will be caught by the `default` case's `strings.HasPrefix` check, or whether it needs its own `case` at the top level, or whether it needs to be a third-level nested switch. The hierarchy is in the developer's head, not in the code.

**Fix:**
Use a proper HTTP router (chi, gorilla/mux, or even `http.ServeMux` with Go 1.22+ patterns). `mux.Handle("POST /internal/v1/auth/passkeys/{id}", ...)`. The entire `newServiceMux` function and every handler's first 10 lines of path parsing disappear.

---

## 7. Error checking by string matching on `err.Error()` — amateur hour

**File:** `cmd/or3-intern/service.go:2682-2689`

```go
case strings.Contains(msg, "not found"):
    writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "artifact not found"})
case strings.Contains(msg, "not available for session"):
    writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "artifact not available for session"})
```

Also at `service.go:2616`:
```go
if strings.Contains(err.Error(), "invalid subagent status filter") {
```

**Why this is bad:**
You're doing control flow based on the English text of error messages. If someone upstream rephrases "not found" to "artifact missing" or "could not locate", your entire error handling silently breaks. If someone accidentally returns an error that *contains* "not found" for a totally different reason, you'll tell the client a lie about what happened.

**Real-world consequence:**
The artifact system returns an IO error about a path "not found" on disk, and you tell the API consumer the artifact itself isn't available, instead of telling them the disk is broken. Bug report: "artifacts say 'not found' but I can see them." Root cause: disk mount failure. Time to debug: 4 hours.

**Fix:**
Use `errors.Is()` with sentinel errors, or define custom error types. This is Go 101.

---

## 8. `mustJSON` silently ignores errors — name is a lie

**File:** `internal/config/config.go:2079-2082`

```go
func mustJSON(v any) []byte {
    b, _ := json.MarshalIndent(v, "", "  ")
    return b
}
```

**Why this is bad:**
The function name says "must" (as in `regexp.MustCompile` — panics on error). It actually silently swallows errors and returns `nil`. If `Config` ever contains a channel or func type (which would make `json.MarshalIndent` fail), you'll silently write a zero-byte config file to disk, obliterating the user's settings with no warning.

**Real-world consequence:**
A user's config.json gets wiped to 0 bytes with no error message. They restart the app and all settings are gone. You will never reproduce this bug because it requires a specific combination of user-edited config fields.

**Fix:**
Either rename to `marshalJSON` and handle the error properly, or actually make it panic like every other Go `Must*` function in the standard library.

---

## 9. `firstNonEmpty` exists in both config.go AND service.go with different signatures

**File:** `internal/config/config.go:1552-1559`

```go
func firstNonEmpty(values ...string) string {
    for _, value := range values {
        if strings.TrimSpace(value) != "" {
            return strings.TrimSpace(value)
        }
    }
    return ""
}
```

**File:** `cmd/or3-intern/service.go:2321-2328`

```go
func serviceFirstNonEmpty(values ...string) string {
    for _, value := range values {
        if trimmed := strings.TrimSpace(value); trimmed != "" {
            return trimmed
        }
    }
    return ""
}
```

**Why this is bad:**
These are identical functions with different names in different packages. Every time someone needs this, they either:
1. Write a third copy
2. Import config from service and create a circular dependency
3. Find one and miss the other

**Real-world consequence:**
Three months from now, someone "cleans up" `serviceFirstNonEmpty` by making it delegate to `config.FirstNonEmpty`. They don't realize `config.firstNonEmpty` is unexported (lowercase). Compiler error. They export it. Now they've leaked an internal helper into the config package's public API. The next person sees `config.FirstNonEmpty` and thinks "oh, this must be a meaningful config concept, let me use it." It's not. It's just a string helper.

**Fix:**
Put this in a shared `internal/stringsx` package, or just live with one copy. Two copies is the worst of both worlds.

---

## 10. Rate limiter uses a single coarse mutex on a mutable global state — contention bomb

**File:** `cmd/or3-intern/service.go:3425-3447`

```go
func (s *serviceServer) allowMutationRequest(r *http.Request) bool {
    // ...
    s.rateMu.Lock()
    defer s.rateMu.Unlock()
    if s.rateCounts == nil || !s.rateWindow.Equal(now) {
        s.rateWindow = now
        s.rateCounts = map[string]int{}
    }
    s.rateCounts[key]++
    return s.rateCounts[key] <= limit
}
```

**Why this is bad:**
Every mutation request on the service blocks on a **single global mutex** while doing map operations. Under moderate load (100+ requests/second), this becomes a serialization point. The mutex is held for a map lookup, increment, and comparison — not long, but it's per-request. All requests funnel through this one lock.

**Real-world consequence:**
During a traffic spike (multiple paired clients, approval flows, cron jobs firing simultaneously), the service's mutation endpoints start queuing on this mutex. Latency spikes. Clients retry. More requests hit the mutex. You've created a positive feedback loop of latency.

**Fix:**
Use `sync.Map` or a sharded approach. Better yet, use a proper rate limiter library (e.g., `golang.org/x/time/rate` or `uber-go/ratelimit`). Even a simple token bucket per-actor would be more scalable than the global mutex map.

---

## 11. `serviceTerminalSession` implements a manual pub/sub event bus — reinventing wheels

**File:** `cmd/or3-intern/service.go:138-221`, `serviceTerminalSession.subscribe()`, `serviceTerminalSession.appendEvent()`

```go
type serviceTerminalSession struct {
    // ...
    mu            sync.Mutex
    events        []serviceTerminalEvent
    subscribers   map[chan serviceTerminalEvent]struct{}
}
```

**Why this is bad:**
You've hand-rolled an in-memory event bus with channels and a subscriber map inside a session struct. The `appendEvent` method iterates all subscribers and attempts a non-blocking send. If a subscriber's channel is full (buffer=32), events are silently dropped. There's no backpressure, no event replay for late subscribers beyond the initial history dump, and the subscriber cleanup (unsubscribe) closes the channel, which the subscriber may still be reading from.

**Real-world consequence:**
A WebSocket client with a slow connection starts lagging. Its channel (buffer=32) fills up. Terminal output events are silently dropped. The client sees missing output chunks and reports "the terminal is broken." You cannot reproduce it without simulating network latency.

**Fix:**
Use a ring buffer per subscriber or drop the custom pub/sub entirely and use a proper streaming pattern. The `streamJob` function already does SSE correctly — the terminal streaming should follow the same pattern instead of this custom event bus.

---

## 12. Closure leaks in `handleTerminalWebSocket` — per-connection allocations that escape

**File:** `cmd/or3-intern/service.go:1773-1781`

```go
writeEvent := func(event serviceTerminalEvent) error {
    if err := conn.SetWriteDeadline(time.Now().Add(serviceTerminalWebSocketWriteTimeout)); err != nil {
        return err
    }
    return conn.WriteJSON(event)
}
closeNormally := func(reason string) {
    _ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason), time.Now().Add(serviceTerminalWebSocketWriteTimeout))
}
```

**Why this is bad:**
These closures capture `conn` and are called in a loop. Every invocation creates a new closure allocation. Worse, the `conn` reference held by these closures means the `websocket.Conn` cannot be GC'd until the closures are GC'd, and the closures can't be GC'd until the function returns. During a long-lived terminal session (hours), this isn't a problem, but during connection storms (many rapid connect/disconnect cycles from a misbehaving client), the allocations add up.

**Real-world consequence:**
A bad client script opens and closes terminal WebSocket connections rapidly. Each connection allocates two closures referencing a `*websocket.Conn`. The GC has to walk these cross-references. Not catastrophic, but it's needless garbage in a hot path.

**Fix:**
Make these methods on a small `terminalWebSocketWriter` struct, or just inline the two lines of code at each call site since they're called from exactly 3 places.

---

## 13. Checking for `nil` receiver on every method — design smell masking a nil-ok pattern that shouldn't exist

**File:** `cmd/or3-intern/service.go`, found in at least 12 methods:

```go
func (s *serviceServer) terminalAvailable() bool {
    if s == nil { return false }
    // ...
}
func (s *serviceServer) cleanupTerminalSessions() {
    if s == nil { return }
    // ...
}
```

**Why this is bad:**
If `s` can be nil, then the caller has a nil pointer to a `*serviceServer` and you're papering over the bug. Every `if s == nil` check is an admission that somewhere in the codebase, someone called a method on a nil receiver and you decided to make that a valid use case instead of fixing the caller. This spreads — now every method needs a nil check because "what if the caller didn't initialize?"

**Real-world consequence:**
A refactor moves the `serviceServer` initialization to happen conditionally. Now half the handlers silently no-op instead of returning proper errors, because all the nil checks return zero values. Debugging "why is terminal not working" becomes a game of "which field was nil when it shouldn't have been?"

**Fix:**
Never allow `serviceServer` to be nil. Initialize it unconditionally and if it shouldn't be used, check at the construction site, not every method.

---

## 14. Model catalog cache — a hand-rolled caching layer with no eviction policy, no max size

**File:** `cmd/or3-intern/service.go:5045-5067`

```go
func (s *serviceServer) configureModelCatalog(ctx context.Context, provider, kind, category string, userFiltered, refresh bool) ([]serviceModelCatalogItem, time.Time, error) {
    cacheKey := strings.Join([]string{provider, kind, category, strconv.FormatBool(userFiltered)}, "|")
    // ...
    if entry, ok := s.modelCatalogCache[cacheKey]; ok && !refresh && now.Sub(entry.FetchedAt) < 24*time.Hour {
        // return cached
    }
    // ... fetch and cache forever
}
```

**Why this is bad:**
The cache has:
- No maximum size — every unique (provider, kind, category, userFiltered) combination is cached forever
- No eviction — entries live for the lifetime of the process
- No invalidation except the 24-hour TTL
- A global mutex that's held while making HTTP calls (at line 5057 you unlock, fetch, then re-lock)

The unlock-then-fetch-then-lock pattern means two concurrent requests for the same cache key will BOTH fetch from the upstream API because neither sees the other's in-flight result.

**Real-world consequence:**
A user configures 5 providers and browses models by category ("chat", "embeddings", "image", "audio", etc.) with and without user-filtered views. That's 5 × 2 × 5 = 50 cache entries, each storing potentially hundreds of model catalog entries. The process memory grows by 10MB+ for data that's looked at once and never again. On a long-running service, this is a slow memory leak.

**Fix:**
Use an LRU cache with a size cap. Use `singleflight` to coalesce concurrent requests for the same key. Or just use an in-memory TTL library like `go-co-op/gocron` or `patrickmn/go-cache` instead of this hand-rolled implementation.

---

## 15. `persistedSubagentEvents` uses `fmt.Sprint` to check for `<nil>` — that's not how you check nil in Go

**File:** `cmd/or3-intern/service.go:3241-3265`

```go
name := strings.TrimSpace(fmt.Sprint(function["name"]))
if name == "" || name == "<nil>" {
    name = strings.TrimSpace(fmt.Sprint(call["name"]))
}
arguments := strings.TrimSpace(fmt.Sprint(function["arguments"]))
if arguments == "<nil>" {
    arguments = ""
}
```

**Why this is bad:**
When you `fmt.Sprint(nil)`, it prints `"<nil>"`. You're relying on the string representation of a nil interface to detect nil values. This is fragile — if Go ever changes the `<nil>` format string, or if the value is `nil` but wrapped in a typed nil (e.g., `(*string)(nil)`), this check fails. Also, `fmt.Sprint` on a `json.RawMessage` that's literally the string `"<nil>"` would be indistinguishable from a nil.

**Real-world consequence:**
A subagent tool call legitimately returns a result containing the literal text `<nil>` (e.g., testing a nil-checking function). Your code strips it out and replaces it with an empty string. The user gets a truncated result and has no idea why.

**Fix:**
Type-assert: `if v, ok := function["name"].(string); ok { name = v }`. This is Go. Use the type system, not string formatting hacks.

---

## 16. `validateAuthOrigin` — localhost/rPID cross-validation is distributed across 60 lines of inline logic

**File:** `internal/config/config.go:2446-2481`

```go
if strings.EqualFold(u.Scheme, "http") {
    if host != "localhost" || rpid != "localhost" {
        return fmt.Errorf("auth origin %q is insecure; only localhost development may use http", origin)
    }
    return nil
}
// ...
if rpid == "localhost" {
    return fmt.Errorf("auth origin %q cannot be used with localhost rpId", origin)
}
if host != rpid && !strings.HasSuffix(host, "."+rpid) {
    return fmt.Errorf("auth origin %q does not match rpId %q", origin, rpid)
}
```

**Why this is bad:**
The `"localhost"` magic string appears 5 times in this function. The logic for "is this a development origin" is interleaved with "is this a valid production origin." If you need to support `127.0.0.1` as a development origin, you now have to modify this spaghetti to also check IP addresses, and suddenly every `"localhost"` comparison becomes a bug.

**Real-world consequence:**
A developer runs the service in Docker with `--network=host` and needs to connect from a browser at `https://192.168.1.5:9100`. The auth system rejects it because the origin IP doesn't match `localhost`. The fix requires restructuring the entire function because the development/production split is hardcoded.

**Fix:**
Extract `isDevelopmentOrigin(rpid, origin)` and `validateProductionOrigin(rpid, origin)` as separate functions with clear boundaries.

---

## 17. `handleTurns` and `runTurnJob` and `runApprovedResumeJob` share 30 identical lines of error handling

**File:** `cmd/or3-intern/service.go:2386-2502`

Both `runTurnJob` and `runApprovedResumeJob` contain:

```go
if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
    s.jobs.Complete(jobID, "aborted", ...)
    return
}
var approvalErr *tools.ApprovalRequiredError
if errors.As(err, &approvalErr) {
    s.jobs.Complete(jobID, "approval_required", ...)
    return
}
if fallback, ok := serviceTurnFallbackText(err, observer); ok {
    observer.finalText = fallback
    s.jobs.Complete(jobID, "completed", ...)
    return
}
s.jobs.Fail(jobID, servicePublicJobError(err), ...)
```

**Why this is bad:**
This is a 20-line error classification ladder that's copy-pasted verbatim between two functions. If you add a new error type (e.g., `QuotaExceededError`), you need to add it in two places. If you forget one, you have inconsistent behavior between regular turns and approval-resumed turns.

**Real-world consequence:**
A new developer adds quota handling to `runTurnJob` but doesn't know `runApprovedResumeJob` exists. Now approved follow-up turns silently fail with a generic "job failed" while regular turns get a helpful "quota exceeded" message. Bug report: "after I approve the tool, it fails with a vague error."

**Fix:**
Extract into `completeJobWithError(jobID, err, observer, sessionKey, meta)` that handles the entire classification.

---

## 18. `Config` is a God struct — 80+ exported fields in a single flat struct

**File:** `internal/config/config.go:101-156`

```go
type Config struct {
    DBPath                     string              `json:"dbPath"`
    ArtifactsDir               string              `json:"artifactsDir"`
    WorkspaceDir               string              `json:"workspaceDir"`
    // ... 78 more fields of every possible config concern
    ContextConfigured bool                 `json:"-"`
}
```

**Why this is bad:**
The `Config` struct doesn't have a single concern. It's the database config, the auth config, the service config, the context management config, the skills config, the channel config (Telegram, Slack, Discord, WhatsApp, Email), the cron config, the provider config, the model routing config... It's the entire application's configuration in one flat bag.

Every function that takes a `Config` gets access to everything. There's no way to say "this function only needs the database path." Every test that creates a `Config` needs to fill in 80 fields (or rely on `Default()`, which couples tests to production defaults).

**Real-world consequence:**
A function in the Telegram handler accidentally reads `config.Service.Secret` because it has access to the entire Config. A refactoring moves the service secret to a different field and nothing breaks at compile time because it's all just field access on the same struct. You find out when Telegram stops sending messages.

**Fix:**
You already have sub-structs (`Channels`, `Service`, `Auth`, etc.). Pass only the sub-struct to functions that don't need the whole thing. Better yet, use interfaces: `type ServiceConfigProvider interface { Service() ServiceConfig }`.

---

## 19. `newServiceMux` — repetitive path registration

**File:** `cmd/or3-intern/service.go:320-367`

```go
mux.Handle("/internal/v1/turns", http.HandlerFunc(server.handleTurns))
mux.Handle("/internal/v1/subagents", http.HandlerFunc(server.handleSubagents))
mux.Handle("/internal/v1/subagents/", http.HandlerFunc(server.handleSubagents))
// ... 30 more lines of root+slash pairs
```

**Why this is bad:**
Every endpoint is registered twice — once with trailing slash, once without. This is a consequence of not using a router that supports path parameters. It's also an admission that your handlers do their own path parsing (the handler receives `/subagents/abc123` and strips the prefix manually).

**Real-world consequence:**
Someone registers a new handler for `/internal/v1/newthing` but forgets the `/` variant. Now requests to `/internal/v1/newthing/` 404 while `/internal/v1/newthing` works. Bug report: "the API is inconsistent, some endpoints need trailing slashes."

**Fix:**
Go 1.22+ `http.ServeMux` supports patterns like `POST /internal/v1/pairing/requests/{id}/approve`. Use it. Kill the manual path parsing in every handler.

---

## 20. Config normalization calls `Default()` inside `normalizeProviderRouting` — side-effect city

**File:** `internal/config/config.go:1434`

```go
func normalizeProviderRouting(cfg *Config) {
    // ...
    defaultCfg := Default()
    if strings.TrimSpace(cfg.Provider.Model) != "" && (strings.TrimSpace(cfg.ModelRouting.Chat.Primary.Model) == "" || cfg.ModelRouting.Chat.Primary.Model == defaultCfg.ModelRouting.Chat.Primary.Model) {
```

**Why this is bad:**
`normalizeProviderRouting` calls `Default()` to compare the current config against the defaults. `Default()` allocates a 200+ field struct just to check if the current model value equals the default model value. That's an enormous allocation for a simple string comparison.

**Real-world consequence:**
Every call to `Load()` or `Save()` — which means every config read/write — allocates the entire 200-field default config just to be used as a constant for comparison. On a busy service that reads config frequently, this is pure waste.

**Fix:**
Extract default constants out as package-level `const` values or package-level variables. `DefaultChatModel`, `DefaultEmbedModel`, etc. Don't allocate a 200-field struct to compare one string.

---

## 21. `serviceSkillItemFromMeta` — 20-line struct literal that could be a constructor

**File:** `cmd/or3-intern/service.go:4537-4566`

```go
func serviceSkillItemFromMeta(skill skills.SkillMeta, cfg config.Config) serviceSkillItem {
    permissionState := strings.TrimSpace(skill.PermissionState)
    if permissionState == "" {
        permissionState = "approved"
    }
    return serviceSkillItem{
        Name:             skill.Name,
        Key:              serviceSkillEntryKey(skill),
        Description:      skill.Description,
        Summary:          skill.Summary,
        // ... 15 more field copies with slice duplication
    }
}
```

**Why this is bad:**
The function takes a `config.Config` but only uses `cfg.Skills.Entries[serviceSkillEntryKey(skill)]` and then only reads `strings.TrimSpace(entry.APIKey) != ""`. You're passing the entire world's configuration to read one bool.

**Real-world consequence:**
Someone refactors the skill system and needs to test this function. They have to construct a full `config.Config` (80+ fields) just to test whether `APIKeyConfigured` is set correctly. The test becomes "construct the universe, then check one field."

**Fix:**
Pass `apiKey string` instead of `cfg config.Config`. Or pass the specific `SkillsConfig` sub-struct. Every function in this file that takes `config.Config` could take a smaller interface.

---

## 22. `serviceConfigureFieldValue` — treats toggle on/off but the semantics are backwards for booleans

**File:** `cmd/or3-intern/service.go:4325-4330`

```go
func serviceConfigureFieldValue(field configureField) any {
    if field.Kind == configureFieldToggle {
        return strings.EqualFold(strings.TrimSpace(field.Value), "on")
    }
    return field.Value
}
```

**Why this is bad:**
You're storing a toggle as a string (`"on"` or `"off"`) and converting to a boolean at the API boundary. This means the internal representation disagrees with the wire representation. Every consumer of this data has to know: "if the kind is toggle, the value is actually a bool. Otherwise, it's a string." This is implicit type information encoded in a different field.

**Real-world consequence:**
A frontend developer sees `{"kind": "toggle", "value": "on"}` and writes code that does `if (field.value === "on")`. Then a backend refactor changes it to `"value": true` and the frontend silently breaks because `true === "on"` is false.

**Fix:**
Always return the typed value. Toggles always return `bool`. Secrets always return `string` (or `"[redacted]"`). Text fields always return `string`. Don't make the consumer decode the kind to interpret the value.

---

## Summary

| Metric | service.go | config.go |
|--------|-----------|-----------|
| **Lines** | 5,667 | 2,685 |
| **Should be split?** | **Yes, aggressively** | **Yes, into load/save/defaults/validation** |
| **God structs** | `serviceServer` (25 fields) | `Config` (80+ fields) |
| **Copy-pasted helpers** | `serviceFirstNonEmpty` duplicates `config.firstNonEmpty` | `mustJSON` swallows errors |
| **String-based error routing** | 3+ instances | 0 |
| **Nil receiver checks** | 12+ methods | 0 |
| **Hand-rolled caches/limiters** | Rate limiter, model catalog cache | None |
| **Biggest architectural sin** | Manual URL routing in every handler | 460-line `Load()` function |

These files are functional but they're a maintenance nightmare. The next person to touch them will spend 80% of their time reading and 20% actually changing code. If you split nothing else, at least split `service.go` — there's zero reason for terminal WebSocket handling to live in the same file as model catalog HTTP fetching.
