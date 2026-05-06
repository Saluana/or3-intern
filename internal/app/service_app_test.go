package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

type registryProbeTool struct {
	name string
}

func (t registryProbeTool) Name() string               { return t.name }
func (t registryProbeTool) Description() string        { return t.name }
func (t registryProbeTool) Parameters() map[string]any { return map[string]any{} }
func (t registryProbeTool) Schema() map[string]any     { return map[string]any{} }
func (t registryProbeTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	reg := agent.ToolRegistryFromContext(ctx)
	if reg == nil {
		return "", errString("missing tool registry in context")
	}
	if reg.Get("replay_probe") == nil {
		return "", errString("expected replay_probe in restricted registry")
	}
	if reg.Get("blocked_probe") != nil {
		return "", errString("blocked_probe should not be available in restricted registry")
	}
	return "ok", nil
}

type errString string

func (e errString) Error() string { return string(e) }

func TestReplayToolCall_UsesRestrictedRegistryContext(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(registryProbeTool{name: "replay_probe"})
	registry.Register(registryProbeTool{name: "blocked_probe"})

	app := &ServiceApp{
		runtime: &agent.Runtime{
			Tools: registry,
		},
	}

	out, err := app.ReplayToolCall(context.Background(), ReplayToolCallRequest{
		ToolName:      "replay_probe",
		ArgumentsJSON: `{}`,
		AllowedTools:  []string{"replay_probe"},
		RestrictTools: true,
	})
	if err != nil {
		t.Fatalf("ReplayToolCall: %v", err)
	}
	if out != "ok" {
		t.Fatalf("expected ok, got %q", out)
	}
}

func TestDetectAgentCLIRunnersWithoutManager(t *testing.T) {
	app := NewServiceAppWithAgentCLI(config.Default(), nil, nil, nil, nil, nil)

	runners, err := app.DetectAgentCLIRunners(context.Background())
	if err != nil {
		t.Fatalf("DetectAgentCLIRunners: %v", err)
	}
	if len(runners) == 0 {
		t.Fatal("expected runner detection results")
	}
	foundOR3 := false
	foundExternal := false
	for _, runner := range runners {
		if runner.ID == string(agentcli.RunnerOR3) {
			foundOR3 = true
		}
		if runner.ID == string(agentcli.RunnerCodex) {
			foundExternal = true
		}
	}
	if !foundOR3 {
		t.Fatalf("expected built-in or3 runner in %#v", runners)
	}
	if !foundExternal {
		t.Fatalf("expected external runners in %#v", runners)
	}
}

func TestResumeApprovedRequest_ReplaysBlockedToolCall(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "service-app.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	providerCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		w.Header().Set("Content-Type", "application/json")
		var req providers.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if len(req.Messages) == 0 {
			t.Fatal("expected replay continuation messages")
		}
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"role":"assistant","content":"continued after approval"}}]}`)
	}))
	t.Cleanup(srv.Close)

	provider := providers.New(srv.URL, "test-key", 10*time.Second)
	provider.HTTP = srv.Client()
	registry := tools.NewRegistry()
	registry.Register(registryProbeTool{name: "resume_probe"})
	runtime := &agent.Runtime{
		DB:       database,
		Provider: provider,
		Model:    "gpt-4",
		Tools:    registry,
		Builder:  &agent.Builder{DB: database, HistoryMax: 20},
	}
	app := &ServiceApp{runtime: runtime}

	toolCall := providers.ToolCall{
		ID:   "tc-resume",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "resume_probe", Arguments: `{}`},
	}
	if _, err := database.AppendMessage(context.Background(), "sess-resume", "user", "run it", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if _, err := database.AppendMessage(context.Background(), "sess-resume", "assistant", "", map[string]any{"tool_calls": []providers.ToolCall{toolCall}}); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	blocked := tools.EncodeToolFailure("resume_probe", nil, "", &tools.ApprovalRequiredError{ToolName: "resume_probe", RequestID: 42})
	if _, err := database.AppendMessage(context.Background(), "sess-resume", "tool", blocked, map[string]any{"tool_call_id": "tc-resume"}); err != nil {
		t.Fatalf("AppendMessage tool: %v", err)
	}

	_, err = app.ResumeApprovedRequest(context.Background(), ResumeApprovedRequest{
		IssuedApproval: approval.IssuedApproval{Request: db.ApprovalRequestRecord{ID: 42, Type: string(approval.SubjectExec), RequesterSessionID: "sess-resume"}, Token: "approved-token"},
		Capability:     tools.CapabilitySafe,
	})
	if err != nil {
		t.Fatalf("ResumeApprovedRequest: %v", err)
	}
	if providerCalls != 1 {
		t.Fatalf("expected one continuation provider call, got %d", providerCalls)
	}
	pp, _, err := runtime.Builder.BuildWithOptions(context.Background(), agent.BuildOptions{SessionKey: "sess-resume"})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	if len(pp.History) < 2 {
		t.Fatalf("expected replayed history, got %#v", pp.History)
	}
	last := pp.History[len(pp.History)-1]
	content, ok := last.Content.(string)
	if last.Role != "assistant" || !ok || content != "continued after approval" {
		t.Fatalf("expected continued assistant reply, got %#v", last)
	}
}

func TestResumeApprovedRequest_DoesNotReplayAlreadyAppliedRequest(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "service-app-duplicate.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	providerCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	provider := providers.New(srv.URL, "test-key", 10*time.Second)
	provider.HTTP = srv.Client()
	registry := tools.NewRegistry()
	registry.Register(registryProbeTool{name: "resume_probe"})
	runtime := &agent.Runtime{
		DB:       database,
		Provider: provider,
		Model:    "gpt-4",
		Tools:    registry,
		Builder:  &agent.Builder{DB: database, HistoryMax: 20},
	}
	app := &ServiceApp{runtime: runtime}

	toolCall := providers.ToolCall{
		ID:   "tc-resume",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "resume_probe", Arguments: `{}`},
	}
	if _, err := database.AppendMessage(context.Background(), "sess-resume-duplicate", "user", "run it", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if _, err := database.AppendMessage(context.Background(), "sess-resume-duplicate", "assistant", "", map[string]any{"tool_calls": []providers.ToolCall{toolCall}}); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	blocked := tools.EncodeToolFailure("resume_probe", nil, "", &tools.ApprovalRequiredError{ToolName: "resume_probe", RequestID: 42})
	if _, err := database.AppendMessage(context.Background(), "sess-resume-duplicate", "tool", blocked, map[string]any{"tool_call_id": "tc-resume"}); err != nil {
		t.Fatalf("AppendMessage blocked tool: %v", err)
	}
	resumed := tools.EncodeToolResult(tools.ToolResult{Kind: "resume_probe", OK: true, Summary: "replayed"})
	if _, err := database.AppendMessage(context.Background(), "sess-resume-duplicate", "tool", resumed, map[string]any{"tool_call_id": "tc-resume"}); err != nil {
		t.Fatalf("AppendMessage resumed tool: %v", err)
	}

	out, err := app.ResumeApprovedRequest(context.Background(), ResumeApprovedRequest{
		IssuedApproval: approval.IssuedApproval{Request: db.ApprovalRequestRecord{ID: 42, Type: string(approval.SubjectExec), RequesterSessionID: "sess-resume-duplicate"}, Token: "approved-token"},
		Capability:     tools.CapabilitySafe,
	})
	if err != nil {
		t.Fatalf("ResumeApprovedRequest: %v", err)
	}
	if out != "Approval was already applied. The latest tool result is already in the conversation." {
		t.Fatalf("expected already-applied message, got %q", out)
	}
	if providerCalls != 0 {
		t.Fatalf("expected no provider calls once the request was already resumed, got %d", providerCalls)
	}
}
