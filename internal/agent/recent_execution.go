package agent

import (
	"context"
	"encoding/json"
	"strings"
)

const maxRecentExecutionChars = 1800

func (b *Builder) buildRecentExecutionState(ctx context.Context, sessionKey string, planMeta ActivePlanMetadata) string {
	if b == nil || b.DB == nil || strings.TrimSpace(sessionKey) == "" {
		return ""
	}
	rows, err := b.DB.GetLastMessagesScoped(ctx, sessionKey, 24)
	if err != nil || len(rows) == 0 {
		return ""
	}
	var commands, files, failures, tests []string
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		switch row.Role {
		case "tool":
			toolName, preview := toolMessageSummary(row.Content, row.PayloadJSON)
			if toolName == "" {
				continue
			}
			switch toolName {
			case "exec", "run_skill", "run_skill_script":
				if preview != "" {
					commands = appendBoundedString(commands, preview, 4)
				}
			case "write_file", "edit_file":
				if preview != "" {
					files = appendBoundedString(files, preview, 4)
				}
			default:
				if strings.Contains(strings.ToLower(preview), "test") {
					tests = appendBoundedString(tests, preview, 3)
				}
			}
			if strings.Contains(strings.ToLower(preview), "fail") || strings.Contains(strings.ToLower(preview), "error") {
				failures = appendBoundedString(failures, oneLine(preview, 200), 2)
			}
		}
	}
	var body strings.Builder
	if len(commands) > 0 {
		body.WriteString("Last commands: ")
		body.WriteString(strings.Join(commands, "; "))
		body.WriteString("\n")
	}
	if len(files) > 0 {
		body.WriteString("Files touched: ")
		body.WriteString(strings.Join(files, "; "))
		body.WriteString("\n")
	}
	if len(tests) > 0 {
		body.WriteString("Tests run: ")
		body.WriteString(strings.Join(tests, "; "))
		body.WriteString("\n")
	}
	if len(failures) > 0 {
		body.WriteString("Last failure: ")
		body.WriteString(failures[len(failures)-1])
		body.WriteString("\n")
	}
	if next := strings.TrimSpace(planMeta.NextStep); next != "" {
		body.WriteString("Next step: ")
		body.WriteString(next)
		body.WriteString("\n")
	}
	out := strings.TrimSpace(body.String())
	if maxRecentExecutionChars > 0 && len(out) > maxRecentExecutionChars {
		out = strings.TrimSpace(out[:maxRecentExecutionChars]) + "\n…[truncated]"
	}
	return out
}

func toolMessageSummary(content, payloadJSON string) (string, string) {
	toolName := ""
	if strings.TrimSpace(payloadJSON) != "" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err == nil {
			toolName = payloadStringValue(payload["tool"])
		}
	}
	preview := oneLine(content, 220)
	return toolName, preview
}
