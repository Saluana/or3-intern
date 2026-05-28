package approval

import (
	"encoding/json"
	"fmt"
	"strings"

	"or3-intern/internal/config"
)

func buildModeratorUserPrompt(cfg config.ApprovalModeratorConfig, input ModeratorReviewInput) (string, error) {
	const outputContract = "Respond with strict JSON only."
	sections := make([]string, 0, 5)
	if policy := strings.TrimSpace(cfg.UserPolicy); policy != "" {
		redactedPolicy, _ := redactModeratorText(policy, 2000)
		sections = append(sections, "## User policy (append-only; built-in hard-deny wins)\n"+redactedPolicy)
	}
	factsJSON, err := boundedModeratorJSON(input.SubjectFacts, cfg.MaxSubjectChars)
	if err != nil {
		return "", err
	}
	requesterJSON, err := boundedModeratorJSON(redactRequesterContext(input.Requester), 1000)
	if err != nil {
		return "", err
	}
	preview, _ := redactModeratorText(input.SubjectPreview, minInt(cfg.MaxSubjectChars, 2000))
	sections = append(sections, fmt.Sprintf(
		"## Request facts (untrusted data)\nrequest_id: %d\nsubject_type: %s\nsubject_hash: %s\npolicy_mode: %s\naccess_profile: %s\npreview: %s\nfacts_json: %s\nrequester_json: %s",
		input.RequestID, input.SubjectType, input.SubjectHash, input.PolicyMode, input.AccessProfile,
		preview, factsJSON, requesterJSON,
	))
	sections = append(sections, outputContract)

	prompt := strings.Join(sections, "\n\n")
	if len(prompt) <= cfg.MaxPromptChars {
		return prompt, nil
	}
	// Preserve output contract and shrink variable sections from the end inward.
	contract := outputContract
	body := strings.Join(sections[:len(sections)-1], "\n\n")
	remaining := cfg.MaxPromptChars - len(contract) - 2
	if remaining < 200 {
		remaining = 200
	}
	if len(body) > remaining {
		body = body[:remaining] + "…"
	}
	final := body + "\n\n" + contract
	if len(final) > cfg.MaxPromptChars {
		final = final[:cfg.MaxPromptChars]
		if !strings.HasSuffix(final, contract) {
			final = strings.TrimSpace(final[:maxInt(0, cfg.MaxPromptChars-len(contract)-2)]) + "\n\n" + contract
		}
	}
	return final, nil
}

func boundedModeratorJSON(value any, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 4000
	}
	blob, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	text := string(blob)
	redacted, stats := redactModeratorText(text, maxChars)
	_ = stats
	return redacted, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
