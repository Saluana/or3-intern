package app

import "strings"

// RunnerBootstrapContext carries OR3 bootstrap files used when building runner
// prompts. AgentInstructions and ToolNotes are loaded for compatibility, but
// runner-native CLIs read their own AGENTS/tool instructions, so OR3 does not
// inject them into the trusted prompt envelope.
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
		b.IdentityText,
		runnerMemoryToolHint,
	}
}

const runnerMemoryToolHint = "Memory context may be provided below. If an OR3 memory tool is available, save durable user preferences/facts only."

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
