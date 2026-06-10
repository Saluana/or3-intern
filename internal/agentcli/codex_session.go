package agentcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// codexSession is a long-lived Codex app-server session. It owns the
// underlying process, the active thread/turn, and any pending request refs
// so that the manager can interrupt, abort, or respond to native requests
// without re-spawning the process.
type codexSession struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	scanBuf     *bufio.Scanner
	rpc         *codexRPC
	processExit chan struct{}
	processErr  error
	startupErr  error
	threadID    string
	turnID      string
	turnCtx     context.Context
	turnCancel  context.CancelFunc
	startedAt   time.Time
	closed      atomic.Bool
	lastRequest atomic.Int64
	requestRefs map[string]NativeRequestRef
}

type codexSessionConfig struct {
	Cwd  string
	Env  []string
	Args []string
}

func newCodexSessionConfig() codexSessionConfig {
	return codexSessionConfig{
		Args: codexAppServerArgs(),
	}
}

func codexAppServerArgs() []string {
	return []string{"app-server", "--listen", "stdio://", "-c", codexDisableMCPConfigOverride}
}

// startCodexSession launches a codex app-server child process and
// initializes the JSON-RPC bridge. The caller is responsible for
// terminating the session with Close when done.
func startCodexSession(ctx context.Context, binary string, cfg codexSessionConfig) (*codexSession, error) {
	if strings.TrimSpace(binary) == "" {
		return nil, errors.New("codex binary is not installed")
	}
	if len(cfg.Args) == 0 {
		cfg.Args = newCodexSessionConfig().Args
	}
	if cfg.Cwd != "" {
		// Cwd is used by app-server to resolve sandbox policy but does not
		// have to exist; the adapter passes it as a turn parameter instead.
	}
	cmd := exec.CommandContext(context.Background(), binary, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex app-server start: %w", err)
	}
	sess := &codexSession{
		cmd:         cmd,
		stdin:       stdin,
		processExit: make(chan struct{}),
		startedAt:   time.Now().UTC(),
		requestRefs: map[string]NativeRequestRef{},
	}
	sess.rpc = newCodexRPC(stdin, stdout)
	// Drain stderr so the process can't block.
	if stderr != nil {
		go func() {
			_, _ = io.Copy(io.Discard, io.LimitReader(stderr, 65536))
		}()
	}
	// Initialize the RPC bridge before any calls.
	sess.rpc.start(
		func(method string, params map[string]any) {
			// Notifications are streamed to callers via the consumer loop.
		},
		func(id int64, method string, params map[string]any) map[string]any {
			// We don't expect server requests during model/list; defer to
			// the consumer loop which tracks them as native request refs.
			return map[string]any{}
		},
	)
	if _, err := sess.rpc.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "or3-intern", "version": "native-runner"},
	}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("codex initialize: %w", err)
	}
	if err := sess.rpc.notify("initialized", map[string]any{}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("codex initialized: %w", err)
	}
	return sess, nil
}

// Close terminates the underlying process and waits for it to exit.
func (s *codexSession) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	cancel := s.turnCancel
	cmd := s.cmd
	rpc := s.rpc
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if rpc != nil {
		rpc.close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if cmd != nil {
		doneCh := make(chan error, 1)
		go func() { doneCh <- cmd.Wait() }()
		select {
		case <-doneCh:
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

// ActiveThread returns the currently active thread id (or empty).
func (s *codexSession) ActiveThread() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID
}

// ActiveTurn returns the currently active turn id (or empty).
func (s *codexSession) ActiveTurn() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnID
}

// StartThread creates or resumes a thread and returns the resolved thread id.
func (s *codexSession) StartThread(ctx context.Context, resumeRef string, params map[string]any) (string, error) {
	if s == nil {
		return "", errors.New("nil session")
	}
	var resp map[string]any
	var err error
	if strings.TrimSpace(resumeRef) != "" {
		callParams := map[string]any{"threadId": strings.TrimSpace(resumeRef)}
		for k, v := range params {
			callParams[k] = v
		}
		resp, err = s.rpc.call(ctx, "thread/resume", callParams)
		if err != nil {
			// Resume can fail with thread-not-found. Fall back to start.
			resp, err = s.rpc.call(ctx, "thread/start", params)
		}
	} else {
		resp, err = s.rpc.call(ctx, "thread/start", params)
	}
	if err != nil {
		return "", err
	}
	threadID := firstNonEmpty(asString(resp["threadId"]), asString(resp["thread_id"]))
	s.mu.Lock()
	s.threadID = threadID
	s.mu.Unlock()
	return threadID, nil
}

// StartTurn sends a turn/start request and returns the turn id. The session
// tracks the active turn so Abort can interrupt it.
func (s *codexSession) StartTurn(ctx context.Context, threadID string, params map[string]any) (string, error) {
	if s == nil {
		return "", errors.New("nil session")
	}
	turnCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.turnID = ""
	s.turnCtx = turnCtx
	s.turnCancel = cancel
	s.mu.Unlock()
	resp, err := s.rpc.call(turnCtx, "turn/start", params)
	if err != nil {
		cancel()
		s.mu.Lock()
		s.turnCtx = nil
		s.turnCancel = nil
		s.mu.Unlock()
		return "", err
	}
	turnID := firstNonEmpty(asString(resp["turnId"]), asString(resp["turn_id"]))
	s.mu.Lock()
	s.turnID = turnID
	s.mu.Unlock()
	return turnID, nil
}

// AbortTurn sends a turn/interrupt request and waits for the active turn to
// settle. If no turn is active, the call is a no-op.
func (s *codexSession) AbortTurn(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	threadID := s.threadID
	turnID := s.turnID
	cancel := s.turnCancel
	s.mu.Unlock()
	if threadID == "" {
		return nil
	}
	params := map[string]any{"threadId": threadID}
	if turnID != "" {
		params["turnId"] = turnID
	}
	_, err := s.rpc.call(ctx, "turn/interrupt", params)
	if cancel != nil {
		cancel()
	}
	return err
}

// RegisterRequestRef records a pending server-initiated request and returns
// a NativeRequestRef handle that can be used to respond later.
func (s *codexSession) RegisterRequestRef(id int64, method string, params map[string]any) NativeRequestRef {
	if s == nil {
		return NativeRequestRef{}
	}
	s.mu.Lock()
	threadID := s.threadID
	turnID := s.turnID
	s.mu.Unlock()
	ref := NativeRequestRef{
		RunnerID:  RunnerCodex,
		RequestID: fmt.Sprintf("%d", id),
		Method:    method,
		ThreadID:  threadID,
		TurnID:    turnID,
		IssuedAt:  time.Now().UnixMilli(),
	}
	ref.Kind = codexRequestKind(method)
	if params != nil {
		if raw, err := json.Marshal(params); err == nil {
			ref.RawPayload = raw
		}
		if msg, ok := params["message"].(string); ok && strings.TrimSpace(msg) != "" {
			ref.Summary = strings.TrimSpace(msg)
		}
	}
	s.mu.Lock()
	s.requestRefs[ref.RequestID] = ref
	s.lastRequest.Store(id)
	s.mu.Unlock()
	return ref
}

// RespondToRequest sends the user's decision to the active codex session
// for the given request id. Returning an error indicates the response
// could not be delivered; callers should fall back to the retry path.
func (s *codexSession) RespondToRequest(ctx context.Context, ref NativeRequestRef, decision NativeRequestDecision) error {
	if s == nil {
		return errors.New("nil session")
	}
	id, err := parseInt64String(ref.RequestID)
	if err != nil {
		return fmt.Errorf("invalid request id %q: %w", ref.RequestID, err)
	}
	// Codex approval responses typically use the standard
	// "request_id"-keyed envelope; downstream methods may also accept the
	// raw decision. We send both shapes so the active turn can resume.
	payload := map[string]any{
		"request_id": id,
		"decision":   decision.Decision,
		"approve":    strings.EqualFold(decision.Decision, "approve"),
		"reason":     decision.Message,
	}
	if len(decision.Raw) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(decision.Raw, &raw); err == nil {
			for k, v := range raw {
				payload[k] = v
			}
		}
	}
	if err := s.rpc.write(map[string]any{
		"method": "requestResponse",
		"params": payload,
	}); err != nil {
		return err
	}
	if err := s.rpc.write(map[string]any{
		"id":     id,
		"result": map[string]any{"decision": decision.Decision, "approve": strings.EqualFold(decision.Decision, "approve")},
	}); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.requestRefs, ref.RequestID)
	s.mu.Unlock()
	return nil
}

func codexRequestKind(method string) NativeRequestKind {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "item/commandexecution/approval", "item/filechange/approval", "item/fileread/approval", "tool/approval", "approval_request", "exec/approvalrequest", "applypatch/approval":
		return NativeRequestApproval
	case "item/tool/userinput", "item/tool/question", "tool/userinput", "question_request":
		return NativeRequestQuestion
	case "item/tool/user_input", "tool/input":
		return NativeRequestInput
	default:
		if strings.Contains(strings.ToLower(method), "approval") {
			return NativeRequestApproval
		}
		if strings.Contains(strings.ToLower(method), "question") {
			return NativeRequestQuestion
		}
		return NativeRequestUnknown
	}
}

// detectCodexPermissionRequestRef extracts a NativeRequestRef from a codex
// server-initiated request event so the chat manager can drive the live
// continuation flow.
func detectCodexPermissionRequestRef(raw AgentRunEvent) (NativeRequestRef, bool) {
	if raw.Type != "structured" || len(raw.Payload) == 0 {
		return NativeRequestRef{}, false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw.Payload, &obj); err != nil {
		return NativeRequestRef{}, false
	}
	method := firstNonEmpty(asString(obj["method"]), asString(obj["type"]))
	kind := codexRequestKind(method)
	if kind == NativeRequestUnknown {
		return NativeRequestRef{}, false
	}
	idValue, ok := obj["id"]
	if !ok {
		if req := mapAnyValue(obj, "request"); req != nil {
			idValue = req["id"]
		}
	}
	if idValue == nil {
		return NativeRequestRef{}, false
	}
	id, err := parseInt64String(fmt.Sprint(idValue))
	if err != nil {
		return NativeRequestRef{}, false
	}
	params := mapAnyValue(obj, "params")
	summary := firstNonEmpty(
		asString(obj["message"]),
		asString(params["message"]),
		asString(params["reason"]),
		asString(params["summary"]),
	)
	ref := NativeRequestRef{
		RunnerID:  RunnerCodex,
		Kind:      kind,
		RequestID: fmt.Sprintf("%d", id),
		Method:    method,
		Summary:   summary,
		IssuedAt:  time.Now().UnixMilli(),
	}
	if rawBytes, err := json.Marshal(obj); err == nil {
		ref.RawPayload = rawBytes
	}
	return ref, true
}

func parseInt64String(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty id")
	}
	var id int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-numeric id %q", value)
		}
		id = id*10 + int64(r-'0')
	}
	return id, nil
}

// codexSkill describes a Codex skill advertised via app-server probes.
type codexSkill struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
}

// codexAccountInfo describes the user account advertised by the app-server.
type codexAccountInfo struct {
	Email      string `json:"email,omitempty"`
	Plan       string `json:"plan,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
	AuthStatus string `json:"auth_status,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	LoggedIn   bool   `json:"logged_in,omitempty"`
}

// codexProbes summarizes a one-shot probe of the codex app-server.
type codexProbes struct {
	Account    codexAccountInfo
	Models     []RunnerModelInfo
	Skills     []codexSkill
	HasFastKey bool
	HasEffort  bool
}

// probeCodexSession queries a live app-server for account, model, and skill
// metadata. It is bounded by the supplied context.
func probeCodexSession(ctx context.Context, sess *codexSession) codexProbes {
	probes := codexProbes{}
	if sess == nil {
		return probes
	}
	// Account probe.
	if resp, err := sess.rpc.call(ctx, "account/read", map[string]any{}); err == nil {
		probes.Account = parseCodexAccount(resp)
	}
	// Model probe (paginated).
	models := []RunnerModelInfo{}
	params := map[string]any{"limit": 200, "includeHidden": false}
	for pages := 0; pages < 5; pages++ {
		resp, err := sess.rpc.call(ctx, "model/list", params)
		if err != nil {
			break
		}
		models = append(models, codexModelListToRunnerModels(resp)...)
		// Inspect a single model for fast/effort capability flags.
		if !probes.HasFastKey {
			probes.HasFastKey = codexModelListHasFastKey(resp)
		}
		if !probes.HasEffort {
			probes.HasEffort = codexModelListHasEffort(resp)
		}
		next, _ := resp["nextCursor"].(string)
		if strings.TrimSpace(next) == "" {
			break
		}
		params["cursor"] = next
	}
	probes.Models = dedupeRunnerModels(models)
	// Skills probe (best-effort).
	if resp, err := sess.rpc.call(ctx, "skills/list", map[string]any{}); err == nil {
		probes.Skills = parseCodexSkills(resp)
	}
	return probes
}

func parseCodexAccount(resp map[string]any) codexAccountInfo {
	info := codexAccountInfo{}
	if acc, ok := resp["account"].(map[string]any); ok {
		info.Email = asString(acc["email"])
		info.Plan = asString(acc["plan"])
		info.AccountID = asString(acc["id"])
		info.ExpiresAt = int64Field(acc["expires_at"])
	} else {
		info.Email = asString(resp["email"])
		info.Plan = asString(resp["plan"])
	}
	if status := asString(resp["authStatus"]); status != "" {
		info.AuthStatus = status
	}
	info.LoggedIn = info.Email != "" || info.AccountID != ""
	return info
}

func parseCodexSkills(resp map[string]any) []codexSkill {
	items, _ := resp["data"].([]any)
	if items == nil {
		items, _ = resp["skills"].([]any)
	}
	out := make([]codexSkill, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(asString(obj["name"]), asString(obj["id"]))
		if name == "" {
			continue
		}
		out = append(out, codexSkill{
			Name:        name,
			DisplayName: firstNonEmpty(asString(obj["displayName"]), asString(obj["display_name"]), name),
			Description: firstNonEmpty(asString(obj["description"]), asString(obj["summary"])),
			Path:        asString(obj["path"]),
		})
	}
	return out
}

func codexModelListHasFastKey(resp map[string]any) bool {
	items, _ := resp["data"].([]any)
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := obj["fastMode"]; ok {
			return true
		}
		if _, ok := obj["supportsFastMode"]; ok {
			return true
		}
	}
	return false
}

func codexModelListHasEffort(resp map[string]any) bool {
	items, _ := resp["data"].([]any)
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := obj["supportedReasoningEfforts"]; ok {
			return true
		}
	}
	return false
}

func int64Field(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}
