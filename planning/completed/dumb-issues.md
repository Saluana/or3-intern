# Dumb Issues - or3-intern Review

## 1) `breakdown.md` is fan fiction, not architecture documentation

**Where:** `breakdown.md:4-4`, `breakdown.md:20-24`, `README.md:3-3`

**Code snippet:**
```md
This is the single main process you run (e.g., `openclaw gateway`).
```

```md
Everything is file-based (super simple, no database hassle):
```

```md
`or3-intern` is a Go rewrite of nanobot with SQLite persistence, hybrid long-term memory retrieval, external channel integrations, autonomous triggers, and a hardened tool runtime.
```

**Why this is bad:**
This file is lying to the reader. It talks about `openclaw gateway` in a repo that ships `or3-intern`, and it claims the system is "file-based" with "no database hassle" while the actual project summary says SQLite persistence up front. That is not a harmless stale note. It poisons onboarding and makes every later design discussion noisier because people first have to figure out which document is real.

**Real-world consequences if left unfixed:**
New contributors will reason about the wrong architecture, cargo-cult the wrong mental model, and waste time chasing behavior that does not exist. Bad docs are not neutral. They actively generate bad code and bad review comments.

**Concrete fix:**
Either delete `breakdown.md` or rewrite it so it describes the current `or3-intern` runtime in repo-native terms.

```md
This is the single main process you run (for example `or3-intern serve`).

History and memory are persisted in SQLite. Workspace files and bootstrap docs are additional context sources, not the primary state store.
```

## 2) `web_fetch` resolves the same host multiple times because duplication apparently counts as defense now

**Where:** `internal/tools/web.go:57-62`, `internal/tools/web.go:81-106`, `internal/tools/web.go:127-139`, `internal/tools/web.go:274-283`, `internal/security/network.go:167-171`

**Code snippet:**
```go
if err := validateFetchURL(ctx, parsed); err != nil {
    return "", err
}
if err := validateURLAgainstPolicies(ctx, parsed, t.HostPolicy, profile); err != nil {
    return "", err
}
...
client = security.WrapHTTPClient(client, t.HostPolicy)
```

```go
addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
```

```go
plan, err := policy.resolveHost(req.Context(), req.URL.Hostname())
```

**Why this is bad:**
`web_fetch` does outbound host validation in at least two separate layers and usually resolves DNS more than once before the request even leaves the process. First `validateFetchURL` does its own lookup with `net.DefaultResolver`. Then policy validation can resolve again. Then `WrapHTTPClient` resolves again to pin the dial target. This is copy-pasted network policy with extra latency and extra failure modes.

Worse, the first lookup bypasses the injectable resolver hook that `internal/security/network.go` already exposes for tests. So you get the cost of duplicated logic and the pain of non-deterministic tests. That is the bad kind of clever: more code, less control.

**Real-world consequences if left unfixed:**
Every fetch is more DNS-sensitive than it needs to be. Flaky resolvers or restricted environments turn simple fetches into random failures. The duplicated logic will drift, and when it drifts you get policy behavior that depends on which copy of the rules happened to run.

**Concrete fix:**
Centralize host validation and resolution in `internal/security`, return a validated host plan once, and reuse it for both blocking rules and the actual dial target.

```go
plan, err := security.ResolveAndValidateURL(ctx, parsed, effectivePolicy)
if err != nil {
    return "", err
}

client = security.WrapHTTPClientWithPlan(client, effectivePolicy, plan)
```

At minimum, stop doing a separate `net.DefaultResolver.LookupIPAddr` inside `validateFetchURL`.

## 3) `TestWebFetch_MaxBytes` is not a test; it is a decorative no-op

**Where:** `internal/tools/web_test.go:93-109`

**Code snippet:**
```go
out, err := tool.Execute(context.Background(), map[string]any{
    "url":      "https://example.com/large",
    "maxBytes": float64(50),
})
if err != nil {
    t.Fatalf("WebFetch: %v", err)
}
// Body should be limited to 50 bytes
_ = out
```

**Why this is bad:**
The test claims to verify `maxBytes`, then throws the output in the trash and asserts nothing. That is fake coverage. The code path runs, the test turns green, and nobody actually learns whether truncation works.

**Real-world consequences if left unfixed:**
The suite gives false confidence around a size-limit control that is supposed to protect memory and output handling. Regressions slide through because the test never had teeth in the first place.

**Concrete fix:**
Assert on the returned body length or exact truncation behavior.

```go
if !strings.Contains(out, "status: 200") {
    t.Fatalf("expected status line, got %q", out)
}
parts := strings.SplitN(out, "\n\n", 2)
if len(parts) != 2 || len(parts[1]) != 50 {
    t.Fatalf("expected 50-byte body, got %d bytes in %q", len(parts[1]), out)
}
```

## 4) `TestHostPolicy_AllowsWildcardHost` is a unit test that still depends on live DNS like an amateur hour integration check

**Where:** `internal/security/network_test.go:21-26`, `internal/security/network.go:84-87`

**Code snippet:**
```go
func TestHostPolicy_AllowsWildcardHost(t *testing.T) {
    policy := HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"*.openai.com"}}
    target, _ := url.Parse("https://api.openai.com/v1/chat/completions")
    if err := policy.ValidateURL(context.Background(), target); err != nil {
        t.Fatalf("expected wildcard host allow, got %v", err)
    }
}
```

```go
addrs, err := lookupIPAddr(ctx, hostname)
if err != nil {
    return resolvedHostPlan{}, err
}
```

**Why this is bad:**
This is supposed to be a unit test for wildcard matching, but it also requires real DNS resolution for `api.openai.com`. The package already has an injectable `lookupIPAddr` hook and other tests in the same file use it correctly. This one just doesn’t bother. That is sloppy test design, not an unavoidable constraint.

**Real-world consequences if left unfixed:**
The suite fails in CI sandboxes, offline dev environments, and anywhere DNS is flaky or intentionally blocked. Engineers stop trusting the test suite because it reports network conditions instead of code correctness.

**Concrete fix:**
Stub `lookupIPAddr` in this test exactly like the other tests in the file already do.

```go
previousLookup := lookupIPAddr
defer func() { lookupIPAddr = previousLookup }()
lookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
    return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
}
```

## 5) The "disabled channels" test still hits the provider endpoint and lies about what it covers

**Where:** `cmd/or3-intern/security_setup_test.go:14-23`, `cmd/or3-intern/security_setup.go:70-74`

**Code snippet:**
```go
func TestValidateConfiguredOutboundEndpoints_IgnoresDisabledChannels(t *testing.T) {
    cfg := config.Default()
    policy := security.HostPolicy{
        Enabled:      true,
        DefaultDeny:  true,
        AllowedHosts: []string{"api.openai.com"},
    }
    if err := validateConfiguredOutboundEndpoints(context.Background(), cfg, policy); err != nil {
        t.Fatalf("expected disabled channel defaults to be ignored, got %v", err)
    }
}
```

```go
for _, endpoint := range []string{cfg.Provider.APIBase} {
    if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
        return err
    }
}
```

**Why this is bad:**
The test name says it is about disabled channel defaults. The function under test still validates the provider API base unconditionally, and `config.Default()` points that at a real host. So the test is not isolating disabled channels at all. It is accidentally a live-DNS test for the provider endpoint wearing a misleading name tag.

**Real-world consequences if left unfixed:**
When this test fails, the failure message sends people in the wrong direction. They go looking at Telegram/Slack/Discord defaults while the real problem is that the fixture still depends on provider DNS. That is how simple debugging turns into pointless thrashing.

**Concrete fix:**
Make the fixture match the test name. Clear or override `cfg.Provider.APIBase`, or explicitly stub resolution if provider validation is part of the intended coverage.

```go
cfg := config.Default()
cfg.Provider.APIBase = ""
```

Or rename the test so it says what it actually does.

## 6) `RenderText` flattens errors into “Warnings” because apparently severity is decorative now

**Where:** `internal/doctor/render.go:10-26`

**Code snippet:**
```go
blocks := report.BlockingFindings()
if len(blocks) > 0 {
    lines = append(lines, "", "Blockers:")
    ...
}
rest := make([]Finding, 0, len(report.Findings))
for _, finding := range report.Findings {
    if finding.Severity != SeverityBlock {
        rest = append(rest, finding)
    }
}
if len(rest) > 0 {
    lines = append(lines, "", "Warnings:")
```

**Why this is bad:**
The renderer only gives `block` findings their own section. Everything else — `warn`, `error`, and even `info` — gets shoved under `Warnings:`. That is not a cosmetic nit. The text output is the CLI contract humans actually read, and right now it lies about severity.

**Real-world consequences if left unfixed:**
Operators will treat real errors like soft warnings, miss startup-impacting conditions, and make the wrong call during incident triage. If the UI lies, the underlying severity model might as well not exist.

**Concrete fix:**
Render separate sections for `Errors`, `Warnings`, and optionally `Info`, or at minimum label the mixed bucket honestly.

```go
errors := filterBySeverity(report.Findings, SeverityError)
warnings := filterBySeverity(report.Findings, SeverityWarn)

if len(errors) > 0 {
    lines = append(lines, "", "Errors:")
    for _, finding := range errors {
        lines = append(lines, renderFindingLine(finding))
    }
}
if len(warnings) > 0 {
    lines = append(lines, "", "Warnings:")
    for _, finding := range warnings {
        lines = append(lines, renderFindingLine(finding))
    }
}
```

## 7) `doctor --probe` is not a probe; it opens and migrates the database like a vandal in a lab coat

**Where:** `internal/doctor/engine.go:1035-1051`, `internal/db/db.go:29-47`, `internal/db/db_test.go:156-159`

**Code snippet:**
```go
func probeFindings(cfg config.Config, opts Options) []Finding {
    findings := []Finding{}
    if strings.TrimSpace(cfg.DBPath) != "" {
        database, err := db.Open(cfg.DBPath)
```

```go
// Open opens path, configures both SQLite drivers, and runs migrations.
func Open(path string) (*DB, error) {
```

```go
// A path inside a non-existent directory shouldn't cause Open to fail
// because SQLite creates the file.
```

**Why this is bad:**
The CLI flag says `--probe` runs “bounded local runtime probes.” The implementation calls `db.Open`, and `db.Open` runs migrations and can create the SQLite file. That means the supposedly diagnostic path mutates state on disk. A doctor command that writes to the patient during examination is garbage design.

**Real-world consequences if left unfixed:**
Users can create new database files, alter schema state, or trigger migrations just by asking for a health check. That contaminates debugging, makes dry-run expectations false, and can leave behind partial state in environments where probe commands are supposed to be safe.

**Concrete fix:**
Use a read-only probe that checks path accessibility and performs a lightweight `PingContext` against a read-only SQLite DSN, or stat the parent directory and report missing prerequisites without touching the database file.

```go
func probeSQLite(path string) error {
    dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(1000)", path)
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        return err
    }
    defer db.Close()
    return db.Ping()
}
```

If the file does not exist, report that explicitly instead of creating it.

## 8) `FixOptions.AutomaticOnly` and `ValidateEndpoints` are fake API surface: one is ignored, the other is a stub that always says everything is fine

**Where:** `internal/doctor/fix.go:15-19`, `internal/doctor/engine.go:1353-1357`

**Code snippet:**
```go
type FixOptions struct {
    AutomaticOnly bool
}

func ApplyAutomaticFixes(cfgPath string, cfg *config.Config, report Report, opts FixOptions) ([]AppliedFix, error) {
```

```go
func ValidateEndpoints(ctx context.Context, cfg config.Config) error {
    _ = ctx
    _ = cfg
    return nil
}
```

**Why this is bad:**
`AutomaticOnly` is passed in from the CLI and then completely ignored. `ValidateEndpoints` looks like a real exported helper and does absolutely nothing. This is dead-weight API design: extra knobs, zero behavior. It increases cognitive load and invites future bugs because callers think they’re getting guarantees that do not exist.

**Real-world consequences if left unfixed:**
Future code will branch on options that are silently meaningless, or worse, call `ValidateEndpoints` assuming outbound validation happened when it did not. That is how security checks become theater.

**Concrete fix:**
Delete both surfaces unless they are wired up immediately. If `AutomaticOnly` is needed, enforce it inside `ApplyAutomaticFixes`. If `ValidateEndpoints` is needed, implement it and cover it with tests before exporting it.

```go
func ApplyAutomaticFixes(cfgPath string, cfg *config.Config, report Report, opts FixOptions) ([]AppliedFix, error) {
    ...
    for _, finding := range report.Findings {
        if opts.AutomaticOnly && finding.FixMode != FixModeAutomatic {
            continue
        }
        ...
    }
}
```

## 9) The chat TUI clears the transcript on `/new` before the backend even confirms it worked

**Where:** `internal/channels/cli/chat_tui.go:353-364`, `internal/agent/runtime.go:499-527`

**Code snippet:**
```go
if line == "/new" {
    m.messages = nil
    m.activity = nil
    m.streamIndex = map[int]int{}
}
m.messages = append(m.messages, chatMessage{role: "user", content: line})
if m.publish == nil || !m.publish(m.sessionKey, line) {
```

```go
if err := r.Consolidator.ArchiveAll(ctx, ev.SessionKey, historyMax); err != nil {
    msg := "Memory archival failed, session not cleared. Please try again."
```

**Why this is bad:**
The UI eagerly wipes the visible transcript the moment the user hits `/new`, before the runtime has archived anything or reset the session. If the bus is full, archival fails, or the DB reset fails, the backend keeps the old session and the frontend has already thrown the conversation in the trash.

**Real-world consequences if left unfixed:**
Users lose the on-screen context for the very conversation the backend is still using. That is how you manufacture mistrust in a chat client: the UI says “new session,” the runtime says “no it isn’t,” and the user is stuck guessing which side is lying.

**Concrete fix:**
Treat `/new` like an async state transition. Keep the current transcript until the runtime sends back a success notice, then clear the UI.

```go
if line == "/new" {
    m.statusText = "Starting new session…"
}

// Later, on an explicit success event:
m.messages = nil
m.activity = nil
m.streamIndex = map[int]int{}
```

## 10) Session switching is contaminated by in-flight replies because bridge events don’t carry a session key

**Where:** `internal/channels/cli/deliver.go:23-27`, `internal/channels/cli/deliver.go:98-107`, `internal/channels/cli/chat_tui.go:383-410`, `internal/channels/cli/chat_tui.go:681-694`

**Code snippet:**
```go
d.bridge.emit(chatAssistantCloseMsg{finalText: text, complete: strings.TrimSpace(text) != ""})
```

```go
type chatAssistantDeltaMsg struct {
    streamID int
    text     string
}
```

```go
case "/session":
    ...
    m.sessionKey = strings.TrimSpace(parts[1])
    m.pendingCount = 0
    m.messages = nil
```

**Why this is bad:**
The TUI supports switching sessions while replies are still in flight, but the bridge messages are not tagged with the originating session. So when an old request finally emits deltas or closes, the current model just slaps that output into whatever session the user switched to most recently.

**Real-world consequences if left unfixed:**
You get reply leakage across sessions. Ask something in `ops:prod`, switch to `personal:notes`, and the old answer can land in the wrong transcript. That is not a cosmetic bug. That is cross-session contamination.

**Concrete fix:**
Include `sessionKey` on every bridge message and ignore events that don’t match the active session.

```go
type chatAssistantDeltaMsg struct {
    sessionKey string
    streamID   int
    text       string
}

if msg.sessionKey != m.sessionKey {
    return m, m.bridge.waitCmd()
}
```

## 11) TTY detection is half-baked: chat mode only checks stdout and can wrongly launch the full-screen UI with piped stdin

**Where:** `internal/channels/cli/terminal.go:12-13`, `internal/channels/cli/cli.go:24-29`

**Code snippet:**
```go
var isTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
```

```go
if isTTY && c.Deliverer != nil {
    return c.runBubbleTea(ctx)
}
```

**Why this is bad:**
Interactive chat requires both input and output to be terminals. This code only checks stdout, so if stdin is piped but stdout is still a TTY, it tries to boot Bubble Tea anyway. Great job: the one mode that actually needs an interactive input device does not verify that it has one.

**Real-world consequences if left unfixed:**
Piped or redirected workflows can land in the wrong UI mode, hang, or behave inconsistently depending on how the shell is attached. That makes automation brittle and debugging annoying for no good reason.

**Concrete fix:**
Track stdin and stdout interactivity separately and require both before entering the TUI.

```go
var stdoutIsTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
var stdinIsTTY = isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())

if stdinIsTTY && stdoutIsTTY && c.Deliverer != nil {
    return c.runBubbleTea(ctx)
}
```

## 12) Plaintext chat uses `bufio.Scanner` and silently bails on oversized input like it’s 2012

**Where:** `internal/channels/cli/cli.go:32-71`

**Code snippet:**
```go
in := bufio.NewScanner(os.Stdin)
...
if !in.Scan() {
    return nil
}
```

**Why this is bad:**
`bufio.Scanner` has a tiny default token limit. Long pasted prompts can trip `ErrTooLong`, and this code does the worst possible thing with that error: it ignores it and exits cleanly. So the user gets neither their message processed nor a useful error. Just a dead chat loop. Spectacular.

**Real-world consequences if left unfixed:**
Large pasted prompts or stack traces can make the CLI stop reading input with no explanation. Users will assume the model or the bus broke, when the real culprit is the input reader quietly face-planting.

**Concrete fix:**
Either increase the scanner buffer and surface `scanner.Err()`, or switch to a reader that does not impose the tiny token cap.

```go
in := bufio.NewScanner(os.Stdin)
in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
...
if !in.Scan() {
    if err := in.Err(); err != nil {
        return err
    }
    return nil
}
```
