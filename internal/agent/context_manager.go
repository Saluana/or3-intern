package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
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
	Summary                 string   `json:"summary"`
	Refs                    []string `json:"refs,omitempty"`
	CompactThroughMessageID int64    `json:"compact_through_message_id,omitempty"`
}

type contextManagerMessage struct {
	ID         int64  `json:"id"`
	SessionKey string `json:"session_key"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	CreatedAt  int64  `json:"created_at"`
}

type contextManagerPruneInput struct {
	Reason          string                  `json:"reason"`
	SessionKey      string                  `json:"session_key"`
	ScopeKey        string                  `json:"scope_key"`
	ExistingSummary string                  `json:"existing_compaction_summary,omitempty"`
	TaskCard        string                  `json:"active_task_card,omitempty"`
	Messages        []contextManagerMessage `json:"messages"`
}

const contextManagerCompactToolName = "compact_context"

const contextManagerCompactSystemPrompt = `You are OR3's context manager.

You MUST call compact_context exactly once.
Do NOT answer conversationally.
Do NOT explain your reasoning.
Do NOT output prose outside the tool call.

Your job is to reduce the live prompt window without deleting the raw chat log.
You are not deleting data. You are choosing which older messages can be replaced in the live prompt by a compact summary.

Primary goal:
- Keep the next assistant turn effective.
- Preserve everything needed to continue the current task safely.
- Compact only older, already-resolved, low-value context.

What must be preserved in the summary when relevant:
- the active task and current user intent
- constraints, requirements, and acceptance criteria
- decisions that were made and why they matter now
- file paths, symbols, commands, tests, and outputs that affect the next step
- errors, blockers, risks, and unresolved questions
- any user preferences about style, safety, brevity, tooling, or workflow

What should usually be dropped from live context:
- greetings, acknowledgements, filler, repeated status updates
- duplicate explanations already captured elsewhere
- low-signal tool chatter and raw dumps with no ongoing relevance
- resolved back-and-forth that no longer changes the next action

Important safety rules:
- compact_through_message_id must exactly match one of the provided message IDs
- never invent, estimate, round, or interpolate a message ID
- if you are unsure about the cutoff, choose a lower valid cutoff or choose 0
- do not compact the newest unresolved user request
- do not compact messages that still contain unresolved requirements, blockers, or decisions needed for the next turn
- prefer compacting a safe prefix of the provided messages

Cutoff selection policy:
1. Read all provided messages in order.
2. Identify the most recent message after which the older context is mostly resolved and can be summarized safely.
3. Choose the highest safe provided message ID as compact_through_message_id.
4. If no safe cutoff exists, set compact_through_message_id to 0 and summary to "".

Summary writing policy:
- summary must be factual, concise, and under 2000 characters
- write for another assistant that must continue work immediately
- prefer concrete nouns, file paths, symbols, commands, and outcomes over vague prose
- include unresolved blockers if any remain
- do not include speculation or details unsupported by the provided messages

Refs policy:
- refs should cite the messages that justify the summary
- use message IDs or short ranges like "messages:12-18"
- keep refs short and useful

Task card updates:
- include task_card_updates only if the active task card is clearly missing important current state
- keep task_card_updates conservative and factual
- if no safe task card update is needed, omit task_card_updates

Before calling the tool, silently verify:
- exactly one compact_context call
- compact_through_message_id is either 0 or an exact provided ID
- summary is empty when cutoff is 0
- summary preserves current-task continuity
- no unsupported claims

When in doubt, be conservative: compact less, not more.`

var contextManagerCompactToolDef = providers.ToolDef{
	Type: "function",
	Function: providers.ToolFunc{
		Name:        contextManagerCompactToolName,
		Description: "Store a validated prompt-window compaction plan. Raw messages are preserved outside the live prompt window.",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"compaction": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"summary":                    map[string]any{"type": "string"},
						"refs":                       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"compact_through_message_id": map[string]any{"type": "integer"},
					},
					"required": []string{"summary", "refs", "compact_through_message_id"},
				},
				"task_card_updates": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"goal":           map[string]any{"type": "string"},
						"plan":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"constraints":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"decisions":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"open_questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"artifact_refs":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"active_files":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"status":         map[string]any{"type": "string"},
					},
				},
			},
			"required": []string{"compaction"},
		},
	},
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
	if proposal.Compaction != nil {
		if err := validateCompactionProposal(*proposal.Compaction); err != nil {
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

func requestContextManagerCompaction(ctx context.Context, provider *providers.Client, model string, input contextManagerPruneInput) (ContextManagerProposal, error) {
	if provider == nil {
		return ContextManagerProposal{}, fmt.Errorf("context manager provider not configured")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gpt-4.1-mini"
	}
	payload, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return ContextManagerProposal{}, err
	}
	messages := []providers.ChatMessage{
		{Role: "system", Content: contextManagerCompactSystemPrompt},
		{Role: "user", Content: string(payload)},
	}
	var lastErr error
	toolChoiceAllowed := true
	for attempt := 0; attempt < 2; attempt++ {
		req := providers.ChatCompletionRequest{Model: model, Messages: messages, Tools: []providers.ToolDef{contextManagerCompactToolDef}, Temperature: 0}
		if toolChoiceAllowed {
			req.ToolChoice = "required"
		}
		resp, err := provider.Chat(ctx, req)
		if err != nil && toolChoiceAllowed && contextManagerProviderRejectedToolChoice(err) {
			toolChoiceAllowed = false
			req.ToolChoice = nil
			resp, err = provider.Chat(ctx, req)
		}
		if err != nil {
			return ContextManagerProposal{}, fmt.Errorf("chat: %w", err)
		}
		proposal, parseErr := parseContextManagerToolResponse(resp)
		if parseErr == nil {
			return proposal, nil
		}
		lastErr = parseErr
		messages = append(messages, providers.ChatMessage{Role: "user", Content: "Your previous response did not call compact_context with valid arguments. Call compact_context exactly once now."})
	}
	return ContextManagerProposal{}, lastErr
}

func parseContextManagerToolResponse(resp providers.ChatCompletionResponse) (ContextManagerProposal, error) {
	if len(resp.Choices) == 0 {
		return ContextManagerProposal{}, fmt.Errorf("no choices returned")
	}
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		if strings.TrimSpace(tc.Function.Name) != contextManagerCompactToolName {
			continue
		}
		return parseContextManagerProposal(tc.Function.Arguments)
	}
	return ContextManagerProposal{}, fmt.Errorf("missing %s tool call", contextManagerCompactToolName)
}

func renderContextCompaction(row db.ContextCompaction, maxChars int) string {
	var b strings.Builder
	if strings.TrimSpace(row.Summary) != "" {
		b.WriteString("Summary: ")
		b.WriteString(strings.TrimSpace(row.Summary))
		b.WriteString("\n")
	}
	if row.CutoffMessageID > 0 {
		b.WriteString(fmt.Sprintf("Compacted through message: %d\n", row.CutoffMessageID))
	}
	if strings.TrimSpace(row.MessageRefsJSON) != "" && strings.TrimSpace(row.MessageRefsJSON) != "[]" {
		b.WriteString("Refs: ")
		b.WriteString(strings.TrimSpace(row.MessageRefsJSON))
		b.WriteString("\n")
	}
	out := strings.TrimSpace(b.String())
	if maxChars > 0 && len(out) > maxChars {
		return strings.TrimSpace(out[:maxChars]) + "\n…[truncated]"
	}
	return out
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

func validateCompactionProposal(compaction CompactionProposal) error {
	if compaction.CompactThroughMessageID < 0 {
		return fmt.Errorf("compaction cutoff cannot be negative")
	}
	if compaction.CompactThroughMessageID > 0 && strings.TrimSpace(compaction.Summary) == "" {
		return fmt.Errorf("compaction summary required when cutoff is set")
	}
	if len(compaction.Summary) > 2000 || len(compaction.Refs) > 30 {
		return fmt.Errorf("compaction proposal exceeds limits")
	}
	return nil
}

func contextManagerProviderRejectedToolChoice(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "tool_choice") {
		return false
	}
	return strings.Contains(msg, "no endpoints") || strings.Contains(msg, "not support") || strings.Contains(msg, "unsupported") || strings.Contains(msg, "invalid")
}

func appendBoundedStringSlice(base []string, additions []string, max int) []string {
	out := append([]string{}, base...)
	for _, item := range additions {
		out = appendBoundedString(out, oneLine(item, 300), max)
	}
	return out
}
