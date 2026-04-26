package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ContextManagerTrigger string

const (
	ContextTriggerOverBudget       ContextManagerTrigger = "over_budget"
	ContextTriggerTaskShift        ContextManagerTrigger = "task_shift"
	ContextTriggerTurnInterval     ContextManagerTrigger = "turn_interval"
	ContextTriggerLargeToolOutput  ContextManagerTrigger = "large_tool_output"
	ContextTriggerLowConfidenceRAG ContextManagerTrigger = "low_confidence_rag"
	ContextTriggerStaleReview      ContextManagerTrigger = "stale_review"
	ContextTriggerMaintenance      ContextManagerTrigger = "maintenance"
)

type ContextManagerPolicy struct {
	Enabled           bool
	AllowedTriggers   []ContextManagerTrigger
	AllowTaskUpdates  bool
	AllowStalePropose bool
	ScopeKey          string
}

type ContextManagerProposal struct {
	TaskCardUpdates    *TaskCardUpdateProposal     `json:"task_card_updates,omitempty"`
	RetrievalDecisions []RetrievalDecisionProposal `json:"retrieval_decisions,omitempty"`
	StaleProposals     []StaleMemoryProposal       `json:"stale_memory_proposals,omitempty"`
	HistorySummaries   []SummaryProposal           `json:"history_summaries,omitempty"`
	ArtifactSummaries  []SummaryProposal           `json:"artifact_summaries,omitempty"`
	Compaction         *CompactionProposal         `json:"compaction,omitempty"`
	DeleteMemoryIDs    []int64                     `json:"delete_memory_ids,omitempty"`
	ProtectedEdits     []string                    `json:"protected_edits,omitempty"`
}

type TaskCardUpdateProposal struct {
	Goal          string   `json:"goal,omitempty"`
	Plan          []string `json:"plan,omitempty"`
	Constraints   []string `json:"constraints,omitempty"`
	Decisions     []string `json:"decisions,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	ArtifactRefs  []string `json:"artifact_refs,omitempty"`
	ActiveFiles   []string `json:"active_files,omitempty"`
	Status        string   `json:"status,omitempty"`
}

type RetrievalDecisionProposal struct {
	Ref    string `json:"ref"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type StaleMemoryProposal struct {
	MemoryID int64  `json:"memory_id"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
	ScopeKey string `json:"scope_key,omitempty"`
}

type SummaryProposal struct {
	Summary string   `json:"summary"`
	Refs    []string `json:"refs,omitempty"`
}

type CompactionProposal struct {
	Summary string   `json:"summary"`
	Refs    []string `json:"refs,omitempty"`
}

func shouldTriggerContextManager(policy ContextManagerPolicy, trigger ContextManagerTrigger, report BudgetReport, turnCount int) bool {
	if !policy.Enabled {
		return false
	}
	if len(policy.AllowedTriggers) > 0 && !triggerAllowed(policy.AllowedTriggers, trigger) {
		return false
	}
	switch trigger {
	case ContextTriggerOverBudget:
		return report.Pressure == "high" || report.Pressure == "emergency" || report.BudgetUsedPercent >= 85
	case ContextTriggerTurnInterval:
		return turnCount > 0 && turnCount%12 == 0
	case ContextTriggerTaskShift, ContextTriggerLargeToolOutput, ContextTriggerLowConfidenceRAG, ContextTriggerStaleReview, ContextTriggerMaintenance:
		return true
	default:
		return false
	}
}

func parseContextManagerProposal(raw string) (ContextManagerProposal, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(strings.TrimSpace(raw))))
	dec.DisallowUnknownFields()
	var proposal ContextManagerProposal
	if err := dec.Decode(&proposal); err != nil {
		return ContextManagerProposal{}, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != nil {
		if err == io.EOF {
			return proposal, nil
		}
		return ContextManagerProposal{}, fmt.Errorf("extra JSON content: %w", err)
	}
	return ContextManagerProposal{}, fmt.Errorf("extra JSON content")
}

func validateContextManagerProposal(proposal ContextManagerProposal, policy ContextManagerPolicy) error {
	if len(proposal.DeleteMemoryIDs) > 0 {
		return fmt.Errorf("context manager cannot delete memories")
	}
	if len(proposal.ProtectedEdits) > 0 {
		return fmt.Errorf("context manager cannot edit protected sections")
	}
	if proposal.TaskCardUpdates != nil && !policy.AllowTaskUpdates {
		return fmt.Errorf("task-card updates are not allowed")
	}
	if len(proposal.StaleProposals) > 0 && !policy.AllowStalePropose {
		return fmt.Errorf("stale-memory proposals are not allowed")
	}
	if proposal.TaskCardUpdates != nil {
		if err := validateTaskCardUpdate(*proposal.TaskCardUpdates); err != nil {
			return err
		}
	}
	for _, stale := range proposal.StaleProposals {
		if stale.MemoryID <= 0 {
			return fmt.Errorf("stale proposal missing memory_id")
		}
		if stale.Action != "mark_stale" && stale.Action != "mark_superseded" && stale.Action != "archive" {
			return fmt.Errorf("unsafe stale proposal action: %s", stale.Action)
		}
		if stale.ScopeKey != "" && policy.ScopeKey != "" && stale.ScopeKey != policy.ScopeKey {
			return fmt.Errorf("stale proposal scope mismatch")
		}
	}
	return validateSummaryProposals(append(proposal.HistorySummaries, proposal.ArtifactSummaries...))
}

func applyContextManagerTaskUpdate(card TaskCard, update TaskCardUpdateProposal) TaskCard {
	if strings.TrimSpace(update.Goal) != "" {
		card.Goal = oneLine(update.Goal, 240)
	}
	card.Plan = appendBoundedStringSlice(card.Plan, update.Plan, 12)
	card.Constraints = appendBoundedStringSlice(card.Constraints, update.Constraints, 12)
	card.Decisions = appendBoundedStringSlice(card.Decisions, update.Decisions, 12)
	card.OpenQuestions = appendBoundedStringSlice(card.OpenQuestions, update.OpenQuestions, 12)
	card.ArtifactRefs = appendBoundedStringSlice(card.ArtifactRefs, update.ArtifactRefs, 12)
	card.ActiveFiles = appendBoundedStringSlice(card.ActiveFiles, update.ActiveFiles, 12)
	if strings.TrimSpace(update.Status) != "" {
		card.Status = oneLine(update.Status, 40)
	}
	return card
}

func deterministicContextManagerFallback(report BudgetReport) []PruneEvent {
	if len(report.Pruned) > 0 {
		return append([]PruneEvent{}, report.Pruned...)
	}
	return []PruneEvent{{Section: "Context Manager", Reason: "invalid or unavailable; deterministic pruning retained"}}
}

func triggerAllowed(allowed []ContextManagerTrigger, trigger ContextManagerTrigger) bool {
	for _, item := range allowed {
		if item == trigger {
			return true
		}
	}
	return false
}

func validateTaskCardUpdate(update TaskCardUpdateProposal) error {
	if len(update.Plan) > 20 || len(update.Constraints) > 20 || len(update.Decisions) > 20 || len(update.OpenQuestions) > 20 || len(update.ArtifactRefs) > 20 || len(update.ActiveFiles) > 20 {
		return fmt.Errorf("task-card update exceeds list limits")
	}
	all := append(append(append(append(append([]string{}, update.Plan...), update.Constraints...), update.Decisions...), update.OpenQuestions...), update.ActiveFiles...)
	for _, item := range all {
		if len(item) > 500 {
			return fmt.Errorf("task-card update item too long")
		}
	}
	return nil
}

func validateSummaryProposals(items []SummaryProposal) error {
	if len(items) > 20 {
		return fmt.Errorf("too many summaries")
	}
	for _, item := range items {
		if strings.TrimSpace(item.Summary) == "" {
			return fmt.Errorf("summary missing text")
		}
		if len(item.Summary) > 1000 || len(item.Refs) > 20 {
			return fmt.Errorf("summary proposal exceeds limits")
		}
	}
	return nil
}

func appendBoundedStringSlice(base []string, additions []string, max int) []string {
	out := append([]string{}, base...)
	for _, item := range additions {
		out = appendBoundedString(out, oneLine(item, 300), max)
	}
	return out
}
