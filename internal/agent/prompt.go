package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
)

const Soul = `# Soul
I am or3-intern, a personal AI assistant.
- Be clear and direct
- Prefer deterministic, bounded work
- Use tools when needed; keep outputs short
`

const AgentInstructions = `# Agent Instructions
- Use pinned memory for stable facts.
- Retrieve relevant memory snippets before answering.
- Keep constant RAM usage: last N messages + top K memories only.
- Large tool outputs must spill to artifacts.
`

const ToolNotes = `# Tool Usage Notes
exec:
- Commands have a timeout
- Dangerous commands blocked
- Output truncated
cron:
- Use cron tool for scheduled reminders.
`

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
		hist = append(hist, providers.ChatMessage{Role: m.Role, Content: m.Content})
	}

	sys := []providers.ChatMessage{
		{Role: "system", Content: Soul},
		{Role: "system", Content: AgentInstructions},
		{Role: "system", Content: "## Pinned Memory\n" + pinnedText},
		{Role: "system", Content: "## Retrieved Memory\n" + memText},
		{Role: "system", Content: "## Skills Inventory\n" + b.Skills.Summary(80)},
		{Role: "system", Content: ToolNotes},
	}
	return PromptParts{System: sys, History: hist}, retrieved, nil
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
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, oneLine(v, 220)))
	}
	s := strings.TrimSpace(b.String())
	if s == "" { return "(none)" }
	return s
}

func formatRetrieved(ms []memory.Retrieved) string {
	if len(ms) == 0 { return "(none)" }
	var b strings.Builder
	for i, m := range ms {
		b.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, m.Source, oneLine(m.Text, 240)))
	}
	return strings.TrimSpace(b.String())
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max { s = s[:max] + "…" }
	return s
}
