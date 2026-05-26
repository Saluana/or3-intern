package agent

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/tools"
)

const explorationToolsBeforePlan = 4

func isPlanToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolNameCreatePlan, ToolNameUpdatePlan, ToolNameCompletePlanTask, ToolNameRemovePlan:
		return true
	default:
		return false
	}
}

// Doctor admin tools (doctor_*) use doctor_create_plan and persisted Doctor plans,
// not the agent task-card create_plan flow. They must not be blocked by EnforceActivePlan.
func isDoctorToolName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), "doctor_")
}

func isExplorationToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case tools.ToolNameReadFile, tools.ToolNameSearchFile, tools.ToolNameListDir, tools.ToolNameReadArtifact, tools.ToolNameMemorySearch:
		return true
	default:
		return false
	}
}

func requiresActivePlan(toolName string, meta tools.ToolMetadata) bool {
	if isPlanToolName(toolName) || isDoctorToolName(toolName) {
		return false
	}
	if tools.IsWriteToolName(toolName) || tools.IsExecutionToolName(toolName) || tools.IsWebToolName(toolName) {
		return true
	}
	if toolName == tools.ToolNameSpawnSubagent {
		return true
	}
	for _, group := range meta.Groups {
		switch group {
		case tools.ToolGroupMCP, tools.ToolGroupService, tools.ToolGroupSkills:
			return true
		}
	}
	return false
}

func (r *Runtime) sessionHasActivePlan(ctx context.Context, sessionKey string) bool {
	if r == nil || r.DB == nil || strings.TrimSpace(sessionKey) == "" {
		return false
	}
	card, ok, err := loadTaskCard(ctx, r.DB, sessionKey)
	if err != nil || !ok {
		return false
	}
	return activePlanIsEstablished(card.Metadata)
}

func (r *Runtime) enforcePlanBeforeTool(ctx context.Context, tool tools.Tool, sessionKey string) error {
	if r == nil || !r.EnforceActivePlan || tool == nil || isPlanToolName(tool.Name()) {
		return nil
	}
	if trustedToolAccessFromContext(ctx) {
		return nil
	}
	meta := tools.ToolMetadata{}
	if r != nil && r.Tools != nil {
		meta = r.Tools.Metadata(tool.Name())
	}
	needsPlan := requiresActivePlan(tool.Name(), meta)
	state, hasState := TurnStateFromContext(ctx)
	if !needsPlan && isExplorationToolName(tool.Name()) {
		if hasState && state.ExplorationToolCalls >= explorationToolsBeforePlan {
			needsPlan = true
		}
	}
	if !needsPlan {
		return nil
	}
	if r.sessionHasActivePlan(ctx, sessionKey) {
		return nil
	}
	return fmt.Errorf("active plan required before %s: call create_plan (or update_plan) with the current user request and next steps, then continue", tool.Name())
}

func (r *Runtime) unfinishedPlanReminder(ctx context.Context, sessionKey string) string {
	if r == nil || r.DB == nil {
		return ""
	}
	card, ok, err := loadTaskCard(ctx, r.DB, sessionKey)
	if err != nil || !ok || !activePlanHasOpenWork(card.Metadata) {
		return ""
	}
	state, hasState := TurnStateFromContext(ctx)
	if hasState && state.PlanGateReminderSent {
		return ""
	}
	var pending []string
	for _, task := range card.Metadata.Tasks {
		switch normalizePlanTaskStatus(task.Status) {
		case planTaskStatusPending, planTaskStatusInProgress:
			pending = append(pending, strings.TrimSpace(task.Title))
		}
	}
	if len(pending) == 0 {
		return ""
	}
	return fmt.Sprintf("Active plan still has unfinished tasks (%s). Finish them with complete_plan_task, update_plan, or explicitly remove_plan before giving a final answer.", strings.Join(pending, "; "))
}
