package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

type fakeChatAdapter struct{}

func (fakeChatAdapter) ID() RunnerID                                      { return RunnerOpenCode }
func (fakeChatAdapter) DisplayName() string                               { return "Fake" }
func (fakeChatAdapter) Spec() RunnerSpec                                  { return RunnerSpec{} }
func (fakeChatAdapter) Detect(context.Context, DetectOptions) RunnerInfo  { return RunnerInfo{} }
func (fakeChatAdapter) BuildCommand(AgentRunRequest) (CommandSpec, error) { return CommandSpec{}, nil }
func (fakeChatAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	return CommandSpec{RunnerID: RunnerOpenCode, Binary: "fake", Args: []string{req.UserMessage}}, nil
}
func (fakeChatAdapter) NormalizeChatEvent(AgentRunEvent) []RunnerChatEvent { return nil }

type fakeRuntime struct {
	id      RunnerID
	aborted []string
}

func (r *fakeRuntime) ID() RunnerID { return r.id }
func (r *fakeRuntime) Info(context.Context, config.AgentCLIConfig, []string) RunnerRuntimeInfo {
	return RunnerRuntimeInfo{Kind: RuntimeNative, State: RuntimeStateReady}
}
func (r *fakeRuntime) Execute(context.Context, NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	return ProcessOutput{}, nil
}
func (r *fakeRuntime) Abort(_ context.Context, jobID string) error {
	r.aborted = append(r.aborted, jobID)
	return nil
}
func (r *fakeRuntime) Stop(context.Context) error { return nil }

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

func TestRunnerRuntimeModeDefaultsAndOverrides(t *testing.T) {
	cfg := config.AgentCLIConfig{}
	if got := runnerRuntimeMode(cfg, RunnerOpenCode); got != RuntimeModeAuto {
		t.Fatalf("default mode = %q, want auto", got)
	}
	cfg.RuntimeMode = map[string]string{"opencode": "cli", "codex": "native", "claude": "weird"}
	if got := runnerRuntimeMode(cfg, RunnerOpenCode); got != RuntimeModeCLI {
		t.Fatalf("opencode mode = %q, want cli", got)
	}
	if got := runnerRuntimeMode(cfg, RunnerCodex); got != RuntimeModeNative {
		t.Fatalf("codex mode = %q, want native", got)
	}
	if got := runnerRuntimeMode(cfg, RunnerClaude); got != RuntimeModeAuto {
		t.Fatalf("unknown mode = %q, want auto", got)
	}
}

func TestBuildRuntimeChatRequest(t *testing.T) {
	meta := map[string]any{
		"runner_chat_session_id":         "chat_sess_1",
		"runner_chat_turn_id":            "turn_1",
		"runner_chat_continuation_mode":  string(ContinuationNative),
		"runner_chat_user_message":       "continue please",
		"runner_chat_replay_prompt":      "replay prompt",
		"runner_chat_native_session_ref": "native_123",
		"_max_turns":                     5,
	}
	raw, _ := json.Marshal(meta)
	req, ok := buildRuntimeChatRequest(db.AgentCLIRun{Task: "fallback", Model: "gpt-5", Mode: string(RunnerModeSafeEdit), Isolation: string(IsolationHostWorkspaceWrite), Cwd: "/tmp", TimeoutSeconds: 60, MetaJSON: string(raw)})
	if !ok {
		t.Fatal("expected runner chat request")
	}
	if req.SessionID != "chat_sess_1" || req.TurnID != "turn_1" || req.NativeSessionRef != "native_123" {
		t.Fatalf("unexpected session fields: %+v", req)
	}
	if req.ContinuationMode != ContinuationNative || req.UserMessage != "continue please" || req.ReplayPrompt != "replay prompt" {
		t.Fatalf("unexpected prompt fields: %+v", req)
	}
	if req.Model != "gpt-5" || req.MaxTurns != 5 || req.Cwd != "/tmp" || req.TimeoutSeconds != 60 {
		t.Fatalf("unexpected run fields: %+v", req)
	}
}

func TestAddCodexPolicies(t *testing.T) {
	params := map[string]any{}
	err := addCodexPolicies(params, db.AgentCLIRun{Mode: string(RunnerModeSafeEdit), Isolation: string(IsolationHostWorkspaceWrite)})
	if err != nil {
		t.Fatalf("addCodexPolicies: %v", err)
	}
	if got := params["approvalPolicy"]; got != "on-request" {
		t.Fatalf("approvalPolicy = %v, want on-request", got)
	}
	sandbox, ok := params["sandboxPolicy"].(map[string]any)
	if !ok || sandbox["mode"] != "workspace-write" {
		t.Fatalf("sandboxPolicy = %#v, want workspace-write", params["sandboxPolicy"])
	}
}

func TestCLIRuntimeBackendBuildsChatCommand(t *testing.T) {
	backend := CLIRuntimeBackend{IDValue: RunnerOpenCode, Adapter: fakeChatAdapter{}}
	if backend.ID() != RunnerOpenCode {
		t.Fatalf("ID = %q, want opencode", backend.ID())
	}
	spec, err := backend.BuildChatCommand(RunnerChatCommandRequest{UserMessage: "hello"})
	if err != nil {
		t.Fatalf("BuildChatCommand: %v", err)
	}
	if spec.Binary != "fake" || len(spec.Args) != 1 || spec.Args[0] != "hello" {
		t.Fatalf("unexpected command spec: %+v", spec)
	}
}

func TestOpenCodeInfoUsesConfiguredLoopbackWithoutBinary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/config/providers":
			_, _ = w.Write([]byte(`{"providers":[{"id":"openai","models":[{"id":"gpt-5","displayName":"GPT-5"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	info := runtime.Info(context.Background(), config.AgentCLIConfig{RuntimeMode: map[string]string{"opencode": "auto"}, NativeServerURLs: map[string]string{"opencode": server.URL}}, []string{"PATH="})
	if info.State != RuntimeStateReady || info.Ownership != RuntimeOwnershipExternal || info.Fallback {
		t.Fatalf("unexpected runtime info: %+v", info)
	}
	if info.Endpoint != server.URL {
		t.Fatalf("endpoint = %q, want %q", info.Endpoint, server.URL)
	}
	foundModel := false
	for _, model := range info.Models {
		if model.ID == "gpt-5" {
			foundModel = true
		}
	}
	if !foundModel {
		t.Fatalf("models = %+v, want gpt-5", info.Models)
	}
}

func TestOpenCodeExecuteEmitsStructuredResponseEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/session":
			_, _ = w.Write([]byte(`{"id":"sess_1"}`))
		case "/session/sess_1/message":
			_, _ = w.Write([]byte(`{"type":"message","text":"done","permission":{"type":"permission.request","path":"/tmp/project"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	var events []AgentRunEvent
	output, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run:     db.AgentCLIRun{ID: "run_1", JobID: "job_1", Task: "hello"},
		Chat:    RunnerChatCommandRequest{UserMessage: "hello"},
		Config:  config.AgentCLIConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:     []string{"PATH="},
		OnEvent: func(event AgentRunEvent) { events = append(events, event) },
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output.FinalTextPreview != "done" {
		t.Fatalf("final text = %q, want done", output.FinalTextPreview)
	}
	if len(events) == 0 {
		t.Fatal("expected native events")
	}
	foundPermission := false
	for _, event := range events {
		if event.Type != "structured" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["type"] == "permission.asked" {
			foundPermission = true
		}
	}
	if !foundPermission {
		t.Fatalf("expected permission.asked event, got %+v", events)
	}
}

func TestStructuredRunnerPermissionDetection(t *testing.T) {
	payload := json.RawMessage(`{"type":"permission.write","params":{"path":"/tmp/project/file.txt","reason":"write file"}}`)
	req, ok := detectOpenCodePermissionRequest(AgentRunEvent{Type: "structured", Payload: payload})
	if !ok {
		t.Fatal("expected opencode permission request")
	}
	if req.RunnerID != string(RunnerOpenCode) || req.Access != runnerPermissionAccessWrite || req.TargetPath != "/tmp/project/file.txt" {
		t.Fatalf("unexpected opencode permission: %+v", req)
	}

	codexPayload := json.RawMessage(`{"method":"codex/requestApproval","params":{"cwd":"/tmp/project"}}`)
	req, ok = detectCodexStructuredPermissionRequest(AgentRunEvent{Type: "structured", Payload: codexPayload})
	if !ok {
		t.Fatal("expected codex permission request")
	}
	if req.RunnerID != string(RunnerCodex) || req.TargetPath != "/tmp/project" {
		t.Fatalf("unexpected codex permission: %+v", req)
	}
}

func TestManagerAbortDispatchesNativeRuntimes(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "or3.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runtime := &fakeRuntime{id: RunnerOpenCode}
	registry := &RunnerRuntimeRegistry{}
	registry.Register(runtime)
	manager := &Manager{DB: database, Jobs: agent.NewJobRegistry(time.Minute, 1024), Runtimes: registry}
	_ = manager.Abort(context.Background(), "job_123")
	if len(runtime.aborted) != 1 || runtime.aborted[0] != "job_123" {
		t.Fatalf("runtime aborts = %+v, want job_123", runtime.aborted)
	}
}

func TestCodexRPCWaitForTurnRequiresExplicitCompletion(t *testing.T) {
	client := newCodexRPC(nopWriteCloser{}, bytes.NewReader(nil))
	client.start(nil, nil)
	if err := client.waitForTurn(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("waitForTurn err = %v, want EOF", err)
	}
	client.close()
}

func TestCodexRPCWaitForTurnAllowsEOFAfterCompletion(t *testing.T) {
	client := newCodexRPC(nopWriteCloser{}, bytes.NewBufferString(`{"method":"turn/completed","params":{}}
`))
	client.start(nil, nil)
	if err := client.waitForTurn(context.Background()); err != nil {
		t.Fatalf("waitForTurn err = %v, want nil", err)
	}
	client.close()
}
