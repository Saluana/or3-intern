package runners

import "strings"

// ChatExecutionInput selects the payload native Codex/OpenCode runtimes should send.
// Replay mode uses the compiled replay transcript; native continuation sends only the
// user delta once a provider session exists; the first native turn still needs bootstrap
// content from Run.Task.
func ChatExecutionInput(chat RunnerChatCommandRequest, runTask string) string {
	switch chat.ContinuationMode {
	case ContinuationNative:
		if strings.TrimSpace(chat.NativeSessionRef) != "" {
			return strings.TrimSpace(chat.UserMessage)
		}
		return firstNonEmpty(strings.TrimSpace(runTask), strings.TrimSpace(chat.UserMessage))
	default:
		return firstNonEmpty(strings.TrimSpace(chat.ReplayPrompt), strings.TrimSpace(runTask), strings.TrimSpace(chat.UserMessage))
	}
}
