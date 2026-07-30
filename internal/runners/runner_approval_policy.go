package runners

import (
	"encoding/json"
	"strings"
)

// DefaultRunnerApprovalAutopilot is the server-owned default when callers omit approval_autopilot.
const DefaultRunnerApprovalAutopilot = true

// ResolveRunnerApprovalAutopilot returns the effective runner approval autopilot policy.
func ResolveRunnerApprovalAutopilot(explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	return DefaultRunnerApprovalAutopilot
}

// RunnerApprovalAutopilotFromTurnMeta reads the per-turn policy from durable turn metadata.
func RunnerApprovalAutopilotFromTurnMeta(metaJSON string) bool {
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return DefaultRunnerApprovalAutopilot
	}
	value, ok := meta["runner_approval_autopilot"]
	if !ok {
		return DefaultRunnerApprovalAutopilot
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return DefaultRunnerApprovalAutopilot
	}
}

func runnerChatTurnMetaJSON(approvalAutopilot bool, memoryDebug RunnerMemoryDebug) string {
	payload := map[string]any{"runner_approval_autopilot": approvalAutopilot}
	if memoryDebug != (RunnerMemoryDebug{}) {
		payload["runner_memory_debug"] = memoryDebug
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
