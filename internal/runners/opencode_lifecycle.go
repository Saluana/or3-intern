package runners

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// openCodeStartupBuf is the maximum number of bytes captured from a managed
// opencode server's startup output (stdout + stderr). The buffer is used to
// surface diagnostic information when startup fails.
const (
	openCodeStartupBuf        = 32 * 1024
	openCodeHealthInterval    = 1 * time.Second
	openCodeShutdownTimeout   = 5 * time.Second
	openCodeIdleCheckInterval = 250 * time.Millisecond
)

// openCodeStartup captures the bounded startup stream of a managed opencode
// process. The captured output is redacted and used as a diagnostic source.
type openCodeStartup struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	max    int
	closed bool
}

func newOpenCodeStartup(max int) *openCodeStartup {
	if max <= 0 {
		max = openCodeStartupBuf
	}
	return &openCodeStartup{max: max}
}

func (s *openCodeStartup) Write(p []byte) (int, error) {
	if s == nil {
		return len(p), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return len(p), nil
	}
	remaining := s.max - s.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		s.buf.Write(p[:remaining])
		return len(p), nil
	}
	s.buf.Write(p)
	return len(p), nil
}

func (s *openCodeStartup) Snapshot() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return redactOpenCodeStartupOutput(s.buf.String())
}

func (s *openCodeStartup) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// openCodeReadyURL is a regular expression that captures the ready URL printed
// by `opencode serve` on startup. We treat stdout lines like
//
//	opencode server listening on http://127.0.0.1:43523
//
// as evidence that the process is ready, falling back to health polling.
var openCodeReadyURL = regexp.MustCompile(`https?://[0-9a-zA-Z\.\-:\[\]]+`)

func redactOpenCodeStartupOutput(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	s = redactEnvLike(s)
	// Trim noisy repeated lines.
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	seen := map[string]int{}
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if count := seen[line]; count < 3 {
			seen[line] = count + 1
			out = append(out, line)
		}
	}
	if len(out) > 80 {
		out = append(out[:80], "...")
	}
	return strings.Join(out, "\n")
}

func redactEnvLike(s string) string {
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GITHUB_TOKEN", "GOOGLE_API_KEY", "AUTHORIZATION", "AUTH_HEADER", "CODEX_HOME", "XDG_RUNTIME_DIR"} {
		idx := strings.Index(strings.ToUpper(s), key)
		if idx < 0 {
			continue
		}
		// Simple redaction: replace token-like substring after `=` or `:`.
		head := s[:idx]
		tail := s[idx:]
		if eq := strings.IndexAny(tail, "=:"); eq >= 0 {
			sep := tail[eq]
			tail = tail[:eq+1] + "[redacted]"
			s = head + tail
			_ = sep
		}
	}
	return s
}

// openCodeProcess models a managed opencode server process. It captures startup
// output, exposes the discovered ready URL, and tracks a kill-switch for the
// process group so the manager can stop the helper cleanly.
type openCodeProcess struct {
	cmd        *exec.Cmd
	readyURL   string
	discovered string
	startup    *openCodeStartup
	startedAt  time.Time
	stopped    atomic.Bool
}

func newOpenCodeProcess(cmd *exec.Cmd) *openCodeProcess {
	return &openCodeProcess{cmd: cmd, startup: newOpenCodeStartup(openCodeStartupBuf), startedAt: time.Now().UTC()}
}

// start launches the process and begins draining stdout/stderr. Returns an
// error if the process fails to start.
func (p *openCodeProcess) start(ctx context.Context) error {
	if p.cmd == nil {
		return fmt.Errorf("nil opencode command")
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := p.cmd.Start(); err != nil {
		return err
	}
	p.startedAt = time.Now().UTC()
	go p.drain(stdout, "stdout")
	go p.drain(stderr, "stderr")
	return nil
}

func (p *openCodeProcess) drain(reader io.Reader, source string) {
	if reader == nil {
		return
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 8*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		p.startup.Write([]byte(line + "\n"))
		if url := parseOpenCodeReadyURL(line); url != "" {
			p.markReady(url, line)
		}
		_ = source
	}
}

func (p *openCodeProcess) markReady(url, source string) {
	if url == "" {
		return
	}
	p.readyURL = url
	p.discovered = source
}

func (p *openCodeProcess) Snapshot() string {
	if p == nil {
		return ""
	}
	return p.startup.Snapshot()
}

func (p *openCodeProcess) Stop(ctx context.Context) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if !p.stopped.CompareAndSwap(false, true) {
		return nil
	}
	if p.startup != nil {
		p.startup.Close()
	}
	KillProcessGroup(p.cmd)
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(openCodeShutdownTimeout):
		return nil
	}
}

func parseOpenCodeReadyURL(line string) string {
	match := openCodeReadyURL.FindString(line)
	if match == "" {
		return ""
	}
	// Only accept loopback URLs to avoid accidentally connecting to a remote
	// address printed by an attacker-controlled process.
	if !strings.HasPrefix(match, "http://127.0.0.1") && !strings.HasPrefix(match, "http://localhost") {
		return ""
	}
	return strings.TrimRight(match, "/")
}

// openCodeLifecycle is the lifecycle manager for a single opencode server
// instance. It owns startup, health checks, idle detection, and shutdown.
type openCodeLifecycle struct {
	mu             sync.Mutex
	process        *openCodeProcess
	healthURL      string
	startedAt      time.Time
	lastActivity   atomic.Int64
	idleTimeout    time.Duration
	healthClient   *http.Client
	stopIdleOnce   sync.Once
	idleStopCh     chan struct{}
	idleStopReason string
	ownership      RunnerRuntimeOwnership
}

func newOpenCodeLifecycle(ownership RunnerRuntimeOwnership, idleTimeout time.Duration) *openCodeLifecycle {
	return &openCodeLifecycle{
		healthClient: &http.Client{Timeout: 2 * time.Second},
		idleTimeout:  idleTimeout,
		ownership:    ownership,
		idleStopCh:   make(chan struct{}),
	}
}

func (l *openCodeLifecycle) adoptProcess(proc *openCodeProcess, healthURL string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.process = proc
	l.healthURL = healthURL
	l.startedAt = time.Now().UTC()
	l.mu.Unlock()
	l.lastActivity.Store(time.Now().UnixMilli())
	if l.ownership == RuntimeOwnershipManaged && l.idleTimeout > 0 {
		go l.idleWatchdog()
	}
}

func (l *openCodeLifecycle) touch() {
	l.lastActivity.Store(time.Now().UnixMilli())
}

func (l *openCodeLifecycle) isExternal() bool {
	if l == nil {
		return false
	}
	return l.ownership == RuntimeOwnershipExternal
}

func (l *openCodeLifecycle) snapshot() (health RunnerNativeHealth, endpoint string, hasProc bool) {
	if l == nil {
		return RunnerNativeHealth{}, "", false
	}
	l.mu.Lock()
	healthURL := l.healthURL
	proc := l.process
	started := l.startedAt
	l.mu.Unlock()
	health = RunnerNativeHealth{
		Reachable:      false,
		Endpoint:       healthURL,
		StartedAt:      started.UnixMilli(),
		IdleTimeoutSec: int(l.idleTimeout / time.Second),
	}
	if l.idleTimeout > 0 {
		health.IdleTimeoutSec = int(l.idleTimeout / time.Second)
	}
	if proc == nil && healthURL == "" {
		return health, "", false
	}
	if healthURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		start := time.Now()
		err := httpJSON(ctx, l.healthClient, http.MethodGet, healthURL+"/global/health", nil, nil)
		health.LatencyMS = time.Since(start).Milliseconds()
		if err == nil {
			health.Reachable = true
		} else {
			health.Detail = err.Error()
		}
	}
	return health, healthURL, proc != nil
}

func (l *openCodeLifecycle) stopIdleWatchdog() {
	if l == nil {
		return
	}
	l.stopIdleOnce.Do(func() { close(l.idleStopCh) })
}

func (l *openCodeLifecycle) idleWatchdog() {
	if l == nil {
		return
	}
	ticker := time.NewTicker(openCodeIdleCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.idleStopCh:
			return
		case <-ticker.C:
			last := time.UnixMilli(l.lastActivity.Load())
			if time.Since(last) < l.idleTimeout {
				continue
			}
			l.stopIdleWatchdog()
			l.idleStopReason = "idle timeout"
			_ = l.Stop(context.Background())
			return
		}
	}
}

// Stop terminates the managed process. It is a no-op for external servers.
func (l *openCodeLifecycle) Stop(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if l.ownership == RuntimeOwnershipExternal {
		// External servers are not owned by us; never kill them.
		return nil
	}
	l.stopIdleWatchdog()
	l.mu.Lock()
	proc := l.process
	l.process = nil
	l.healthURL = ""
	l.mu.Unlock()
	if proc == nil {
		return nil
	}
	return proc.Stop(ctx)
}

// openCodeLifecycleSnapshot returns a JSON-friendly snapshot of the
// lifecycle state for use in payloads and logs.
func openCodeLifecycleSnapshot(l *openCodeLifecycle) map[string]any {
	if l == nil {
		return nil
	}
	health, endpoint, hasProc := l.snapshot()
	out := map[string]any{
		"endpoint":         endpoint,
		"reachable":        health.Reachable,
		"latency_ms":       health.LatencyMS,
		"started_at":       health.StartedAt,
		"has_process":      hasProc,
		"ownership":        string(l.ownership),
		"idle_seconds":     int(l.idleTimeout / time.Second),
		"last_activity_at": l.lastActivity.Load(),
	}
	if health.Detail != "" {
		out["detail"] = health.Detail
	}
	if proc := l.process; proc != nil {
		if snap := proc.Snapshot(); snap != "" {
			out["startup"] = snap
		}
	}
	return out
}

// openCodeStartOptions configures a managed opencode server start.
type openCodeStartOptions struct {
	Env           []string
	StartupBudget time.Duration
	PollInterval  time.Duration
}

// openCodeStartupResult captures the outcome of a managed opencode start.
type openCodeStartupResult struct {
	URL     string
	Process *openCodeProcess
	Health  RunnerNativeHealth
	// Output is the bounded startup log captured from the process.
	Output string
	// Error is the failure reason if startup did not succeed.
	Error string
}

// openCodeStart launches a managed opencode server and waits for it to be
// ready. It returns a structured result that callers can use to populate
// runtime metadata and event payloads.
func openCodeStart(ctx context.Context, binary string, opts openCodeStartOptions) openCodeStartupResult {
	if strings.TrimSpace(binary) == "" {
		return openCodeStartupResult{Error: "opencode binary is not installed"}
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return openCodeStartupResult{Error: "could not allocate loopback port: " + err.Error()}
	}
	host := "127.0.0.1"
	cmd := exec.CommandContext(context.Background(), binary, "serve", "--hostname", host, "--port", strconv.Itoa(port))
	cmd.Env = opts.Env
	applyProcessGroup(cmd)
	proc := newOpenCodeProcess(cmd)
	if err := proc.start(ctx); err != nil {
		return openCodeStartupResult{Error: "opencode serve failed to start: " + err.Error(), Output: proc.Snapshot()}
	}
	expected := fmt.Sprintf("http://%s:%d", host, port)
	budget := opts.StartupBudget
	if budget <= 0 {
		budget = 10 * time.Second
	}
	poll := opts.PollInterval
	if poll <= 0 {
		poll = 150 * time.Millisecond
	}
	deadline := time.Now().Add(budget)
	healthClient := &http.Client{Timeout: 2 * time.Second}
	bestErr := ""
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			_ = proc.Stop(context.Background())
			return openCodeStartupResult{Error: "opencode start canceled: " + ctx.Err().Error(), Output: proc.Snapshot(), Process: proc}
		case <-time.After(poll):
		}
		if url := proc.readyURL; url != "" {
			return openCodeStartupResult{URL: url, Process: proc, Output: proc.Snapshot(), Health: RunnerNativeHealth{Reachable: true, Endpoint: url, StartedAt: proc.startedAt.UnixMilli()}}
		}
		probeCtx, cancelProbe := context.WithTimeout(ctx, 1*time.Second)
		err := httpJSON(probeCtx, healthClient, http.MethodGet, expected+"/global/health", nil, nil)
		cancelProbe()
		if err == nil {
			return openCodeStartupResult{URL: expected, Process: proc, Output: proc.Snapshot(), Health: RunnerNativeHealth{Reachable: true, Endpoint: expected, StartedAt: proc.startedAt.UnixMilli()}}
		}
		bestErr = err.Error()
		// Process exited?
		if proc.cmd != nil && proc.cmd.ProcessState != nil && proc.cmd.ProcessState.Exited() {
			break
		}
	}
	_ = proc.Stop(context.Background())
	detail := bestErr
	if detail == "" {
		detail = "opencode server did not become healthy"
	}
	return openCodeStartupResult{Error: detail, Output: proc.Snapshot(), Process: proc}
}

// openCodeProviderInventory normalizes the /config/providers response into
// RunnerProviderInfo values for the app.
func openCodeProviderInventory(value any) []RunnerProviderInfo {
	providers := []RunnerProviderInfo{}
	if root, ok := value.(map[string]any); ok {
		if rawDefaults, ok := root["default"].(map[string]any); ok {
			for _, model := range rawDefaults {
				_ = model
			}
		}
		seen := map[string]bool{}
		if providersAny, ok := root["providers"].([]any); ok {
			for _, item := range providersAny {
				obj, ok := item.(map[string]any)
				if !ok {
					continue
				}
				id := firstNonEmpty(asString(obj["id"]), asString(obj["providerID"]))
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				providers = append(providers, RunnerProviderInfo{
					ID:          id,
					Name:        firstNonEmpty(asString(obj["name"]), openCodeProviderDisplayName(id)),
					Description: asString(obj["description"]),
					Source:      "server",
				})
			}
		}
		if len(providers) == 0 {
			if providersAny, ok := root["providers"].(map[string]any); ok {
				for id, item := range providersAny {
					id = strings.TrimSpace(id)
					if id == "" || seen[id] {
						continue
					}
					seen[id] = true
					info := RunnerProviderInfo{ID: id, Name: openCodeProviderDisplayName(id), Source: "server"}
					if obj, ok := item.(map[string]any); ok {
						info.Name = firstNonEmpty(asString(obj["name"]), info.Name)
						info.Description = asString(obj["description"])
					}
					providers = append(providers, info)
				}
			}
		}
	}
	return providers
}

// openCodeInventory aggregates model, provider, and agent info from a single
// /config endpoint response (or from CLI discovery as a fallback).
type openCodeInventory struct {
	Providers []RunnerProviderInfo
	Models    []RunnerModelInfo
	Agents    []RunnerAgentInfo
	Raw       json.RawMessage
}

// openCodeInventoryFromServer queries the server for the canonical inventory.
func openCodeInventoryFromServer(ctx context.Context, client *http.Client, endpoint string) (openCodeInventory, error) {
	inv := openCodeInventory{}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return inv, fmt.Errorf("empty endpoint")
	}
	var raw json.RawMessage
	if err := httpJSON(ctx, client, http.MethodGet, endpoint+"/config", nil, &raw); err != nil {
		return inv, err
	}
	inv.Raw = raw
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return inv, err
	}
	inv.Providers = openCodeProviderInventory(decoded)
	inv.Models = flattenModelInfo(decoded)
	inv.Agents = openCodeAgentInventory(decoded)
	return inv, nil
}

// openCodeAgentInventory extracts a list of agents from the /config response.
func openCodeAgentInventory(value any) []RunnerAgentInfo {
	agents := []RunnerAgentInfo{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if kind, _ := x["kind"].(string); strings.EqualFold(kind, "agent") {
				name := firstNonEmpty(asString(x["name"]), asString(x["id"]))
				if name != "" {
					agents = append(agents, RunnerAgentInfo{
						Name:        name,
						DisplayName: firstNonEmpty(asString(x["displayName"]), asString(x["display_name"]), name),
						Description: asString(x["description"]),
						Default:     boolField(x, "default"),
						BuiltIn:     boolField(x, "builtIn") || boolField(x, "built_in"),
						Mode:        firstNonEmpty(asString(x["mode"]), asString(x["type"])),
					})
				}
				return
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
	return agents
}

// detectOpenCodePermissionRequestRef inspects a raw event payload and
// returns the structured request ref (id, session id, summary) when the
// event corresponds to an opencode permission or question request.
func detectOpenCodePermissionRequestRef(raw RunnerRunEvent, sessionID string) (NativeRequestRef, bool) {
	if raw.Type != "structured" || len(raw.Payload) == 0 {
		return NativeRequestRef{}, false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw.Payload, &obj); err != nil {
		return NativeRequestRef{}, false
	}
	eventType := strings.ToLower(firstNonEmpty(asString(obj["type"]), asString(obj["method"])))
	if !strings.Contains(eventType, "permission") && !strings.Contains(eventType, "question") {
		return NativeRequestRef{}, false
	}
	permission, _ := obj["permission"].(map[string]any)
	request, _ := obj["request"].(map[string]any)
	params, _ := obj["params"].(map[string]any)
	id := firstNonEmpty(
		asString(obj["id"]),
		asString(obj["requestId"]),
		asString(obj["request_id"]),
		asString(permission["id"]),
		asString(permission["requestId"]),
		asString(permission["request_id"]),
		asString(request["id"]),
		asString(request["requestId"]),
		asString(request["request_id"]),
		asString(params["id"]),
		asString(params["requestId"]),
		asString(params["request_id"]),
	)
	if id == "" {
		return NativeRequestRef{}, false
	}
	summary := firstNonEmpty(
		asString(obj["message"]),
		asString(obj["summary"]),
		asString(permission["message"]),
		asString(permission["summary"]),
		asString(request["message"]),
		asString(params["message"]),
	)
	kind := NativeRequestApproval
	if strings.Contains(eventType, "question") {
		kind = NativeRequestQuestion
	}
	ref := NativeRequestRef{
		RunnerID:  RunnerOpenCode,
		Kind:      kind,
		RequestID: id,
		SessionID: firstNonEmpty(sessionID, asString(obj["sessionID"]), asString(obj["session_id"])),
		Method:    eventType,
		Summary:   summary,
		IssuedAt:  time.Now().UnixMilli(),
	}
	if rawBytes, err := json.Marshal(obj); err == nil {
		ref.RawPayload = rawBytes
	}
	return ref, true
}
