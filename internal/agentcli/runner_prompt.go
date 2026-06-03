package agentcli

import (
	"strings"
)

const (
	runnerPromptBuilderVersion = "runner-prompt-v1"
	runnerPromptDefaultMaxBytes = 48 * 1024
)

// RunnerPromptContext assembles trusted OR3 instructions, bounded context, and
// the untrusted user/task payload for external runner CLIs.
type RunnerPromptContext struct {
	TrustedSystemInstructions []string
	ContextBlocks             []string
	UserMessage               string
	TriggerKind               string
	MaxBytes                  int
}

// BuildRunnerPrompt renders a deterministic runner task string with explicit
// boundaries between trusted OR3 instructions and untrusted user content.
func BuildRunnerPrompt(ctx RunnerPromptContext) string {
	maxBytes := ctx.MaxBytes
	if maxBytes <= 0 {
		maxBytes = runnerPromptDefaultMaxBytes
	}
	var b strings.Builder
	b.WriteString("<trusted_or3_system_instructions>\n")
	for _, block := range ctx.TrustedSystemInstructions {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		b.WriteString(block)
		b.WriteByte('\n')
	}
	b.WriteString("</trusted_or3_system_instructions>\n")

	b.WriteString("\n<or3_context>\n")
	if ctx.TriggerKind != "" {
		b.WriteString("trigger: ")
		b.WriteString(strings.TrimSpace(ctx.TriggerKind))
		b.WriteByte('\n')
	}
	for _, block := range ctx.ContextBlocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		b.WriteString(block)
		b.WriteByte('\n')
	}
	b.WriteString("</or3_context>\n")

	user := strings.TrimSpace(ctx.UserMessage)
	b.WriteString("\n<user_task>\n")
	b.WriteString(user)
	b.WriteString("\n</user_task>\n")

	out := b.String()
	if len(out) > maxBytes {
		out = truncateRunnerPrompt(out, maxBytes, user)
	}
	return out
}

func truncateRunnerPrompt(full string, maxBytes int, userMessage string) string {
	user := strings.TrimSpace(userMessage)
	userBlock := "\n<user_task>\n" + user + "\n</user_task>\n"
	if len(userBlock) >= maxBytes {
		return userBlock[len(userBlock)-maxBytes:]
	}
	keep := maxBytes - len(userBlock)
	if keep <= 0 {
		return userBlock
	}
	prefix := full[:len(full)-len(userBlock)]
	if len(prefix) > keep {
		prefix = "[...truncated...]\n" + prefix[len(prefix)-keep+len("[...truncated...]\n"):]
	}
	return prefix + userBlock
}

// PromptBuilderVersion returns the active runner prompt schema version for caches.
func PromptBuilderVersion() string {
	return runnerPromptBuilderVersion
}
