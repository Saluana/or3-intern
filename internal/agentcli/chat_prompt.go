package agentcli

import (
	"fmt"
	"strings"
)

// Bounded replay limits. design.md says these can be promoted to
// AgentCLIConfig if defaults prove insufficient. For now they live as
// constants near the prompt builder.
const (
	replayMaxTurns = 12
	replayMaxBytes = 48 * 1024 // 48 KiB
)

// BuildReplayPrompt constructs a transcript prompt from previously persisted
// runner_chat_turns plus a new user message. Newer turns are preserved when
// truncation is required. The result is always a non-empty string ending
// with the new user message.
func BuildReplayPrompt(history []RunnerChatTurn, newUserMessage string) string {
	return BuildReplayPromptBounded(history, newUserMessage, replayMaxTurns, replayMaxBytes)
}

// BuildReplayPromptBounded is BuildReplayPrompt with explicit limits.
func BuildReplayPromptBounded(history []RunnerChatTurn, newUserMessage string, maxTurns, maxBytes int) string {
	if maxTurns <= 0 {
		maxTurns = replayMaxTurns
	}
	if maxBytes <= 0 {
		maxBytes = replayMaxBytes
	}
	completed := filterCompletedHistory(history)
	// Keep newest first, then reverse for output.
	turnLimitTruncated := false
	if len(completed) > maxTurns {
		completed = completed[len(completed)-maxTurns:]
		turnLimitTruncated = true
	}
	rendered, truncated := renderTurnsBounded(completed, maxBytes)
	truncated = truncated || turnLimitTruncated
	var b strings.Builder
	b.WriteString("System: This conversation is being replayed for context. ")
	b.WriteString("Previous turns are provided below in chronological order. ")
	b.WriteString("Treat them as authoritative chat history.\n")
	if truncated {
		b.WriteString("System: Earlier turns were truncated to fit context limits.\n")
	}
	if rendered != "" {
		b.WriteString("\n--- Previous turns ---\n")
		b.WriteString(rendered)
		b.WriteString("\n--- End previous turns ---\n")
	}
	b.WriteString("\nUser: ")
	b.WriteString(strings.TrimSpace(newUserMessage))
	b.WriteString("\n")
	return b.String()
}

func filterCompletedHistory(history []RunnerChatTurn) []RunnerChatTurn {
	out := make([]RunnerChatTurn, 0, len(history))
	for _, t := range history {
		// Use the design.md status names. We accept the agentcli RunnerChatTurn
		// type, which may be populated by either the manager or tests.
		switch t.Status {
		case "succeeded", "completed", "":
			// "" is permitted so simple test cases without explicit status
			// flags still render.
		default:
			continue
		}
		if strings.TrimSpace(t.UserText) == "" && strings.TrimSpace(t.FinalText) == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// renderTurnsBounded renders newest-first under maxBytes, then re-orders
// chronologically. Returns (text, truncated).
func renderTurnsBounded(turns []RunnerChatTurn, maxBytes int) (string, bool) {
	if len(turns) == 0 {
		return "", false
	}
	// Render each turn into a string.
	rendered := make([]string, len(turns))
	for i, t := range turns {
		rendered[i] = renderSingleTurn(t)
	}
	// Walk from newest to oldest accumulating bytes.
	included := make([]string, 0, len(turns))
	used := 0
	truncated := false
	for i := len(rendered) - 1; i >= 0; i-- {
		next := used + len(rendered[i])
		if next > maxBytes {
			// If even the newest single turn doesn't fit, deterministically
			// truncate it and stop.
			if len(included) == 0 {
				included = append(included, truncateFront(rendered[i], maxBytes))
				truncated = true
			} else {
				truncated = true
			}
			break
		}
		// Prepend so chronological order is preserved.
		included = append([]string{rendered[i]}, included...)
		used = next
	}
	if len(included) < len(turns) {
		truncated = true
	}
	return strings.Join(included, "\n"), truncated
}

func renderSingleTurn(t RunnerChatTurn) string {
	user := strings.TrimSpace(t.UserText)
	final := sanitizeReplayAssistantText(t.FinalText)
	switch {
	case user != "" && final != "":
		return fmt.Sprintf("User: %s\nAssistant: %s\n", user, final)
	case user != "":
		return fmt.Sprintf("User: %s\n", user)
	case final != "":
		return fmt.Sprintf("Assistant: %s\n", final)
	default:
		return ""
	}
}

func sanitizeReplayAssistantText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if nested := extractGeminiAssistantFromSerialized(trimmed, 0); nested != "" {
		return nested
	}
	return trimmed
}

// truncateFront keeps the latter portion of `s` so the most recent text wins
// when a single turn alone exceeds `maxBytes`.
func truncateFront(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	const marker = "[...truncated...]\n"
	if maxBytes <= len(marker) {
		return marker
	}
	keep := maxBytes - len(marker)
	return marker + s[len(s)-keep:]
}
