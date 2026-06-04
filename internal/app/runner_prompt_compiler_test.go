package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/scope"
)

func TestRunnerPromptCompilerIncludesStableAndVolatile(t *testing.T) {
	cfg := config.Default()
	compiler := NewRunnerPromptCompiler(cfg, RunnerBootstrapContext{
		Soul:              "You are OR3 soul.",
		AgentInstructions: "Follow AGENTS.",
		ToolNotes:         "Use TOOLS.",
		IdentityText:      "IDENTITY",
		StaticMemory:      "MEMORY",
	}, RunnerContextDeps{})
	out := compiler.Compile(context.Background(), RunnerPromptCompileInput{
		SessionKey:  "cli:default",
		UserTask:    "do the thing",
		TriggerKind: "user_message",
	})
	if !strings.Contains(out.CompiledPrompt, "You are OR3 soul.") {
		t.Fatalf("missing soul in compiled prompt: %q", out.CompiledPrompt)
	}
	if !strings.Contains(out.CompiledPrompt, "<user_task>") || !strings.Contains(out.CompiledPrompt, "do the thing") {
		t.Fatalf("missing user task: %q", out.CompiledPrompt)
	}
	for _, excluded := range []string{"Follow AGENTS.", "Use TOOLS."} {
		if strings.Contains(out.CompiledPrompt, excluded) {
			t.Fatalf("runner-native instructions should not be injected: %q", out.CompiledPrompt)
		}
	}
	if len(out.StableInstructions) != 3 {
		t.Fatalf("expected stable blocks, got %#v", out.StableInstructions)
	}
}

func TestRunnerPromptCompilerOptOut(t *testing.T) {
	cfg := config.Default()
	compiler := NewRunnerPromptCompiler(cfg, RunnerBootstrapContext{Soul: "hidden"}, RunnerContextDeps{})
	out := compiler.Compile(context.Background(), RunnerPromptCompileInput{
		UserTask: "raw only",
		Meta:     map[string]any{"or3_context": "none"},
	})
	if out.CompiledPrompt != "raw only" {
		t.Fatalf("expected raw task, got %q", out.CompiledPrompt)
	}
	if out.Mode != OR3ContextNone {
		t.Fatalf("mode=%q", out.Mode)
	}
}

func TestRunnerPromptCompilerPlacesExtraContextBeforeUserTask(t *testing.T) {
	cfg := config.Default()
	compiler := NewRunnerPromptCompiler(cfg, RunnerBootstrapContext{Soul: "stable soul"}, RunnerContextDeps{})
	out := compiler.Compile(context.Background(), RunnerPromptCompileInput{
		SessionKey:         "cli:default",
		UserTask:           "current task",
		TriggerKind:        "user_message",
		ExtraContextBlocks: []string{"replay_history:\nUser: previous\nAssistant: answer"},
	})
	prompt := out.CompiledPrompt
	if !strings.HasPrefix(prompt, "<trusted_or3_system_instructions>") {
		t.Fatalf("compiled prompt must start with trusted instructions: %q", prompt)
	}
	historyIdx := strings.Index(prompt, "replay_history:")
	userIdx := strings.Index(prompt, "<user_task>")
	if historyIdx < 0 || userIdx < 0 || historyIdx > userIdx {
		t.Fatalf("expected replay history before user task, got %q", prompt)
	}
	if !strings.Contains(prompt, "\ncurrent task\n</user_task>") {
		t.Fatalf("current task not isolated in user task block: %q", prompt)
	}
}

func TestRunnerPromptCompilerInjectsBoundedMemoryContext(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "memory-context.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := database.UpsertPinned(ctx, scope.GlobalMemoryScope, "shared", "global pinned fact"); err != nil {
		t.Fatalf("UpsertPinned global: %v", err)
	}
	if err := database.UpsertPinned(ctx, "cli:default", "local", "session pinned fact"); err != nil {
		t.Fatalf("UpsertPinned local: %v", err)
	}
	compiler := NewRunnerPromptCompiler(config.Default(), RunnerBootstrapContext{
		Soul:         "stable soul",
		StaticMemory: strings.Repeat("static memory ", 200),
	}, RunnerContextDeps{DB: database})
	out := compiler.Compile(ctx, RunnerPromptCompileInput{
		SessionKey:  "cli:default",
		UserTask:    "current task",
		TriggerKind: "user_message",
	})
	prompt := out.CompiledPrompt
	for _, want := range []string{"memory_context:", "pinned_memory:", "shared: global pinned fact", "local: session pinned fact", "memory_digest:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
	if strings.Contains(prompt, strings.Repeat("static memory ", 200)) {
		t.Fatalf("static memory should be compacted, got %q", prompt)
	}
	userBlock := prompt[strings.Index(prompt, "<user_task>"):]
	if strings.Contains(userBlock, "pinned_memory") || strings.Contains(userBlock, "memory_digest") {
		t.Fatalf("memory leaked into user task block: %q", userBlock)
	}
}

func TestRunnerPromptCompilerInjectsRetrievedMemoryInContext(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "retrieved-memory.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if _, err := database.InsertMemoryNote(ctx, "cli:default", "orchid fact: water on Fridays", nil, sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	compiler := NewRunnerPromptCompiler(config.Default(), RunnerBootstrapContext{Soul: "stable soul"}, RunnerContextDeps{
		DB:  database,
		Mem: memory.NewRetriever(database),
	})
	out := compiler.Compile(ctx, RunnerPromptCompileInput{
		SessionKey:  "cli:default",
		UserTask:    "orchid",
		TriggerKind: "user_message",
	})
	prompt := out.CompiledPrompt
	if !strings.Contains(prompt, "memory_context:") || !strings.Contains(prompt, "retrieved_memory:") {
		t.Fatalf("expected retrieved memory context, got %q", prompt)
	}
	userBlock := prompt[strings.Index(prompt, "<user_task>"):]
	if strings.Contains(userBlock, "water on Fridays") {
		t.Fatalf("retrieved memory leaked into user task block: %q", userBlock)
	}
}

func TestPrepareAgentRunRequestDefaultsToCompiledContext(t *testing.T) {
	cfg := config.Default()
	compiler := NewRunnerPromptCompiler(cfg, RunnerBootstrapContext{Soul: "bootstrap soul"}, RunnerContextDeps{})
	req := compiler.PrepareAgentRunRequest(context.Background(), agentcli.AgentRunRequest{
		ParentSessionKey: "cron:job",
		Task:             "review repo",
	})
	if !strings.Contains(req.Task, "bootstrap soul") {
		t.Fatalf("expected compiled OR3 context, got %q", req.Task)
	}
	if !strings.Contains(req.Task, "review repo") {
		t.Fatalf("expected user task preserved: %q", req.Task)
	}
	if req.Meta["ui_task"] != "review repo" {
		t.Fatalf("expected raw ui_task metadata, got %#v", req.Meta)
	}
}

func TestPrepareAgentRunRequestHonorsOptOut(t *testing.T) {
	cfg := config.Default()
	compiler := NewRunnerPromptCompiler(cfg, RunnerBootstrapContext{Soul: "bootstrap soul"}, RunnerContextDeps{})
	req := compiler.PrepareAgentRunRequest(context.Background(), agentcli.AgentRunRequest{
		Task: "plain task",
		Meta: map[string]any{"or3_context": "none"},
	})
	if req.Task != "plain task" {
		t.Fatalf("expected raw task, got %q", req.Task)
	}
}
