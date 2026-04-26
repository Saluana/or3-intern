package agent

import (
	"strings"
	"testing"
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
