package agentcli

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

type runnerDetectCacheEntry struct {
	info       RunnerInfo
	fetchedAt  time.Time
	refreshing bool
}

// AllRunners returns the standard runner specs for all supported external CLIs.
func AllRunners() []RunnerSpec {
	return []RunnerSpec{
		{
			ID:          RunnerOpenCode,
			DisplayName: "OpenCode",
			Binary:      "opencode",
			VersionArgs: []string{"--version"},
			AuthCheck:   &SmallCommandSpec{Args: []string{"auth", "list"}, Timeout: 3},
			Supports: RunnerSupports{
				StructuredOutput:    true,
				StreamingJSON:       true,
				ModelFlag:           true,
				SafeSandboxFlag:     false,
				DangerousBypassFlag: true,
				StdinPrompt:         false,
				Chat: RunnerChatCapabilities{
					ChatSelectable:            true,
					ChatReplay:                true,
					ChatNativeSession:         true,
					ChatResume:                true,
					ChatSessionRefExtractable: true,
				},
			},
		},
		{
			ID:          RunnerCodex,
			DisplayName: "Codex",
			Binary:      "codex",
			VersionArgs: []string{"--help"},
			AuthCheck:   &SmallCommandSpec{Args: []string{"login", "status"}, Timeout: 3},
			Supports: RunnerSupports{
				StructuredOutput:    true,
				StreamingJSON:       true,
				ModelFlag:           true,
				PermissionsMode:     false,
				SafeSandboxFlag:     true,
				DangerousBypassFlag: true,
				StdinPrompt:         false,
				Chat: RunnerChatCapabilities{
					ChatSelectable:            true,
					ChatReplay:                true,
					ChatNativeSession:         true,
					ChatResume:                true,
					ChatSessionRefExtractable: true,
					StreamToolEvents:          true,
				},
			},
		},
		{
			ID:          RunnerClaude,
			DisplayName: "Claude Code",
			Binary:      "claude",
			VersionArgs: []string{"--version"},
			AuthCheck:   &SmallCommandSpec{Args: []string{"auth", "status"}, Timeout: 3},
			Supports: RunnerSupports{
				StructuredOutput:    true,
				StreamingJSON:       true,
				ModelFlag:           true,
				PermissionsMode:     true,
				SafeSandboxFlag:     false,
				DangerousBypassFlag: true,
				StdinPrompt:         false,
				Chat: RunnerChatCapabilities{
					ChatSelectable:            true,
					ChatReplay:                true,
					ChatNativeSession:         true,
					ChatResume:                true,
					ChatSessionRefExtractable: true,
					StreamToolEvents:          true,
				},
			},
		},
		{
			ID:          RunnerGemini,
			DisplayName: "Gemini CLI",
			Binary:      "gemini",
			VersionArgs: []string{"--version"},
			AuthCheck:   nil,
			Supports: RunnerSupports{
				StructuredOutput:    true,
				StreamingJSON:       true,
				ModelFlag:           true,
				PermissionsMode:     true,
				SafeSandboxFlag:     false,
				DangerousBypassFlag: true,
				StdinPrompt:         false,
				Chat: RunnerChatCapabilities{
					ChatSelectable:            true,
					ChatReplay:                true,
					ChatNativeSession:         true,
					ChatResume:                true,
					ChatSessionRefExtractable: true,
					StreamToolEvents:          true,
				},
			},
		},
		{
			ID:          RunnerOR3,
			DisplayName: "OR3 Intern",
			Binary:      "",
			VersionArgs: nil,
			AuthCheck:   nil,
			Supports: RunnerSupports{
				StructuredOutput:    false,
				StreamingJSON:       false,
				ModelFlag:           true,
				PermissionsMode:     false,
				SafeSandboxFlag:     false,
				DangerousBypassFlag: false,
				StdinPrompt:         false,
				Chat: RunnerChatCapabilities{
					ChatSelectable: true,
					ChatReplay:     true,
				},
			},
		},
	}
}

// RunnerRegistry maps runner IDs to their specs and adapters.
type RunnerRegistry struct {
	mu          sync.RWMutex
	specs       map[RunnerID]RunnerSpec
	adapters    map[RunnerID]RunnerAdapter
	detectCache map[RunnerID]runnerDetectCacheEntry
	now         func() time.Time
}

// NewRunnerRegistry creates a registry from a set of specs and optional adapters.
func NewRunnerRegistry(specs []RunnerSpec, adapters []RunnerAdapter) *RunnerRegistry {
	r := &RunnerRegistry{
		specs:       make(map[RunnerID]RunnerSpec, len(specs)),
		adapters:    make(map[RunnerID]RunnerAdapter),
		detectCache: make(map[RunnerID]runnerDetectCacheEntry),
		now:         time.Now,
	}
	for _, s := range specs {
		r.specs[s.ID] = s
	}
	for _, a := range adapters {
		if a != nil {
			r.adapters[a.ID()] = a
		}
	}
	return r
}

// Spec returns the runner spec by ID.
func (r *RunnerRegistry) Spec(id RunnerID) (RunnerSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.specs[id]
	return s, ok
}

// Adapter returns the runner adapter by ID.
func (r *RunnerRegistry) Adapter(id RunnerID) (RunnerAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[id]
	return a, ok
}

// DetectAll runs detection on all registered external runners.
func (r *RunnerRegistry) DetectAll(ctx context.Context, opts DetectOptions) []RunnerInfo {
	r.mu.RLock()
	specs := make([]RunnerSpec, 0, len(r.specs))
	for _, s := range r.specs {
		specs = append(specs, s)
	}
	r.mu.RUnlock()

	results := make([]RunnerInfo, len(specs))
	var wg sync.WaitGroup
	for i, spec := range specs {
		wg.Add(1)
		go func(i int, spec RunnerSpec) {
			defer wg.Done()
			results[i] = Detect(ctx, spec, opts)
		}(i, spec)
	}
	wg.Wait()
	r.storeDetectResults(results)
	return results
}

// DetectCached returns a recent cached detection result when available.
func (r *RunnerRegistry) DetectCached(id RunnerID, ttl time.Duration) (RunnerInfo, bool) {
	if ttl <= 0 {
		return RunnerInfo{}, false
	}
	r.mu.RLock()
	entry, ok := r.detectCache[id]
	now := r.now
	r.mu.RUnlock()
	if !ok || entry.fetchedAt.IsZero() || now().Sub(entry.fetchedAt) > ttl {
		return RunnerInfo{}, false
	}
	return entry.info, true
}

// RefreshDetectAsync refreshes detection for a runner in the background.
func (r *RunnerRegistry) RefreshDetectAsync(id RunnerID, opts DetectOptions) {
	r.mu.Lock()
	spec, ok := r.specs[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	entry := r.detectCache[id]
	if entry.refreshing {
		r.mu.Unlock()
		return
	}
	entry.refreshing = true
	r.detectCache[id] = entry
	stateNow := r.now
	r.mu.Unlock()

	go func(spec RunnerSpec) {
		info := Detect(context.Background(), spec, opts)
		r.mu.Lock()
		r.detectCache[id] = runnerDetectCacheEntry{
			info:      info,
			fetchedAt: stateNow(),
		}
		r.mu.Unlock()
	}(spec)
}

// RefreshAllAsync refreshes detection for all registered runners in the background.
func (r *RunnerRegistry) RefreshAllAsync(opts DetectOptions) {
	r.mu.RLock()
	ids := make([]RunnerID, 0, len(r.specs))
	for id := range r.specs {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	for _, id := range ids {
		r.RefreshDetectAsync(id, opts)
	}
}

// BuildCommand builds a command for the given request using the matching adapter.
func (r *RunnerRegistry) BuildCommand(req AgentRunRequest) (CommandSpec, error) {
	adapter, ok := r.Adapter(RunnerID(req.RunnerID))
	if !ok {
		return CommandSpec{}, fmt.Errorf("no adapter for runner %q", req.RunnerID)
	}
	return adapter.BuildCommand(req)
}

// BuildChatCommand builds a chat-turn command for a runner that supports the
// RunnerChatAdapter extension.
func (r *RunnerRegistry) BuildChatCommand(id RunnerID, req RunnerChatCommandRequest) (CommandSpec, error) {
	adapter, ok := r.Adapter(id)
	if !ok {
		return CommandSpec{}, fmt.Errorf("no adapter for runner %q", id)
	}
	chatAdapter, ok := adapter.(RunnerChatAdapter)
	if !ok {
		return CommandSpec{}, fmt.Errorf("runner %q does not support chat commands", id)
	}
	return chatAdapter.BuildChatCommand(req)
}

// ValidateRunPolicy checks that the requested mode and isolation are compatible.
func ValidateRunPolicy(mode RunnerMode, isolation RunIsolation, allowSandboxAuto bool) error {
	switch mode {
	case RunnerModeReview:
		if isolation != IsolationHostReadOnly && isolation != IsolationSandboxWrite {
			return fmt.Errorf("review mode requires host_readonly or sandbox_workspace_write isolation, got %q", isolation)
		}
	case RunnerModeSafeEdit, "":
		if isolation != IsolationHostWorkspaceWrite && isolation != IsolationSandboxWrite {
			return fmt.Errorf("safe_edit mode requires host_workspace_write or sandbox_workspace_write isolation, got %q", isolation)
		}
	case RunnerModeSandboxAuto:
		if isolation != IsolationSandboxDangerous {
			return fmt.Errorf("sandbox_auto mode requires sandbox_dangerous isolation, got %q", isolation)
		}
		if !allowSandboxAuto {
			return fmt.Errorf("sandbox_auto mode is disabled by config")
		}
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
	return nil
}

// NewDefaultRegistry creates a fully-wired registry with all standard runners and adapters.
func NewDefaultRegistry() *RunnerRegistry {
	specs := AllRunners()
	openCodeSpec := findSpec(specs, RunnerOpenCode)
	codexSpec := findSpec(specs, RunnerCodex)
	claudeSpec := findSpec(specs, RunnerClaude)
	geminiSpec := findSpec(specs, RunnerGemini)
	return NewRunnerRegistry(specs, []RunnerAdapter{
		&OpenCodeAdapter{spec: openCodeSpec},
		&CodexAdapter{spec: codexSpec},
		&ClaudeAdapter{spec: claudeSpec},
		&GeminiAdapter{spec: geminiSpec},
	})
}

func findSpec(specs []RunnerSpec, id RunnerID) RunnerSpec {
	for _, s := range specs {
		if s.ID == id {
			return s
		}
	}
	return RunnerSpec{ID: id, Binary: string(id)}
}

func (r *RunnerRegistry) storeDetectResults(results []RunnerInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for _, result := range results {
		r.detectCache[RunnerID(result.ID)] = runnerDetectCacheEntry{
			info:      result,
			fetchedAt: now,
		}
	}
}

// NewOpenCodeAdapter creates an OpenCode adapter wired to the standard spec.
func NewOpenCodeAdapter() *OpenCodeAdapter {
	return &OpenCodeAdapter{spec: openCodeSpec()}
}

// NewCodexAdapter creates a Codex adapter wired to the standard spec.
func NewCodexAdapter() *CodexAdapter {
	return &CodexAdapter{spec: codexSpec()}
}

// NewClaudeAdapter creates a Claude Code adapter wired to the standard spec.
func NewClaudeAdapter() *ClaudeAdapter {
	return &ClaudeAdapter{spec: claudeSpec()}
}

// NewGeminiAdapter creates a Gemini CLI adapter wired to the standard spec.
func NewGeminiAdapter() *GeminiAdapter {
	return &GeminiAdapter{spec: geminiSpec()}
}

func openCodeSpec() RunnerSpec { return findSpec(AllRunners(), RunnerOpenCode) }
func codexSpec() RunnerSpec    { return findSpec(AllRunners(), RunnerCodex) }
func claudeSpec() RunnerSpec   { return findSpec(AllRunners(), RunnerClaude) }
func geminiSpec() RunnerSpec   { return findSpec(AllRunners(), RunnerGemini) }

// OpenCodeAdapter builds argv for OpenCode CLI.
type OpenCodeAdapter struct{ spec RunnerSpec }

func (a *OpenCodeAdapter) ID() RunnerID        { return RunnerOpenCode }
func (a *OpenCodeAdapter) DisplayName() string { return "OpenCode" }
func (a *OpenCodeAdapter) Spec() RunnerSpec    { return a.spec }
func (a *OpenCodeAdapter) Detect(ctx context.Context, opts DetectOptions) RunnerInfo {
	return Detect(ctx, a.spec, opts)
}

func (a *OpenCodeAdapter) BuildCommand(req AgentRunRequest) (CommandSpec, error) {
	args := []string{"run", "--format", "json"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	mode := RunnerMode(req.Mode)
	if mode == RunnerModeSandboxAuto {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, req.Task)
	return CommandSpec{
		RunnerID:    a.ID(),
		Binary:      a.spec.Binary,
		Args:        args,
		Cwd:         req.Cwd,
		OutputMode:  OutputJSON,
		ArgvPreview: append([]string{}, args...),
	}, nil
}

// CodexAdapter builds argv for Codex CLI.
type CodexAdapter struct{ spec RunnerSpec }

func (a *CodexAdapter) ID() RunnerID        { return RunnerCodex }
func (a *CodexAdapter) DisplayName() string { return "Codex" }
func (a *CodexAdapter) Spec() RunnerSpec    { return a.spec }
func (a *CodexAdapter) Detect(ctx context.Context, opts DetectOptions) RunnerInfo {
	return Detect(ctx, a.spec, opts)
}

func (a *CodexAdapter) BuildCommand(req AgentRunRequest) (CommandSpec, error) {
	mode := RunnerMode(req.Mode)
	args := make([]string, 0, 16)
	if mode != RunnerModeSandboxAuto {
		args = append(args, "--ask-for-approval", "never")
	}
	args = append(args, "exec", "--json", "--color", "never", "--skip-git-repo-check")
	if req.Cwd != "" {
		args = append(args, "--cd", req.Cwd)
	}
	if permission, ok := runnerPermissionFromMeta(req.Meta); ok && permission.Access == runnerPermissionAccessWrite {
		args = append(args, "--add-dir", permission.TargetPath)
	}

	switch mode {
	case RunnerModeReview:
		args = append(args, "--sandbox", "read-only")
	case RunnerModeSafeEdit, "":
		args = append(args, "--sandbox", "workspace-write")
	case RunnerModeSandboxAuto:
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	default:
		return CommandSpec{}, fmt.Errorf("unsupported codex mode %q", req.Mode)
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	args = append(args, req.Task)
	return CommandSpec{
		RunnerID:    a.ID(),
		Binary:      a.spec.Binary,
		Args:        args,
		Cwd:         req.Cwd,
		OutputMode:  OutputJSONL,
		ArgvPreview: append([]string{}, args...),
	}, nil
}

// ClaudeAdapter builds argv for Claude Code CLI.
type ClaudeAdapter struct{ spec RunnerSpec }

func (a *ClaudeAdapter) ID() RunnerID        { return RunnerClaude }
func (a *ClaudeAdapter) DisplayName() string { return "Claude Code" }
func (a *ClaudeAdapter) Spec() RunnerSpec    { return a.spec }
func (a *ClaudeAdapter) Detect(ctx context.Context, opts DetectOptions) RunnerInfo {
	return Detect(ctx, a.spec, opts)
}

func (a *ClaudeAdapter) BuildCommand(req AgentRunRequest) (CommandSpec, error) {
	args := []string{
		"--bare",
		"-p", req.Task,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
	}

	mode := RunnerMode(req.Mode)
	switch mode {
	case RunnerModeReview:
		args = append(args, "--permission-mode", "plan")
	case RunnerModeSafeEdit, "":
		args = append(args, "--permission-mode", "acceptEdits")
	case RunnerModeSandboxAuto:
		args = append(args, "--permission-mode", "bypassPermissions")
	default:
		return CommandSpec{}, fmt.Errorf("unsupported claude mode %q", req.Mode)
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(req.MaxTurns))
	}
	return CommandSpec{
		RunnerID:    a.ID(),
		Binary:      a.spec.Binary,
		Args:        args,
		Cwd:         req.Cwd,
		OutputMode:  OutputJSONL,
		ArgvPreview: append([]string{}, args...),
	}, nil
}

// GeminiAdapter builds argv for Gemini CLI.
type GeminiAdapter struct{ spec RunnerSpec }

func (a *GeminiAdapter) ID() RunnerID        { return RunnerGemini }
func (a *GeminiAdapter) DisplayName() string { return "Gemini CLI" }
func (a *GeminiAdapter) Spec() RunnerSpec    { return a.spec }
func (a *GeminiAdapter) Detect(ctx context.Context, opts DetectOptions) RunnerInfo {
	return Detect(ctx, a.spec, opts)
}

func (a *GeminiAdapter) BuildCommand(req AgentRunRequest) (CommandSpec, error) {
	args := []string{"--prompt", req.Task, "--output-format", "json"}

	mode := RunnerMode(req.Mode)
	switch mode {
	case RunnerModeReview:
		args = append(args, "--approval-mode", "default")
	case RunnerModeSafeEdit, "":
		args = append(args, "--approval-mode", "auto_edit")
	case RunnerModeSandboxAuto:
		args = append(args, "--approval-mode", "yolo")
	default:
		return CommandSpec{}, fmt.Errorf("unsupported gemini mode %q", req.Mode)
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	return CommandSpec{
		RunnerID:    a.ID(),
		Binary:      a.spec.Binary,
		Args:        args,
		Cwd:         req.Cwd,
		OutputMode:  OutputJSON,
		ArgvPreview: append([]string{}, args...),
	}, nil
}
