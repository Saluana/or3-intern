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
	MaxInputTokens       int
	BudgetUsedPercent    float64
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
	Protected bool
	Required  bool
	TokenCap  int
	MinTokens int
	Snippets  []ContextSnippet
}

type ContextPacket struct {
	StableSections   []ContextSection
	VolatileSections []ContextSection
	RecentHistory    []providers.ChatMessage
	OutputReserve    int
	MaxInputTokens   int
	SafetyMargin     int
	Budget           BudgetReport
}

type SectionUsage struct {
	Name            string
	EstimatedTokens int
	LimitTokens     int
	Protected       bool
	Truncated       bool
}

type PruneEvent struct {
	Section     string
	Reason      string
	TokensSaved int
	Ref         ContextRef
}

type ContextSectionBudgets struct {
	SystemCore       int
	SoulIdentity     int
	ToolPolicy       int
	ActiveTaskCard   int
	PinnedMemory     int
	MemoryDigest     int
	RecentHistory    int
	RetrievedMemory  int
	WorkspaceContext int
	ToolSchemas      int
}

type systemPromptSection struct {
	Title     string
	Text      string
	Protected bool
	TokenCap  int
	MinTokens int
}

const (
	defaultContextMaxInputTokens      = 16000
	defaultContextOutputReserveTokens = 1200
	defaultContextSafetyMarginTokens  = 400
)

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

func pressureStateForBudget(used, max int) string {
	if max <= 0 {
		return pressureState(used)
	}
	pct := used * 100 / max
	switch {
	case pct >= 95:
		return "emergency"
	case pct >= 85:
		return "high"
	case pct >= 70:
		return "warning"
	default:
		return "normal"
	}
}

func (b *Builder) buildContextPacket(pinnedText, digestText, memText, identityText, staticMemoryText, heartbeatText, structuredContextText, docContextText, workspaceContextText string) ContextPacket {
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 {
		maxEach = defaultBootstrapMaxChars
	}
	packet := ContextPacket{
		MaxInputTokens: b.contextMaxInputTokens(),
		OutputReserve:  b.contextOutputReserveTokens(),
		SafetyMargin:   b.contextSafetyMarginTokens(),
	}
	for _, section := range b.stablePromptSections(pinnedText, digestText, memText, identityText, staticMemoryText, docContextText, workspaceContextText) {
		packet.StableSections = append(packet.StableSections, budgetSection(section.Title, section.Text, section.Protected, section.TokenCap, section.MinTokens, maxEach))
	}
	for _, section := range b.volatilePromptSections(heartbeatText, structuredContextText) {
		packet.VolatileSections = append(packet.VolatileSections, budgetSection(section.Title, section.Text, section.Protected, section.TokenCap, section.MinTokens, maxEach))
	}
	return packet
}

func (b *Builder) contextMaxInputTokens() int {
	if b != nil && b.ContextMaxInputTokens > 0 {
		return b.ContextMaxInputTokens
	}
	return defaultContextMaxInputTokens
}

func (b *Builder) contextOutputReserveTokens() int {
	if b != nil && b.ContextOutputReserveTokens > 0 {
		return b.ContextOutputReserveTokens
	}
	return defaultContextOutputReserveTokens
}

func (b *Builder) contextSafetyMarginTokens() int {
	if b != nil && b.ContextSafetyMarginTokens > 0 {
		return b.ContextSafetyMarginTokens
	}
	return defaultContextSafetyMarginTokens
}

func (b *Builder) contextSectionBudgets() ContextSectionBudgets {
	if b == nil {
		return ContextSectionBudgets{}
	}
	return b.ContextSectionBudgets
}

func minProtectedTokens(cap int) int {
	if cap <= 0 {
		return 1
	}
	if cap < 64 {
		return cap
	}
	return cap / 4
}

func budgetSection(name, text string, protected bool, tokenCap, minTokens, maxChars int) ContextSection {
	text = strings.TrimSpace(text)
	section := ContextSection{Name: name, Text: text, Protected: protected, Required: protected, TokenCap: tokenCap, MinTokens: minTokens}
	if tokenCap <= 0 || text == "" {
		return section
	}
	limit := tokenCap
	if protected && minTokens > limit {
		limit = minTokens
	}
	section.Text = truncateTextToTokens(truncateText(text, maxChars), limit)
	return section
}

func truncateTextToTokens(text string, maxTokens int) string {
	text = strings.TrimSpace(text)
	if maxTokens <= 0 || estimateTextTokens(text) <= maxTokens {
		return text
	}
	maxChars := maxTokens * 4
	if maxChars <= 0 || maxChars >= len(text) {
		return text
	}
	return strings.TrimSpace(text[:maxChars]) + "\n...[truncated]"
}

func renderStablePrefix(p ContextPacket) string {
	var out strings.Builder
	out.WriteString("# System Prompt\n")
	for _, s := range p.StableSections {
		out.WriteString("\n## ")
		out.WriteString(s.Name)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(s.Text))
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

func renderVolatileSuffix(p ContextPacket) string {
	var out strings.Builder
	for _, s := range p.VolatileSections {
		out.WriteString("## ")
		out.WriteString(s.Name)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(s.Text))
		out.WriteString("\n\n")
	}
	return strings.TrimSpace(out.String())
}

func renderProviderMessages(p ContextPacket, b *Builder) []providers.ChatMessage {
	stable := renderStablePrefix(p)
	volatile := renderVolatileSuffix(p)
	var content any
	if b != nil && b.Provider != nil && b.Provider.SupportsExplicitPromptCache() {
		content = providers.BuildCacheAwareSystemContent(stable, volatile)
	} else if strings.TrimSpace(volatile) == "" {
		content = stable
	} else {
		content = strings.TrimSpace(stable + "\n\n" + volatile)
	}
	return []providers.ChatMessage{{Role: "system", Content: content}}
}

func estimatePacketBudget(packet ContextPacket, b *Builder) BudgetReport {
	sys := renderProviderMessages(packet, b)
	systemTokens := estimateMessagesTokens(sys)
	historyTokens := estimateMessagesTokens(packet.RecentHistory)
	used := systemTokens + historyTokens
	maxInput := packet.MaxInputTokens
	if maxInput <= 0 {
		maxInput = defaultContextMaxInputTokens
	}
	outputReserve := packet.OutputReserve
	if outputReserve <= 0 {
		outputReserve = defaultContextOutputReserveTokens
	}
	usable := maxInput - outputReserve - packet.SafetyMargin
	if usable <= 0 {
		usable = maxInput
	}
	report := BudgetReport{
		EstimatedInputTokens: used,
		SystemTokens:         systemTokens,
		HistoryTokens:        historyTokens,
		OutputReserveTokens:  outputReserve,
		MaxInputTokens:       maxInput,
		Pressure:             pressureStateForBudget(used+outputReserve, maxInput),
		Sections:             make([]SectionUsage, 0, len(packet.StableSections)+len(packet.VolatileSections)+1),
	}
	if maxInput > 0 {
		report.BudgetUsedPercent = float64(used+outputReserve) / float64(maxInput) * 100
	}
	for _, s := range append(append([]ContextSection{}, packet.StableSections...), packet.VolatileSections...) {
		tokens := estimateTextTokens(s.Text)
		truncated := s.TokenCap > 0 && estimateTextTokens(strings.TrimSuffix(s.Text, "\n...[truncated]")) >= s.TokenCap && strings.Contains(s.Text, "[truncated]")
		report.Sections = append(report.Sections, SectionUsage{
			Name:            s.Name,
			EstimatedTokens: tokens,
			LimitTokens:     s.TokenCap,
			Protected:       s.Protected,
			Truncated:       truncated,
		})
		if strings.Contains(s.Text, "[truncated]") {
			report.Pruned = append(report.Pruned, PruneEvent{
				Section:     s.Name,
				Reason:      "section token cap",
				TokensSaved: max(0, tokens-s.TokenCap),
			})
		}
	}
	report.Sections = append(report.Sections, SectionUsage{
		Name:            "Recent History",
		EstimatedTokens: historyTokens,
		LimitTokens:     0,
	})
	if used > usable {
		report.Pruned = append(report.Pruned, PruneEvent{
			Section:     "Prompt",
			Reason:      "input budget exceeded after output reserve",
			TokensSaved: used - usable,
		})
	}
	return report
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
