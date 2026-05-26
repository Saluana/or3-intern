package agent

import "strings"

// maybeReplaceStaleActivePlan clears an in-progress plan when a new user message
// meaningfully changes task intent so stale goals cannot linger across turns.
func maybeReplaceStaleActivePlan(meta *ActivePlanMetadata, newMessage string, newMessageID int64) {
	if meta == nil || newMessageID <= 0 {
		return
	}
	if !activePlanHasOpenWork(*meta) {
		return
	}
	if !shouldReplaceActivePlan(*meta, newMessage, newMessageID) {
		return
	}
	meta.Tasks = nil
	meta.CompletionNotes = nil
	meta.NextStep = ""
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = oneLine(newMessage, 120)
	}
}

func shouldReplaceActivePlan(meta ActivePlanMetadata, newMessage string, newMessageID int64) bool {
	if meta.CurrentRequestMessageID <= 0 || newMessageID <= meta.CurrentRequestMessageID {
		return false
	}
	old := strings.TrimSpace(meta.CurrentRequest)
	newMsg := strings.TrimSpace(newMessage)
	if old == "" || newMsg == "" || old == newMsg {
		return false
	}
	if strings.Contains(old, newMsg) || strings.Contains(newMsg, old) {
		return false
	}
	return true
}
