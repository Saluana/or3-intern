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
	budgets := b.contextSectionBudgets()
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 {
		maxEach = defaultBootstrapMaxChars
	}
	skillsMax := b.SkillsSummaryMax
	if skillsMax <= 0 {
		skillsMax = defaultSkillsSummaryMax
	}

	soul := strings.TrimSpace(b.Soul)
	if soul == "" {
		soul = DefaultSoul
	}
	inst := strings.TrimSpace(b.AgentInstructions)
	if inst == "" {
		inst = DefaultAgentInstructions
	}
	notes := strings.TrimSpace(b.ToolNotes)
	if notes == "" {
		notes = DefaultToolNotes
	}

	packet := ContextPacket{
		MaxInputTokens: b.contextMaxInputTokens(),
		OutputReserve:  b.contextOutputReserveTokens(),
		SafetyMargin:   b.contextSafetyMarginTokens(),
	}
	packet.StableSections = append(packet.StableSections,
		budgetSection("SOUL.md", soul, true, budgets.SoulIdentity, minProtectedTokens(budgets.SoulIdentity), maxEach),
	)
	if t := strings.TrimSpace(identityText); t != "" {
		packet.StableSections = append(packet.StableSections, budgetSection("Identity", t, true, budgets.SoulIdentity, minProtectedTokens(budgets.SoulIdentity), maxEach))
	}
	packet.StableSections = append(packet.StableSections,
		budgetSection("AGENTS.md", inst, true, budgets.SoulIdentity, minProtectedTokens(budgets.SoulIdentity), maxEach),
	)
	if t := strings.TrimSpace(staticMemoryText); t != "" {
		packet.StableSections = append(packet.StableSections, budgetSection("Static Memory", t, false, 0, 0, maxEach))
	}
	packet.StableSections = append(packet.StableSections,
		budgetSection("TOOLS.md", notes, true, budgets.ToolPolicy, minProtectedTokens(budgets.ToolPolicy), maxEach),
		budgetSection("Pinned Memory", pinnedText, true, budgets.PinnedMemory, minProtectedTokens(budgets.PinnedMemory), maxEach),
	)
	if t := strings.TrimSpace(digestText); t != "" {
		packet.StableSections = append(packet.StableSections, budgetSection("Memory Digest", t, false, budgets.MemoryDigest, 0, maxEach))
	}
	packet.StableSections = append(packet.StableSections, budgetSection("Retrieved Memory", memText, false, budgets.RetrievedMemory, 0, maxEach))
	if t := strings.TrimSpace(workspaceContextText); t != "" {
		packet.StableSections = append(packet.StableSections, budgetSection("Workspace Context", t, false, budgets.WorkspaceContext, 0, maxEach))
	}
	if t := strings.TrimSpace(docContextText); t != "" {
		packet.StableSections = append(packet.StableSections, budgetSection("Indexed File Context", t, false, budgets.WorkspaceContext, 0, maxEach))
	}
	packet.StableSections = append(packet.StableSections, budgetSection("Skills Inventory", b.Skills.ModelSummary(skillsMax), false, budgets.ToolSchemas, 0, maxEach))

	if t := strings.TrimSpace(heartbeatText); t != "" {
		packet.VolatileSections = append(packet.VolatileSections, budgetSection("Heartbeat", t, false, 0, 0, maxEach))
	}
	if t := strings.TrimSpace(structuredContextText); t != "" {
		protected := strings.Contains(t, "active_task_card:")
		packet.VolatileSections = append(packet.VolatileSections, budgetSection("Structured Trigger Context", t, protected, budgets.ActiveTaskCard, minProtectedTokens(budgets.ActiveTaskCard), maxEach))
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
