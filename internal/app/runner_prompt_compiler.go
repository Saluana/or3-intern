package app

import (
	"context"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/runners"
)

const (
	metaOR3Context        = "or3_context"
	or3ContextAuto        = "auto"
	or3ContextNone        = "none"
	defaultPromptMaxBytes = 48 * 1024
)

// OR3ContextMode controls whether high-level runner launches receive the OR3
// prompt envelope or only the raw task text.
type OR3ContextMode string

const (
	OR3ContextAuto OR3ContextMode = or3ContextAuto
	OR3ContextNone OR3ContextMode = or3ContextNone
)

// RunnerPromptCompileInput is the normalized input for prompt compilation.
type RunnerPromptCompileInput struct {
	SessionKey         string
	UserTask           string
	TriggerKind        string
	Meta               map[string]any
	ExtraContextBlocks []string
}

// RunnerPromptCompileResult splits stable bootstrap content from volatile context.
type RunnerPromptCompileResult struct {
	Mode               OR3ContextMode
	StableInstructions []string
	VolatileContext    []string
	UserTask           string
	CompiledPrompt     string
	RawTask            string
	TriggerKind        string
	MemoryRefresh      string
	MemoryDebug        runners.RunnerMemoryDebug
}

// RunnerPromptCompiler assembles cache-friendly runner prompts for external runners.
type RunnerPromptCompiler struct {
	cfg        config.Config
	bootstrap  RunnerBootstrapContext
	context    *RunnerContextBuilder
	contextMax int
}

// NewRunnerPromptCompiler constructs a compiler when runner chat is enabled.
func NewRunnerPromptCompiler(cfg config.Config, bootstrap RunnerBootstrapContext, deps RunnerContextDeps) *RunnerPromptCompiler {
	return &RunnerPromptCompiler{
		cfg:        cfg,
		bootstrap:  bootstrap,
		context:    NewRunnerContextBuilder(cfg, deps),
		contextMax: defaultPromptMaxBytes,
	}
}

func (c *RunnerPromptCompiler) bootstrapForTrigger(triggerKind string) RunnerBootstrapContext {
	if c == nil {
		return RunnerBootstrapContext{}
	}
	if isAutonomousTrigger(triggerKind) {
		return LoadRunnerBootstrapContext(c.cfg)
	}
	return c.bootstrap
}

// Compile builds a tiered runner prompt unless the caller opts out.
func (c *RunnerPromptCompiler) Compile(ctx context.Context, in RunnerPromptCompileInput) RunnerPromptCompileResult {
	userTask := strings.TrimSpace(in.UserTask)
	mode := OR3ContextModeFromMeta(in.Meta)
	raw := userTask
	if mode == OR3ContextNone {
		return RunnerPromptCompileResult{
			Mode:           OR3ContextNone,
			UserTask:       userTask,
			CompiledPrompt: raw,
			RawTask:        raw,
			TriggerKind:    normalizeTriggerKind(in.TriggerKind),
		}
	}
	triggerKind := normalizeTriggerKind(in.TriggerKind)
	bootstrap := c.bootstrapForTrigger(triggerKind)
	stable := bootstrap.trustedBlocks()
	var volatile []string
	var memoryDebug runners.RunnerMemoryDebug
	if c.context != nil {
		ctxResult := c.context.BuildContextWithMeta(ctx, in.SessionKey, userTask, triggerKind, bootstrap)
		volatile = ctxResult.Blocks
		memoryDebug = ctxResult.Debug
	} else {
		volatile = bootstrap.contextBlocks(triggerKind)
	}
	for _, block := range in.ExtraContextBlocks {
		if strings.TrimSpace(block) != "" {
			volatile = append(volatile, block)
		}
	}
	compiled := runners.BuildRunnerPrompt(runners.RunnerPromptContext{
		TrustedSystemInstructions: stable,
		ContextBlocks:             volatile,
		UserMessage:               userTask,
		TriggerKind:               triggerKind,
		MaxBytes:                  c.contextMax,
	})
	return RunnerPromptCompileResult{
		Mode:               OR3ContextAuto,
		StableInstructions: stable,
		VolatileContext:    volatile,
		UserTask:           userTask,
		CompiledPrompt:     compiled,
		RawTask:            raw,
		TriggerKind:        triggerKind,
		MemoryDebug:        memoryDebug,
	}
}

// PrepareRunnerRunRequest applies OR3 prompt compilation to background runner jobs.
func (c *RunnerPromptCompiler) PrepareRunnerRunRequest(ctx context.Context, req runners.RunnerRunRequest) runners.RunnerRunRequest {
	if c == nil {
		return req
	}
	rawTask := strings.TrimSpace(req.Task)
	compiled := c.Compile(ctx, RunnerPromptCompileInput{
		SessionKey:  strings.TrimSpace(req.ParentSessionKey),
		UserTask:    req.Task,
		TriggerKind: agentRunTriggerKind(req.Meta),
		Meta:        req.Meta,
	})
	if compiled.Mode == OR3ContextNone {
		req.Task = compiled.RawTask
		return req
	}
	if req.Meta == nil {
		req.Meta = map[string]any{}
	}
	if _, exists := req.Meta["ui_task"]; !exists && rawTask != "" {
		req.Meta["ui_task"] = rawTask
	}
	req.Meta["or3_context_injected"] = true
	req.Task = compiled.CompiledPrompt
	return req
}

// OR3ContextModeFromMeta reads Meta["or3_context"]; default is auto.
func OR3ContextModeFromMeta(meta map[string]any) OR3ContextMode {
	if meta == nil {
		return OR3ContextAuto
	}
	raw, _ := meta[metaOR3Context].(string)
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case or3ContextNone, "off", "false", "0":
		return OR3ContextNone
	default:
		return OR3ContextAuto
	}
}

func normalizeTriggerKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "user_message"
	}
	return kind
}

func agentRunTriggerKind(meta map[string]any) string {
	if meta == nil {
		return "agent_run"
	}
	if raw, ok := meta["trigger_kind"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return "agent_run"
}
