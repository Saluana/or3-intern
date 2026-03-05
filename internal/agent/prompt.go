package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
)

const DefaultSoul = `# Soul
I am or3-intern, a personal AI assistant.
- Be clear and direct
- Prefer deterministic, bounded work
- Use tools when needed; keep outputs short
`

const DefaultAgentInstructions = `# Agent Instructions
- Use pinned memory for stable facts.
- Retrieve relevant memory snippets before answering.
- Keep constant RAM usage: last N messages + top K memories only.
- Large tool outputs must spill to artifacts.
`

const DefaultToolNotes = `# Tool Usage Notes
exec:
- Commands have a timeout
- Dangerous commands blocked
- Output truncated
cron:
- Use cron tool for scheduled reminders.
`

const (
	defaultBootstrapMaxChars = 20000
	defaultBootstrapTotalMaxChars = 150000
	defaultPinnedOneLineMax = 220
	defaultRetrievedOneLineMax = 240
	defaultSkillsSummaryMax = 80
)

type PromptParts struct {
	System []providers.ChatMessage
	History []providers.ChatMessage
}

type Builder struct {
	DB *db.DB
	Skills skills.Inventory
	Mem *memory.Retriever
	Provider *providers.Client
	EmbedModel string

	Soul string
	AgentInstructions string
	ToolNotes string
	BootstrapMaxChars int
	BootstrapTotalMaxChars int
	SkillsSummaryMax int

	HistoryMax int
	VectorK int
	FTSK int
	TopK int
}

func (b *Builder) Build(ctx context.Context, sessionKey string, userMessage string) (PromptParts, []memory.Retrieved, error) {
	pinned, err := b.DB.GetPinned(ctx)
	if err != nil { return PromptParts{}, nil, err }
	pinnedText := formatPinned(pinned)

	// embed and retrieve
	var retrieved []memory.Retrieved
	if b.Mem != nil && b.Provider != nil && strings.TrimSpace(userMessage) != "" {
		vec, err := b.Provider.Embed(ctx, b.EmbedModel, userMessage)
		if err == nil {
			retrieved, _ = b.Mem.Retrieve(ctx, userMessage, vec, b.VectorK, b.FTSK, b.TopK)
		}
	}
	memText := formatRetrieved(retrieved)

	histRows, err := b.DB.GetLastMessages(ctx, sessionKey, b.HistoryMax)
	if err != nil { return PromptParts{}, nil, err }
	hist := make([]providers.ChatMessage, 0, len(histRows))
	for _, m := range histRows {
		msg := providers.ChatMessage{Role: m.Role, Content: m.Content}
		if m.Role == "assistant" {
			var payload map[string]any
			if err := json.Unmarshal([]byte(m.PayloadJSON), &payload); err == nil {
				if raw, ok := payload["tool_calls"]; ok {
					b, _ := json.Marshal(raw)
					var tcs []providers.ToolCall
					if err := json.Unmarshal(b, &tcs); err == nil {
						msg.ToolCalls = tcs
					}
				}
			}
		}
		hist = append(hist, msg)
	}

	sysText := b.composeSystemPrompt(pinnedText, memText)
	sys := []providers.ChatMessage{
		{Role: "system", Content: sysText},
	}
	return PromptParts{System: sys, History: hist}, retrieved, nil
}

func (b *Builder) composeSystemPrompt(pinnedText, memText string) string {
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 { maxEach = defaultBootstrapMaxChars }
	maxTotal := b.BootstrapTotalMaxChars
	if maxTotal <= 0 { maxTotal = defaultBootstrapTotalMaxChars }
	skillsMax := b.SkillsSummaryMax
	if skillsMax <= 0 { skillsMax = defaultSkillsSummaryMax }

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

	sections := []struct {
		title string
		text string
	}{
		{title: "SOUL.md", text: truncateText(soul, maxEach)},
		{title: "AGENTS.md", text: truncateText(inst, maxEach)},
		{title: "TOOLS.md", text: truncateText(notes, maxEach)},
		{title: "Pinned Memory", text: pinnedText},
		{title: "Retrieved Memory", text: memText},
		{title: "Skills Inventory", text: b.Skills.Summary(skillsMax)},
	}

	var out strings.Builder
	out.WriteString("# System Prompt\n")
	for _, s := range sections {
		out.WriteString("\n## ")
		out.WriteString(s.title)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(s.text))
		out.WriteString("\n")
	}
	return truncateText(strings.TrimSpace(out.String()), maxTotal)
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "\n…[truncated]"
	}
	return s
}

func formatPinned(m map[string]string) string {
	if len(m) == 0 { return "(none)" }
	keys := make([]string, 0, len(m))
	for k := range m { keys = append(keys, k) }
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := strings.TrimSpace(m[k])
		if v == "" { continue }
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, oneLine(v, defaultPinnedOneLineMax)))
	}
	s := strings.TrimSpace(b.String())
	if s == "" { return "(none)" }
	return s
}

func formatRetrieved(ms []memory.Retrieved) string {
	if len(ms) == 0 { return "(none)" }
	var b strings.Builder
	for i, m := range ms {
		b.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, m.Source, oneLine(m.Text, defaultRetrievedOneLineMax)))
	}
	return strings.TrimSpace(b.String())
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max { s = s[:max] + "…" }
	return s
}
