package agent

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

const (
	ToolNameCreatePlan        = "create_plan"
	ToolNameUpdatePlan        = "update_plan"
	ToolNameCompletePlanTask  = "complete_plan_task"
	ToolNameRemovePlan        = "remove_plan"
)

// PlanToolBase provides shared DB access for native plan tools.
type PlanToolBase struct {
	tools.Base
	DB *db.DB
}

func NewPlanToolBase(d *db.DB) PlanToolBase {
	return PlanToolBase{DB: d}
}

func (PlanToolBase) planSession(ctx context.Context) string {
	if key := ConversationSessionFromContext(ctx); key != "" {
		return key
	}
	return tools.SessionFromContext(ctx)
}

func (b PlanToolBase) loadOrInitCard(ctx context.Context, sessionKey string) (TaskCard, error) {
	if b.DB == nil {
		return TaskCard{}, fmt.Errorf("db not set")
	}
	if strings.TrimSpace(sessionKey) == "" {
		return TaskCard{}, fmt.Errorf("session key required")
	}
	card, ok, err := loadTaskCard(ctx, b.DB, sessionKey)
	if err != nil {
		return TaskCard{}, err
	}
	if !ok {
		card = TaskCard{Status: "active"}
	}
	if strings.TrimSpace(card.Goal) == "" {
		state, hasState := TurnStateFromContext(ctx)
		if hasState && strings.TrimSpace(state.UserMessage) != "" {
			card.Goal = oneLine(state.UserMessage, 240)
			card.Metadata.CurrentRequest = oneLine(state.UserMessage, maxPlanNoteChars)
			card.Metadata.CurrentRequestMessageID = state.UserMessageID
		}
	}
	return card, nil
}

func (b PlanToolBase) saveCard(ctx context.Context, sessionKey string, card TaskCard) error {
	scopeKey := resolveTaskCardScope(ctx, b.DB, sessionKey, "")
	card.Status = statusOrDefault(card.Status)
	return saveTaskCard(ctx, b.DB, sessionKey, scopeKey, card)
}

type CreatePlanTool struct {
	PlanToolBase
}

func (t *CreatePlanTool) Name() string { return ToolNameCreatePlan }
func (t *CreatePlanTool) Description() string {
	return "Create or replace the active work plan for this turn. Required before write, exec, web, or extended exploration on complex tasks."
}
func (t *CreatePlanTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "description": "Short plan title."},
			"goal":  map[string]any{"type": "string", "description": "Optional goal summary if different from the user request."},
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"title":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"status":      map[string]any{"type": "string"},
					},
					"required": []string{"title"},
				},
			},
			"next_step": map[string]any{"type": "string", "description": "Immediate next action after creating the plan."},
		},
		"required": []string{"title", "tasks"},
	}
}
func (t *CreatePlanTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *CreatePlanTool) Metadata() tools.ToolMetadata {
	return tools.ToolMetadata{Groups: []string{tools.ToolGroupPlan}, Capabilities: []string{string(tools.CapabilitySafe)}}
}

func (t *CreatePlanTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	sessionKey := t.planSession(ctx)
	card, err := t.loadOrInitCard(ctx, sessionKey)
	if err != nil {
		return "", err
	}
	title := oneLine(fmt.Sprint(params["title"]), 200)
	if title == "" {
		return "", fmt.Errorf("title required")
	}
	rawTasks, _ := params["tasks"].([]any)
	if len(rawTasks) == 0 {
		return "", fmt.Errorf("at least one task required")
	}
	tasks := make([]ActivePlanTask, 0, len(rawTasks))
	for i, raw := range rawTasks {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		task := ActivePlanTask{
			ID:          strings.TrimSpace(fmt.Sprint(item["id"])),
			Title:       oneLine(fmt.Sprint(item["title"]), 200),
			Description: oneLine(fmt.Sprint(item["description"]), maxPlanTaskDescription),
			Status:      normalizePlanTaskStatus(fmt.Sprint(item["status"])),
		}
		if task.ID == "" {
			task.ID = fmt.Sprintf("t%d", i+1)
		}
		if task.Title == "" {
			return "", fmt.Errorf("task title required")
		}
		if task.Status == planTaskStatusPending && i == 0 {
			task.Status = planTaskStatusInProgress
		}
		tasks = append(tasks, task)
	}
	if goal := oneLine(fmt.Sprint(params["goal"]), 240); goal != "" {
		card.Goal = goal
	} else if strings.TrimSpace(card.Goal) == "" {
		card.Goal = title
	}
	card.Metadata = ActivePlanMetadata{
		Title:                   title,
		CurrentRequest:          card.Metadata.CurrentRequest,
		CurrentRequestMessageID: card.Metadata.CurrentRequestMessageID,
		NextStep:                capPlanNote(fmt.Sprint(params["next_step"])),
		Tasks:                   tasks,
		CompletionNotes:         card.Metadata.CompletionNotes,
	}
	if strings.TrimSpace(card.Metadata.CurrentRequest) == "" {
		state, ok := TurnStateFromContext(ctx)
		if ok {
			card.Metadata.CurrentRequest = oneLine(state.UserMessage, maxPlanNoteChars)
			card.Metadata.CurrentRequestMessageID = state.UserMessageID
		}
	}
	syncLegacyPlanFromTasks(&card, &card.Metadata)
	if err := t.saveCard(ctx, sessionKey, card); err != nil {
		return "", err
	}
	return tools.EncodeToolResult(tools.ToolResult{
		Kind:    ToolNameCreatePlan,
		OK:      true,
		Summary: fmt.Sprintf("Created plan %q with %d task(s).", title, len(tasks)),
	}), nil
}

type UpdatePlanTool struct {
	PlanToolBase
}

func (t *UpdatePlanTool) Name() string { return ToolNameUpdatePlan }
func (t *UpdatePlanTool) Description() string {
	return "Update the active work plan title, tasks, statuses, or next-step note."
}
func (t *UpdatePlanTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":     map[string]any{"type": "string"},
			"goal":      map[string]any{"type": "string"},
			"next_step": map[string]any{"type": "string"},
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"title":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"status":      map[string]any{"type": "string"},
					},
					"required": []string{"id"},
				},
			},
		},
	}
}
func (t *UpdatePlanTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *UpdatePlanTool) Metadata() tools.ToolMetadata {
	return tools.ToolMetadata{Groups: []string{tools.ToolGroupPlan}, Capabilities: []string{string(tools.CapabilitySafe)}}
}

func (t *UpdatePlanTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	sessionKey := t.planSession(ctx)
	card, err := t.loadOrInitCard(ctx, sessionKey)
	if err != nil {
		return "", err
	}
	if !activePlanIsEstablished(card.Metadata) {
		return "", fmt.Errorf("no active plan; call create_plan first")
	}
	if title := oneLine(fmt.Sprint(params["title"]), 200); title != "" {
		card.Metadata.Title = title
	}
	if goal := oneLine(fmt.Sprint(params["goal"]), 240); goal != "" {
		card.Goal = goal
	}
	if next := capPlanNote(fmt.Sprint(params["next_step"])); next != "" {
		card.Metadata.NextStep = next
	}
	if rawTasks, ok := params["tasks"].([]any); ok {
		for _, raw := range rawTasks {
			item, _ := raw.(map[string]any)
			if item == nil {
				continue
			}
			id := strings.TrimSpace(fmt.Sprint(item["id"]))
			if id == "" {
				continue
			}
			task, idx := findPlanTask(&card.Metadata, id)
			if idx < 0 {
				newTask := ActivePlanTask{ID: id, Status: planTaskStatusPending}
				if title := oneLine(fmt.Sprint(item["title"]), 200); title != "" {
					newTask.Title = title
				}
				if newTask.Title == "" {
					return "", fmt.Errorf("new task %q requires title", id)
				}
				card.Metadata.Tasks = append(card.Metadata.Tasks, newTask)
				idx = len(card.Metadata.Tasks) - 1
				task = &card.Metadata.Tasks[idx]
			}
			if title := oneLine(fmt.Sprint(item["title"]), 200); title != "" {
				task.Title = title
			}
			if desc := oneLine(fmt.Sprint(item["description"]), maxPlanTaskDescription); desc != "" {
				task.Description = desc
			}
			if status := strings.TrimSpace(fmt.Sprint(item["status"])); status != "" {
				task.Status = normalizePlanTaskStatus(status)
			}
			card.Metadata.Tasks[idx] = *task
		}
	}
	syncLegacyPlanFromTasks(&card, &card.Metadata)
	if err := t.saveCard(ctx, sessionKey, card); err != nil {
		return "", err
	}
	return tools.EncodeToolResult(tools.ToolResult{
		Kind:    ToolNameUpdatePlan,
		OK:      true,
		Summary: "Updated active plan.",
	}), nil
}

type CompletePlanTaskTool struct {
	PlanToolBase
}

func (t *CompletePlanTaskTool) Name() string { return ToolNameCompletePlanTask }
func (t *CompletePlanTaskTool) Description() string {
	return "Mark a plan task complete with a short completion summary and next-step note."
}
func (t *CompletePlanTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id":            map[string]any{"type": "string"},
			"completion_summary": map[string]any{"type": "string"},
			"next_step":          map[string]any{"type": "string"},
		},
		"required": []string{"task_id", "completion_summary", "next_step"},
	}
}
func (t *CompletePlanTaskTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *CompletePlanTaskTool) Metadata() tools.ToolMetadata {
	return tools.ToolMetadata{Groups: []string{tools.ToolGroupPlan}, Capabilities: []string{string(tools.CapabilitySafe)}}
}

func (t *CompletePlanTaskTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	sessionKey := t.planSession(ctx)
	card, err := t.loadOrInitCard(ctx, sessionKey)
	if err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(fmt.Sprint(params["task_id"]))
	summary := capPlanNote(fmt.Sprint(params["completion_summary"]))
	nextStep := capPlanNote(fmt.Sprint(params["next_step"]))
	if taskID == "" || summary == "" || nextStep == "" {
		return "", fmt.Errorf("task_id, completion_summary, and next_step are required")
	}
	task, idx := findPlanTask(&card.Metadata, taskID)
	if idx < 0 {
		return "", fmt.Errorf("task %q not found", taskID)
	}
	task.Status = planTaskStatusCompleted
	task.CompletionNote = summary
	card.Metadata.Tasks[idx] = *task
	card.Metadata.NextStep = nextStep
	card.Metadata.CompletionNotes = appendBoundedString(card.Metadata.CompletionNotes, summary, maxCompletionNotes)
	syncLegacyPlanFromTasks(&card, &card.Metadata)
	if err := t.saveCard(ctx, sessionKey, card); err != nil {
		return "", err
	}
	return tools.EncodeToolResult(tools.ToolResult{
		Kind:    ToolNameCompletePlanTask,
		OK:      true,
		Summary: fmt.Sprintf("Completed task %s.", taskID),
	}), nil
}

type RemovePlanTool struct {
	PlanToolBase
}

func (t *RemovePlanTool) Name() string { return ToolNameRemovePlan }
func (t *RemovePlanTool) Description() string {
	return "Clear the active work plan after all tasks are finished or the turn no longer needs plan tracking."
}
func (t *RemovePlanTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"reason": map[string]any{"type": "string"},
	}}
}
func (t *RemovePlanTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *RemovePlanTool) Metadata() tools.ToolMetadata {
	return tools.ToolMetadata{Groups: []string{tools.ToolGroupPlan}, Capabilities: []string{string(tools.CapabilitySafe)}}
}

func (t *RemovePlanTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	sessionKey := t.planSession(ctx)
	if t.DB == nil {
		return "", fmt.Errorf("db not set")
	}
	if err := t.DB.CompleteActiveTaskState(ctx, sessionKey); err != nil {
		return "", err
	}
	_ = params
	return tools.EncodeToolResult(tools.ToolResult{
		Kind:    ToolNameRemovePlan,
		OK:      true,
		Summary: "Removed active plan.",
	}), nil
}
