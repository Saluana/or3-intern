package agent

import (
	"fmt"
	"strings"

	"or3-intern/internal/providers"
)

// BudgetReport contains deterministic prompt sizing diagnostics.
type BudgetReport struct {
	EstimatedInputTokens int
	SystemTokens         int
	HistoryTokens        int
	OutputReserveTokens  int
	Pressure             string
	Sections             []SectionUsage
	Pruned               []PruneEvent
	Rejected             []string
}

type ContextRef struct {
	Kind string
	ID   string
}

type ContextSnippet struct {
	Text   string
	Score  float64
	Reason string
	Ref    ContextRef
}

type ContextSection struct {
	Name      string
	Text      string
	Required  bool
	TokenCap  int
	MinTokens int
	Snippets  []ContextSnippet
}

type ContextPacket struct {
	StableSections   []ContextSection
	VolatileSections []ContextSection
	Budget           BudgetReport
}

type SectionUsage struct {
	Name            string
	EstimatedTokens int
	LimitTokens     int
}

type PruneEvent struct {
	Section string
	Reason  string
}

func estimatePromptBudget(system, history []providers.ChatMessage) BudgetReport {
	systemTokens := estimateMessagesTokens(system)
	historyTokens := estimateMessagesTokens(history)
	return BudgetReport{
		EstimatedInputTokens: systemTokens + historyTokens,
		SystemTokens:         systemTokens,
		HistoryTokens:        historyTokens,
		Pressure:             pressureState(systemTokens + historyTokens),
	}
}

func pressureState(tokens int) string {
	switch {
	case tokens >= 12000:
		return "emergency"
	case tokens >= 9000:
		return "high"
	case tokens >= 7000:
		return "warning"
	default:
		return "normal"
	}
}

func estimateMessagesTokens(msgs []providers.ChatMessage) int {
	total := 0
	for _, msg := range msgs {
		// Deterministic rough estimate: ~1 token per 4 chars + small framing cost.
		total += 4
		total += estimateTextTokens(msg.Role)
		total += estimateTextTokens(messageContentString(msg.Content))
		total += estimateTextTokens(msg.Name)
		total += estimateTextTokens(msg.ToolCallID)
	}
	return total
}

func estimateTextTokens(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

func messageContentString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
