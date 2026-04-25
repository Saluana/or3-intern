package agent

import "or3-intern/internal/providers"

// PressureLevel represents context pressure state.
type PressureLevel int

const (
	PressureNormal    PressureLevel = iota
	PressureWarning
	PressureHigh
	PressureEmergency
)

// ContextSection names a section in the context packet.
type ContextSection struct {
	Name      string
	Text      string
	Tokens    int
	Protected bool
}

// PruneEvent records a pruning decision.
type PruneEvent struct {
	Section string
	Reason  string
	Removed int // tokens removed
}

// SectionUsage records how much of a section was used.
type SectionUsage struct {
	Name            string
	EstimatedTokens int
	CharLen         int
}

// BudgetReport summarizes context budget usage for a turn.
type BudgetReport struct {
	TotalBudgetTokens    int
	OutputReserve        int
	EstimatedInputTokens int
	PressureLevel        PressureLevel
	Sections             []SectionUsage
	PruneEvents          []PruneEvent
	LargestSections      []SectionUsage // top 3
}

// ContextPacket holds assembled context sections before rendering.
type ContextPacket struct {
	StablePrefix   string
	VolatileSuffix string
	Budget         BudgetReport
}

// estimateTokens approximates token count using (len(text) + 3) / 4.
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// estimateTokensMessages sums token estimates for all messages.
func estimateTokensMessages(msgs []providers.ChatMessage) int {
	total := 0
	for _, m := range msgs {
		switch v := m.Content.(type) {
		case string:
			total += estimateTokens(v)
		}
	}
	return total
}

// pressureLevel returns the pressure level based on token usage.
// normal <70%, warning 70-85%, high 85-95%, emergency >95%
func pressureLevel(usedTokens, totalBudget int) PressureLevel {
	if totalBudget <= 0 {
		return PressureNormal
	}
	pct := float64(usedTokens) / float64(totalBudget)
	switch {
	case pct >= 0.95:
		return PressureEmergency
	case pct >= 0.85:
		return PressureHigh
	case pct >= 0.70:
		return PressureWarning
	default:
		return PressureNormal
	}
}

// buildBudgetReport builds a BudgetReport from the given sections and budget.
func buildBudgetReport(sections []ContextSection, totalBudget, outputReserve int) BudgetReport {
	usages := make([]SectionUsage, 0, len(sections))
	total := 0
	for _, s := range sections {
		tok := estimateTokens(s.Text)
		usages = append(usages, SectionUsage{
			Name:            s.Name,
			EstimatedTokens: tok,
			CharLen:         len(s.Text),
		})
		total += tok
	}
	// Find top 3 largest
	largest := make([]SectionUsage, 0, 3)
	sorted := append([]SectionUsage{}, usages...)
	for i := 0; i < len(sorted) && i < 3; i++ {
		maxIdx := i
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].EstimatedTokens > sorted[maxIdx].EstimatedTokens {
				maxIdx = j
			}
		}
		sorted[i], sorted[maxIdx] = sorted[maxIdx], sorted[i]
		largest = append(largest, sorted[i])
	}
	return BudgetReport{
		TotalBudgetTokens:    totalBudget,
		OutputReserve:        outputReserve,
		EstimatedInputTokens: total,
		PressureLevel:        pressureLevel(total, totalBudget),
		Sections:             usages,
		LargestSections:      largest,
	}
}
