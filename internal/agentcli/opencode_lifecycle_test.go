package agentcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
)

func jsonUnmarshalForTest(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

func TestOpenCodeStartupBufferRedactsSensitiveValues(t *testing.T) {
	buf := newOpenCodeStartup(openCodeStartupBuf)
	buf.Write([]byte("OPENAI_API_KEY=sk-12345 secret=abc\n"))
	buf.Write([]byte("normal log line 1\n"))
	buf.Write([]byte("normal log line 2\n"))
	snap := buf.Snapshot()
	if strings.Contains(snap, "sk-12345") {
		t.Fatalf("expected redacted snapshot, got: %s", snap)
	}
	if !strings.Contains(snap, "OPENAI_API_KEY") {
		t.Fatalf("expected key name to remain visible, got: %s", snap)
	}
}

func TestOpenCodeStartupBufferTrimsRepeatedLines(t *testing.T) {
	buf := newOpenCodeStartup(openCodeStartupBuf)
	for i := 0; i < 20; i++ {
		buf.Write([]byte("same line\n"))
	}
	snap := buf.Snapshot()
	if strings.Count(snap, "same line") > 3 {
		t.Fatalf("expected repeated lines to be bounded, got: %s", snap)
	}
}

func TestParseOpenCodeReadyURLDetectsLoopback(t *testing.T) {
	if got := parseOpenCodeReadyURL("opencode server listening on http://127.0.0.1:43523"); got != "http://127.0.0.1:43523" {
		t.Fatalf("expected loopback URL, got %q", got)
	}
	if got := parseOpenCodeReadyURL("opencode server listening on http://localhost:43523"); got != "http://localhost:43523" {
		t.Fatalf("expected localhost URL, got %q", got)
	}
	if got := parseOpenCodeReadyURL("opencode server listening on http://attacker.example.com:80"); got != "" {
		t.Fatalf("expected rejection of non-loopback URL, got %q", got)
	}
	if got := parseOpenCodeReadyURL("nothing useful here"); got != "" {
		t.Fatalf("expected empty for non-URL line, got %q", got)
	}
}

func TestOpenCodeLifecycleStopIsNoOpForExternalServers(t *testing.T) {
	lc := newOpenCodeLifecycle(RuntimeOwnershipExternal, time.Minute)
	if err := lc.Stop(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !lc.isExternal() {
		t.Fatal("expected external ownership")
	}
}

func TestOpenCodeLifecycleTouchAndSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	lc := newOpenCodeLifecycle(RuntimeOwnershipManaged, time.Hour)
	lc.adoptProcess(newOpenCodeProcess(exec.Command("true")), server.URL)
	health, endpoint, hasProc := lc.snapshot()
	if !hasProc {
		t.Fatal("expected hasProc")
	}
	if endpoint != server.URL {
		t.Fatalf("endpoint = %q, want %q", endpoint, server.URL)
	}
	if !health.Reachable {
		t.Fatalf("expected reachable health, got %+v", health)
	}
}

func TestOpenCodeLifecycleIdleTimeoutStopsManaged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping process group test on windows")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skipf("sleep not available: %v", err)
	}
	lc := newOpenCodeLifecycle(RuntimeOwnershipManaged, 25*time.Millisecond)
	cmd := exec.Command("sleep", "30")
	applyProcessGroup(cmd)
	proc := newOpenCodeProcess(cmd)
	if err := proc.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if proc.cmd.Process == nil {
		t.Fatal("process not started")
	}
	t.Logf("process pid=%d", proc.cmd.Process.Pid)
	lc.adoptProcess(proc, "http://127.0.0.1:1")
	// Trigger Stop directly to verify the lifecycle's kill path works.
	doneCh := make(chan error, 1)
	go func() { doneCh <- lc.Stop(context.Background()) }()
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("lc.Stop: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("lc.Stop did not return")
	}
	if proc.cmd.ProcessState == nil {
		t.Fatalf("expected process state, got nil")
	}
	// The lifecycle sent SIGTERM via process group; the process should be
	// either exited cleanly or terminated. We just check that the wait
	// status reports termination.
	if proc.cmd.ProcessState.Exited() {
		return
	}
	if !strings.Contains(proc.cmd.ProcessState.String(), "terminat") && !strings.Contains(proc.cmd.ProcessState.String(), "killed") {
		t.Fatalf("expected process to be terminated, state=%+v", proc.cmd.ProcessState)
	}
}

func TestOpenCodeProviderInventoryNormalizes(t *testing.T) {
	var raw any
	if err := jsonUnmarshalForTest(`{
		"providers":[
			{"id":"openai","name":"OpenAI","description":"Primary"},
			{"id":"anthropic","name":"Anthropic"},
			{"id":"openai"}
		]
	}`, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	inv := openCodeProviderInventory(raw)
	if len(inv) != 2 {
		t.Fatalf("expected 2 providers, got %d (%+v)", len(inv), inv)
	}
	if inv[0].ID != "openai" || inv[0].Name != "OpenAI" {
		t.Fatalf("unexpected first provider: %+v", inv[0])
	}
}

func TestOpenCodeAgentInventoryExtractsAgents(t *testing.T) {
	var raw any
	if err := jsonUnmarshalForTest(`{
		"agents":[
			{"name":"build","displayName":"Build","description":"default build agent","kind":"agent","default":true},
			{"name":"plan","kind":"agent","builtIn":true}
		]
	}`, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	agents := openCodeAgentInventory(raw)
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d (%+v)", len(agents), agents)
	}
	if agents[0].Name != "build" || !agents[0].Default {
		t.Fatalf("unexpected first agent: %+v", agents[0])
	}
	if !agents[1].BuiltIn {
		t.Fatalf("expected second agent built_in: %+v", agents[1])
	}
}

func TestOpenCodeInfoExternalServerReportsHealthAndNextAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/config":
			_, _ = w.Write([]byte(`{
				"providers":[{"id":"openai","name":"OpenAI"}],
				"agents":[{"name":"build","displayName":"Build","kind":"agent"}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	info := runtime.Info(context.Background(), config.AgentCLIConfig{
		RuntimeMode:      map[string]string{"opencode": "auto"},
		NativeServerURLs: map[string]string{"opencode": server.URL},
	}, []string{"PATH="})
	if info.State != RuntimeStateReady {
		t.Fatalf("expected ready, got %+v", info)
	}
	if info.Ownership != RuntimeOwnershipExternal {
		t.Fatalf("expected external ownership, got %+v", info)
	}
	if info.NextAction == "" {
		t.Fatal("expected next action for external server")
	}
	if info.Health == nil || !info.Health.Reachable {
		t.Fatalf("expected health, got %+v", info.Health)
	}
	if len(info.Providers) != 1 || info.Providers[0].ID != "openai" {
		t.Fatalf("expected providers, got %+v", info.Providers)
	}
	if len(info.Agents) != 1 || info.Agents[0].Name != "build" {
		t.Fatalf("expected agents, got %+v", info.Agents)
	}
}

func TestOpenCodeInfoBinaryMissingSetsNextAction(t *testing.T) {
	runtime := NewOpenCodeNativeRuntime()
	env := []string{"PATH=" + os.TempDir()}
	info := runtime.Info(context.Background(), config.AgentCLIConfig{
		RuntimeMode: map[string]string{"opencode": "auto"},
	}, env)
	if info.State != RuntimeStateUnavailable {
		t.Fatalf("expected unavailable, got %+v", info)
	}
	if info.NextAction == "" {
		t.Fatal("expected next action hint")
	}
}

func TestOpenCodeInfoExternalServerUnreachableReportsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	runtime := NewOpenCodeNativeRuntime()
	info := runtime.Info(context.Background(), config.AgentCLIConfig{
		RuntimeMode:      map[string]string{"opencode": "auto"},
		NativeServerURLs: map[string]string{"opencode": srv.URL},
	}, []string{"PATH="})
	if info.State != RuntimeStateError {
		t.Fatalf("expected error, got %+v", info)
	}
	if info.FallbackReason == "" {
		t.Fatal("expected fallback reason")
	}
	if info.NextAction == "" {
		t.Fatal("expected next action hint")
	}
}
