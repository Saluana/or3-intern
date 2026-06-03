package app

import "strings"

// RunnerBootstrapContext carries static OR3 bootstrap files used when building
// runner prompts. Callers populate this from workspace bootstrap files.
type RunnerBootstrapContext struct {
	Soul              string
	AgentInstructions string
	ToolNotes         string
	IdentityText      string
	StaticMemory      string
	HeartbeatTasks    string
}

func (b RunnerBootstrapContext) trustedBlocks() []string {
	return []string{
		b.Soul,
		b.AgentInstructions,
		b.ToolNotes,
		b.IdentityText,
		b.StaticMemory,
	}
}

func (b RunnerBootstrapContext) contextBlocks(triggerKind string) []string {
	if !isAutonomousTrigger(triggerKind) {
		return nil
	}
	if text := strings.TrimSpace(b.HeartbeatTasks); text != "" {
		return []string{"heartbeat_tasks:\n" + text}
	}
	return nil
}

func isAutonomousTrigger(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "heartbeat", "cron", "webhook", "file_watch", "system":
		return true
	default:
		return false
	}
}
