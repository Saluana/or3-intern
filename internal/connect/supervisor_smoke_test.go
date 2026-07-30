package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

const supervisorSmokeSecret = "or3-connect-supervisor-smoke-secret-2026"

type supervisorSmokeFixture struct {
	root             string
	serviceHome      string
	configPath       string
	stateDir         string
	workspaceDir     string
	allowedDir       string
	databaseDir      string
	databasePath     string
	artifactsDir     string
	codexPath        string
	openCodePath     string
	cloudflaredPath  string
	deniedWritePath  string
	listenAddress    string
	serviceURL       string
	serviceSpec      ServiceSpec
	supervisorLabel  string
	supervisorTarget string
}

type supervisorSmokeHandle struct {
	platform   string
	label      string
	target     string
	stateDir   string
	stopped    bool
	installed  bool
	started    bool
	supervisor string
}

// TestConnectSupervisorServiceSmoke is deliberately opt-in because it installs
// a short-lived system service. CI enables the Linux gate on a disposable
// GitHub-hosted VM. The macOS gate is reserved for a labeled, disposable,
// passwordless-sudo self-hosted runner because a PR must not install a system
// LaunchDaemon on an ordinary GitHub-hosted macOS machine.
func TestConnectSupervisorServiceSmoke(t *testing.T) {
	gate := ""
	switch runtime.GOOS {
	case "linux":
		gate = "OR3_CONNECT_SYSTEMD_SMOKE"
	case "darwin":
		gate = "OR3_CONNECT_LAUNCHD_SMOKE"
	default:
		t.Skipf("system supervisor smoke is unsupported on %s", runtime.GOOS)
	}
	if os.Getenv(gate) != "1" {
		t.Skipf("set %s=1 only on a disposable host with non-interactive system supervisor privileges", gate)
	}

	binary := strings.TrimSpace(os.Getenv("OR3_CONNECT_SMOKE_BINARY"))
	if binary == "" {
		t.Fatal("OR3_CONNECT_SMOKE_BINARY must point to the prebuilt or3-intern binary")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatalf("resolve OR3_CONNECT_SMOKE_BINARY: %v", err)
	}
	if info, statErr := os.Stat(binary); statErr != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("OR3_CONNECT_SMOKE_BINARY is not executable: path=%q err=%v", binary, statErr)
	}
	requireNonInteractiveSupervisorPrivileges(t)

	provider := newSupervisorSmokeProvider(t)
	fixture := newSupervisorSmokeFixture(t, binary, provider.URL+"/v1")
	handle := startSupervisorSmokeService(t, fixture)

	client := &http.Client{Timeout: 5 * time.Second}
	runnerPayload, err := waitForSupervisorService(client, fixture.serviceURL, supervisorSmokeSecret, 35*time.Second)
	if err != nil {
		t.Fatalf("supervised service did not become ready: %v\n%s", err, handle.diagnostics())
	}
	assertSupervisorRunners(t, runnerPayload, fixture)

	const persistedSessionKey = "supervisor-smoke:persisted"
	mustSupervisorJSON(t, client, fixture.serviceURL, supervisorSmokeSecret, http.MethodPost, "/internal/v1/chat-sessions", map[string]any{
		"session_key":  persistedSessionKey,
		"title":        "Supervisor SQLite smoke",
		"runner_id":    "codex",
		"runner_label": "Codex",
	}, http.StatusCreated)

	mustSupervisorJSON(t, client, fixture.serviceURL, supervisorSmokeSecret, http.MethodPost, "/internal/v1/files/mkdir", map[string]any{
		"root_id": "workspace",
		"path":    ".",
		"name":    "supervisor-service-write",
	}, http.StatusCreated)
	mustSupervisorJSON(t, client, fixture.serviceURL, supervisorSmokeSecret, http.MethodPut, "/internal/v1/files/write", map[string]any{
		"root_id": "workspace",
		"path":    "supervisor-service-write/service-write.txt",
		"content": "service file write ok\n",
		"create":  true,
	}, http.StatusCreated)
	mustSupervisorMultipart(t, client, fixture.serviceURL, supervisorSmokeSecret, "/internal/v1/files/upload", map[string]string{
		"root_id": "workspace",
		"path":    "supervisor-service-write",
	}, "file", "service-upload.txt", []byte("service upload ok\n"), http.StatusCreated)

	artifactPayload := mustSupervisorMultipart(t, client, fixture.serviceURL, supervisorSmokeSecret, "/internal/v1/artifacts", map[string]string{
		"session_key": persistedSessionKey,
	}, "file", "artifact-smoke.txt", []byte("artifact write ok\n"), http.StatusCreated)
	artifactID := supervisorString(artifactPayload, "artifact_id")
	if artifactID == "" {
		t.Fatalf("artifact upload did not return artifact_id: %#v", artifactPayload)
	}
	artifactRead := mustSupervisorJSON(t, client, fixture.serviceURL, supervisorSmokeSecret, http.MethodGet,
		"/internal/v1/artifacts/"+url.PathEscape(artifactID)+"?session_key="+url.QueryEscape(persistedSessionKey), nil, http.StatusOK)
	if got := supervisorString(artifactRead, "content"); got != "artifact write ok\n" {
		t.Fatalf("artifact read content = %q", got)
	}

	runSupervisorTurn(t, client, fixture, "codex", "codex supervisor smoke ok")
	runSupervisorTurn(t, client, fixture, "opencode", "opencode supervisor smoke ok")
	assertRunnerEnvironmentMarker(t, filepath.Join(fixture.workspaceDir, "supervisor-codex.txt"), "codex", fixture)
	assertRunnerEnvironmentMarker(t, filepath.Join(fixture.workspaceDir, "supervisor-opencode.txt"), "opencode", fixture)

	if runtime.GOOS == "linux" {
		if _, statErr := os.Stat(fixture.deniedWritePath); !os.IsNotExist(statErr) {
			t.Fatalf("ProtectSystem=strict allowed a write outside ReadWritePaths: path=%q err=%v", fixture.deniedWritePath, statErr)
		}
	}

	handle.stop(t)

	persisted, err := db.Open(fixture.databasePath)
	if err != nil {
		t.Fatalf("open supervisor-written SQLite database: %v", err)
	}
	defer persisted.Close()
	meta, err := persisted.GetChatSessionMeta(context.Background(), persistedSessionKey)
	if err != nil {
		t.Fatalf("read supervisor-written chat session from SQLite: %v", err)
	}
	if meta.Title != "Supervisor SQLite smoke" || meta.RunnerID != "codex" {
		t.Fatalf("unexpected supervisor-written SQLite row: %#v", meta)
	}
	if body, readErr := os.ReadFile(filepath.Join(fixture.artifactsDir, artifactID)); readErr != nil || string(body) != "artifact write ok\n" {
		t.Fatalf("supervisor artifact file mismatch: body=%q err=%v", body, readErr)
	}
	for path, want := range map[string]string{
		filepath.Join(fixture.workspaceDir, "supervisor-service-write", "service-write.txt"):  "service file write ok\n",
		filepath.Join(fixture.workspaceDir, "supervisor-service-write", "service-upload.txt"): "service upload ok\n",
	} {
		body, readErr := os.ReadFile(path)
		if readErr != nil || string(body) != want {
			t.Fatalf("supervisor workspace write mismatch: path=%q body=%q err=%v", path, body, readErr)
		}
	}
}

func newSupervisorSmokeProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":  []map[string]any{{"index": 0, "embedding": []float64{0, 0, 0}}},
				"usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1},
			})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "provider smoke ok"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newSupervisorSmokeFixture(t *testing.T, binary, providerURL string) supervisorSmokeFixture {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatalf("resolve current user: %v", err)
	}
	root, err := os.MkdirTemp(current.HomeDir, ".or3-connect-supervisor-smoke-")
	if err != nil {
		t.Fatalf("create supervisor smoke fixture: %v", err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(root); removeErr != nil {
			t.Logf("remove supervisor smoke fixture %q: %v", root, removeErr)
		}
	})

	fixture := supervisorSmokeFixture{
		root:            root,
		serviceHome:     filepath.Join(root, "home"),
		configPath:      filepath.Join(root, "config", "config.json"),
		stateDir:        filepath.Join(root, "state"),
		workspaceDir:    filepath.Join(root, "workspace"),
		allowedDir:      filepath.Join(root, "allowed"),
		databaseDir:     filepath.Join(root, "database"),
		artifactsDir:    filepath.Join(root, "artifacts"),
		cloudflaredPath: filepath.Join(root, "bin", "cloudflared"),
		deniedWritePath: filepath.Join(root, "must-remain-read-only.txt"),
	}
	fixture.databasePath = filepath.Join(fixture.databaseDir, "or3-intern.sqlite")
	codexDir := filepath.Join(fixture.serviceHome, ".local", "bin")
	openCodeDir := filepath.Join(fixture.serviceHome, ".npm-global", "bin")
	fixture.codexPath = filepath.Join(codexDir, "codex")
	fixture.openCodePath = filepath.Join(openCodeDir, "opencode")

	for _, dir := range []string{
		filepath.Dir(fixture.configPath),
		fixture.stateDir,
		fixture.workspaceDir,
		fixture.allowedDir,
		fixture.databaseDir,
		fixture.artifactsDir,
		filepath.Dir(fixture.cloudflaredPath),
		codexDir,
		openCodeDir,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create supervisor smoke directory %q: %v", dir, err)
		}
	}
	for _, path := range []string{
		filepath.Join(fixture.stateDir, "connect.log"),
		filepath.Join(fixture.stateDir, "connect-error.log"),
	} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("create supervisor smoke log %q: %v", path, err)
		}
	}

	writeSupervisorSmokeExecutable(t, fixture.cloudflaredPath, `#!/bin/sh
trap 'exit 0' TERM INT
while :; do
  /bin/sleep 1 &
  wait $!
done
`)
	writeSupervisorSmokeExecutable(t, fixture.codexPath, supervisorRunnerScript("codex", fixture.deniedWritePath, `{"type":"thread.started","thread_id":"supervisor-codex"}
{"type":"item.completed","item":{"id":"message-1","type":"agent_message","text":"codex supervisor smoke ok"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`))
	writeSupervisorSmokeExecutable(t, fixture.openCodePath, supervisorRunnerScript("opencode", fixture.deniedWritePath,
		`{"type":"text","part":{"type":"text","text":"opencode supervisor smoke ok"}}`))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve service port: %v", err)
	}
	fixture.listenAddress = listener.Addr().String()
	_ = listener.Close()
	fixture.serviceURL = "http://" + fixture.listenAddress

	cfg := config.Default()
	cfg.DBPath = fixture.databasePath
	cfg.ArtifactsDir = fixture.artifactsDir
	cfg.WorkspaceDir = fixture.workspaceDir
	cfg.AllowedDir = fixture.allowedDir
	cfg.SoulFile = filepath.Join(fixture.stateDir, "SOUL.md")
	cfg.AgentsFile = filepath.Join(fixture.stateDir, "AGENTS.md")
	cfg.ToolsFile = filepath.Join(fixture.stateDir, "TOOLS.md")
	cfg.IdentityFile = filepath.Join(fixture.stateDir, "IDENTITY.md")
	cfg.MemoryFile = filepath.Join(fixture.stateDir, "MEMORY.md")
	cfg.ConsolidationEnabled = false
	cfg.Provider.APIBase = providerURL
	cfg.Provider.APIKey = "provider-smoke-key"
	cfg.Provider.EmbedDimensions = 3
	openAI := cfg.Providers["openai"]
	openAI.APIBase = providerURL
	openAI.APIKey = "provider-smoke-key"
	openAI.DefaultDimensions = 3
	cfg.Providers["openai"] = openAI
	cfg.Runners.RuntimeMode = map[string]string{"codex": "cli", "opencode": "cli"}
	cfg.Runners.ChildEnvAllowlist = []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"}
	cfg.Cron.Enabled = false
	cfg.Cron.StorePath = filepath.Join(fixture.stateDir, "cron.db")
	cfg.Heartbeat.Enabled = false
	cfg.Heartbeat.TasksFile = filepath.Join(fixture.stateDir, "HEARTBEAT.md")
	cfg.Security.SecretStore.KeyFile = filepath.Join(fixture.stateDir, "master.key")
	cfg.Security.Audit.KeyFile = filepath.Join(fixture.stateDir, "audit.key")
	cfg.Security.Approvals.KeyFile = filepath.Join(fixture.stateDir, "approvals.key")
	cfg.Skills.ManagedDir = filepath.Join(fixture.stateDir, "skills")
	cfg.Skills.Load.GlobalDir = filepath.Join(fixture.serviceHome, ".agents", "skills")
	cfg.Service.Enabled = true
	cfg.Service.Listen = fixture.listenAddress
	cfg.Service.Secret = supervisorSmokeSecret
	cfg.Service.SharedSecretRole = "operator"
	cfg.Service.MutationRateLimitPerMinute = 120
	if err := config.Save(fixture.configPath, cfg); err != nil {
		t.Fatalf("save supervisor smoke config: %v", err)
	}
	if err := SaveState(fixture.stateDir, State{
		Version:         StateVersion,
		ConfigPath:      fixture.configPath,
		CloudflaredPath: fixture.cloudflaredPath,
		EnvironmentName: "Supervisor smoke",
		Installed:       true,
		Stage:           "online",
		ConnectedAt:     time.Now().UTC(),
	}, TunnelCredential{Token: "supervisor-smoke-tunnel-token"}); err != nil {
		t.Fatalf("save supervisor smoke Connect state: %v", err)
	}

	group := current.Gid
	if resolved, lookupErr := user.LookupGroupId(current.Gid); lookupErr == nil {
		group = resolved.Name
	}
	writablePaths, err := serviceWritablePaths(fixture.stateDir, cfg)
	if err != nil {
		t.Fatalf("resolve supervisor writable paths: %v", err)
	}
	unique := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	if runtime.GOOS == "darwin" {
		fixture.supervisorLabel = "chat.or3.connect.smoke." + strings.ReplaceAll(unique, "-", ".")
		fixture.supervisorTarget = filepath.Join("/Library/LaunchDaemons", fixture.supervisorLabel+".plist")
	} else {
		fixture.supervisorLabel = "or3-connect-smoke-" + unique
		fixture.supervisorTarget = filepath.Join("/etc/systemd/system", fixture.supervisorLabel+".service")
	}
	fixture.serviceSpec = ServiceSpec{
		Label:      fixture.supervisorLabel,
		User:       current.Username,
		Group:      group,
		WorkingDir: filepath.Dir(fixture.configPath),
		Binary:     binary,
		ConfigPath: fixture.configPath,
		StateDir:   fixture.stateDir,
		StdoutPath: filepath.Join(fixture.stateDir, "connect.log"),
		StderrPath: filepath.Join(fixture.stateDir, "connect-error.log"),
		Path: serviceExecutablePath(strings.Join([]string{
			codexDir,
			openCodeDir,
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/usr/sbin",
			"/sbin",
		}, string(os.PathListSeparator)), fixture.serviceHome),
		Home:          fixture.serviceHome,
		TempDir:       "/tmp",
		WritablePaths: writablePaths,
	}
	return fixture
}

func supervisorRunnerScript(runnerID, deniedPath, output string) string {
	versionProbe := `[ "$1" = "--version" ]`
	versionText := "OpenCode supervisor smoke"
	authProbe := `[ "$1" = "auth" ] && [ "$2" = "list" ]`
	if runnerID == "codex" {
		versionProbe = `[ "$1" = "--help" ]`
		versionText = "Codex supervisor smoke"
		authProbe = `[ "$1" = "login" ] && [ "$2" = "status" ]`
	}
	return fmt.Sprintf(`#!/bin/sh
if %s; then
  printf '%%s\n' '%s'
  exit 0
fi
if %s; then
  exit 0
fi
denied=blocked
if printf 'sandbox escape\n' 2>/dev/null > %s; then
  denied=write-succeeded
fi
tmp_write=failed
tmp_probe="$TMPDIR/or3-connect-supervisor-smoke-$$"
if printf 'private tmp ok\n' 2>/dev/null > "$tmp_probe"; then
  tmp_write=ok
  rm -f "$tmp_probe"
fi
{
  printf 'RUNNER=%%s\n' '%s'
  printf 'PATH=%%s\n' "$PATH"
  printf 'HOME=%%s\n' "$HOME"
  printf 'TMPDIR=%%s\n' "$TMPDIR"
  printf 'PWD=%%s\n' "$PWD"
  printf 'OR3_SERVICE_SECRET=%%s\n' "${OR3_SERVICE_SECRET-unset}"
  printf 'DENIED=%%s\n' "$denied"
  printf 'TMPDIR_WRITE=%%s\n' "$tmp_write"
} > "$PWD/supervisor-%s.txt"
printf '%%s\n' '%s'
`, versionProbe, versionText, authProbe, shellQuote(deniedPath), runnerID, runnerID, output)
}

func writeSupervisorSmokeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write supervisor smoke executable %q: %v", path, err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func requireNonInteractiveSupervisorPrivileges(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		if out, err := exec.Command("sudo", "-n", "true").CombinedOutput(); err != nil {
			t.Fatalf("supervisor smoke requires non-interactive sudo: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	switch runtime.GOOS {
	case "linux":
		if out, err := exec.Command("systemctl", "list-units", "--no-pager", "--no-legend").CombinedOutput(); err != nil {
			t.Fatalf("systemd is not available: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	case "darwin":
		if out, err := exec.Command("launchctl", "print", "system").CombinedOutput(); err != nil {
			t.Fatalf("system launchd domain is not available: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
}

func startSupervisorSmokeService(t *testing.T, fixture supervisorSmokeFixture) *supervisorSmokeHandle {
	t.Helper()
	body, err := RenderService(fixture.serviceSpec, runtime.GOOS)
	if err != nil {
		t.Fatalf("render supervisor smoke service: %v", err)
	}
	source := filepath.Join(fixture.root, "generated-service")
	if runtime.GOOS == "darwin" {
		source += ".plist"
	} else {
		source += ".service"
	}
	if err := os.WriteFile(source, []byte(body), 0o644); err != nil {
		t.Fatalf("write rendered supervisor service: %v", err)
	}
	handle := &supervisorSmokeHandle{
		platform:   runtime.GOOS,
		label:      fixture.supervisorLabel,
		target:     fixture.supervisorTarget,
		stateDir:   fixture.stateDir,
		supervisor: fixture.supervisorLabel,
	}
	if runtime.GOOS == "linux" {
		handle.supervisor += ".service"
	}
	t.Cleanup(func() { handle.stop(t) })

	group := "root"
	if runtime.GOOS == "darwin" {
		group = "wheel"
	}
	// The target is unique to this test, so cleanup may safely remove it even
	// when install reports an ambiguous partial failure.
	handle.installed = true
	if out, err := privilegedSupervisorOutput("install", "-o", "root", "-g", group, "-m", "0644", source, handle.target); err != nil {
		t.Fatalf("install supervisor smoke service: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	switch runtime.GOOS {
	case "linux":
		mustPrivilegedSupervisorCommand(t, "systemctl", "daemon-reload")
		mustPrivilegedSupervisorCommand(t, "systemctl", "start", handle.supervisor)
		handle.started = true
		show := mustPrivilegedSupervisorCommand(t, "systemctl", "show", handle.supervisor,
			"--property=ProtectSystem", "--property=PrivateTmp", "--property=NoNewPrivileges", "--property=ReadWritePaths")
		for _, expected := range []string{"ProtectSystem=strict", "PrivateTmp=yes", "NoNewPrivileges=yes"} {
			if !strings.Contains(show, expected) {
				t.Fatalf("actual systemd unit is missing %q:\n%s", expected, show)
			}
		}
		for _, writable := range fixture.serviceSpec.WritablePaths {
			if !strings.Contains(show, writable) {
				t.Fatalf("actual systemd unit omitted writable path %q:\n%s", writable, show)
			}
		}
	case "darwin":
		mustPrivilegedSupervisorCommand(t, "launchctl", "bootstrap", "system", handle.target)
		handle.started = true
	}
	return handle
}

func (h *supervisorSmokeHandle) stop(t *testing.T) {
	t.Helper()
	if h == nil || h.stopped {
		return
	}
	h.stopped = true
	switch h.platform {
	case "linux":
		if out, err := privilegedSupervisorOutput("systemctl", "stop", h.supervisor); err != nil && h.started {
			t.Errorf("stop systemd smoke service: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		_, _ = privilegedSupervisorOutput("systemctl", "reset-failed", h.supervisor)
	case "darwin":
		if out, err := privilegedSupervisorOutput("launchctl", "bootout", "system/"+h.label); err != nil && h.started {
			t.Errorf("boot out launchd smoke service: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	if h.installed {
		if out, err := privilegedSupervisorOutput("rm", "-f", h.target); err != nil {
			t.Errorf("remove supervisor smoke definition: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	if h.platform == "linux" {
		if out, err := privilegedSupervisorOutput("systemctl", "daemon-reload"); err != nil && h.installed {
			t.Errorf("reload systemd after smoke cleanup: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
}

func (h *supervisorSmokeHandle) diagnostics() string {
	if h == nil {
		return "supervisor diagnostics unavailable"
	}
	var sections []string
	if h.platform == "linux" {
		if out, _ := privilegedSupervisorOutput("systemctl", "status", "--no-pager", "--full", h.supervisor); len(out) > 0 {
			sections = append(sections, "systemctl status:\n"+string(out))
		}
		if out, _ := privilegedSupervisorOutput("journalctl", "-u", h.supervisor, "--no-pager", "-n", "100"); len(out) > 0 {
			sections = append(sections, "journal:\n"+string(out))
		}
	} else {
		if out, _ := privilegedSupervisorOutput("launchctl", "print", "system/"+h.label); len(out) > 0 {
			sections = append(sections, "launchctl print:\n"+string(out))
		}
	}
	for _, name := range []string{"connect.log", "connect-error.log"} {
		if body, err := os.ReadFile(filepath.Join(h.stateDir, name)); err == nil && len(body) > 0 {
			sections = append(sections, name+":\n"+string(body))
		}
	}
	if len(sections) == 0 {
		return "supervisor produced no diagnostics"
	}
	return strings.Join(sections, "\n")
}

func privilegedSupervisorOutput(name string, args ...string) ([]byte, error) {
	if os.Geteuid() == 0 {
		return exec.Command(name, args...).CombinedOutput()
	}
	sudoArgs := append([]string{"-n", name}, args...)
	return exec.Command("sudo", sudoArgs...).CombinedOutput()
}

func mustPrivilegedSupervisorCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := privilegedSupervisorOutput(name, args...)
	if err != nil {
		t.Fatalf("%s %s: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func waitForSupervisorService(client *http.Client, baseURL, secret string, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		payload, status, err := supervisorJSON(client, baseURL, secret, http.MethodGet, "/internal/v1/chat-runners", nil)
		if err == nil && status == http.StatusOK {
			return payload, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("runner discovery returned HTTP %d: %#v", status, payload)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, lastErr
}

func assertSupervisorRunners(t *testing.T, payload map[string]any, fixture supervisorSmokeFixture) {
	t.Helper()
	raw, ok := payload["runners"].([]any)
	if !ok {
		t.Fatalf("runner discovery returned an invalid payload: %#v", payload)
	}
	expectedPaths := map[string]string{"codex": fixture.codexPath, "opencode": fixture.openCodePath}
	found := map[string]bool{}
	for _, item := range raw {
		runner, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := supervisorString(runner, "id")
		expectedPath, wanted := expectedPaths[id]
		if !wanted {
			continue
		}
		found[id] = true
		if status := supervisorString(runner, "status"); status != "available" {
			t.Fatalf("%s runner status = %q: %#v", id, status, runner)
		}
		if binaryPath := supervisorString(runner, "binary_path"); binaryPath != expectedPath {
			t.Fatalf("%s runner path = %q, want %q", id, binaryPath, expectedPath)
		}
	}
	for id := range expectedPaths {
		if !found[id] {
			t.Fatalf("supervised runner discovery omitted %s: %#v", id, payload)
		}
	}
}

func runSupervisorTurn(t *testing.T, client *http.Client, fixture supervisorSmokeFixture, runnerID, expectedText string) {
	t.Helper()
	session := mustSupervisorJSON(t, client, fixture.serviceURL, supervisorSmokeSecret, http.MethodPost, "/internal/v1/runner-chat/sessions", map[string]any{
		"app_session_key":    "supervisor-smoke:" + runnerID,
		"runner_id":          runnerID,
		"continuation_mode":  "replay",
		"mode":               "safe_edit",
		"isolation":          "host_workspace_write",
		"cwd":                fixture.workspaceDir,
		"approval_autopilot": false,
	}, http.StatusCreated)
	sessionID := supervisorString(session, "id")
	if sessionID == "" {
		t.Fatalf("%s runner session missing id: %#v", runnerID, session)
	}
	started := mustSupervisorJSON(t, client, fixture.serviceURL, supervisorSmokeSecret, http.MethodPost,
		"/internal/v1/runner-chat/sessions/"+url.PathEscape(sessionID)+"/turns", map[string]any{
			"user_message":      "write the supervisor smoke marker",
			"continuation_mode": "replay",
			"mode":              "safe_edit",
			"isolation":         "host_workspace_write",
			"cwd":               fixture.workspaceDir,
			"timeout_seconds":   20,
		}, http.StatusAccepted)
	turnID := supervisorString(started, "turn_id")
	if turnID == "" {
		t.Fatalf("%s runner turn missing id: %#v", runnerID, started)
	}

	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		turn := mustSupervisorJSON(t, client, fixture.serviceURL, supervisorSmokeSecret, http.MethodGet,
			"/internal/v1/runner-chat/sessions/"+url.PathEscape(sessionID)+"/turns/"+url.PathEscape(turnID), nil, http.StatusOK)
		switch status := supervisorString(turn, "status"); status {
		case "succeeded":
			if finalText := supervisorString(turn, "final_text"); !strings.Contains(finalText, expectedText) {
				t.Fatalf("%s final text = %q, want it to contain %q", runnerID, finalText, expectedText)
			}
			return
		case "failed", "aborted", "timed_out":
			t.Fatalf("%s supervised turn ended with %s: %#v", runnerID, status, turn)
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("%s supervised turn did not finish", runnerID)
}

func assertRunnerEnvironmentMarker(t *testing.T, path, runnerID string, fixture supervisorSmokeFixture) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s supervised runner marker: %v", runnerID, err)
	}
	text := string(body)
	for _, expected := range []string{
		"RUNNER=" + runnerID,
		"HOME=" + fixture.serviceHome,
		"TMPDIR=/tmp",
		"PWD=" + fixture.workspaceDir,
		"OR3_SERVICE_SECRET=unset",
		"TMPDIR_WRITE=ok",
	} {
		if !strings.Contains(text, expected+"\n") {
			t.Fatalf("%s marker omitted %q:\n%s", runnerID, expected, text)
		}
	}
	if !strings.Contains(text, filepath.Dir(fixture.codexPath)) ||
		!strings.Contains(text, filepath.Dir(fixture.openCodePath)) {
		t.Fatalf("%s marker PATH omitted controlled runner locations:\n%s", runnerID, text)
	}
	if runtime.GOOS == "linux" && !strings.Contains(text, "DENIED=blocked\n") {
		t.Fatalf("%s escaped ProtectSystem=strict:\n%s", runnerID, text)
	}
}

func mustSupervisorJSON(t *testing.T, client *http.Client, baseURL, secret, method, path string, body any, wantStatus int) map[string]any {
	t.Helper()
	payload, status, err := supervisorJSON(client, baseURL, secret, method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if status != wantStatus {
		t.Fatalf("%s %s returned HTTP %d, want %d: %#v", method, path, status, wantStatus, payload)
	}
	return payload
}

func supervisorJSON(client *http.Client, baseURL, secret, method, path string, body any) (map[string]any, int, error) {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		requestBody = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, baseURL+path, requestBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, response.StatusCode, err
	}
	payload := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, response.StatusCode, fmt.Errorf("decode response: %w (body=%q)", err, raw)
		}
	}
	return payload, response.StatusCode, nil
}

func mustSupervisorMultipart(t *testing.T, client *http.Client, baseURL, secret, path string, fields map[string]string, fileField, filename string, content []byte, wantStatus int) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write multipart field %s: %v", key, err)
		}
	}
	part, err := writer.CreateFormFile(fileField, filename)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, &body)
	if err != nil {
		t.Fatalf("create multipart request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read POST %s response: %v", path, err)
	}
	payload := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode POST %s response: %v (body=%q)", path, err, raw)
		}
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("POST %s returned HTTP %d, want %d: %#v", path, response.StatusCode, wantStatus, payload)
	}
	return payload
}

func supervisorString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
