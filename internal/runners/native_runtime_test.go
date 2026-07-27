package runners

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
)

type fakeChatAdapter struct{}

func (fakeChatAdapter) ID() RunnerID                                       { return RunnerOpenCode }
func (fakeChatAdapter) DisplayName() string                                { return "Fake" }
func (fakeChatAdapter) Spec() RunnerSpec                                   { return RunnerSpec{} }
func (fakeChatAdapter) Detect(context.Context, DetectOptions) RunnerInfo   { return RunnerInfo{} }
func (fakeChatAdapter) BuildCommand(RunnerRunRequest) (CommandSpec, error) { return CommandSpec{}, nil }
func (fakeChatAdapter) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	return CommandSpec{RunnerID: RunnerOpenCode, Binary: "fake", Args: []string{req.UserMessage}}, nil
}
func (fakeChatAdapter) NormalizeChatEvent(RunnerRunEvent) []RunnerChatEvent { return nil }

type fakeRuntime struct {
	id           RunnerID
	out          ProcessOutput
	err          error
	executeCalls int
	aborted      []string
}

func (r *fakeRuntime) ID() RunnerID { return r.id }
func (r *fakeRuntime) Info(context.Context, config.RunnersConfig, []string) RunnerRuntimeInfo {
	return RunnerRuntimeInfo{Kind: RuntimeNative, State: RuntimeStateReady}
}
func (r *fakeRuntime) Execute(context.Context, NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	r.executeCalls++
	return r.out, r.err
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
	cfg := config.RunnersConfig{}
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
	if got := runnerRuntimeMode(cfg, RunnerID("legacy-runner")); got != RuntimeModeAuto {
		t.Fatalf("unknown mode = %q, want auto", got)
	}
}

func TestCodexAppServerArgsDisableUserMCPServers(t *testing.T) {
	want := []string{"app-server", "--listen", "stdio://", "-c", "mcp_servers={}"}
	assertArgsEqual(t, want, codexAppServerArgs())
}

func TestCodexNativeReplayExecutionInputUsesReplayPrompt(t *testing.T) {
	run := db.RunnerRun{Task: "compiled run task with soul"}
	chat := RunnerChatCommandRequest{
		ContinuationMode: ContinuationReplay,
		ReplayPrompt:     "compiled replay with soul",
		UserMessage:      "raw user only",
	}
	if got := ChatExecutionInput(chat, run.Task); got != "compiled replay with soul" {
		t.Fatalf("replay native execution should use replay prompt, got %q", got)
	}
}

func TestOpenCodeNativeReplayExecutionInputUsesReplayPrompt(t *testing.T) {
	chat := RunnerChatCommandRequest{
		ContinuationMode: ContinuationReplay,
		ReplayPrompt:     "compiled replay with soul",
		UserMessage:      "raw user only",
	}
	if got := ChatExecutionInput(chat, "compiled run task with soul"); got != "compiled replay with soul" {
		t.Fatalf("replay native execution should use replay prompt, got %q", got)
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
	req, ok := buildRuntimeChatRequest(db.RunnerRun{Task: "fallback", Model: "gpt-5", Mode: string(RunnerModeSafeEdit), Isolation: string(IsolationHostWorkspaceWrite), Cwd: "/tmp", TimeoutSeconds: 60, MetaJSON: string(raw)})
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
	err := addCodexPolicies(params, db.RunnerRun{Mode: string(RunnerModeSafeEdit), Isolation: string(IsolationHostWorkspaceWrite)})
	if err != nil {
		t.Fatalf("addCodexPolicies: %v", err)
	}
	if got := params["approvalPolicy"]; got != "on-request" {
		t.Fatalf("approvalPolicy = %v, want on-request", got)
	}
	sandbox, ok := params["sandboxPolicy"].(map[string]any)
	if !ok || sandbox["type"] != "workspaceWrite" {
		t.Fatalf("sandboxPolicy = %#v, want workspaceWrite", params["sandboxPolicy"])
	}
}

func TestAddCodexPoliciesUsesAppServerSandboxVariants(t *testing.T) {
	cases := []struct {
		name      string
		isolation string
		wantType  string
	}{
		{name: "read only", isolation: string(IsolationHostReadOnly), wantType: "readOnly"},
		{name: "workspace write", isolation: string(IsolationHostWorkspaceWrite), wantType: "workspaceWrite"},
		{name: "sandbox write", isolation: string(IsolationSandboxWrite), wantType: "workspaceWrite"},
		{name: "danger", isolation: string(IsolationSandboxDangerous), wantType: "dangerFullAccess"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{}
			if err := addCodexPolicies(params, db.RunnerRun{Mode: string(RunnerModeSafeEdit), Isolation: tc.isolation}); err != nil {
				t.Fatalf("addCodexPolicies: %v", err)
			}
			sandbox, ok := params["sandboxPolicy"].(map[string]any)
			if !ok {
				t.Fatalf("sandboxPolicy = %#v, want object", params["sandboxPolicy"])
			}
			if got := sandbox["type"]; got != tc.wantType {
				t.Fatalf("sandbox type = %v, want %s", got, tc.wantType)
			}
			if _, ok := sandbox["mode"]; ok {
				t.Fatalf("sandboxPolicy still uses legacy mode field: %#v", sandbox)
			}
		})
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
	info := runtime.Info(context.Background(), config.RunnersConfig{RuntimeMode: map[string]string{"opencode": "auto"}, NativeServerURLs: map[string]string{"opencode": server.URL}}, []string{"PATH="})
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

func TestOpenCodeInfoManagedHealthFailureKeepsCLIModels(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
if [ "$1" = "models" ]; then
  printf '%s\n' \
    'openrouter/mimo-v2.5' \
    '{' \
    '  "id": "mimo-v2.5",' \
    '  "providerID": "openrouter",' \
    '  "name": "MiMo v2.5"' \
    '}'
  exit 0
fi
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}

	runtime := NewOpenCodeNativeRuntime()
	runtime.endpoint = "http://127.0.0.1:1"
	info := runtime.Info(context.Background(), config.RunnersConfig{
		RuntimeMode: map[string]string{"opencode": "auto"},
	}, []string{"PATH=" + dir})
	if info.State != RuntimeStateError {
		t.Fatalf("expected health error, got %+v", info)
	}
	if info.FallbackReason != "health check failed" {
		t.Fatalf("fallback reason = %q", info.FallbackReason)
	}
	if len(info.Models) != 1 || info.Models[0].ID != "mimo-v2.5" || info.Models[0].Provider != "openrouter" {
		t.Fatalf("models = %+v, want CLI fallback model", info.Models)
	}
}

func TestOpenCodeNativeRuntimeDoesNotCapTurnRequestAtClientTimeout(t *testing.T) {
	runtime := NewOpenCodeNativeRuntime()
	if runtime.client.Timeout != 0 {
		t.Fatalf("OpenCode native client timeout = %v, want context-controlled timeout", runtime.client.Timeout)
	}
}

func TestFlattenOpenCodeModelsPreservesVariantsAndDefaults(t *testing.T) {
	var raw any
	if err := json.Unmarshal([]byte(`{
		"default":{"openai":"gpt-5"},
		"providers":[{"id":"openai","name":"OpenAI","models":{"gpt-5":{"name":"GPT-5","variants":{"low":{},"medium":{},"high":{}}}}}]
	}`), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	models := flattenModelInfo(raw)
	if len(models) != 1 {
		t.Fatalf("models = %+v, want one", models)
	}
	model := models[0]
	if model.ID != "gpt-5" || model.Provider != "openai" || model.ProviderName != "OpenAI" || !model.Default {
		t.Fatalf("unexpected model metadata: %+v", model)
	}
	if !reflect.DeepEqual(model.Reasoning, []string{"low", "medium", "high"}) {
		t.Fatalf("reasoning = %+v", model.Reasoning)
	}
}

func TestCodexModelListToRunnerModelsMapsReasoning(t *testing.T) {
	var resp map[string]any
	if err := json.Unmarshal([]byte(`{"data":[{"model":"gpt-5","displayName":"GPT-5","modelProvider":"openai","isDefault":true,"defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"low"},{"reasoningEffort":"high"}]}]}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	models := codexModelListToRunnerModels(resp)
	if len(models) != 1 {
		t.Fatalf("models = %+v, want one", models)
	}
	model := models[0]
	if model.ID != "gpt-5" || model.Provider != "openai" || model.ProviderName != "OpenAI Codex" || !model.Default || model.ReasoningDefault != "medium" {
		t.Fatalf("unexpected model metadata: %+v", model)
	}
	if !reflect.DeepEqual(model.Reasoning, []string{"low", "high"}) {
		t.Fatalf("reasoning = %+v", model.Reasoning)
	}
}

func TestCodexRPCFinalTextDoesNotDuplicateCompletedAgentMessage(t *testing.T) {
	rpc := &codexRPC{}
	rpc.captureNotificationText("item/agentMessage/delta", map[string]any{"delta": "OR3"})
	rpc.captureNotificationText("item/agentMessage/delta", map[string]any{"delta": "_OK"})
	rpc.captureNotificationText("item/completed", map[string]any{
		"item": map[string]any{"type": "agentMessage", "text": "OR3_OK"},
	})
	if got := rpc.finalText(); got != "OR3_OK" {
		t.Fatalf("final text = %q, want OR3_OK", got)
	}
}

func TestReasoningOptionsUseSemanticOrder(t *testing.T) {
	got := sortedUniqueStrings([]string{"xhigh", "medium", "low", "high", "none", "max"})
	want := []string{"none", "low", "medium", "high", "xhigh", "max"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reasoning order = %+v, want %+v", got, want)
	}
}

func TestResolveOpenCodeModelMapsVendorPrefixedRequest(t *testing.T) {
	catalog := []RunnerModelInfo{
		{ID: "mimo-v2.5", Provider: "openrouter", ProviderName: "OpenRouter"},
		{ID: "gpt-5", Provider: "openai", ProviderName: "OpenAI"},
	}
	resolved := resolveOpenCodeModel(catalog, "xiaomi/mimo-v2.5")
	if resolved == nil || resolved.Provider != "openrouter" || resolved.ID != "mimo-v2.5" {
		t.Fatalf("resolved = %+v, want openrouter/mimo-v2.5", resolved)
	}
}

func TestOpenCodeExecuteResolvesVendorPrefixedModel(t *testing.T) {
	var messageBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/config/providers":
			_, _ = w.Write([]byte(`{"providers":[{"id":"openrouter","name":"OpenRouter","models":{"mimo-v2.5":{"name":"MiMo v2.5"}}}]}`))
		case "/session":
			_, _ = w.Write([]byte(`{"id":"sess_1"}`))
		case "/session/sess_1/message":
			if err := json.NewDecoder(r.Body).Decode(&messageBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_, _ = w.Write([]byte(`{"type":"message","text":"done"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	_, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run:    db.RunnerRun{ID: "run_1", JobID: "job_1", Task: "hello", Model: "xiaomi/mimo-v2.5"},
		Chat:   RunnerChatCommandRequest{UserMessage: "hello"},
		Config: config.RunnersConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:    []string{"PATH="},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if messageBody["providerID"] != "openrouter" || messageBody["modelID"] != "mimo-v2.5" {
		t.Fatalf("unexpected model request: %#v", messageBody)
	}
}

func TestOpenCodeExecuteSendsVariantOnlyWhenSupported(t *testing.T) {
	var messageBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/config/providers":
			_, _ = w.Write([]byte(`{"default":{"openai":"gpt-5"},"providers":[{"id":"openai","models":{"gpt-5":{"name":"GPT-5","variants":{"low":{},"high":{}}}}}]}`))
		case "/session":
			_, _ = w.Write([]byte(`{"id":"sess_1"}`))
		case "/session/sess_1/message":
			if err := json.NewDecoder(r.Body).Decode(&messageBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_, _ = w.Write([]byte(`{"type":"message","text":"done"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	_, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run:    db.RunnerRun{ID: "run_1", JobID: "job_1", Task: "hello", Model: "gpt-5"},
		Chat:   RunnerChatCommandRequest{UserMessage: "hello", Meta: map[string]any{"runner_thinking_level": "high"}},
		Config: config.RunnersConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:    []string{"PATH="},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if messageBody["providerID"] != "openai" || messageBody["modelID"] != "gpt-5" || messageBody["variant"] != "high" {
		t.Fatalf("unexpected model request: %#v", messageBody)
	}
}

func TestOpenCodeExecuteReplayIgnoresNativeSessionRef(t *testing.T) {
	var postedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/config/providers":
			_, _ = w.Write([]byte(`{"providers":[]}`))
		case "/session":
			_, _ = w.Write([]byte(`{"id":"fresh_session"}`))
		case "/session/fresh_session/message", "/session/stale_session/message":
			postedPath = r.URL.Path
			_, _ = w.Write([]byte(`{"type":"message","text":"done"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	_, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run: db.RunnerRun{ID: "run_1", JobID: "job_1", Task: "hello"},
		Chat: RunnerChatCommandRequest{
			UserMessage:      "hello",
			NativeSessionRef: "stale_session",
			ContinuationMode: ContinuationReplay,
		},
		Config: config.RunnersConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:    []string{"PATH="},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if postedPath != "/session/fresh_session/message" {
		t.Fatalf("postedPath = %q, want fresh replay session", postedPath)
	}
}

func TestParseOpenCodeModelsCLIOutputPreservesProviderAndVariants(t *testing.T) {
	models := parseOpenCodeModelsCLIOutput([]byte(`opencode-go/deepseek-v4-pro
{
  "id": "deepseek-v4-pro",
  "providerID": "opencode-go",
  "name": "DeepSeek V4 Pro",
  "variants": {"low": {}, "medium": {}, "high": {}}
}
opencode/gpt-5.2
{
  "id": "gpt-5.2",
  "providerID": "opencode",
  "name": "GPT-5.2",
  "variants": {"none": {}, "low": {}, "medium": {}, "high": {}}
}
`))
	if len(models) != 2 {
		t.Fatalf("models = %+v, want two", models)
	}
	if models[0].Provider != "opencode-go" || models[0].ProviderName != "OpenCode Go" || models[0].ID != "deepseek-v4-pro" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
	if !reflect.DeepEqual(models[0].Reasoning, []string{"low", "medium", "high"}) {
		t.Fatalf("first reasoning = %+v", models[0].Reasoning)
	}
	if models[1].Provider != "opencode" || models[1].ProviderName != "OpenCode Zen" || models[1].ID != "gpt-5.2" {
		t.Fatalf("unexpected second model: %+v", models[1])
	}
	if !reflect.DeepEqual(models[1].Reasoning, []string{"none", "low", "medium", "high"}) {
		t.Fatalf("second reasoning = %+v", models[1].Reasoning)
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
			_, _ = w.Write([]byte(`{"type":"message","text":"done","question":{"type":"question.request","question":"Proceed?"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	var events []RunnerRunEvent
	output, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run:     db.RunnerRun{ID: "run_1", JobID: "job_1", Task: "hello"},
		Chat:    RunnerChatCommandRequest{UserMessage: "hello"},
		Config:  config.RunnersConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:     []string{"PATH="},
		OnEvent: func(event RunnerRunEvent) { events = append(events, event) },
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
	foundQuestion := false
	for _, event := range events {
		if event.Type != "structured" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["type"] == "question.asked" {
			foundQuestion = true
		}
	}
	if !foundQuestion {
		t.Fatalf("expected question.asked event, got %+v", events)
	}
}

func TestOpenCodeExecuteDoesNotTreatErrorEnvelopeAsFinalText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/session":
			_, _ = w.Write([]byte(`{"id":"sess_1"}`))
		case "/session/sess_1/message":
			_, _ = w.Write([]byte(`{"type":"error","timestamp":1780726148111,"sessionID":"sess_1","error":{"name":"UnknownError","data":{"message":"Unexpected server error. Check server logs for details.","ref":"err_085b596b"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runtime := NewOpenCodeNativeRuntime()
	output, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run:    db.RunnerRun{ID: "run_1", JobID: "job_1", Task: "hello"},
		Chat:   RunnerChatCommandRequest{UserMessage: "hello"},
		Config: config.RunnersConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:    []string{"PATH="},
	})
	if err == nil {
		t.Fatal("expected OpenCode error response to fail")
	}
	if output.FinalTextPreview != "" {
		t.Fatalf("expected empty final text for error envelope, got %q", output.FinalTextPreview)
	}
	if output.StderrPreview != "Unexpected server error. Check server logs for details." {
		t.Fatalf("stderr = %q", output.StderrPreview)
	}
}

func TestCodexExecuteRetriesAfterAuthRefreshFailure(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "attempts")
	serverPath := filepath.Join(dir, "fake_codex_server.py")
	if err := os.WriteFile(serverPath, []byte(`import json
import os
import sys

state_path = sys.argv[1]
try:
    with open(state_path) as f:
        attempt = int(f.read().strip() or "0")
except FileNotFoundError:
    attempt = 0
attempt += 1
with open(state_path, "w") as f:
    f.write(str(attempt))

def send(msg):
    print(json.dumps(msg), flush=True)

for line in sys.stdin:
    msg = json.loads(line)
    mid = msg.get("id")
    method = msg.get("method")
    if mid is None:
        continue
    if method == "initialize":
        send({"id": mid, "result": {}})
    elif method == "thread/start":
        if attempt == 1:
            send({"id": mid, "error": {"message": "Auth(TokenRefreshFailed(\"Server returned error response: invalid_grant: refresh token invalid\"))"}})
            sys.exit(0)
        send({"id": mid, "result": {"thread": {"id": "thread-2"}}})
    elif method == "turn/start":
        params = msg.get("params") or {}
        if params.get("threadId") != "thread-2":
            send({"id": mid, "error": {"code": -32600, "message": "invalid thread id: invalid length: expected length 32 for simple format, found 0"}})
            continue
        turn_input = params.get("input")
        if not isinstance(turn_input, list):
            send({"id": mid, "error": {"code": -32600, "message": "Invalid request: invalid type: string, expected a sequence"}})
            continue
        if not turn_input or turn_input[0].get("type") != "text" or turn_input[0].get("text") != "hello":
            send({"id": mid, "error": {"code": -32600, "message": "Invalid request: invalid text input item"}})
            continue
        sandbox_policy = params.get("sandboxPolicy") or {}
        if sandbox_policy.get("type") != "workspaceWrite":
            send({"id": mid, "error": {"code": -32600, "message": "Invalid request: missing field type"}})
            continue
        if "mode" in sandbox_policy:
            send({"id": mid, "error": {"code": -32600, "message": "Invalid request: legacy sandbox mode field"}})
            continue
        send({"id": mid, "result": {"turn": {"id": "turn-2"}}})
        send({"method": "item/completed", "params": {"item": {"type": "agentMessage", "text": "OR3_SMOKE_OK"}}})
        send({"method": "turn/completed", "params": {}})
    elif method == "model/list":
        send({"id": mid, "result": {"models": []}})
    else:
        send({"id": mid, "result": {}})
`), 0o600); err != nil {
		t.Fatalf("write fake server: %v", err)
	}
	writeFakeBinary(t, dir, "codex", fmt.Sprintf(`if [ "$1" = "--version" ]; then
  echo "codex fake"
  exit 0
fi
exec python3 %q %q
`, serverPath, statePath))

	runtime := NewCodexNativeRuntime()
	output, err := runtime.Execute(context.Background(), NativeRuntimeExecuteRequest{
		Run: db.RunnerRun{
			ID:        "run_1",
			JobID:     "job_1",
			Task:      "hello",
			Cwd:       dir,
			Mode:      string(RunnerModeSafeEdit),
			Isolation: string(IsolationHostWorkspaceWrite),
			Status:    db.RunnerRunStatusRunning,
		},
		Chat:   RunnerChatCommandRequest{UserMessage: "hello"},
		Config: config.RunnersConfig{},
		Env:    []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output.FinalTextPreview != "OR3_SMOKE_OK" {
		t.Fatalf("final text = %q, want smoke output", output.FinalTextPreview)
	}
	attemptsRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if strings.TrimSpace(string(attemptsRaw)) != "2" {
		t.Fatalf("attempts = %q, want 2", attemptsRaw)
	}
}

func TestCodexAuthRefreshFailureMessageIsActionable(t *testing.T) {
	err := errors.New(`codex rpc error: map[message:Auth(TokenRefreshFailed("Server returned error response: invalid_grant: refresh token invalid"))]`)
	if !isCodexAuthRefreshFailure(err, "") {
		t.Fatal("expected token refresh failure to be classified")
	}
	msg := codexAuthRefreshFailureMessage(err, "")
	if !strings.Contains(msg, "Run `codex login`") || !strings.Contains(msg, "invalid_grant") {
		t.Fatalf("message is not actionable: %q", msg)
	}
}

func TestManagerDoesNotFallbackToCLIForCodexAuthRefreshFailure(t *testing.T) {
	database := openRunnerTestDB(t)
	t.Cleanup(func() { _ = database.Close() })
	runtime := &fakeRuntime{
		id:  RunnerCodex,
		err: errors.New(`codex rpc error: map[message:Auth(TokenRefreshFailed("Server returned error response: invalid_grant: refresh token invalid"))]`),
	}
	registry := &RunnerRuntimeRegistry{}
	registry.Register(runtime)
	manager := &Manager{
		DB:       database,
		Jobs:     jobs.NewRegistry(time.Minute, 1024),
		Runtimes: registry,
		Cfg:      config.RunnersConfig{RuntimeMode: map[string]string{"codex": "auto"}},
	}
	meta, _ := json.Marshal(map[string]any{
		"runner_chat_session_id":        "session_1",
		"runner_chat_turn_id":           "turn_1",
		"runner_chat_continuation_mode": string(ContinuationNative),
		"runner_chat_user_message":      "hello",
	})
	out, handled := manager.tryExecuteNativeRun(context.Background(), db.RunnerRun{
		ID:        "run_1",
		JobID:     "job_1",
		RunnerID:  string(RunnerCodex),
		Task:      "hello",
		Cwd:       t.TempDir(),
		Mode:      string(RunnerModeSafeEdit),
		Isolation: string(IsolationHostWorkspaceWrite),
		MetaJSON:  string(meta),
	})
	if !handled {
		t.Fatal("expected auth refresh failure to be handled without CLI fallback")
	}
	if runtime.executeCalls != 1 {
		t.Fatalf("execute calls = %d, want 1", runtime.executeCalls)
	}
	if !strings.Contains(out.StderrPreview, "Run `codex login`") {
		t.Fatalf("stderr = %q, want actionable codex login message", out.StderrPreview)
	}
}

func TestManagerDoesNotFallbackToCLIForCodexNativeAppServerFailure(t *testing.T) {
	database := openRunnerTestDB(t)
	t.Cleanup(func() { _ = database.Close() })
	runtime := &fakeRuntime{
		id:  RunnerCodex,
		out: ProcessOutput{StderrPreview: "failed to load skill waveapps-accounting: invalid YAML"},
		err: errors.New("codex app-server turn failed"),
	}
	registry := &RunnerRuntimeRegistry{}
	registry.Register(runtime)
	manager := &Manager{
		DB:       database,
		Jobs:     jobs.NewRegistry(time.Minute, 1024),
		Runtimes: registry,
		Cfg:      config.RunnersConfig{RuntimeMode: map[string]string{"codex": "auto"}},
	}
	meta, _ := json.Marshal(map[string]any{
		"runner_chat_session_id":        "session_1",
		"runner_chat_turn_id":           "turn_1",
		"runner_chat_continuation_mode": string(ContinuationNative),
		"runner_chat_user_message":      "hello",
	})
	out, handled := manager.tryExecuteNativeRun(context.Background(), db.RunnerRun{
		ID:        "run_1",
		JobID:     "job_1",
		RunnerID:  string(RunnerCodex),
		Task:      "hello",
		Cwd:       t.TempDir(),
		Mode:      string(RunnerModeSafeEdit),
		Isolation: string(IsolationHostWorkspaceWrite),
		MetaJSON:  string(meta),
	})
	if !handled {
		t.Fatal("expected codex app-server failure to be handled without CLI fallback")
	}
	if runtime.executeCalls != 1 {
		t.Fatalf("execute calls = %d, want 1", runtime.executeCalls)
	}
	if out.ExitCode != -1 {
		t.Fatalf("exit code = %d, want -1", out.ExitCode)
	}
	if !strings.Contains(out.StderrPreview, "failed to load skill") {
		t.Fatalf("stderr = %q, want native stderr diagnostic", out.StderrPreview)
	}
}

func TestManagerCLIPathNormalizesCodexAuthRefreshFailure(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "codex", `echo 'Auth(TokenRefreshFailed("Server returned error response: invalid_grant: refresh token invalid"))' >&2
exit 1`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager, database, _ := newTestManager(t)
	manager.Cfg.RuntimeMode = map[string]string{"codex": "cli"}
	manager.ctx = context.Background()
	run := mustInsertRunnerRun(t, database, db.RunnerRun{
		ID:        "run_cli_auth",
		JobID:     "job_cli_auth",
		RunnerID:  string(RunnerCodex),
		Task:      "hello",
		Cwd:       dir,
		Mode:      string(RunnerModeSafeEdit),
		Isolation: string(IsolationHostWorkspaceWrite),
		Status:    db.RunnerRunStatusRunning,
	})
	manager.executeRun(run)

	stored := mustGetRunnerRun(t, database, run.ID)
	if stored.Status != db.RunnerRunStatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}
	if !strings.Contains(stored.ErrorMessage, "Run `codex login`") {
		t.Fatalf("error message = %q, want actionable codex login message", stored.ErrorMessage)
	}
	if strings.Contains(stored.FinalTextPreview, "TokenRefreshFailed") {
		t.Fatalf("final text leaked raw auth error: %q", stored.FinalTextPreview)
	}
}

func TestOpenCodeExecuteStopsOnPermissionRequest(t *testing.T) {
	const sessionID = "sess_permission"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.WriteHeader(http.StatusOK)
		case "/event":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatalf("response writer is not flusher")
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
				"type": "permission.updated",
				"properties": map[string]any{
					"permission": map[string]any{
						"type": "write",
						"path": "/tmp/project/file.txt",
					},
				},
			}))
			flusher.Flush()
			<-r.Context().Done()
		case "/session":
			_, _ = w.Write([]byte(`{"id":"` + sessionID + `"}`))
		case "/session/" + sessionID + "/message":
			_, _ = w.Write([]byte(`{"type":"message","permission":{"type":"permission.write","path":"/tmp/project/file.txt"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runtime := NewOpenCodeNativeRuntime()
	var sawApprovalEvent bool
	_, err := runtime.Execute(ctx, NativeRuntimeExecuteRequest{
		Run:    db.RunnerRun{ID: "run_1", JobID: "job_1", Task: "hello"},
		Chat:   RunnerChatCommandRequest{UserMessage: "hello"},
		Config: config.RunnersConfig{NativeServerURLs: map[string]string{"opencode": server.URL}},
		Env:    []string{"PATH="},
		OnEvent: func(event RunnerRunEvent) {
			if _, ok := detectOpenCodePermissionRequest(event); ok {
				sawApprovalEvent = true
			}
		},
	})
	if !errors.Is(err, errNativeApprovalRequired) {
		t.Fatalf("Execute err = %v, want errNativeApprovalRequired", err)
	}
	if !sawApprovalEvent {
		t.Fatal("expected permission event to be emitted")
	}
}

func TestStructuredRunnerPermissionDetection(t *testing.T) {
	payload := json.RawMessage(`{"type":"permission.write","params":{"path":"/tmp/project/file.txt","reason":"write file"}}`)
	req, ok := detectOpenCodePermissionRequest(RunnerRunEvent{Type: "structured", Payload: payload})
	if !ok {
		t.Fatal("expected opencode permission request")
	}
	if req.RunnerID != string(RunnerOpenCode) || req.Access != runnerPermissionAccessWrite || req.TargetPath != "/tmp/project/file.txt" {
		t.Fatalf("unexpected opencode permission: %+v", req)
	}

	nestedPayload := json.RawMessage(`{"type":"permission.asked","permission":{"type":"edit","path":"/tmp/project/nested.txt"}}`)
	req, ok = detectOpenCodePermissionRequest(RunnerRunEvent{Type: "structured", Payload: nestedPayload})
	if !ok {
		t.Fatal("expected nested opencode permission request")
	}
	if req.Access != runnerPermissionAccessWrite || req.TargetPath != "/tmp/project/nested.txt" {
		t.Fatalf("unexpected nested opencode permission: %+v", req)
	}

	codexPayload := json.RawMessage(`{"method":"codex/requestApproval","params":{"cwd":"/tmp/project"}}`)
	req, ok = detectCodexStructuredPermissionRequest(RunnerRunEvent{Type: "structured", Payload: codexPayload})
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
	manager := &Manager{DB: database, Jobs: jobs.NewRegistry(time.Minute, 1024), Runtimes: registry}
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

func TestEmitCodexNotificationStructuredDoesNotDuplicateTextAsStdout(t *testing.T) {
	var seq int64
	var events []RunnerRunEvent
	emitCodexNotificationStructured(
		&seq,
		func(event RunnerRunEvent) { events = append(events, event) },
		"item/agentMessage/delta",
		map[string]any{"delta": "hello"},
	)

	if len(events) != 1 {
		t.Fatalf("events = %#v, want one structured event", events)
	}
	if events[0].Type != "structured" || events[0].Stream != "" || events[0].Chunk != "" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
	normalized := (&CodexAdapter{}).NormalizeChatEvent(events[0])
	if len(normalized) != 1 || normalized[0].Type != "text_delta" || normalized[0].Text != "hello" {
		t.Fatalf("unexpected normalized event: %#v", normalized)
	}
}
