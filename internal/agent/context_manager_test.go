package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/providers"
)

func TestContextManagerProposalRejectsUnknownJSON(t *testing.T) {
	_, err := parseContextManagerProposal(`{"unknown": true}`)
	if err == nil {
		t.Fatalf("expected unknown fields to be rejected")
	}
}

func TestContextManagerProposalRejectsTrailingText(t *testing.T) {
	_, err := parseContextManagerProposal(`{"compaction":{"summary":"x"}} extra`)
	if err == nil || !strings.Contains(err.Error(), "extra JSON content") {
		t.Fatalf("expected trailing text rejection, got %v", err)
	}
}

func TestContextManagerValidationRejectsProtectedAndDeletes(t *testing.T) {
	policy := ContextManagerPolicy{Enabled: true, AllowTaskUpdates: true, AllowStalePropose: true, ScopeKey: "sess"}
	if err := validateContextManagerProposal(ContextManagerProposal{DeleteMemoryIDs: []int64{1}}, policy); err == nil || !strings.Contains(err.Error(), "delete") {
		t.Fatalf("expected memory delete rejection, got %v", err)
	}
	if err := validateContextManagerProposal(ContextManagerProposal{ProtectedEdits: []string{"SOUL.md"}}, policy); err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("expected protected section rejection, got %v", err)
	}
}

func TestContextManagerSafeTaskCardMerge(t *testing.T) {
	card := applyContextManagerTaskUpdate(TaskCard{Goal: "old", Constraints: []string{"no bloat"}}, TaskCardUpdateProposal{
		Goal:      "ship packets",
		Decisions: []string{"reuse existing packages"},
		Status:    "active",
	})
	if card.Goal != "ship packets" || len(card.Constraints) != 1 || len(card.Decisions) != 1 {
		t.Fatalf("expected safe merge preserving constraints, got %+v", card)
	}
}

func TestContextManagerStaleProposalScopeAndActionGuards(t *testing.T) {
	policy := ContextManagerPolicy{Enabled: true, AllowStalePropose: true, ScopeKey: "sess"}
	badAction := ContextManagerProposal{StaleProposals: []StaleMemoryProposal{{MemoryID: 1, Action: "delete", ScopeKey: "sess"}}}
	if err := validateContextManagerProposal(badAction, policy); err == nil {
		t.Fatalf("expected unsafe stale action rejection")
	}
	badScope := ContextManagerProposal{StaleProposals: []StaleMemoryProposal{{MemoryID: 1, Action: "mark_stale", ScopeKey: "other"}}}
	if err := validateContextManagerProposal(badScope, policy); err == nil {
		t.Fatalf("expected stale proposal scope rejection")
	}
}

func TestContextManagerCompactionValidation(t *testing.T) {
	policy := ContextManagerPolicy{Enabled: true, AllowTaskUpdates: true, ScopeKey: "sess"}
	missingSummary := ContextManagerProposal{Compaction: &CompactionProposal{CompactThroughMessageID: 42}}
	if err := validateContextManagerProposal(missingSummary, policy); err == nil {
		t.Fatalf("expected compaction summary requirement")
	}
	valid := ContextManagerProposal{Compaction: &CompactionProposal{Summary: "old context resolved", CompactThroughMessageID: 42, Refs: []string{"messages:1-42"}}}
	if err := validateContextManagerProposal(valid, policy); err != nil {
		t.Fatalf("expected valid compaction proposal: %v", err)
	}
}

func TestContextManagerTriggersAndFallback(t *testing.T) {
	policy := ContextManagerPolicy{Enabled: true, AllowedTriggers: []ContextManagerTrigger{ContextTriggerOverBudget}}
	if !shouldTriggerContextManager(policy, ContextTriggerOverBudget, BudgetReport{Pressure: "high"}, 0) {
		t.Fatalf("expected high pressure to trigger")
	}
	if shouldTriggerContextManager(policy, ContextTriggerLargeToolOutput, BudgetReport{Pressure: "high"}, 0) {
		t.Fatalf("expected disallowed trigger to be skipped")
	}
	fallback := deterministicContextManagerFallback(BudgetReport{})
	if len(fallback) == 0 || !strings.Contains(fallback[0].Reason, "deterministic") {
		t.Fatalf("expected deterministic fallback reason, got %+v", fallback)
	}
}

func TestNormalizeCompactionCutoff(t *testing.T) {
	messages := []contextManagerMessage{{ID: 400}, {ID: 404}, {ID: 406}}
	cutoff, adjusted, err := normalizeCompactionCutoff(405, messages)
	if err != nil {
		t.Fatalf("normalizeCompactionCutoff: %v", err)
	}
	if cutoff != 404 || !adjusted {
		t.Fatalf("expected cutoff 404 adjusted=true, got cutoff=%d adjusted=%v", cutoff, adjusted)
	}
	cutoff, adjusted, err = normalizeCompactionCutoff(406, messages)
	if err != nil {
		t.Fatalf("normalizeCompactionCutoff exact: %v", err)
	}
	if cutoff != 406 || adjusted {
		t.Fatalf("expected exact cutoff without adjustment, got cutoff=%d adjusted=%v", cutoff, adjusted)
	}
}

func TestRequestContextManagerCompaction_ProviderAndRetryPaths(t *testing.T) {
	input := contextManagerPruneInput{SessionKey: "sess", Messages: []contextManagerMessage{{ID: 1, Role: "user", Content: "hello"}}}

	if _, err := requestContextManagerCompaction(context.Background(), nil, "", input); err == nil || !strings.Contains(err.Error(), "provider not configured") {
		t.Fatalf("expected nil-provider error, got %v", err)
	}

	var requests []providers.ChatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req providers.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, req)
		if len(requests) == 1 {
			http.Error(w, "tool_choice unsupported by provider", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{{
						"type": "function",
						"function": map[string]any{
							"name":      contextManagerCompactToolName,
							"arguments": `{"compaction":{"summary":"keep the task card","compact_through_message_id":1}}`,
						},
					}},
				},
			}},
		})
	}))
	defer server.Close()

	provider := providers.New(server.URL, "test-key", 5*time.Second)
	provider.HTTP = server.Client()
	proposal, err := requestContextManagerCompaction(context.Background(), provider, "", input)
	if err != nil {
		t.Fatalf("requestContextManagerCompaction: %v", err)
	}
	if proposal.Compaction == nil || proposal.Compaction.CompactThroughMessageID != 1 {
		t.Fatalf("expected parsed compaction proposal, got %+v", proposal)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 chat attempts, got %d", len(requests))
	}
	if requests[0].Model != "gpt-4.1-mini" {
		t.Fatalf("expected default model, got %q", requests[0].Model)
	}
	if requests[0].ToolChoice != "required" {
		t.Fatalf("expected first request to require tool choice, got %#v", requests[0].ToolChoice)
	}
	if requests[1].ToolChoice != nil {
		t.Fatalf("expected retry without tool choice, got %#v", requests[1].ToolChoice)
	}
	if len(requests[1].Messages) != 2 {
		t.Fatalf("expected immediate retry to reuse original messages, got %d", len(requests[1].Messages))
	}
}

func TestRequestContextManagerCompaction_ReturnsParseErrorAfterTwoAttempts(t *testing.T) {
	var requests []providers.ChatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req providers.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{{
						"type": "function",
						"function": map[string]any{
							"name":      "other_tool",
							"arguments": `{}`,
						},
					}},
				},
			}},
		})
	}))
	defer server.Close()

	provider := providers.New(server.URL, "test-key", 5*time.Second)
	provider.HTTP = server.Client()
	_, err := requestContextManagerCompaction(context.Background(), provider, "compact-model", contextManagerPruneInput{SessionKey: "sess"})
	if err == nil || !strings.Contains(err.Error(), "missing "+contextManagerCompactToolName) {
		t.Fatalf("expected missing tool call error, got %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 parse attempts, got %d", len(requests))
	}
	if len(requests[0].Messages) != 2 || len(requests[1].Messages) != 3 {
		t.Fatalf("expected follow-up correction message on second attempt, got lens %d and %d", len(requests[0].Messages), len(requests[1].Messages))
	}
	if got := requests[1].Messages[2].Content; got != "Your previous response did not call compact_context with valid arguments. Call compact_context exactly once now." {
		t.Fatalf("unexpected retry message: %#v", got)
	}
}

func TestParseContextManagerToolResponse(t *testing.T) {
	t.Run("errors when no choices", func(t *testing.T) {
		_, err := parseContextManagerToolResponse(providers.ChatCompletionResponse{})
		if err == nil || !strings.Contains(err.Error(), "no choices") {
			t.Fatalf("expected no choices error, got %v", err)
		}
	})

	t.Run("errors when tool call missing", func(t *testing.T) {
		_, err := parseContextManagerToolResponse(chatResponseWithToolCalls())
		if err == nil || !strings.Contains(err.Error(), "missing "+contextManagerCompactToolName) {
			t.Fatalf("expected missing tool error, got %v", err)
		}
	})

	t.Run("ignores non matching tool calls", func(t *testing.T) {
		_, err := parseContextManagerToolResponse(chatResponseWithToolCalls(
			toolCall("other", `{}`),
			toolCall("still_other", `{}`),
		))
		if err == nil || !strings.Contains(err.Error(), "missing "+contextManagerCompactToolName) {
			t.Fatalf("expected missing tool error, got %v", err)
		}
	})
}

func TestShouldTriggerContextManager_CoversBudgetPercentAndIntervals(t *testing.T) {
	policy := ContextManagerPolicy{Enabled: true, AllowedTriggers: []ContextManagerTrigger{ContextTriggerOverBudget, ContextTriggerTurnInterval}}
	if !shouldTriggerContextManager(policy, ContextTriggerOverBudget, BudgetReport{BudgetUsedPercent: 85}, 0) {
		t.Fatalf("expected 85%% budget use to trigger even without high pressure")
	}
	for turnCount, want := range map[int]bool{11: false, 12: true, 13: false} {
		if got := shouldTriggerContextManager(policy, ContextTriggerTurnInterval, BudgetReport{}, turnCount); got != want {
			t.Fatalf("turn interval trigger for %d = %v, want %v", turnCount, got, want)
		}
	}
}

func TestContextManagerValidationHelpersEnforceLimits(t *testing.T) {
	tooMany := make([]string, 21)
	if err := validateTaskCardUpdate(TaskCardUpdateProposal{Plan: tooMany}); err == nil || !strings.Contains(err.Error(), "list limits") {
		t.Fatalf("expected list limit error, got %v", err)
	}
	if err := validateTaskCardUpdate(TaskCardUpdateProposal{Plan: []string{strings.Repeat("x", 501)}}); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected item length error, got %v", err)
	}
	validSummary := SummaryProposal{Summary: "keep the latest context", Refs: []string{"message:1"}}
	if err := validateSummaryProposals([]SummaryProposal{validSummary}); err != nil {
		t.Fatalf("expected valid summary proposal, got %v", err)
	}
	tooManySummaries := make([]SummaryProposal, 21)
	for i := range tooManySummaries {
		tooManySummaries[i] = validSummary
	}
	if err := validateSummaryProposals(tooManySummaries); err == nil || !strings.Contains(err.Error(), "too many summaries") {
		t.Fatalf("expected too many summaries error, got %v", err)
	}
	if err := validateSummaryProposals([]SummaryProposal{{Summary: strings.Repeat("s", 1001)}}); err == nil || !strings.Contains(err.Error(), "exceeds limits") {
		t.Fatalf("expected summary length error, got %v", err)
	}
	if err := validateSummaryProposals([]SummaryProposal{{Summary: "ok", Refs: make([]string, 21)}}); err == nil || !strings.Contains(err.Error(), "exceeds limits") {
		t.Fatalf("expected refs limit error, got %v", err)
	}
}

func TestContextManagerProviderRejectedToolChoice(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "missing tool choice marker", err: providers.ProviderError{Err: http.ErrBodyNotAllowed}, want: false},
		{name: "no endpoints", err: errString("tool_choice: no endpoints available"), want: true},
		{name: "not support", err: errString("tool_choice does not support required"), want: true},
		{name: "unsupported", err: errString("tool_choice unsupported"), want: true},
		{name: "invalid", err: errString("tool_choice invalid"), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextManagerProviderRejectedToolChoice(tc.err); got != tc.want {
				t.Fatalf("contextManagerProviderRejectedToolChoice(%v)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func chatResponseWithToolCalls(calls ...providers.ToolCall) providers.ChatCompletionResponse {
	var resp providers.ChatCompletionResponse
	resp.Choices = append(resp.Choices, struct {
		Message struct {
			Role      string               `json:"role"`
			Content   any                  `json:"content"`
			ToolCalls []providers.ToolCall `json:"tool_calls"`
		} `json:"message"`
	}{})
	resp.Choices[0].Message.ToolCalls = calls
	return resp
}

func toolCall(name, args string) providers.ToolCall {
	call := providers.ToolCall{Type: "function"}
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}
