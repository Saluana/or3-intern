package agentcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

// NativeRuntimeExecuteRequest is the normalized execution request for a native backend.
type NativeRuntimeExecuteRequest struct {
	Run     db.AgentCLIRun
	Chat    RunnerChatCommandRequest
	Config  config.AgentCLIConfig
	Env     []string
	OnEvent func(AgentRunEvent)
}

var errNativeApprovalRequired = errors.New("native runner approval required")

// NativeRunnerRuntime executes a runner turn through its local server/runtime API.
type NativeRunnerRuntime interface {
	ID() RunnerID
	Info(ctx context.Context, cfg config.AgentCLIConfig, env []string) RunnerRuntimeInfo
	Execute(ctx context.Context, req NativeRuntimeExecuteRequest) (ProcessOutput, error)
	Abort(ctx context.Context, jobID string) error
	Stop(ctx context.Context) error
}

// CLIRuntimeBackend is the explicit compatibility backend for existing chat
// adapters. The manager still runs this path through its established process
// execution code, but this wrapper gives tests and discovery code a concrete
// runtime boundary for CLI fallback behavior.
type CLIRuntimeBackend struct {
	IDValue RunnerID
	Adapter RunnerChatAdapter
	Process *ProcessManager
}

func (b CLIRuntimeBackend) ID() RunnerID { return b.IDValue }

func (b CLIRuntimeBackend) BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error) {
	if b.Adapter == nil {
		return CommandSpec{}, fmt.Errorf("cli runtime backend %q has no adapter", b.IDValue)
	}
	return b.Adapter.BuildChatCommand(req)
}

// RunnerRuntimeRegistry stores optional native backends by runner id.
type RunnerRuntimeRegistry struct {
	mu       sync.RWMutex
	runtimes map[RunnerID]NativeRunnerRuntime
}

func NewDefaultRuntimeRegistry() *RunnerRuntimeRegistry {
	registry := &RunnerRuntimeRegistry{runtimes: map[RunnerID]NativeRunnerRuntime{}}
	registry.Register(NewOpenCodeNativeRuntime())
	registry.Register(NewCodexNativeRuntime())
	return registry
}

func (r *RunnerRuntimeRegistry) Register(runtime NativeRunnerRuntime) {
	if r == nil || runtime == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runtimes == nil {
		r.runtimes = map[RunnerID]NativeRunnerRuntime{}
	}
	r.runtimes[runtime.ID()] = runtime
}

func (r *RunnerRuntimeRegistry) Get(id RunnerID) (NativeRunnerRuntime, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	runtime, ok := r.runtimes[id]
	return runtime, ok
}

func (r *RunnerRuntimeRegistry) ForEach(fn func(NativeRunnerRuntime)) {
	if r == nil || fn == nil {
		return
	}
	r.mu.RLock()
	runtimes := make([]NativeRunnerRuntime, 0, len(r.runtimes))
	for _, runtime := range r.runtimes {
		runtimes = append(runtimes, runtime)
	}
	r.mu.RUnlock()
	for _, runtime := range runtimes {
		fn(runtime)
	}
}

func runnerRuntimeMode(cfg config.AgentCLIConfig, id RunnerID) RunnerRuntimeMode {
	mode := strings.ToLower(strings.TrimSpace(cfg.RuntimeMode[string(id)]))
	switch RunnerRuntimeMode(mode) {
	case RuntimeModeNative, RuntimeModeCLI, RuntimeModeAuto:
		return RunnerRuntimeMode(mode)
	default:
		return RuntimeModeAuto
	}
}

func defaultRuntimeInfo(cfg config.AgentCLIConfig, id RunnerID, hasNative bool) RunnerRuntimeInfo {
	mode := runnerRuntimeMode(cfg, id)
	info := RunnerRuntimeInfo{
		Kind:      RuntimeCLI,
		Mode:      mode,
		State:     RuntimeStateUnavailable,
		Ownership: RuntimeOwnershipNone,
		Fallback:  true,
	}
	if model := strings.TrimSpace(cfg.DefaultModels[string(id)]); model != "" {
		info.DefaultModel = model
	}
	if !hasNative || mode == RuntimeModeCLI {
		info.FallbackReason = "using CLI adapter"
		return info
	}
	info.Kind = RuntimeNative
	info.State = RuntimeStateFallback
	info.FallbackReason = "native runtime is lazy-started on first use"
	return info
}

func buildRuntimeChatRequest(run db.AgentCLIRun) (RunnerChatCommandRequest, bool) {
	meta := parseAgentRunMeta(run.MetaJSON)
	sessionID := strings.TrimSpace(stringMeta(meta, "runner_chat_session_id"))
	if sessionID == "" {
		return RunnerChatCommandRequest{}, false
	}
	chatReq := RunnerChatCommandRequest{
		SessionID:        sessionID,
		TurnID:           stringMeta(meta, "runner_chat_turn_id"),
		NativeSessionRef: stringMeta(meta, "runner_chat_native_session_ref"),
		ContinuationMode: ContinuationMode(firstNonEmptyStringMeta(meta, "runner_chat_continuation_mode", string(ContinuationReplay))),
		ReplayPrompt:     firstNonEmptyStringMeta(meta, "runner_chat_replay_prompt", run.Task),
		UserMessage:      firstNonEmptyStringMeta(meta, "runner_chat_user_message", run.Task),
		Model:            run.Model,
		Mode:             run.Mode,
		Isolation:        run.Isolation,
		Cwd:              run.Cwd,
		TimeoutSeconds:   run.TimeoutSeconds,
		Meta:             meta,
	}
	if mt, ok := meta["_max_turns"]; ok {
		switch v := mt.(type) {
		case float64:
			chatReq.MaxTurns = int(v)
		case int:
			chatReq.MaxTurns = v
		}
	}
	return chatReq, true
}

func runtimeEvent(seq *int64, eventType string, payload map[string]any) AgentRunEvent {
	raw, _ := json.Marshal(payload)
	return AgentRunEvent{
		Type:    eventType,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Seq:     atomic.AddInt64(seq, 1),
		Payload: raw,
	}
}

func textChunkEvent(seq *int64, chunk string) AgentRunEvent {
	return AgentRunEvent{
		Type:   "output",
		TS:     time.Now().UTC().Format(time.RFC3339Nano),
		Seq:    atomic.AddInt64(seq, 1),
		Stream: "stdout",
		Chunk:  chunk,
	}
}

func emitNativeStructured(seq *int64, onEvent func(AgentRunEvent), payload map[string]any) {
	if onEvent == nil {
		return
	}
	onEvent(runtimeEvent(seq, "structured", payload))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func executableAvailable(binary string, env []string) bool {
	_, err := ResolveExecutable(binary, env)
	return err == nil
}

func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener addr %T", listener.Addr())
	}
	return addr.Port, nil
}

func httpJSON(ctx context.Context, client *http.Client, method, url string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s failed: %s %s", method, url, resp.Status, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// OpenCodeNativeRuntime talks to a local opencode HTTP server.
type OpenCodeNativeRuntime struct {
	mu             sync.Mutex
	endpoint       string
	ownership      RunnerRuntimeOwnership
	cmd            *exec.Cmd
	client         *http.Client
	activeSessions map[string]string
}

func NewOpenCodeNativeRuntime() *OpenCodeNativeRuntime {
	return &OpenCodeNativeRuntime{client: &http.Client{Timeout: 60 * time.Second}, activeSessions: map[string]string{}}
}

func (r *OpenCodeNativeRuntime) ID() RunnerID { return RunnerOpenCode }

func (r *OpenCodeNativeRuntime) Info(ctx context.Context, cfg config.AgentCLIConfig, env []string) RunnerRuntimeInfo {
	info := defaultRuntimeInfo(cfg, RunnerOpenCode, true)
	info.Kind = RuntimeNative
	if mode := runnerRuntimeMode(cfg, RunnerOpenCode); mode == RuntimeModeCLI {
		return defaultRuntimeInfo(cfg, RunnerOpenCode, true)
	}
	if configured := strings.TrimRight(strings.TrimSpace(cfg.NativeServerURLs[string(RunnerOpenCode)]), "/"); configured != "" && r.health(ctx, configured) == nil {
		info.Endpoint = configured
		info.State = RuntimeStateReady
		info.Ownership = RuntimeOwnershipExternal
		info.Fallback = false
		info.FallbackReason = ""
		info.Models = r.models(ctx, configured)
		return info
	}
	if !executableAvailable("opencode", env) {
		info.State = RuntimeStateUnavailable
		info.Fallback = true
		info.FallbackReason = "opencode binary is not installed"
		return info
	}
	r.mu.Lock()
	endpoint := r.endpoint
	ownership := r.ownership
	r.mu.Unlock()
	if endpoint == "" {
		info.State = RuntimeStateFallback
		info.Fallback = true
		info.FallbackReason = "native runtime will start when first used"
		info.Models = r.modelsFromCLI(ctx, env)
		return info
	}
	info.Endpoint = endpoint
	if err := r.health(ctx, endpoint); err != nil {
		info.State = RuntimeStateError
		info.Message = err.Error()
		info.Fallback = true
		info.FallbackReason = "health check failed"
		return info
	}
	info.State = RuntimeStateReady
	info.Ownership = ownership
	if info.Ownership == "" {
		info.Ownership = RuntimeOwnershipUnknown
	}
	info.Fallback = false
	info.FallbackReason = ""
	info.Models = r.models(ctx, endpoint)
	if len(info.Models) == 0 {
		info.Models = r.modelsFromCLI(ctx, env)
	}
	return info
}

func (r *OpenCodeNativeRuntime) Execute(ctx context.Context, req NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	started := time.Now()
	var seq int64
	if strings.TrimSpace(req.Config.NativeServerURLs[string(RunnerOpenCode)]) == "" && !executableAvailable("opencode", req.Env) {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, fmt.Errorf("opencode binary is not installed")
	}
	endpoint, err := r.ensureServer(ctx, req.Config, req.Env)
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	emitNativeStructured(&seq, req.OnEvent, map[string]any{"type": "runtime.started", "runtime": "opencode-server", "endpoint": endpoint})
	sessionID := strings.TrimSpace(req.Chat.NativeSessionRef)
	if sessionID == "" {
		var session map[string]any
		if err := httpJSON(ctx, r.client, http.MethodPost, endpoint+"/session", map[string]any{}, &session); err != nil {
			return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
		}
		sessionID = firstNonEmpty(fmt.Sprint(session["id"]), fmt.Sprint(session["ID"]), fmt.Sprint(session["sessionID"]), fmt.Sprint(session["session_id"]))
		if sessionID == "<nil>" {
			sessionID = ""
		}
		if sessionID != "" {
			emitNativeStructured(&seq, req.OnEvent, map[string]any{"type": "session", "session_id": sessionID})
		}
	}
	if sessionID == "" {
		return ProcessOutput{ExitCode: -1, StderrPreview: "opencode did not return a session id", DurationMS: time.Since(started).Milliseconds()}, fmt.Errorf("opencode did not return a session id")
	}
	r.trackSession(req.Run.JobID, sessionID)
	defer r.untrackSession(req.Run.JobID)
	abortWatcherDone := make(chan struct{})
	defer close(abortWatcherDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = r.abortSession(context.Background(), endpoint, sessionID)
		case <-abortWatcherDone:
		}
	}()
	prompt := firstNonEmpty(req.Chat.UserMessage, req.Run.Task)
	messageBody := map[string]any{
		"parts": []map[string]any{{"type": "text", "text": prompt}},
	}
	if model := firstNonEmpty(req.Run.Model, req.Config.DefaultModels[string(RunnerOpenCode)]); model != "" {
		messageBody["model"] = r.openCodeModelRequest(ctx, endpoint, model, requestedThinkingLevel(req.Chat.Meta))
	}
	var response map[string]any
	if err := httpJSON(ctx, r.client, http.MethodPost, endpoint+"/session/"+sessionID+"/message", messageBody, &response); err != nil {
		return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
	}
	emitOpenCodeResponseEvents(&seq, req.OnEvent, sessionID, response)
	finalText := extractText(response)
	if finalText != "" && req.OnEvent != nil {
		req.OnEvent(textChunkEvent(&seq, finalText))
	}
	return ProcessOutput{ExitCode: 0, StdoutPreview: finalText, FinalTextPreview: finalText, DurationMS: time.Since(started).Milliseconds()}, nil
}

func (r *OpenCodeNativeRuntime) Abort(ctx context.Context, jobID string) error {
	r.mu.Lock()
	sessionID := r.activeSessions[jobID]
	endpoint := r.endpoint
	r.mu.Unlock()
	if sessionID == "" || endpoint == "" {
		return nil
	}
	return r.abortSession(ctx, endpoint, sessionID)
}

func (r *OpenCodeNativeRuntime) Stop(ctx context.Context) error {
	r.mu.Lock()
	cmd := r.cmd
	r.cmd = nil
	r.endpoint = ""
	r.ownership = RuntimeOwnershipNone
	r.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		return cmd.Wait()
	}
	return nil
}

func (r *OpenCodeNativeRuntime) ensureServer(ctx context.Context, cfg config.AgentCLIConfig, env []string) (string, error) {
	if configured := strings.TrimRight(strings.TrimSpace(cfg.NativeServerURLs[string(RunnerOpenCode)]), "/"); configured != "" {
		if err := r.health(ctx, configured); err == nil {
			r.mu.Lock()
			r.endpoint = configured
			r.cmd = nil
			r.ownership = RuntimeOwnershipExternal
			r.mu.Unlock()
			return configured, nil
		}
	}
	r.mu.Lock()
	endpoint := r.endpoint
	ownership := r.ownership
	r.mu.Unlock()
	if endpoint != "" && r.health(ctx, endpoint) == nil {
		return endpoint, nil
	}
	if endpoint != "" && ownership == RuntimeOwnershipExternal {
		r.mu.Lock()
		if r.endpoint == endpoint {
			r.endpoint = ""
			r.ownership = RuntimeOwnershipNone
		}
		r.mu.Unlock()
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return "", err
	}
	binary, err := ResolveExecutable("opencode", env)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(context.Background(), binary, "serve", "--hostname", "127.0.0.1", "--port", fmt.Sprintf("%d", port))
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", err
	}
	endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
	r.mu.Lock()
	r.endpoint = endpoint
	r.cmd = cmd
	r.ownership = RuntimeOwnershipManaged
	r.mu.Unlock()
	startup := time.Duration(cfg.NativeServerStartupSeconds) * time.Second
	if startup <= 0 {
		startup = 10 * time.Second
	}
	deadline := time.Now().Add(startup)
	for time.Now().Before(deadline) {
		if err := r.health(ctx, endpoint); err == nil {
			return endpoint, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	_ = r.Stop(context.Background())
	return "", fmt.Errorf("opencode server did not become healthy")
}

func (r *OpenCodeNativeRuntime) trackSession(jobID, sessionID string) {
	if jobID == "" || sessionID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activeSessions[jobID] = sessionID
}

func (r *OpenCodeNativeRuntime) untrackSession(jobID string) {
	if jobID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.activeSessions, jobID)
}

func (r *OpenCodeNativeRuntime) abortSession(ctx context.Context, endpoint, sessionID string) error {
	return httpJSON(ctx, r.client, http.MethodPost, endpoint+"/session/"+sessionID+"/abort", nil, nil)
}

func emitOpenCodeResponseEvents(seq *int64, onEvent func(AgentRunEvent), sessionID string, response map[string]any) {
	emitNativeStructured(seq, onEvent, map[string]any{"type": "message", "session_id": sessionID, "raw": response})
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if typ := firstNonEmpty(asString(v["type"]), asString(v["event"])); typ != "" {
				payload := make(map[string]any, len(v)+1)
				for key, item := range v {
					payload[key] = item
				}
				payload["type"] = normalizeOpenCodeNativeEventType(typ)
				payload["session_id"] = sessionID
				emitNativeStructured(seq, onEvent, payload)
			}
			for _, item := range v {
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(response)
}

func normalizeOpenCodeNativeEventType(raw string) string {
	switch strings.TrimSpace(raw) {
	case "permission", "permission.ask", "permission.requested", "permission.request":
		return "permission.asked"
	case "question", "question.ask", "question.requested", "question.request":
		return "question.asked"
	default:
		return raw
	}
}

func (r *OpenCodeNativeRuntime) health(ctx context.Context, endpoint string) error {
	return httpJSON(ctx, r.client, http.MethodGet, endpoint+"/global/health", nil, nil)
}

func (r *OpenCodeNativeRuntime) models(ctx context.Context, endpoint string) []RunnerModelInfo {
	var providers any
	if err := httpJSON(ctx, r.client, http.MethodGet, endpoint+"/config/providers", nil, &providers); err != nil {
		return nil
	}
	return flattenModelInfo(providers)
}

func (r *OpenCodeNativeRuntime) modelsFromCLI(ctx context.Context, env []string) []RunnerModelInfo {
	binary, err := ResolveExecutable("opencode", env)
	if err != nil {
		return nil
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(discoveryCtx, binary, "models", "--verbose")
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseOpenCodeModelsCLIOutput(output)
}

func (r *OpenCodeNativeRuntime) openCodeModelRequest(ctx context.Context, endpoint, model, thinking string) any {
	providerID, modelID := splitProviderModel(model)
	if thinking == "" {
		return model
	}
	for _, info := range r.models(ctx, endpoint) {
		if info.ID != modelID && info.ID != model {
			continue
		}
		if !stringInSlice(thinking, info.Reasoning) {
			continue
		}
		provider := firstNonEmpty(providerID, info.Provider)
		if provider == "" {
			return model
		}
		return map[string]any{"providerID": provider, "modelID": info.ID, "variant": thinking}
	}
	return model
}

// CodexNativeRuntime talks to codex app-server over stdio JSON-RPC.
type CodexNativeRuntime struct{}

func NewCodexNativeRuntime() *CodexNativeRuntime { return &CodexNativeRuntime{} }
func (r *CodexNativeRuntime) ID() RunnerID       { return RunnerCodex }

func (r *CodexNativeRuntime) Info(ctx context.Context, cfg config.AgentCLIConfig, env []string) RunnerRuntimeInfo {
	info := defaultRuntimeInfo(cfg, RunnerCodex, true)
	info.Kind = RuntimeNative
	if mode := runnerRuntimeMode(cfg, RunnerCodex); mode == RuntimeModeCLI {
		return defaultRuntimeInfo(cfg, RunnerCodex, true)
	}
	if !executableAvailable("codex", env) {
		info.State = RuntimeStateUnavailable
		info.Fallback = true
		info.FallbackReason = "codex binary is not installed"
		return info
	}
	info.State = RuntimeStateFallback
	info.Fallback = true
	info.FallbackReason = "codex app-server is started per turn"
	info.Ownership = RuntimeOwnershipManaged
	info.Models = r.models(ctx, cfg, env)
	if model := strings.TrimSpace(cfg.DefaultModels[string(RunnerCodex)]); model != "" {
		info.DefaultModel = model
		if len(info.Models) == 0 {
			info.Models = []RunnerModelInfo{{ID: model, DisplayName: model, Default: true}}
		} else {
			for i := range info.Models {
				if info.Models[i].ID == model {
					info.Models[i].Default = true
				}
			}
		}
	}
	return info
}

func (r *CodexNativeRuntime) models(ctx context.Context, cfg config.AgentCLIConfig, env []string) []RunnerModelInfo {
	binary, err := ResolveExecutable("codex", env)
	if err != nil {
		return nil
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(discoveryCtx, binary, "app-server", "--listen", "stdio://")
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return nil
	}
	if stderr != nil {
		go func() { _, _ = io.Copy(io.Discard, io.LimitReader(stderr, 65536)) }()
	}
	client := newCodexRPC(stdin, stdout)
	client.start(nil, nil)
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); client.close() }()
	if _, err := client.call(discoveryCtx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "or3-intern", "version": "native-runner"}}); err != nil {
		return nil
	}
	_ = client.notify("initialized", map[string]any{})
	models := []RunnerModelInfo{}
	params := map[string]any{"limit": 200, "includeHidden": false}
	for pages := 0; pages < 5; pages++ {
		resp, err := client.call(discoveryCtx, "model/list", params)
		if err != nil {
			return models
		}
		models = append(models, codexModelListToRunnerModels(resp)...)
		next, _ := resp["nextCursor"].(string)
		if strings.TrimSpace(next) == "" {
			break
		}
		params["cursor"] = next
	}
	return dedupeRunnerModels(models)
}

func (r *CodexNativeRuntime) Execute(ctx context.Context, req NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	started := time.Now()
	var seq int64
	binary, err := ResolveExecutable("codex", req.Env)
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	cmd.Env = req.Env
	cmd.Dir = req.Run.Cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	if err := cmd.Start(); err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	client := newCodexRPC(stdin, stdout)
	approvalRequired := atomic.Bool{}
	stderrDone := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(io.LimitReader(stderr, 65536))
		stderrDone <- string(data)
	}()
	client.start(func(method string, params map[string]any) {
		emitNativeStructured(&seq, req.OnEvent, map[string]any{"type": method, "raw": params})
		if delta := extractText(params); delta != "" && req.OnEvent != nil {
			req.OnEvent(textChunkEvent(&seq, delta))
		}
	}, func(id int64, method string, params map[string]any) map[string]any {
		approvalRequired.Store(true)
		emitNativeStructured(&seq, req.OnEvent, map[string]any{"type": method, "method": method, "params": params, "request_id": id})
		select {
		case client.turnDone <- errNativeApprovalRequired:
		default:
		}
		return map[string]any{"error": map[string]any{"code": -32001, "message": "approval required in OR3"}}
	})
	defer client.close()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if _, err := client.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "or3-intern", "version": "native-runner"}}); err != nil {
		return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
	}
	_ = client.notify("initialized", map[string]any{})
	threadParams := map[string]any{"cwd": req.Run.Cwd}
	var threadResp map[string]any
	if req.Chat.NativeSessionRef != "" && req.Chat.ContinuationMode == ContinuationNative {
		resumed, err := client.call(ctx, "thread/resume", map[string]any{"threadId": req.Chat.NativeSessionRef, "cwd": req.Run.Cwd})
		if err == nil {
			threadResp = resumed
			threadParams = nil
		}
	}
	if threadParams != nil {
		if model := firstNonEmpty(req.Run.Model, req.Config.DefaultModels[string(RunnerCodex)]); model != "" {
			threadParams["model"] = model
		}
	}
	if threadResp != nil {
		// Resumed above.
	} else if threadParams == nil {
		threadResp = map[string]any{"threadId": req.Chat.NativeSessionRef}
	} else {
		threadResp, err = client.call(ctx, "thread/start", threadParams)
		if err != nil {
			return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
		}
	}
	threadID := firstNonEmpty(fmt.Sprint(threadResp["threadId"]), fmt.Sprint(threadResp["thread_id"]), req.Chat.NativeSessionRef)
	if threadID == "<nil>" || threadID == "" {
		threadID = req.Chat.NativeSessionRef
	}
	emitNativeStructured(&seq, req.OnEvent, map[string]any{"type": "thread.started", "thread_id": threadID, "raw": threadResp})
	turnParams := map[string]any{"threadId": threadID, "input": req.Chat.UserMessage, "cwd": req.Run.Cwd}
	selectedModel := firstNonEmpty(req.Run.Model, req.Config.DefaultModels[string(RunnerCodex)])
	if model := selectedModel; model != "" {
		turnParams["model"] = model
	}
	if thinking := requestedThinkingLevel(req.Chat.Meta); thinking != "" {
		if r.codexSupportsEffort(ctx, client, selectedModel, thinking) {
			turnParams["effort"] = thinking
		}
	}
	if err := addCodexPolicies(turnParams, req.Run); err != nil {
		return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
	}
	if permission, ok := runnerPermissionFromMeta(req.Chat.Meta); ok && permission.Access == runnerPermissionAccessWrite {
		turnParams["writableRoots"] = []string{permission.TargetPath}
	}
	if _, err := client.call(ctx, "turn/start", turnParams); err != nil {
		if approvalRequired.Load() {
			return ProcessOutput{ExitCode: -1, StderrPreview: errNativeApprovalRequired.Error(), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
		}
		return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
	}
	if err := client.waitForTurn(ctx); err != nil {
		if approvalRequired.Load() {
			return ProcessOutput{ExitCode: -1, StderrPreview: errNativeApprovalRequired.Error(), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
		}
		stderrText := ""
		select {
		case stderrText = <-stderrDone:
		default:
		}
		return ProcessOutput{ExitCode: -1, StderrPreview: firstNonEmpty(stderrText, err.Error()), DurationMS: time.Since(started).Milliseconds()}, err
	}
	final := client.finalText()
	stderrText := ""
	select {
	case stderrText = <-stderrDone:
	default:
	}
	return ProcessOutput{ExitCode: 0, StdoutPreview: final, StderrPreview: stderrText, FinalTextPreview: final, DurationMS: time.Since(started).Milliseconds()}, nil
}

func (r *CodexNativeRuntime) Abort(ctx context.Context, jobID string) error { return nil }
func (r *CodexNativeRuntime) Stop(ctx context.Context) error                { return nil }

func (r *CodexNativeRuntime) codexSupportsEffort(ctx context.Context, client *codexRPC, modelID, effort string) bool {
	modelID = strings.TrimSpace(modelID)
	effort = strings.ToLower(strings.TrimSpace(effort))
	if modelID == "" || effort == "" {
		return false
	}
	resp, err := client.call(ctx, "model/list", map[string]any{"limit": 200, "includeHidden": false})
	if err != nil {
		return false
	}
	for _, model := range codexModelListToRunnerModels(resp) {
		if model.ID == modelID && stringInSlice(effort, model.Reasoning) {
			return true
		}
	}
	return false
}

func addCodexPolicies(params map[string]any, run db.AgentCLIRun) error {
	switch RunnerMode(run.Mode) {
	case RunnerModeReview:
		params["approvalPolicy"] = "untrusted"
	case RunnerModeSafeEdit:
		params["approvalPolicy"] = "on-request"
	case RunnerModeSandboxAuto:
		params["approvalPolicy"] = "never"
	}
	switch RunIsolation(run.Isolation) {
	case IsolationHostReadOnly:
		params["sandboxPolicy"] = map[string]any{"mode": "read-only"}
	case IsolationHostWorkspaceWrite, IsolationSandboxWrite:
		params["sandboxPolicy"] = map[string]any{"mode": "workspace-write"}
	case IsolationSandboxDangerous:
		params["sandboxPolicy"] = map[string]any{"mode": "danger-full-access"}
	default:
		return fmt.Errorf("unsupported isolation %q", run.Isolation)
	}
	return nil
}

type codexRPC struct {
	stdin        io.WriteCloser
	scanner      *bufio.Scanner
	mu           sync.Mutex
	nextID       int64
	pending      map[int64]chan rpcResponse
	done         chan struct{}
	turnDone     chan error
	turnComplete atomic.Bool
	textMu       sync.Mutex
	text         strings.Builder
}

type rpcResponse struct {
	Result map[string]any
	Err    error
}

func newCodexRPC(stdin io.WriteCloser, stdout io.Reader) *codexRPC {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &codexRPC{stdin: stdin, scanner: scanner, pending: map[int64]chan rpcResponse{}, done: make(chan struct{}), turnDone: make(chan error, 1)}
}

func (c *codexRPC) start(onNotification func(string, map[string]any), onRequest func(int64, string, map[string]any) map[string]any) {
	go func() {
		defer close(c.done)
		for c.scanner.Scan() {
			var msg map[string]any
			if err := json.Unmarshal(c.scanner.Bytes(), &msg); err != nil {
				continue
			}
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			if id, ok := numberID(msg["id"]); ok {
				if method != "" && onRequest != nil {
					_ = c.write(map[string]any{"id": id, "jsonrpc": "2.0", "result": onRequest(id, method, params)})
					continue
				}
				c.handleResponse(id, msg)
				continue
			}
			if onNotification != nil && method != "" {
				onNotification(method, params)
			}
			if delta := extractText(params); delta != "" {
				c.textMu.Lock()
				c.text.WriteString(delta)
				c.textMu.Unlock()
			}
			if method == "turn/completed" || method == "turn/completed/notification" {
				c.turnComplete.Store(true)
				select {
				case c.turnDone <- nil:
				default:
				}
			}
		}
		if err := c.scanner.Err(); err != nil {
			c.failAll(err)
			select {
			case c.turnDone <- err:
			default:
			}
		} else {
			c.failAll(io.EOF)
			select {
			case c.turnDone <- io.EOF:
			default:
			}
		}
	}()
}

func (c *codexRPC) call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		return resp.Result, resp.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *codexRPC) notify(method string, params map[string]any) error {
	return c.write(map[string]any{"method": method, "params": params})
}

func (c *codexRPC) write(msg map[string]any) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.stdin.Write(append(raw, '\n'))
	return err
}

func (c *codexRPC) handleResponse(id int64, msg map[string]any) {
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch == nil {
		return
	}
	if rawErr, ok := msg["error"]; ok && rawErr != nil {
		ch <- rpcResponse{Err: fmt.Errorf("codex rpc error: %v", rawErr)}
		return
	}
	result, _ := msg["result"].(map[string]any)
	ch <- rpcResponse{Result: result}
}

func (c *codexRPC) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- rpcResponse{Err: err}
	}
}

func (c *codexRPC) waitForTurn(ctx context.Context) error {
	select {
	case err := <-c.turnDone:
		if errors.Is(err, io.EOF) && c.turnComplete.Load() {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *codexRPC) finalText() string {
	c.textMu.Lock()
	defer c.textMu.Unlock()
	return c.text.String()
}

func (c *codexRPC) close() {
	_ = c.stdin.Close()
	<-c.done
}

func numberID(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

func extractText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"text", "delta", "content", "message", "final_text", "finalText"} {
			if text, ok := v[key].(string); ok && text != "" {
				return text
			}
		}
		if parts, ok := v["parts"].([]any); ok {
			var out strings.Builder
			for _, part := range parts {
				out.WriteString(extractText(part))
			}
			return out.String()
		}
		if item, ok := v["item"].(map[string]any); ok {
			return extractText(item)
		}
		if raw, ok := v["raw"].(map[string]any); ok {
			return extractText(raw)
		}
	case []any:
		var out strings.Builder
		for _, item := range v {
			out.WriteString(extractText(item))
		}
		return out.String()
	}
	return ""
}

func flattenModelInfo(value any) []RunnerModelInfo {
	models := []RunnerModelInfo{}
	defaults := map[string]string{}
	if root, ok := value.(map[string]any); ok {
		if rawDefaults, ok := root["default"].(map[string]any); ok {
			for provider, model := range rawDefaults {
				if text := asString(model); text != "" {
					defaults[provider] = text
				}
			}
		}
	}
	var walk func(any, string, string)
	walk = func(v any, provider string, providerName string) {
		switch x := v.(type) {
		case map[string]any:
			providerID := firstNonEmpty(asString(x["providerID"]), asString(x["provider_id"]), provider)
			if rawModels, ok := x["models"].(map[string]any); ok {
				nextProvider := firstNonEmpty(asString(x["id"]), asString(x["name"]), providerID)
				nextProviderName := firstNonEmpty(asString(x["name"]), providerName, nextProvider)
				for modelID, modelValue := range rawModels {
					if modelMap, ok := modelValue.(map[string]any); ok {
						models = append(models, openCodeModelMapToRunnerModel(modelID, nextProvider, nextProviderName, defaults[nextProvider], modelMap))
					} else if modelID != "" {
						models = append(models, RunnerModelInfo{ID: modelID, DisplayName: modelID, Provider: nextProvider, ProviderName: nextProviderName, Default: defaults[nextProvider] == modelID})
					}
				}
				return
			}
			id := firstNonEmpty(asString(x["id"]), asString(x["model"]), asString(x["name"]))
			if id != "" && provider != "" {
				models = append(models, openCodeModelMapToRunnerModel(id, providerID, providerName, defaults[providerID], x))
			}
			nextProvider := firstNonEmpty(provider, asString(x["id"]), asString(x["name"]))
			nextProviderName := firstNonEmpty(providerName, asString(x["name"]), nextProvider)
			for key, child := range x {
				childProvider := provider
				childProviderName := providerName
				if key == "models" {
					childProvider = nextProvider
					childProviderName = nextProviderName
				}
				walk(child, childProvider, childProviderName)
			}
		case []any:
			for _, item := range x {
				walk(item, provider, providerName)
			}
		}
	}
	walk(value, "", "")
	return dedupeRunnerModels(models)
}

func openCodeModelMapToRunnerModel(id, provider, providerName, defaultID string, x map[string]any) RunnerModelInfo {
	reasoning := variantKeys(x["variants"])
	return RunnerModelInfo{
		ID:           firstNonEmpty(asString(x["id"]), asString(x["model"]), id),
		DisplayName:  firstNonEmpty(asString(x["name"]), asString(x["displayName"]), asString(x["display_name"]), id),
		Provider:     firstNonEmpty(asString(x["providerID"]), asString(x["provider_id"]), provider),
		ProviderName: firstNonEmpty(providerName, openCodeProviderDisplayName(firstNonEmpty(asString(x["providerID"]), asString(x["provider_id"]), provider))),
		Default:      defaultID != "" && defaultID == firstNonEmpty(asString(x["id"]), asString(x["model"]), id),
		Reasoning:    reasoning,
	}
}

func parseOpenCodeModelsCLIOutput(output []byte) []RunnerModelInfo {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	models := []RunnerModelInfo{}
	provider := ""
	modelID := ""
	var object strings.Builder
	braceDepth := 0
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if braceDepth == 0 {
			if before, after, ok := strings.Cut(trimmed, "/"); ok && before != "" && after != "" && !strings.HasPrefix(trimmed, "{") {
				provider = before
				modelID = after
				continue
			}
			if trimmed != "{" || provider == "" || modelID == "" {
				continue
			}
			object.Reset()
		}
		if braceDepth > 0 || trimmed == "{" {
			object.WriteString(line)
			object.WriteByte('\n')
			braceDepth += strings.Count(line, "{")
			braceDepth -= strings.Count(line, "}")
			if braceDepth == 0 {
				var raw map[string]any
				if err := json.Unmarshal([]byte(object.String()), &raw); err == nil {
					models = append(models, openCodeModelMapToRunnerModel(modelID, provider, openCodeProviderDisplayName(provider), "", raw))
				}
				provider = ""
				modelID = ""
			}
		}
	}
	return dedupeRunnerModels(models)
}

func openCodeProviderDisplayName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "opencode":
		return "OpenCode Zen"
	case "opencode-go":
		return "OpenCode Go"
	case "kimi-for-coding":
		return "Kimi For Coding"
	case "openai":
		return "OpenAI"
	}
	return provider
}

func codexModelListToRunnerModels(resp map[string]any) []RunnerModelInfo {
	items, _ := resp["data"].([]any)
	out := make([]RunnerModelInfo, 0, len(items))
	for _, item := range items {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := firstNonEmpty(asString(model["model"]), asString(model["id"]))
		if id == "" {
			continue
		}
		out = append(out, RunnerModelInfo{
			ID:               id,
			DisplayName:      firstNonEmpty(asString(model["displayName"]), asString(model["display_name"]), id),
			Provider:         firstNonEmpty(asString(model["modelProvider"]), asString(model["model_provider"]), "openai"),
			ProviderName:     "OpenAI Codex",
			Default:          boolField(model, "isDefault") || boolField(model, "default"),
			Reasoning:        codexReasoningOptions(model["supportedReasoningEfforts"]),
			ReasoningDefault: asString(model["defaultReasoningEffort"]),
		})
	}
	return out
}

func codexReasoningOptions(value any) []string {
	items, _ := value.([]any)
	out := []string{}
	for _, item := range items {
		if text := asString(item); text != "" {
			out = append(out, text)
			continue
		}
		if obj, ok := item.(map[string]any); ok {
			if text := asString(obj["reasoningEffort"]); text != "" {
				out = append(out, text)
			}
		}
	}
	return sortedUniqueStrings(out)
}

func variantKeys(value any) []string {
	variants, ok := value.(map[string]any)
	if !ok || len(variants) == 0 {
		return nil
	}
	keys := make([]string, 0, len(variants))
	for key := range variants {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	return sortedUniqueStrings(keys)
}

func dedupeRunnerModels(models []RunnerModelInfo) []RunnerModelInfo {
	seen := map[string]int{}
	out := []RunnerModelInfo{}
	for _, model := range models {
		if model.ID == "" {
			continue
		}
		key := model.Provider + "/" + model.ID
		if idx, ok := seen[key]; ok {
			if out[idx].DisplayName == "" {
				out[idx].DisplayName = model.DisplayName
			}
			if len(out[idx].Reasoning) == 0 {
				out[idx].Reasoning = model.Reasoning
			}
			if out[idx].ReasoningDefault == "" {
				out[idx].ReasoningDefault = model.ReasoningDefault
			}
			out[idx].Default = out[idx].Default || model.Default
			continue
		}
		seen[key] = len(out)
		out = append(out, model)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName)
	})
	return out
}

func requestedThinkingLevel(meta map[string]any) string {
	for _, key := range []string{"runner_thinking_level", "runner_reasoning_effort", "thinking_level", "reasoning_effort"} {
		if value := strings.ToLower(strings.TrimSpace(stringMeta(meta, key))); value != "" {
			return value
		}
	}
	return ""
}

func splitProviderModel(model string) (string, string) {
	provider, id, ok := strings.Cut(strings.TrimSpace(model), "/")
	if ok && provider != "" && id != "" {
		return provider, id
	}
	return "", strings.TrimSpace(model)
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftRank, leftKnown := reasoningRank(out[i])
		rightRank, rightKnown := reasoningRank(out[j])
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return out[i] < out[j]
	})
	return out
}

func reasoningRank(value string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "off", "disabled":
		return 0, true
	case "minimal":
		return 1, true
	case "low":
		return 2, true
	case "medium", "med", "normal":
		return 3, true
	case "high":
		return 4, true
	case "xhigh", "extra-high", "extra_high":
		return 5, true
	case "max", "maximum":
		return 6, true
	}
	return 100, false
}

func stringInSlice(value string, values []string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range values {
		if strings.ToLower(strings.TrimSpace(candidate)) == value {
			return true
		}
	}
	return false
}

func boolField(record map[string]any, key string) bool {
	value, _ := record[key].(bool)
	return value
}

func asString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func nativeEnv(cfg config.AgentCLIConfig) []string {
	return BuildAgentCLIEnv(os.Environ(), cfg.ChildEnvAllowlist, nil)
}
