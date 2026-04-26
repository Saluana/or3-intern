package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"or3-intern/internal/db"
)

type TaskCard struct {
	Goal          string   `json:"goal"`
	Plan          []string `json:"plan"`
	Constraints   []string `json:"constraints"`
	Decisions     []string `json:"decisions"`
	OpenQuestions []string `json:"open_questions"`
	MessageRefs   []int64  `json:"message_refs"`
	MemoryRefs    []int64  `json:"memory_refs"`
	ArtifactRefs  []string `json:"artifact_refs"`
	ActiveFiles   []string `json:"active_files"`
	Status        string   `json:"status"`
}

func loadTaskCard(ctx context.Context, d *db.DB, sessionKey string) (TaskCard, bool, error) {
	if d == nil || strings.TrimSpace(sessionKey) == "" {
		return TaskCard{}, false, nil
	}
	row, ok, err := d.GetActiveTaskState(ctx, sessionKey)
	if err != nil || !ok {
		return TaskCard{}, false, err
	}
	var card TaskCard
	card.Goal = row.Goal
	card.Status = row.Status
	_ = json.Unmarshal([]byte(row.PlanJSON), &card.Plan)
	_ = json.Unmarshal([]byte(row.ConstraintsJSON), &card.Constraints)
	_ = json.Unmarshal([]byte(row.DecisionsJSON), &card.Decisions)
	_ = json.Unmarshal([]byte(row.OpenQuestionsJSON), &card.OpenQuestions)
	_ = json.Unmarshal([]byte(row.MessageRefsJSON), &card.MessageRefs)
	_ = json.Unmarshal([]byte(row.MemoryRefsJSON), &card.MemoryRefs)
	_ = json.Unmarshal([]byte(row.ArtifactRefsJSON), &card.ArtifactRefs)
	_ = json.Unmarshal([]byte(row.ActiveFilesJSON), &card.ActiveFiles)
	if strings.TrimSpace(row.ScopeKey) == "" {
		row.ScopeKey = resolveTaskCardScope(ctx, d, sessionKey, "")
	}
	return card, true, nil
}

func saveTaskCard(ctx context.Context, d *db.DB, sessionKey, scopeKey string, card TaskCard) error {
	if d == nil || strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	toJSON := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	scopeKey = resolveTaskCardScope(ctx, d, sessionKey, scopeKey)
	return d.UpsertActiveTaskState(ctx, db.TaskStateRow{
		SessionKey:        sessionKey,
		ScopeKey:          scopeKey,
		Status:            statusOrDefault(card.Status),
		Goal:              strings.TrimSpace(card.Goal),
		PlanJSON:          toJSON(card.Plan),
		ConstraintsJSON:   toJSON(card.Constraints),
		DecisionsJSON:     toJSON(card.Decisions),
		OpenQuestionsJSON: toJSON(card.OpenQuestions),
		MessageRefsJSON:   toJSON(card.MessageRefs),
		MemoryRefsJSON:    toJSON(card.MemoryRefs),
		ArtifactRefsJSON:  toJSON(card.ArtifactRefs),
		ActiveFilesJSON:   toJSON(card.ActiveFiles),
		MetadataJSON:      "{}",
	})
}

func resolveTaskCardScope(ctx context.Context, d *db.DB, sessionKey, scopeKey string) string {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey != "" {
		return scopeKey
	}
	if d != nil && strings.TrimSpace(sessionKey) != "" {
		if resolved, err := d.ResolveScopeKey(ctx, sessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			return resolved
		}
	}
	return strings.TrimSpace(sessionKey)
}

func statusOrDefault(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "active"
	}
	return status
}

func renderTaskCard(card TaskCard, maxChars int) string {
	var b strings.Builder
	if strings.TrimSpace(card.Status) != "" {
		b.WriteString("Status: ")
		b.WriteString(strings.TrimSpace(card.Status))
		b.WriteString("\n")
	}
	if strings.TrimSpace(card.Goal) != "" {
		b.WriteString("Goal: ")
		b.WriteString(strings.TrimSpace(card.Goal))
		b.WriteString("\n")
	}
	for _, v := range card.Constraints {
		if strings.TrimSpace(v) == "" {
			continue
		}
		b.WriteString("Constraint: ")
		b.WriteString(strings.TrimSpace(v))
		b.WriteString("\n")
	}
	for _, v := range card.Decisions {
		if strings.TrimSpace(v) == "" {
			continue
		}
		b.WriteString("Decision: ")
		b.WriteString(strings.TrimSpace(v))
		b.WriteString("\n")
	}
	for _, v := range card.OpenQuestions {
		if strings.TrimSpace(v) == "" {
			continue
		}
		b.WriteString("Open Question: ")
		b.WriteString(strings.TrimSpace(v))
		b.WriteString("\n")
	}
	if len(card.ArtifactRefs) > 0 {
		b.WriteString(fmt.Sprintf("Refs: artifacts=%v\n", card.ArtifactRefs))
	}
	if len(card.MessageRefs) > 0 {
		b.WriteString(fmt.Sprintf("Refs: messages=%v\n", card.MessageRefs))
	}
	if len(card.ActiveFiles) > 0 {
		b.WriteString(fmt.Sprintf("Refs: files=%v\n", card.ActiveFiles))
	}
	out := strings.TrimSpace(b.String())
	if maxChars > 0 && len(out) > maxChars {
		return strings.TrimSpace(out[:maxChars]) + "\n…[truncated]"
	}
	return out
}

func appendBoundedInt64(values []int64, id int64, max int) []int64 {
	if id <= 0 {
		return values
	}
	out := append(values, id)
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func appendBoundedString(values []string, value string, max int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	out := append(values, value)
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}
