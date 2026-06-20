package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/runners"
)

func openChannelCommandTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "channel-commands.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestParseChannelCommandAllowsTelegramBotSuffix(t *testing.T) {
	cmd, args, ok := parseChannelCommand("/runner@or3_bot opencode")
	if !ok || cmd != "runner" || len(args) != 1 || args[0] != "opencode" {
		t.Fatalf("unexpected parse result: cmd=%q args=%#v ok=%v", cmd, args, ok)
	}
}

func TestChannelCommandHandlerPersistsAndInjectsWorkspace(t *testing.T) {
	ctx := context.Background()
	database := openChannelCommandTestDB(t)
	handler := &channelCommandHandler{Config: config.Default(), DB: database}
	workspace := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ev := bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:workspace", Message: "/workspace " + workspace}
	if _, handled, err := handler.Handle(ctx, ev); !handled || err != nil {
		t.Fatalf("expected /workspace handled without error, handled=%v err=%v", handled, err)
	}
	meta, err := database.GetChatSessionMeta(ctx, ev.SessionKey)
	if err != nil {
		t.Fatalf("GetChatSessionMeta: %v", err)
	}
	if meta.RunnerCwd != workspace {
		t.Fatalf("runner cwd = %q, want %q", meta.RunnerCwd, workspace)
	}
	next, handled, err := handler.Handle(ctx, bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: ev.SessionKey, Message: "pwd", Meta: map[string]any{}})
	if handled || err != nil {
		t.Fatalf("expected normal turn to continue, handled=%v err=%v", handled, err)
	}
	if next.Meta["cwd"] != workspace || next.Meta["_cwd"] != workspace {
		t.Fatalf("expected cwd metadata injection, got %#v", next.Meta)
	}
}

func TestResolveChannelWorkspacePathExpandsHomeAndRejectsFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	got, err := resolveChannelWorkspacePath("~/project")
	if err != nil || got != project {
		t.Fatalf("resolved = %q, err=%v, want %q", got, err, project)
	}
	file := filepath.Join(home, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := resolveChannelWorkspacePath(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected file rejection, got %v", err)
	}
}

func TestChannelCommandHandlerPersistsAndInjectsPreferences(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Runners.Default = "opencode"
	database := openChannelCommandTestDB(t)
	handler := &channelCommandHandler{Config: cfg, DB: database}
	ev := bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", From: "123", Message: "/runner opencode", Meta: map[string]any{"chat_id": "123"}}
	if _, handled, err := handler.Handle(ctx, ev); !handled || err != nil {
		t.Fatalf("expected /runner handled without error, handled=%v err=%v", handled, err)
	}
	meta, err := database.GetChatSessionMeta(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("GetChatSessionMeta: %v", err)
	}
	if meta.RunnerID != "opencode" || meta.RunnerModel != "" {
		t.Fatalf("unexpected saved preference: %#v", meta)
	}
	next, handled, err := handler.Handle(ctx, bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "hello", Meta: map[string]any{}})
	if handled || err != nil {
		t.Fatalf("expected normal turn to continue, handled=%v err=%v", handled, err)
	}
	if next.Meta["runner_id"] != "opencode" {
		t.Fatalf("expected runner metadata injection, got %#v", next.Meta)
	}
}

func TestChannelCommandHandlerResetClearsPreferences(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Runners.Default = "opencode"
	database := openChannelCommandTestDB(t)
	if _, err := database.SetChatSessionRunnerPreference(ctx, "telegram:123", "opencode", "OpenCode", "gpt-5"); err != nil {
		t.Fatalf("SetChatSessionRunnerPreference: %v", err)
	}
	if _, err := database.SetChatSessionRunnerCwd(ctx, "telegram:123", t.TempDir()); err != nil {
		t.Fatalf("SetChatSessionRunnerCwd: %v", err)
	}
	handler := &channelCommandHandler{Config: cfg, DB: database}
	if _, handled, err := handler.Handle(ctx, bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/reset"}); !handled || err != nil {
		t.Fatalf("expected /reset handled without error, handled=%v err=%v", handled, err)
	}
	meta, err := database.GetChatSessionMeta(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("GetChatSessionMeta: %v", err)
	}
	if meta.RunnerID != "" || meta.RunnerLabel != "" || meta.RunnerModel != "" || meta.RunnerCwd != "" {
		t.Fatalf("expected cleared preference, got %#v", meta)
	}
}

func TestChannelCommandHandlerDoesNotInterceptApprovalCommands(t *testing.T) {
	handler := &channelCommandHandler{Config: config.Default(), DB: openChannelCommandTestDB(t)}
	_, handled, err := handler.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/approve req_123"})
	if handled || err != nil {
		t.Fatalf("expected approval command to pass through, handled=%v err=%v", handled, err)
	}
}

func TestChannelCommandHandlerNormalizesOpenCodeProviderModel(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Runners.Default = string(runners.RunnerOpenCode)
	database := openChannelCommandTestDB(t)
	handler := &channelCommandHandler{Config: cfg, DB: database}

	ev := bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/model openrouter/deepseek-v4-flash-free"}
	if _, handled, err := handler.Handle(ctx, ev); !handled || err != nil {
		t.Fatalf("expected /model handled without error, handled=%v err=%v", handled, err)
	}
	meta, err := database.GetChatSessionMeta(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("GetChatSessionMeta: %v", err)
	}
	if meta.RunnerModel != "deepseek-v4-flash-free" {
		t.Fatalf("expected canonical OpenCode model id, got %#v", meta)
	}
}

func TestChannelCommandHandlerOpenCodeAllowsModelOutsidePartialCatalog(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Runners.Default = string(runners.RunnerOpenCode)
	database := openChannelCommandTestDB(t)
	catalog := []runners.RunnerModelInfo{
		{ID: "gpt-5", DisplayName: "GPT-5", Provider: "openai", ProviderName: "OpenAI"},
	}
	handler := &channelCommandHandler{
		Config:        cfg,
		DB:            database,
		RunnerManager: &runners.Manager{Registry: runners.NewRunnerRegistry(runners.SelectableRunners(), nil), Runtimes: modelRuntimeRegistry(catalog)},
	}

	ev := bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/model nemotron-3-ultra-free"}
	if _, handled, err := handler.Handle(ctx, ev); !handled || err != nil {
		t.Fatalf("expected /model handled without error, handled=%v err=%v", handled, err)
	}
	meta, err := database.GetChatSessionMeta(ctx, "telegram:123")
	if err != nil {
		t.Fatalf("GetChatSessionMeta: %v", err)
	}
	if meta.RunnerModel != "nemotron-3-ultra-free" {
		t.Fatalf("expected saved OpenCode model outside partial catalog, got %#v", meta)
	}
}

func TestChannelCommandHandlerSavedOpenCodeModelSurvivesPartialCatalogRefresh(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Runners.Default = string(runners.RunnerOpenCode)
	database := openChannelCommandTestDB(t)
	if _, err := database.SetChatSessionRunnerPreference(ctx, "telegram:123", string(runners.RunnerOpenCode), "OpenCode", "nemotron-3-ultra-free"); err != nil {
		t.Fatalf("SetChatSessionRunnerPreference: %v", err)
	}
	catalog := []runners.RunnerModelInfo{
		{ID: "gpt-5", DisplayName: "GPT-5", Provider: "openai", ProviderName: "OpenAI"},
	}
	handler := &channelCommandHandler{
		Config:        cfg,
		DB:            database,
		RunnerManager: &runners.Manager{Registry: runners.NewRunnerRegistry(runners.SelectableRunners(), nil), Runtimes: modelRuntimeRegistry(catalog)},
	}

	next, handled, err := handler.Handle(ctx, bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "Can you help me bruv. I lost my keys", Meta: map[string]any{}})
	if handled || err != nil {
		t.Fatalf("expected saved OpenCode model to continue despite partial catalog, handled=%v err=%v", handled, err)
	}
	if next.Meta["runner_id"] != string(runners.RunnerOpenCode) || next.Meta["model"] != "nemotron-3-ultra-free" {
		t.Fatalf("expected runner/model metadata injection, got %#v", next.Meta)
	}
}

func TestChannelCommandHandlerModelsShowsProviderButtons(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	catalog := []runners.RunnerModelInfo{
		{ID: "kimi-k2.5", DisplayName: "Kimi K2.5", Provider: "openrouter", ProviderName: "OpenRouter"},
		{ID: "gpt-5", DisplayName: "GPT-5", Provider: "openai", ProviderName: "OpenAI"},
	}
	channels := rootchannels.NewManager()
	capture := &captureChannel{name: "telegram"}
	if err := channels.Register(capture); err != nil {
		t.Fatalf("Register: %v", err)
	}
	handler := &channelCommandHandler{
		Config:        cfg,
		DB:            openChannelCommandTestDB(t),
		RunnerManager: &runners.Manager{Registry: runners.NewRunnerRegistry(runners.SelectableRunners(), nil), Runtimes: modelRuntimeRegistry(catalog)},
		Channels:      channels,
	}

	ev := bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/models", Meta: map[string]any{"chat_id": "123", "reply_to_message_id": int64(99)}}
	if _, handled, err := handler.Handle(ctx, ev); !handled || err != nil {
		t.Fatalf("expected /models handled without error, handled=%v err=%v", handled, err)
	}
	if got := capture.lastText(); !strings.Contains(got, "Tap a provider") || !strings.Contains(got, "/model <exact-id>") {
		t.Fatalf("unexpected provider picker text: %q", got)
	}
	if len(capture.metas) != 1 {
		t.Fatalf("expected one delivery meta, got %#v", capture.metas)
	}
	if _, ok := capture.metas[0]["telegram_reply_markup"]; !ok {
		t.Fatalf("expected telegram provider buttons, got %#v", capture.metas[0])
	}
	if capture.metas[0]["reply_to_message_id"] != int64(99) {
		t.Fatalf("expected reply metadata to be preserved, got %#v", capture.metas[0])
	}
}

func TestChannelCommandHandlerModelsProviderListsExactIDs(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	catalog := []runners.RunnerModelInfo{
		{ID: "kimi-k2.5", DisplayName: "Kimi K2.5", Provider: "openrouter", ProviderName: "OpenRouter"},
		{ID: "gpt-5", DisplayName: "GPT-5", Provider: "openai", ProviderName: "OpenAI"},
	}
	channels := rootchannels.NewManager()
	capture := &captureChannel{name: "telegram"}
	if err := channels.Register(capture); err != nil {
		t.Fatalf("Register: %v", err)
	}
	handler := &channelCommandHandler{
		Config:        cfg,
		DB:            openChannelCommandTestDB(t),
		RunnerManager: &runners.Manager{Registry: runners.NewRunnerRegistry(runners.SelectableRunners(), nil), Runtimes: modelRuntimeRegistry(catalog)},
		Channels:      channels,
	}

	ev := bus.Event{Type: bus.EventUserMessage, Channel: "telegram", SessionKey: "telegram:123", Message: "/models opencode openrouter", Meta: map[string]any{"chat_id": "123"}}
	if _, handled, err := handler.Handle(ctx, ev); !handled || err != nil {
		t.Fatalf("expected /models handled without error, handled=%v err=%v", handled, err)
	}
	got := capture.lastText()
	if !strings.Contains(got, "- kimi-k2.5") || strings.Contains(got, "- gpt-5") || !strings.Contains(got, "/model kimi-k2.5") {
		t.Fatalf("unexpected provider model list: %q", got)
	}
}

type channelCommandModelRuntime struct {
	models []runners.RunnerModelInfo
}

func (r channelCommandModelRuntime) ID() runners.RunnerID { return runners.RunnerOpenCode }
func (r channelCommandModelRuntime) Info(context.Context, config.RunnersConfig, []string) runners.RunnerRuntimeInfo {
	return runners.RunnerRuntimeInfo{Models: r.models}
}
func (r channelCommandModelRuntime) Execute(context.Context, runners.NativeRuntimeExecuteRequest) (runners.ProcessOutput, error) {
	return runners.ProcessOutput{}, nil
}
func (r channelCommandModelRuntime) Abort(context.Context, string) error { return nil }
func (r channelCommandModelRuntime) Stop(context.Context) error          { return nil }

func modelRuntimeRegistry(models []runners.RunnerModelInfo) *runners.RunnerRuntimeRegistry {
	registry := &runners.RunnerRuntimeRegistry{}
	registry.Register(channelCommandModelRuntime{models: models})
	return registry
}
