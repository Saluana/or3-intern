package agent

import (
	"strings"

	"or3-intern/internal/db"
)

// TaskCard represents the active task state for a session.
type TaskCard struct {
	Goal          string
	Plan          string
	Constraints   []string
	Decisions     []string
	OpenQuestions []string
	MessageRefs   []string
	MemoryRefs    []string
	ArtifactRefs  []string
	ActiveFiles   []string
	Status        string
}

// RenderTaskCard renders the task card as natural text bounded by maxChars.
func RenderTaskCard(tc TaskCard, maxChars int) string {
	if tc.Goal == "" && tc.Plan == "" && len(tc.Constraints) == 0 {
		return ""
	}
	var sb strings.Builder
	if tc.Goal != "" {
		sb.WriteString("Goal: " + tc.Goal + "\n")
	}
	if tc.Plan != "" {
		sb.WriteString("Plan: " + tc.Plan + "\n")
	}
	if len(tc.Constraints) > 0 {
		sb.WriteString("Constraints:\n")
		for _, c := range tc.Constraints {
			sb.WriteString("- " + c + "\n")
		}
	}
	if len(tc.Decisions) > 0 {
		sb.WriteString("Decisions:\n")
		for _, d := range tc.Decisions {
			sb.WriteString("- " + d + "\n")
		}
	}
	if len(tc.OpenQuestions) > 0 {
		sb.WriteString("Open Questions:\n")
		for _, q := range tc.OpenQuestions {
			sb.WriteString("- " + q + "\n")
		}
	}
	if len(tc.ActiveFiles) > 0 {
		sb.WriteString("Active Files: " + strings.Join(tc.ActiveFiles, ", ") + "\n")
	}
	out := strings.TrimSpace(sb.String())
	if maxChars > 0 && len(out) > maxChars {
		return out[:maxChars] + "\n…[truncated]"
	}
	return out
}

// TaskCardFromDB converts a db.TaskState to a TaskCard.
func TaskCardFromDB(ts db.TaskState) TaskCard {
	return TaskCard{
		Goal:          ts.Goal,
		Plan:          ts.Plan,
		Constraints:   splitLines(ts.Constraints),
		Decisions:     splitLines(ts.Decisions),
		OpenQuestions: splitLines(ts.OpenQuestions),
		MessageRefs:   splitLines(ts.MessageRefs),
		MemoryRefs:    splitLines(ts.MemoryRefs),
		ArtifactRefs:  splitLines(ts.ArtifactRefs),
		ActiveFiles:   splitLines(ts.ActiveFiles),
		Status:        ts.Status,
	}
}

// TaskCardToDB converts a TaskCard to a db.TaskState for storage.
func TaskCardToDB(sessionKey string, tc TaskCard) db.TaskState {
	return db.TaskState{
		SessionKey:    sessionKey,
		Goal:          tc.Goal,
		Plan:          tc.Plan,
		Constraints:   joinLines(tc.Constraints),
		Decisions:     joinLines(tc.Decisions),
		OpenQuestions: joinLines(tc.OpenQuestions),
		MessageRefs:   joinLines(tc.MessageRefs),
		MemoryRefs:    joinLines(tc.MemoryRefs),
		ArtifactRefs:  joinLines(tc.ArtifactRefs),
		ActiveFiles:   joinLines(tc.ActiveFiles),
		Status:        tc.Status,
	}
}

func splitLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func joinLines(ss []string) string {
	return strings.Join(ss, "\n")
}
