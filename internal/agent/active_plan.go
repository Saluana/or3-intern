package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	planTaskStatusPending    = "pending"
	planTaskStatusInProgress = "in_progress"
	planTaskStatusCompleted  = "completed"
	planTaskStatusBlocked    = "blocked"

	maxPlanNoteChars       = 280
	maxPlanTasksInPrompt   = 8
	maxCompletionNotes     = 3
	maxPlanTaskDescription = 500
)

// ActivePlanTask is one tracked step in the active work plan.
type ActivePlanTask struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	Status         string `json:"status"`
	CompletionNote string `json:"completion_note,omitempty"`
}

// ActivePlanMetadata is persisted in task_state.metadata_json.
type ActivePlanMetadata struct {
	Title                   string           `json:"title,omitempty"`
	CurrentRequest          string           `json:"current_request,omitempty"`
	CurrentRequestMessageID int64            `json:"current_request_message_id,omitempty"`
	NextStep                string           `json:"next_step,omitempty"`
	Tasks                   []ActivePlanTask `json:"tasks,omitempty"`
	CompletionNotes         []string         `json:"completion_notes,omitempty"`
}

func parseActivePlanMetadata(raw string) ActivePlanMetadata {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return ActivePlanMetadata{}
	}
	var meta ActivePlanMetadata
	_ = json.Unmarshal([]byte(raw), &meta)
	return meta
}

func marshalActivePlanMetadata(meta ActivePlanMetadata) string {
	b, err := json.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func normalizePlanTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case planTaskStatusPending, planTaskStatusInProgress, planTaskStatusCompleted, planTaskStatusBlocked:
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return planTaskStatusPending
	}
}

func capPlanNote(text string) string {
	return oneLine(text, maxPlanNoteChars)
}

func syncLegacyPlanFromTasks(card *TaskCard, meta *ActivePlanMetadata) {
	if card == nil || meta == nil {
		return
	}
	titles := make([]string, 0, len(meta.Tasks))
	for _, task := range meta.Tasks {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			continue
		}
		titles = append(titles, title)
	}
	if len(titles) > 0 {
		card.Plan = titles
	}
}

func activePlanHasOpenWork(meta ActivePlanMetadata) bool {
	for _, task := range meta.Tasks {
		switch normalizePlanTaskStatus(task.Status) {
		case planTaskStatusPending, planTaskStatusInProgress, planTaskStatusBlocked:
			return true
		}
	}
	return false
}

func clearActiveTurnMetadata(meta *ActivePlanMetadata) {
	if meta == nil {
		return
	}
	meta.CurrentRequest = ""
	meta.CurrentRequestMessageID = 0
}

func activePlanIsEstablished(meta ActivePlanMetadata) bool {
	return len(meta.Tasks) > 0 || strings.TrimSpace(meta.Title) != ""
}

func renderCurrentTurn(userMessage string, messageID int64) string {
	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" {
		return ""
	}
	var b strings.Builder
	if messageID > 0 {
		b.WriteString(fmt.Sprintf("Message ID: %d\n", messageID))
	}
	b.WriteString("Request: ")
	b.WriteString(userMessage)
	return strings.TrimSpace(b.String())
}

func renderActivePlanCompact(card TaskCard, meta ActivePlanMetadata, maxChars int) string {
	var b strings.Builder
	if strings.TrimSpace(meta.Title) != "" {
		b.WriteString("Plan: ")
		b.WriteString(strings.TrimSpace(meta.Title))
		b.WriteString("\n")
	} else if strings.TrimSpace(card.Goal) != "" {
		b.WriteString("Goal: ")
		b.WriteString(strings.TrimSpace(card.Goal))
		b.WriteString("\n")
	}
	if strings.TrimSpace(meta.CurrentRequest) != "" {
		b.WriteString("Current request: ")
		b.WriteString(strings.TrimSpace(meta.CurrentRequest))
		b.WriteString("\n")
	}
	active := 0
	for _, task := range meta.Tasks {
		status := normalizePlanTaskStatus(task.Status)
		if status == planTaskStatusCompleted {
			continue
		}
		if active >= maxPlanTasksInPrompt {
			break
		}
		title := strings.TrimSpace(task.Title)
		if title == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", strings.TrimSpace(task.ID), title, status))
		active++
	}
	notes := meta.CompletionNotes
	if len(notes) > maxCompletionNotes {
		notes = notes[len(notes)-maxCompletionNotes:]
	}
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		b.WriteString("Done: ")
		b.WriteString(note)
		b.WriteString("\n")
	}
	if strings.TrimSpace(meta.NextStep) != "" {
		b.WriteString("Next step: ")
		b.WriteString(strings.TrimSpace(meta.NextStep))
		b.WriteString("\n")
	}
	if active == 0 && len(meta.Tasks) > 0 {
		for _, task := range meta.Tasks {
			if strings.TrimSpace(task.CompletionNote) == "" {
				continue
			}
			b.WriteString("Done: ")
			b.WriteString(strings.TrimSpace(task.CompletionNote))
			b.WriteString("\n")
			break
		}
	}
	if len(meta.Tasks) == 0 {
		for _, v := range card.Plan {
			if strings.TrimSpace(v) == "" {
				continue
			}
			if strings.Contains(b.String(), "Plan: "+strings.TrimSpace(v)) {
				continue
			}
			b.WriteString("Plan: ")
			b.WriteString(strings.TrimSpace(v))
			b.WriteString("\n")
		}
	}
	out := strings.TrimSpace(b.String())
	if maxChars > 0 && len(out) > maxChars {
		return strings.TrimSpace(out[:maxChars]) + "\n…[truncated]"
	}
	return out
}

func protectedCompactionMinMessageID(meta ActivePlanMetadata, fallbackUserMessageID int64) int64 {
	protected := int64(0)
	if meta.CurrentRequestMessageID > 0 {
		protected = meta.CurrentRequestMessageID
	} else if fallbackUserMessageID > 0 {
		protected = fallbackUserMessageID
	}
	if !activePlanHasOpenWork(meta) {
		return protected
	}
	for _, task := range meta.Tasks {
		switch normalizePlanTaskStatus(task.Status) {
		case planTaskStatusPending, planTaskStatusInProgress:
			// Active plan refs could be added later; current request is the main floor.
		}
	}
	return protected
}

func findPlanTask(meta *ActivePlanMetadata, taskID string) (*ActivePlanTask, int) {
	taskID = strings.TrimSpace(taskID)
	for i := range meta.Tasks {
		if strings.TrimSpace(meta.Tasks[i].ID) == taskID {
			return &meta.Tasks[i], i
		}
	}
	return nil, -1
}

func nextPlanTaskID(meta ActivePlanMetadata) string {
	max := 0
	for _, task := range meta.Tasks {
		id := strings.TrimSpace(task.ID)
		if !strings.HasPrefix(id, "t") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(id, "t%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("t%d", max+1)
}
