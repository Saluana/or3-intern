package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

type rawDispatchTool struct {
	commandName string
	skillName   string
	command     string
	secret      string
}

type inspectEnvTool struct {
	secret string
}

func (t *rawDispatchTool) Name() string               { return "raw_dispatch" }
func (t *rawDispatchTool) Description() string        { return "captures raw slash command dispatch" }
func (t *rawDispatchTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (t *rawDispatchTool) Schema() map[string]any {
	return tools.Base{}.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *rawDispatchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	t.commandName = strings.TrimSpace(params["commandName"].(string))
	t.skillName = strings.TrimSpace(params["skillName"].(string))
	t.command = strings.TrimSpace(params["command"].(string))
	t.secret = tools.EnvFromContext(ctx)["API_SECRET"]
	return "ran:" + t.command, nil
}

func (t *inspectEnvTool) Name() string               { return "inspect_env" }
func (t *inspectEnvTool) Description() string        { return "captures scoped env during tool calls" }
func (t *inspectEnvTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (t *inspectEnvTool) Schema() map[string]any {
	return tools.Base{}.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *inspectEnvTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	t.secret = tools.EnvFromContext(ctx)["API_SECRET"]
	return "env:" + t.secret, nil
}

func makeRuntimeSkillInventory(t *testing.T, body string, entry skills.EntryConfig) skills.Inventory {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "slash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	return skills.ScanWithOptions(skills.LoadOptions{
		Roots: []skills.Root{{Path: root, Source: skills.SourceWorkspace}},
		Entries: map[string]skills.EntryConfig{
			"slash": entry,
		},
		AvailableTools: map[string]struct{}{"inspect_env": {}, "raw_dispatch": {}, "read_skill": {}, "exec": {}},
		Env:            map[string]string{},
	})
}

func TestRuntime_ExplicitSkillCommand_DispatchesRawArgsAndScopedEnv(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	tool := &rawDispatchTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	rt := &Runtime{
		DB:    d,
		Tools: reg,
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: dispatches raw args
command-dispatch: tool
command-tool: raw_dispatch
metadata:
  openclaw:
    primaryEnv: API_SECRET
---
# Slash
`, skills.EntryConfig{APIKey: "secret-value"})},
		Deliver: deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-slash",
		Channel:    "cli",
		From:       "user",
		Message:    "/slash hello there",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if tool.commandName != "slash" || tool.skillName != "slash" || tool.command != "hello there" {
		t.Fatalf("unexpected dispatch payload: %#v", tool)
	}
	if tool.secret != "secret-value" {
		t.Fatalf("expected scoped env injection, got %q", tool.secret)
	}
	if os.Getenv("API_SECRET") != "" {
		t.Fatal("expected process env to remain untouched")
	}
	if got := strings.Join(deliver.messages, "\n"); !strings.Contains(got, "ran:hello there") {
		t.Fatalf("expected dispatched tool output, got %q", got)
	}
}

func TestRuntime_ExplicitSkillCommand_ExecDispatchRemainsRunnableWithoutShellPermissionMetadata(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	reg := tools.NewRegistry()
	reg.Register(&tools.ExecTool{Timeout: 5 * time.Second, EnableLegacyShell: true})
	rt := &Runtime{
		DB:    d,
		Tools: reg,
		Hardening: config.HardeningConfig{
			PrivilegedTools: true,
		},
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: dispatches raw args to exec
command-dispatch: tool
command-tool: exec
---
# Slash
`, skills.EntryConfig{})},
		Deliver: deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-exec-dispatch", Channel: "cli", From: "user", Message: "/slash echo hello"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := strings.Join(deliver.messages, "\n"); !strings.Contains(got, "hello") {
		t.Fatalf("expected exec dispatch output, got %q", got)
	}
}

func TestRuntime_NonSkillSlashMessage_FallsBackToModel(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "handled as chat"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4.1-mini",
		Tools:        tools.NewRegistry(),
		MaxToolLoops: 2,
		Builder:      &Builder{DB: d, HistoryMax: 10},
		Deliver:      deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-missing",
		Channel:    "cli",
		From:       "user",
		Message:    "/unknown do stuff",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(deliver.messages) == 0 || deliver.messages[0] != "handled as chat" {
		t.Fatalf("unexpected delivery: %#v", deliver.messages)
	}
}

func TestRuntime_NormalToolCalls_DoNotReceiveGlobalSkillEnv(t *testing.T) {
	d := openRuntimeTestDB(t)
	tool := &inspectEnvTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "inspect_env", Arguments: `{}`},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: "done"},
			}},
		})
	}))
	defer server.Close()

	provider := providers.New(server.URL, "k", 5*time.Second)
	provider.HTTP = server.Client()
	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4.1-mini",
		Tools:        reg,
		MaxToolLoops: 2,
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: seeded skill
metadata:
  openclaw:
    primaryEnv: API_SECRET
---
# Slash
`, skills.EntryConfig{APIKey: "secret-value"})},
		Deliver: deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-normal-env",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if tool.secret != "" {
		t.Fatalf("expected no skill env on normal tool call, got %q", tool.secret)
	}
}

func TestRuntime_ExplicitSkillConversation_ToolCallsReceiveScopedEnv(t *testing.T) {
	d := openRuntimeTestDB(t)
	tool := &inspectEnvTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "inspect_env", Arguments: `{}`},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: "done"},
			}},
		})
	}))
	defer server.Close()

	provider := providers.New(server.URL, "k", 5*time.Second)
	provider.HTTP = server.Client()
	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4.1-mini",
		Tools:        reg,
		MaxToolLoops: 2,
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: seeded skill
metadata:
  openclaw:
    primaryEnv: API_SECRET
---
# Slash
Use this skill carefully.
`, skills.EntryConfig{APIKey: "secret-value"})},
		Deliver: deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-explicit-env",
		Channel:    "cli",
		From:       "user",
		Message:    "/slash run the tool",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if tool.secret != "secret-value" {
		t.Fatalf("expected scoped env injection, got %q", tool.secret)
	}
}

func TestRuntime_ExplicitSkillConversation_DeclaredToolAllowlistIsEnforced(t *testing.T) {
	d := openRuntimeTestDB(t)
	tool := &inspectEnvTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "inspect_env", Arguments: `{}`},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: "done"},
			}},
		})
	}))
	defer server.Close()

	provider := providers.New(server.URL, "k", 5*time.Second)
	provider.HTTP = server.Client()
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4.1-mini",
		Tools:        reg,
		MaxToolLoops: 2,
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: seeded skill
tools: [read_skill]
metadata:
  openclaw:
    primaryEnv: API_SECRET
---
# Slash
Use this skill carefully.
`, skills.EntryConfig{APIKey: "secret-value"})},
		Deliver: &mockDeliverer{},
	}

	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-skill-tools", Channel: "cli", From: "user", Message: "/slash run the tool"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if tool.secret != "" {
		t.Fatalf("expected denied tool to not receive env, got %q", tool.secret)
	}
	msgs, err := d.GetLastMessages(context.Background(), "sess-skill-tools", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "tool denied by skill policy: inspect_env") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected skill policy denial in tool history, got %#v", msgs)
	}
}

func TestRuntime_ExplicitSkillConversation_ExecutionPermissionIsEnforced(t *testing.T) {
	d := openRuntimeTestDB(t)
	reg := tools.NewRegistry()
	reg.Register(&tools.ExecTool{AllowedPrograms: []string{"echo"}, Timeout: 5 * time.Second})

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "exec", Arguments: `{"program":"echo","args":["hi"]}`},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: "done"},
			}},
		})
	}))
	defer server.Close()

	provider := providers.New(server.URL, "k", 5*time.Second)
	provider.HTTP = server.Client()
	hardening := config.Default().Hardening
	hardening.GuardedTools = true
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4.1-mini",
		Tools:        reg,
		MaxToolLoops: 2,
		Hardening:    hardening,
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: seeded skill
tools: [exec]
permissions:
  shell: false
---
# Slash
No execution permission.
`, skills.EntryConfig{})},
		Deliver: &mockDeliverer{},
	}

	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-skill-exec", Channel: "cli", From: "user", Message: "/slash run the tool"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	msgs, err := d.GetLastMessages(context.Background(), "sess-skill-exec", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "execution denied by skill policy: exec") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected skill execution denial in tool history, got %#v", msgs)
	}
}

func TestRuntime_ExplicitSkillCommand_FallbackSeedsModel(t *testing.T) {
	d := openRuntimeTestDB(t)
	var captured providers.ChatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(providers.ChatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{
				Message: struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				}{Role: "assistant", Content: "seeded"},
			}},
		})
	}))
	defer server.Close()

	provider := providers.New(server.URL, "k", 5*time.Second)
	provider.HTTP = server.Client()
	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4.1-mini",
		Tools:        tools.NewRegistry(),
		MaxToolLoops: 2,
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: seeded skill
---
# Slash
Use this skill carefully.
`, skills.EntryConfig{})},
		Deliver: deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-seeded",
		Channel:    "cli",
		From:       "user",
		Message:    "/slash solve this",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(captured.Messages) < 2 {
		t.Fatalf("expected seeded system message, got %#v", captured.Messages)
	}
	found := false
	for _, msg := range captured.Messages {
		text, _ := msg.Content.(string)
		if strings.Contains(text, "Explicit skill requested: slash") && strings.Contains(text, "Use this skill carefully.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected explicit skill seed in messages: %#v", captured.Messages)
	}
	if len(deliver.messages) == 0 || deliver.messages[0] != "seeded" {
		t.Fatalf("unexpected final delivery: %#v", deliver.messages)
	}
}

func TestRuntime_ExplicitSkillCommand_UnsupportedDispatchTool(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:    d,
		Tools: tools.NewRegistry(),
		Builder: &Builder{DB: d, HistoryMax: 10, Skills: makeRuntimeSkillInventory(t, `---
name: slash
description: dispatches to unsupported tool
command-dispatch: tool
command-tool: missing_tool
---
# Slash
`, skills.EntryConfig{})},
		Deliver: deliver,
	}

	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-unsupported",
		Channel:    "cli",
		From:       "user",
		Message:    "/slash hi",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(deliver.messages) == 0 || !strings.Contains(deliver.messages[0], "requires unsupported tool: missing_tool") {
		t.Fatalf("unexpected delivery: %#v", deliver.messages)
	}
}
