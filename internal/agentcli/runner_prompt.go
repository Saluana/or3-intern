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
	user := strings.TrimSpace(ctx.UserMessage)
	userBlock := runnerPromptUserBlock(user)
	stable := runnerPromptTrustedBlock(ctx.TrustedSystemInstructions)
	volatile := runnerPromptContextBlock(ctx.TriggerKind, ctx.ContextBlocks)
	prefixBudget := maxBytes - len(userBlock)
	if prefixBudget < 0 {
		if len(userBlock) > maxBytes {
			return userBlock[len(userBlock)-maxBytes:]
		}
		return userBlock
	}
	if len(stable)+len(volatile) > prefixBudget {
		volatileBudget := prefixBudget - len(stable)
		if volatileBudget < 0 {
			volatileBudget = 0
		}
		volatile = truncateRunnerPromptPrefix(volatile, volatileBudget)
		if len(stable)+len(volatile) > prefixBudget {
			stableBudget := prefixBudget - len(volatile)
			if stableBudget < 0 {
				stableBudget = 0
			}
			stable = truncateRunnerPromptPrefix(stable, stableBudget)
		}
	}
	return stable + volatile + userBlock
}

func runnerPromptUserBlock(user string) string {
	return "\n<user_task>\n" + user + "\n</user_task>\n"
}

func runnerPromptTrustedBlock(blocks []string) string {
	var b strings.Builder
	b.WriteString("<trusted_or3_system_instructions>\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		b.WriteString(block)
		b.WriteByte('\n')
	}
	b.WriteString("</trusted_or3_system_instructions>\n")
	return b.String()
}

func runnerPromptContextBlock(triggerKind string, blocks []string) string {
	var b strings.Builder
	b.WriteString("\n<or3_context>\n")
	if triggerKind != "" {
		b.WriteString("trigger: ")
		b.WriteString(strings.TrimSpace(triggerKind))
		b.WriteByte('\n')
	}
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		b.WriteString(block)
		b.WriteByte('\n')
	}
	b.WriteString("</or3_context>\n")
	return b.String()
}

func truncateRunnerPromptPrefix(prefix string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(prefix) <= maxBytes {
		return prefix
	}
	marker := "[...truncated...]\n"
	if maxBytes <= len(marker) {
		return prefix[len(prefix)-maxBytes:]
	}
	return marker + prefix[len(prefix)-(maxBytes-len(marker)):]
}

// PromptBuilderVersion returns the active runner prompt schema version for caches.
func PromptBuilderVersion() string {
	return runnerPromptBuilderVersion
}
