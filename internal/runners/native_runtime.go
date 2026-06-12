package runners

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
	Run     db.RunnerRun
	Chat    RunnerChatCommandRequest
	Config  config.RunnersConfig
	Env     []string
	OnEvent func(RunnerRunEvent)
}

var errNativeApprovalRequired = errors.New("native runner approval required")

// NativeRunnerRuntime executes a runner turn through its local server/runtime API.
type NativeRunnerRuntime interface {
	ID() RunnerID
	Info(ctx context.Context, cfg config.RunnersConfig, env []string) RunnerRuntimeInfo
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

func runnerRuntimeMode(cfg config.RunnersConfig, id RunnerID) RunnerRuntimeMode {
	mode := strings.ToLower(strings.TrimSpace(cfg.RuntimeMode[string(id)]))
	switch RunnerRuntimeMode(mode) {
	case RuntimeModeNative, RuntimeModeCLI, RuntimeModeAuto:
		return RunnerRuntimeMode(mode)
	default:
		return RuntimeModeAuto
	}
}

func defaultRuntimeInfo(cfg config.RunnersConfig, id RunnerID, hasNative bool) RunnerRuntimeInfo {
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

func buildRuntimeChatRequest(run db.RunnerRun) (RunnerChatCommandRequest, bool) {
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

func runtimeEvent(seq *int64, eventType string, payload map[string]any) RunnerRunEvent {
	raw, _ := json.Marshal(payload)
	return RunnerRunEvent{
		Type:    eventType,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Seq:     atomic.AddInt64(seq, 1),
		Payload: raw,
	}
}

func textChunkEvent(seq *int64, chunk string) RunnerRunEvent {
	return RunnerRunEvent{
		Type:   "output",
		TS:     time.Now().UTC().Format(time.RFC3339Nano),
		Seq:    atomic.AddInt64(seq, 1),
		Stream: "stdout",
		Chunk:  chunk,
	}
}

func emitNativeStructured(seq *int64, onEvent func(RunnerRunEvent), payload map[string]any) {
	if onEvent == nil {
		return
	}
	onEvent(runtimeEvent(seq, "structured", payload))
}

func emitCodexNotificationStructured(seq *int64, onEvent func(RunnerRunEvent), method string, params map[string]any) {
	payload := map[string]any{
		"type":   method,
		"method": method,
		"params": params,
		"raw":    params,
	}
	for key, value := range params {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	emitNativeStructured(seq, onEvent, payload)
}

func runtimeVersionFromBinary(ctx context.Context, env []string, binary string, args ...string) string {
	path, err := ResolveExecutable(binary, env)
	if err != nil {
		return ""
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return firstLine(out)
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
	activeRequests map[string]NativeRequestRef
	lifecycle      *openCodeLifecycle
}

func NewOpenCodeNativeRuntime() *OpenCodeNativeRuntime {
	return &OpenCodeNativeRuntime{client: &http.Client{}, activeSessions: map[string]string{}, activeRequests: map[string]NativeRequestRef{}}
}

// Compile-time assertion that OpenCodeNativeRuntime supports request responses.
var _ NativeRequestResponder = (*OpenCodeNativeRuntime)(nil)
var _ NativeTurnContinuer = (*OpenCodeNativeRuntime)(nil)

func (r *OpenCodeNativeRuntime) ID() RunnerID { return RunnerOpenCode }

func (r *OpenCodeNativeRuntime) Info(ctx context.Context, cfg config.RunnersConfig, env []string) RunnerRuntimeInfo {
	info := defaultRuntimeInfo(cfg, RunnerOpenCode, true)
	info.Kind = RuntimeNative
	info.Version = firstNonEmpty(info.Version, runtimeVersionFromBinary(ctx, env, "opencode", "--version"))
	if mode := runnerRuntimeMode(cfg, RunnerOpenCode); mode == RuntimeModeCLI {
		return defaultRuntimeInfo(cfg, RunnerOpenCode, true)
	}
	if configured := strings.TrimRight(strings.TrimSpace(cfg.NativeServerURLs[string(RunnerOpenCode)]), "/"); configured != "" {
		if r.health(ctx, configured) == nil {
			info.Endpoint = configured
			info.State = RuntimeStateReady
			info.Ownership = RuntimeOwnershipExternal
			info.Fallback = false
			info.FallbackReason = ""
			info.NextAction = openCodeExternalReadyAction()
			if inventory, err := openCodeInventoryFromServer(ctx, r.client, configured); err == nil {
				info.Models = inventory.Models
				info.Providers = inventory.Providers
				info.Agents = inventory.Agents
			} else {
				info.Models = r.models(ctx, configured)
			}
			health := RunnerNativeHealth{Reachable: true, Endpoint: configured, StartedAt: time.Now().UnixMilli(), LastCheckedAt: time.Now().UnixMilli()}
			info.Health = &health
			return info
		}
		info.State = RuntimeStateError
		info.Fallback = true
		info.FallbackReason = "external opencode server is unreachable"
		info.Message = "configured opencode server did not respond to /global/health"
		info.NextAction = "verify the configured opencode server is running and reachable"
		return info
	}
	if !executableAvailable("opencode", env) {
		info.State = RuntimeStateUnavailable
		info.Fallback = true
		info.FallbackReason = "opencode binary is not installed"
		info.NextAction = "install opencode or set nativeServerUrls.opencode to an existing server"
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
		info.NextAction = "send a message and the managed server will start on demand"
		info.Models = r.modelsFromCLI(ctx, env)
		return info
	}
	info.Endpoint = endpoint
	if err := r.health(ctx, endpoint); err != nil {
		info.State = RuntimeStateError
		info.Message = err.Error()
		info.Fallback = true
		info.FallbackReason = "health check failed"
		info.NextAction = "the managed server will be restarted on the next turn"
		return info
	}
	info.State = RuntimeStateReady
	info.Ownership = ownership
	if info.Ownership == "" {
		info.Ownership = RuntimeOwnershipUnknown
	}
	info.Fallback = false
	info.FallbackReason = ""
	if inventory, err := openCodeInventoryFromServer(ctx, r.client, endpoint); err == nil {
		info.Models = inventory.Models
		info.Providers = inventory.Providers
		info.Agents = inventory.Agents
	}
	if len(info.Models) == 0 {
		info.Models = r.modelsFromCLI(ctx, env)
	}
	health, _, _ := r.lifecycleSnapshot()
	info.Health = &health
	return info
}

func openCodeExternalReadyAction() string {
	return "external opencode server is ready; OR3 will not manage its lifecycle"
}

func (r *OpenCodeNativeRuntime) lifecycleSnapshot() (RunnerNativeHealth, string, bool) {
	if r.lifecycle == nil {
		return RunnerNativeHealth{}, "", false
	}
	return r.lifecycle.snapshot()
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
	sessionID := ""
	if req.Chat.ContinuationMode == ContinuationNative {
		sessionID = strings.TrimSpace(req.Chat.NativeSessionRef)
	}
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
	messageCtx, cancelMessage := context.WithCancel(ctx)
	defer cancelMessage()
	approvalRequired := atomic.Bool{}
	onEvent := func(e RunnerRunEvent) {
		if _, ok := detectOpenCodePermissionRequest(e); ok {
			approvalRequired.Store(true)
			cancelMessage()
		}
		if ref, ok := detectOpenCodePermissionRequestRef(e, sessionID); ok {
			r.trackRequest(ref)
		}
		if req.OnEvent != nil {
			req.OnEvent(e)
		}
	}
	prompt := ChatExecutionInput(req.Chat, req.Run.Task)
	messageBody := map[string]any{
		"parts": []map[string]any{{"type": "text", "text": prompt}},
	}
	if model := firstNonEmpty(req.Run.Model, req.Config.DefaultModels[string(RunnerOpenCode)]); model != "" {
		mergeOpenCodeModelIntoBody(messageBody, r.openCodeModelRequest(ctx, endpoint, req.Env, model, requestedThinkingLevel(req.Chat.Meta)))
	}

	streamState := newOpenCodeStreamState()
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_ = streamOpenCodeGlobalEvents(streamCtx, r.client, endpoint, sessionID, onEvent, &seq, streamState)
	}()

	var response map[string]any
	messageErr := httpJSON(messageCtx, r.client, http.MethodPost, endpoint+"/session/"+sessionID+"/message", messageBody, &response)

	cancelStream()
	select {
	case <-streamDone:
	case <-time.After(750 * time.Millisecond):
	}

	if messageErr != nil {
		if approvalRequired.Load() {
			return ProcessOutput{ExitCode: -1, StderrPreview: errNativeApprovalRequired.Error(), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
		}
		return ProcessOutput{ExitCode: -1, StderrPreview: messageErr.Error(), DurationMS: time.Since(started).Milliseconds()}, messageErr
	}
	if !streamState.streamedText.Load() {
		emitOpenCodeResponseEvents(&seq, onEvent, sessionID, response)
	}
	if approvalRequired.Load() {
		return ProcessOutput{ExitCode: -1, StderrPreview: errNativeApprovalRequired.Error(), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
	}
	if errMsg := extractOpenCodeErrorMessage(response); errMsg != "" {
		return ProcessOutput{ExitCode: 1, StderrPreview: errMsg, DurationMS: time.Since(started).Milliseconds()}, fmt.Errorf("%s", errMsg)
	}
	finalText := extractOpenCodeVisibleText(response)
	if finalText != "" && !streamState.streamedText.Load() {
		onEvent(textChunkEvent(&seq, finalText))
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
	lc := r.lifecycle
	r.lifecycle = nil
	r.cmd = nil
	r.endpoint = ""
	r.ownership = RuntimeOwnershipNone
	r.mu.Unlock()
	if lc == nil {
		return nil
	}
	return lc.Stop(ctx)
}

// RespondToNativeRequest sends the user's decision back to the running
// opencode server. Returns an error when no managed server exists or the
// request id is no longer tracked.
func (r *OpenCodeNativeRuntime) RespondToNativeRequest(ctx context.Context, ref NativeRequestRef, decision NativeRequestDecision) error {
	r.mu.Lock()
	endpoint := r.endpoint
	if r.activeRequests != nil {
		if tracked, ok := r.activeRequests[ref.RequestID]; ok {
			if ref.SessionID == "" {
				ref.SessionID = tracked.SessionID
			}
			if ref.ThreadID == "" {
				ref.ThreadID = tracked.ThreadID
			}
			if ref.Summary == "" {
				ref.Summary = tracked.Summary
			}
		}
	}
	r.mu.Unlock()
	if endpoint == "" {
		return errors.New("opencode server is not running")
	}
	// OpenCode's HTTP API exposes a permission reply endpoint. The
	// sessionId is required; permissionId identifies the request.
	sessionID := firstNonEmpty(ref.SessionID, ref.ThreadID)
	if sessionID == "" {
		return errors.New("opencode request is missing session id")
	}
	body := map[string]any{"response": decision.Decision}
	if strings.EqualFold(decision.Decision, "reject") || strings.EqualFold(decision.Decision, "deny") {
		body["response"] = "denied"
	}
	if strings.EqualFold(decision.Decision, "approve") {
		body["response"] = "always"
	}
	if decision.Message != "" {
		body["message"] = decision.Message
	}
	// Best-effort: try a few plausible endpoints. The server may have
	// renamed the route across versions; the chat manager handles failures
	// by falling back to the existing approval-token retry.
	candidates := []string{
		fmt.Sprintf("%s/session/%s/permissions/%s", endpoint, sessionID, ref.RequestID),
		fmt.Sprintf("%s/permissions/%s", endpoint, ref.RequestID),
	}
	for _, url := range candidates {
		_, err := httpPostJSON(ctx, r.client, url, body, nil)
		if err == nil {
			r.untrackRequest(ref.RequestID)
			return nil
		}
	}
	return errors.New("opencode could not accept the request response")
}

// ContinuePendingTurn waits for assistant output after the user approved a
// permission request. The session must still be tracked for the same job id.
func (r *OpenCodeNativeRuntime) ContinuePendingTurn(ctx context.Context, req NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	started := time.Now()
	var seq int64
	endpoint, err := r.ensureServer(ctx, req.Config, req.Env)
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	r.mu.Lock()
	sessionID := r.activeSessions[req.Run.JobID]
	r.mu.Unlock()
	if sessionID == "" {
		sessionID = strings.TrimSpace(req.Chat.NativeSessionRef)
	}
	if sessionID == "" {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, fmt.Errorf("opencode session not found for job %s", req.Run.JobID)
	}
	approvalRequired := atomic.Bool{}
	onEvent := func(e RunnerRunEvent) {
		if _, ok := detectOpenCodePermissionRequest(e); ok {
			approvalRequired.Store(true)
		}
		if ref, ok := detectOpenCodePermissionRequestRef(e, sessionID); ok {
			r.trackRequest(ref)
		}
		if req.OnEvent != nil {
			req.OnEvent(e)
		}
	}
	streamState := newOpenCodeStreamState()
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		_ = streamOpenCodeGlobalEvents(streamCtx, r.client, endpoint, sessionID, onEvent, &seq, streamState)
	}()
	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelWait()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			cancelStream()
			select {
			case <-streamDone:
			case <-time.After(750 * time.Millisecond):
			}
			if approvalRequired.Load() {
				return ProcessOutput{ExitCode: -1, StderrPreview: errNativeApprovalRequired.Error(), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
			}
			if err := waitCtx.Err(); err != nil {
				return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
			}
			return ProcessOutput{ExitCode: -1, StderrPreview: "timed out waiting for opencode turn", DurationMS: time.Since(started).Milliseconds()}, fmt.Errorf("timed out waiting for opencode turn")
		case <-ticker.C:
			if approvalRequired.Load() {
				cancelStream()
				select {
				case <-streamDone:
				case <-time.After(750 * time.Millisecond):
				}
				return ProcessOutput{ExitCode: -1, StderrPreview: errNativeApprovalRequired.Error(), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
			}
			if streamState.streamedText.Load() {
				cancelStream()
				select {
				case <-streamDone:
				case <-time.After(750 * time.Millisecond):
				}
				finalText := openCodeStreamFinalText(streamState)
				if finalText != "" {
					onEvent(textChunkEvent(&seq, finalText))
				}
				return ProcessOutput{ExitCode: 0, StdoutPreview: finalText, FinalTextPreview: finalText, DurationMS: time.Since(started).Milliseconds()}, nil
			}
		}
	}
}

func openCodeStreamFinalText(state *openCodeStreamState) string {
	if state == nil {
		return ""
	}
	var parts []string
	for _, text := range state.lastTextByPart {
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// httpPostJSON is a small helper for the respond path; falls back to
// the package-level httpJSON for compatibility.
func httpPostJSON(ctx context.Context, client *http.Client, url string, body any, out any) (int, error) {
	if err := httpJSON(ctx, client, http.MethodPost, url, body, out); err != nil {
		return 0, err
	}
	return http.StatusOK, nil
}

func (r *OpenCodeNativeRuntime) ensureServer(ctx context.Context, cfg config.RunnersConfig, env []string) (string, error) {
	if configured := strings.TrimRight(strings.TrimSpace(cfg.NativeServerURLs[string(RunnerOpenCode)]), "/"); configured != "" {
		if err := r.health(ctx, configured); err == nil {
			r.mu.Lock()
			r.endpoint = configured
			r.cmd = nil
			r.ownership = RuntimeOwnershipExternal
			if r.lifecycle != nil && r.lifecycle.isExternal() {
				_ = r.lifecycle.Stop(ctx)
				r.lifecycle = nil
			}
			r.mu.Unlock()
			return configured, nil
		}
		return "", fmt.Errorf("configured opencode server %s is not reachable", configured)
	}
	r.mu.Lock()
	endpoint := r.endpoint
	lc := r.lifecycle
	r.mu.Unlock()
	if endpoint != "" && lc != nil && !lc.isExternal() && r.health(ctx, endpoint) == nil {
		lc.touch()
		return endpoint, nil
	}
	if endpoint == "" {
		binary, err := ResolveExecutable("opencode", env)
		if err != nil {
			return "", err
		}
		idleTimeout := time.Duration(cfg.NativeServerIdleSeconds) * time.Second
		if idleTimeout <= 0 {
			idleTimeout = 5 * time.Minute
		}
		lc = newOpenCodeLifecycle(RuntimeOwnershipManaged, idleTimeout)
		start := openCodeStart(ctx, binary, openCodeStartOptions{
			Env:           env,
			StartupBudget: time.Duration(cfg.NativeServerStartupSeconds) * time.Second,
		})
		if start.Error != "" {
			_ = lc.Stop(context.Background())
			return "", fmt.Errorf("%s: %s", start.Error, strings.TrimSpace(start.Output))
		}
		lc.adoptProcess(start.Process, start.URL)
		r.mu.Lock()
		r.endpoint = start.URL
		r.cmd = start.Process.cmd
		r.ownership = RuntimeOwnershipManaged
		r.lifecycle = lc
		r.mu.Unlock()
		return start.URL, nil
	}
	// Existing endpoint but unhealthy: clean up and try again.
	if lc != nil {
		_ = lc.Stop(context.Background())
	}
	r.mu.Lock()
	r.endpoint = ""
	r.cmd = nil
	r.ownership = RuntimeOwnershipNone
	r.lifecycle = nil
	r.mu.Unlock()
	return r.ensureServer(ctx, cfg, env)
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

// trackRequest records a pending native request so the chat manager can
// later respond to it via RespondToNativeRequest.
func (r *OpenCodeNativeRuntime) trackRequest(ref NativeRequestRef) {
	if ref.RequestID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeRequests == nil {
		r.activeRequests = map[string]NativeRequestRef{}
	}
	r.activeRequests[ref.RequestID] = ref
}

// untrackRequest removes a tracked native request.
func (r *OpenCodeNativeRuntime) untrackRequest(requestID string) {
	if requestID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.activeRequests, requestID)
}

// listRequests returns the tracked request refs.
func (r *OpenCodeNativeRuntime) listRequests() []NativeRequestRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]NativeRequestRef, 0, len(r.activeRequests))
	for _, ref := range r.activeRequests {
		out = append(out, ref)
	}
	return out
}

func (r *OpenCodeNativeRuntime) abortSession(ctx context.Context, endpoint, sessionID string) error {
	return httpJSON(ctx, r.client, http.MethodPost, endpoint+"/session/"+sessionID+"/abort", nil, nil)
}

func (r *OpenCodeNativeRuntime) abortSessionBestEffort(endpoint, sessionID string) {
	abortCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.abortSession(abortCtx, endpoint, sessionID)
}

func emitOpenCodeResponseEvents(seq *int64, onEvent func(RunnerRunEvent), sessionID string, response map[string]any) {
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
	if r.client == nil {
		r.client = &http.Client{Timeout: 2 * time.Second}
	}
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return httpJSON(healthCtx, r.client, http.MethodGet, endpoint+"/global/health", nil, nil)
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

func (r *OpenCodeNativeRuntime) openCodeModelRequest(ctx context.Context, endpoint string, env []string, model, thinking string) any {
	return openCodeModelRequestForCatalog(openCodeCatalogWithFallback(ctx, endpoint, env, r), model, thinking)
}

func openCodeModelBody(info RunnerModelInfo, thinking string) any {
	provider := strings.TrimSpace(info.Provider)
	modelID := strings.TrimSpace(info.ID)
	if provider == "" || modelID == "" {
		return modelID
	}
	if thinking != "" && stringInSlice(thinking, info.Reasoning) {
		return map[string]any{"providerID": provider, "modelID": modelID, "variant": thinking}
	}
	return map[string]any{"providerID": provider, "modelID": modelID}
}

// resolveOpenCodeModel maps UI/session model strings onto OpenCode catalog entries.
// The app may send vendor-prefixed values such as "xiaomi/mimo-v2.5" while OpenCode
// expects providerID "openrouter" (or similar) with modelID "mimo-v2.5".
func resolveOpenCodeModel(catalog []RunnerModelInfo, requested string) *RunnerModelInfo {
	requested = strings.TrimSpace(requested)
	if requested == "" || len(catalog) == 0 {
		return nil
	}
	providerPart, modelPart, split := strings.Cut(requested, "/")
	if split && modelPart != "" && !strings.Contains(modelPart, "/") {
		for i := range catalog {
			if catalog[i].ID == modelPart {
				return &catalog[i]
			}
		}
	}
	for i := range catalog {
		if catalog[i].ID == requested {
			return &catalog[i]
		}
	}
	if !split {
		return nil
	}
	for i := range catalog {
		entry := &catalog[i]
		if entry.Provider == providerPart && entry.ID == modelPart {
			return entry
		}
		if entry.ID == requested {
			return entry
		}
		if entry.ID == modelPart {
			return entry
		}
	}
	return nil
}

// CodexNativeRuntime talks to codex app-server over stdio JSON-RPC. It owns
// a process-scoped session cache so that consecutive turns can reuse the
// same app-server, thread, and turn refs. The cache is bounded by an idle
// timeout and is torn down by Stop.
type CodexNativeRuntime struct {
	mu            sync.Mutex
	activeSession *codexSession
	activeJobID   string
	lastUsedAt    atomic.Int64
	idleTimeout   time.Duration
}

// Compile-time assertion that CodexNativeRuntime supports request responses.
var _ NativeRequestResponder = (*CodexNativeRuntime)(nil)
var _ NativeTurnContinuer = (*CodexNativeRuntime)(nil)

func NewCodexNativeRuntime() *CodexNativeRuntime {
	return &CodexNativeRuntime{idleTimeout: 5 * time.Minute}
}

func (r *CodexNativeRuntime) ID() RunnerID { return RunnerCodex }

func (r *CodexNativeRuntime) Info(ctx context.Context, cfg config.RunnersConfig, env []string) RunnerRuntimeInfo {
	info := defaultRuntimeInfo(cfg, RunnerCodex, true)
	info.Kind = RuntimeNative
	info.Version = firstNonEmpty(info.Version, runtimeVersionFromBinary(ctx, env, "codex", "--version"))
	if mode := runnerRuntimeMode(cfg, RunnerCodex); mode == RuntimeModeCLI {
		return defaultRuntimeInfo(cfg, RunnerCodex, true)
	}
	if !executableAvailable("codex", env) {
		info.State = RuntimeStateUnavailable
		info.Fallback = true
		info.FallbackReason = "codex binary is not installed"
		info.NextAction = "install codex or set runtimeMode.codex=cli to use the CLI fallback"
		return info
	}
	info.State = RuntimeStateFallback
	info.Fallback = true
	info.FallbackReason = "codex app-server is started per turn"
	info.Ownership = RuntimeOwnershipManaged
	info.NextAction = "send a message and the codex app-server session will start on demand"
	models := r.models(ctx, cfg, env)
	info.Models = models
	authStatus, authDetail := r.probeAuth(ctx, cfg, env)
	if authStatus != "" {
		info.AuthStatus = authStatus
	}
	if authDetail != "" {
		info.AuthDetail = authDetail
	}
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
	r.mu.Lock()
	sess := r.activeSession
	threadID := ""
	turnID := ""
	startedAt := int64(0)
	if sess != nil {
		threadID = sess.ActiveThread()
		turnID = sess.ActiveTurn()
		startedAt = sess.startedAt.UnixMilli()
	}
	r.mu.Unlock()
	if sess != nil {
		health := RunnerNativeHealth{
			Reachable:     true,
			Endpoint:      "stdio://",
			StartedAt:     startedAt,
			LastCheckedAt: time.Now().UnixMilli(),
			Detail:        "codex app-server session is alive",
		}
		info.Health = &health
		info.Refs = RunnerRuntimeRefs{ThreadID: threadID, TurnID: turnID}
	}
	return info
}

// probeAuth starts a one-shot app-server session to read account/auth info.
// It returns AuthReady when an account email or id is reported, otherwise
// AuthMissing or AuthUnknown. The session is closed before returning.
func (r *CodexNativeRuntime) probeAuth(ctx context.Context, cfg config.RunnersConfig, env []string) (AuthStatus, string) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	binary, err := ResolveExecutable("codex", env)
	if err != nil {
		return AuthUnknown, ""
	}
	sess, err := startCodexSession(probeCtx, binary, codexSessionConfig{Env: env})
	if err != nil {
		return AuthUnknown, err.Error()
	}
	defer sess.Close(context.Background())
	probes := probeCodexSession(probeCtx, sess)
	if probes.Account.LoggedIn {
		return AuthReady, probes.Account.Email
	}
	return AuthMissing, "codex account is not authenticated"
}

func (r *CodexNativeRuntime) models(ctx context.Context, cfg config.RunnersConfig, env []string) []RunnerModelInfo {
	binary, err := ResolveExecutable("codex", env)
	if err != nil {
		return nil
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(discoveryCtx, binary, codexAppServerArgs()...)
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
	out, err := r.executeOnce(ctx, req)
	if !isCodexAuthRefreshFailure(err, out.StderrPreview) {
		return out, err
	}
	_ = r.Stop(context.Background())
	retryOut, retryErr := r.executeOnce(ctx, req)
	if !isCodexAuthRefreshFailure(retryErr, retryOut.StderrPreview) {
		return retryOut, retryErr
	}
	msg := codexAuthRefreshFailureMessage(retryErr, retryOut.StderrPreview)
	retryOut.ExitCode = -1
	retryOut.StderrPreview = msg
	return retryOut, errors.New(msg)
}

func (r *CodexNativeRuntime) executeOnce(ctx context.Context, req NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	started := time.Now()
	var seq int64
	binary, err := ResolveExecutable("codex", req.Env)
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	sess, err := r.acquireSession(ctx, binary, req)
	if err != nil {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, err
	}
	approvalRequired := atomic.Bool{}
	sess.ResetStderr()
	// Capture notifications and server-issued requests so the chat manager
	// can render approvals and runtime warnings. Each request is also
	// stored as a NativeRequestRef for later response.
	sess.rpc.beginTurn()
	sess.rpc.start(
		func(method string, params map[string]any) {
			emitCodexNotificationStructured(&seq, req.OnEvent, method, params)
		},
		func(id int64, method string, params map[string]any) map[string]any {
			approvalRequired.Store(true)
			ref := sess.RegisterRequestRef(id, method, params)
			payload := map[string]any{
				"type":       method,
				"method":     method,
				"params":     params,
				"request_id": id,
			}
			if ref.RequestID != "" {
				payload["native_request_ref"] = map[string]any{
					"request_id": ref.RequestID,
					"kind":       string(ref.Kind),
					"method":     ref.Method,
					"thread_id":  ref.ThreadID,
					"turn_id":    ref.TurnID,
					"summary":    ref.Summary,
				}
			}
			emitNativeStructured(&seq, req.OnEvent, payload)
			select {
			case sess.rpc.turnDone <- errNativeApprovalRequired:
			default:
			}
			return map[string]any{"error": map[string]any{"code": -32001, "message": "approval required in OR3"}}
		},
	)
	defer func() {
		r.lastUsedAt.Store(time.Now().UnixMilli())
	}()
	threadParams := map[string]any{"cwd": req.Run.Cwd}
	if model := firstNonEmpty(req.Run.Model, req.Config.DefaultModels[string(RunnerCodex)]); model != "" {
		threadParams["model"] = model
	}
	threadID, err := sess.StartThread(ctx, "", threadParams)
	if err != nil {
		return ProcessOutput{ExitCode: -1, StderrPreview: firstNonEmpty(sess.StderrPreview(), err.Error()), DurationMS: time.Since(started).Milliseconds()}, err
	}
	if req.Chat.NativeSessionRef != "" && req.Chat.ContinuationMode == ContinuationNative {
		// Try resuming a known thread id; fall back to a fresh start.
		resumedID, resumeErr := sess.StartThread(ctx, req.Chat.NativeSessionRef, threadParams)
		if resumeErr == nil && resumedID != "" {
			threadID = resumedID
		}
	}
	emitNativeStructured(&seq, req.OnEvent, map[string]any{"type": "thread.started", "thread_id": threadID})
	turnParams := map[string]any{"threadId": threadID, "input": codexTextInput(ChatExecutionInput(req.Chat, req.Run.Task)), "cwd": req.Run.Cwd}
	selectedModel := firstNonEmpty(req.Run.Model, req.Config.DefaultModels[string(RunnerCodex)])
	if model := selectedModel; model != "" {
		turnParams["model"] = model
	}
	if thinking := requestedThinkingLevel(req.Chat.Meta); thinking != "" {
		if r.codexSupportsEffort(ctx, sess.rpc, selectedModel, thinking) {
			turnParams["effort"] = thinking
		}
	}
	if err := addCodexPolicies(turnParams, req.Run); err != nil {
		return ProcessOutput{ExitCode: -1, StderrPreview: firstNonEmpty(sess.StderrPreview(), err.Error()), DurationMS: time.Since(started).Milliseconds()}, err
	}
	if permission, ok := runnerPermissionFromMeta(req.Chat.Meta); ok && permission.Access == runnerPermissionAccessWrite {
		if sandbox, ok := turnParams["sandboxPolicy"].(map[string]any); ok && sandbox["type"] == "workspaceWrite" {
			sandbox["writableRoots"] = []string{permission.TargetPath}
		}
	}
	if _, err := sess.StartTurn(ctx, threadID, turnParams); err != nil {
		if approvalRequired.Load() {
			return ProcessOutput{ExitCode: -1, StderrPreview: firstNonEmpty(sess.StderrPreview(), errNativeApprovalRequired.Error()), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
		}
		return ProcessOutput{ExitCode: -1, StderrPreview: firstNonEmpty(sess.StderrPreview(), err.Error()), DurationMS: time.Since(started).Milliseconds()}, err
	}
	if err := sess.rpc.waitForTurn(ctx); err != nil {
		if approvalRequired.Load() {
			return ProcessOutput{ExitCode: -1, StderrPreview: firstNonEmpty(sess.StderrPreview(), errNativeApprovalRequired.Error()), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
		}
		return ProcessOutput{ExitCode: -1, StderrPreview: firstNonEmpty(sess.StderrPreview(), err.Error()), DurationMS: time.Since(started).Milliseconds()}, err
	}
	final := sess.rpc.finalText()
	stderrText := sess.StderrPreview()
	return ProcessOutput{ExitCode: 0, StdoutPreview: final, StderrPreview: stderrText, FinalTextPreview: final, DurationMS: time.Since(started).Milliseconds()}, nil
}

func isCodexAuthRefreshFailure(err error, stderr string) bool {
	text := strings.ToLower(strings.TrimSpace(firstNonEmpty(stderr, errorString(err))))
	if text == "" {
		return false
	}
	return strings.Contains(text, "tokenrefreshfailed") ||
		strings.Contains(text, "invalid_grant") ||
		(strings.Contains(text, "refresh token") && strings.Contains(text, "invalid"))
}

func codexAuthRefreshFailureMessage(err error, stderr string) string {
	detail := strings.TrimSpace(firstNonEmpty(stderr, errorString(err)))
	if detail == "" {
		detail = "Codex could not refresh its ChatGPT login token."
	}
	return "Codex authentication failed while refreshing its login token. Run `codex login` to reconnect Codex, then retry the runner turn. Detail: " + detail
}

func codexTextInput(text string) []map[string]any {
	return []map[string]any{{"type": "text", "text": text}}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// acquireSession returns a live app-server session, reusing the previous
// session when possible. It enforces a lazy idle expiry: a session older
// than the runtime's idle timeout is torn down before a new one is started.
func (r *CodexNativeRuntime) acquireSession(ctx context.Context, binary string, req NativeRuntimeExecuteRequest) (*codexSession, error) {
	r.mu.Lock()
	sess := r.activeSession
	jobID := r.activeJobID
	r.mu.Unlock()
	timeout := r.idleTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if sess != nil {
		if jobID != "" && jobID != req.Run.JobID {
			// Different job wants the session; tear it down.
			_ = sess.Close(context.Background())
			r.clearSession()
			sess = nil
		} else if time.Since(sess.startedAt) > timeout {
			_ = sess.Close(context.Background())
			r.clearSession()
			sess = nil
		}
	}
	if sess == nil {
		newSess, err := startCodexSession(ctx, binary, codexSessionConfig{Env: req.Env, Cwd: req.Run.Cwd})
		if err != nil {
			return nil, err
		}
		sess = newSess
	}
	r.mu.Lock()
	r.activeSession = sess
	r.activeJobID = req.Run.JobID
	r.mu.Unlock()
	r.lastUsedAt.Store(time.Now().UnixMilli())
	return sess, nil
}

func (r *CodexNativeRuntime) clearSession() {
	r.mu.Lock()
	r.activeSession = nil
	r.activeJobID = ""
	r.mu.Unlock()
}

// Abort interrupts the active turn (if any) on the cached session. The
// session itself is preserved so subsequent turns can resume normally.
func (r *CodexNativeRuntime) Abort(ctx context.Context, jobID string) error {
	r.mu.Lock()
	sess := r.activeSession
	activeJobID := r.activeJobID
	r.mu.Unlock()
	if sess == nil {
		return nil
	}
	if activeJobID != "" && jobID != "" && activeJobID != jobID {
		return nil
	}
	return sess.AbortTurn(ctx)
}

// Stop terminates any cached app-server session. Safe to call multiple
// times and from Manager.Stop during service shutdown.
func (r *CodexNativeRuntime) Stop(ctx context.Context) error {
	r.mu.Lock()
	sess := r.activeSession
	r.activeSession = nil
	r.activeJobID = ""
	r.mu.Unlock()
	if sess == nil {
		return nil
	}
	return sess.Close(ctx)
}

// RespondToNativeRequest resumes the active turn by sending the user's
// decision back to the app-server. Returns a non-nil error when no live
// session exists; callers should fall back to the approval-token retry.
func (r *CodexNativeRuntime) RespondToNativeRequest(ctx context.Context, ref NativeRequestRef, decision NativeRequestDecision) error {
	r.mu.Lock()
	sess := r.activeSession
	r.mu.Unlock()
	if sess == nil {
		return errors.New("codex session is not alive")
	}
	return sess.RespondToRequest(ctx, ref, decision)
}

// ContinuePendingTurn waits for the active codex app-server turn to finish
// after the user approved a pending native request.
func (r *CodexNativeRuntime) ContinuePendingTurn(ctx context.Context, req NativeRuntimeExecuteRequest) (ProcessOutput, error) {
	started := time.Now()
	r.mu.Lock()
	sess := r.activeSession
	jobID := r.activeJobID
	r.mu.Unlock()
	if sess == nil || (jobID != "" && jobID != req.Run.JobID) {
		return ProcessOutput{ExitCode: -1, DurationMS: time.Since(started).Milliseconds()}, fmt.Errorf("codex session is not alive for job %s", req.Run.JobID)
	}
	sess.rpc.beginTurn()
	if err := sess.rpc.waitForTurn(ctx); err != nil {
		if errors.Is(err, errNativeApprovalRequired) {
			return ProcessOutput{ExitCode: -1, StderrPreview: errNativeApprovalRequired.Error(), DurationMS: time.Since(started).Milliseconds()}, errNativeApprovalRequired
		}
		return ProcessOutput{ExitCode: -1, StderrPreview: err.Error(), DurationMS: time.Since(started).Milliseconds()}, err
	}
	final := sess.rpc.finalText()
	return ProcessOutput{ExitCode: 0, StdoutPreview: final, FinalTextPreview: final, DurationMS: time.Since(started).Milliseconds()}, nil
}

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

func addCodexPolicies(params map[string]any, run db.RunnerRun) error {
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
		params["sandboxPolicy"] = map[string]any{"type": "readOnly"}
	case IsolationHostWorkspaceWrite, IsolationSandboxWrite:
		params["sandboxPolicy"] = map[string]any{"type": "workspaceWrite"}
	case IsolationSandboxDangerous:
		params["sandboxPolicy"] = map[string]any{"type": "dangerFullAccess"}
	default:
		return fmt.Errorf("unsupported isolation %q", run.Isolation)
	}
	return nil
}

type codexRPC struct {
	stdin        io.WriteCloser
	scanner      *bufio.Scanner
	mu           sync.Mutex
	handlerMu    sync.RWMutex
	started      atomic.Bool
	nextID       int64
	pending      map[int64]chan rpcResponse
	done         chan struct{}
	turnDone     chan error
	turnComplete atomic.Bool
	textMu       sync.Mutex
	text         strings.Builder
	onNotify     func(string, map[string]any)
	onRequest    func(int64, string, map[string]any) map[string]any
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
	c.handlerMu.Lock()
	c.onNotify = onNotification
	c.onRequest = onRequest
	c.handlerMu.Unlock()
	if !c.started.CompareAndSwap(false, true) {
		return
	}
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
				c.handlerMu.RLock()
				requestHandler := c.onRequest
				c.handlerMu.RUnlock()
				if method != "" && requestHandler != nil {
					_ = c.write(map[string]any{"id": id, "jsonrpc": "2.0", "result": requestHandler(id, method, params)})
					continue
				}
				c.handleResponse(id, msg)
				continue
			}
			c.handlerMu.RLock()
			notificationHandler := c.onNotify
			c.handlerMu.RUnlock()
			if notificationHandler != nil && method != "" {
				notificationHandler(method, params)
			}
			c.captureNotificationText(method, params)
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

func (c *codexRPC) beginTurn() {
	c.textMu.Lock()
	c.text.Reset()
	c.textMu.Unlock()
	c.turnComplete.Store(false)
	for {
		select {
		case <-c.turnDone:
		default:
			return
		}
	}
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

func (c *codexRPC) captureNotificationText(method string, params map[string]any) {
	if c == nil {
		return
	}
	if delta := codexAssistantMessageDelta(method, params); delta != "" {
		c.textMu.Lock()
		c.text.WriteString(delta)
		c.textMu.Unlock()
		return
	}
	completed := codexCompletedAgentMessageText(method, params)
	if completed == "" {
		return
	}
	c.textMu.Lock()
	defer c.textMu.Unlock()
	current := c.text.String()
	switch {
	case current == "":
		c.text.WriteString(completed)
	case current == completed:
		return
	case strings.HasPrefix(completed, current):
		c.text.Reset()
		c.text.WriteString(completed)
	case !strings.Contains(current, completed):
		c.text.WriteString(completed)
	}
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

func codexAssistantMessageDelta(method string, params map[string]any) string {
	if strings.EqualFold(strings.TrimSpace(method), "item/agentMessage/delta") {
		return extractDeltaString(firstNonNil(params["delta"], params["text"], params["content"]))
	}
	return ""
}

func codexCompletedAgentMessageText(method string, params map[string]any) string {
	if !strings.EqualFold(strings.TrimSpace(method), "item/completed") {
		return ""
	}
	item := mapField(params, "item")
	if codexItemType(item) != runtimeItemAssistantMessage {
		return ""
	}
	return extractDeltaString(firstNonNil(item["text"], item["content"], item["message"]))
}

func extractOpenCodeVisibleText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		if openCodePartIsReasoning(v) {
			return ""
		}
		if parts, ok := v["parts"].([]any); ok {
			var out strings.Builder
			for _, part := range parts {
				out.WriteString(extractOpenCodeVisibleText(part))
			}
			if text := out.String(); text != "" {
				return text
			}
		}
		if part := mapField(v, "part"); len(part) > 0 {
			if text := extractOpenCodeVisibleText(part); text != "" {
				return text
			}
		}
		for _, key := range []string{"text", "delta", "content", "message", "final_text", "finalText"} {
			if text, ok := v[key].(string); ok && text != "" {
				return text
			}
		}
		if item, ok := v["item"].(map[string]any); ok {
			return extractOpenCodeVisibleText(item)
		}
		if raw, ok := v["raw"].(map[string]any); ok {
			return extractOpenCodeVisibleText(raw)
		}
	case []any:
		var out strings.Builder
		for _, item := range v {
			out.WriteString(extractOpenCodeVisibleText(item))
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

func nativeEnv(cfg config.RunnersConfig) []string {
	return BuildRunnerEnv(os.Environ(), cfg.ChildEnvAllowlist, nil)
}
