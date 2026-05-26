package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/config"
	"or3-intern/internal/providers"
)

type ProviderModerator struct {
	Client  *providers.Client
	Model   string
	Config  config.ApprovalModeratorConfig
	Timeout time.Duration
}

func NewProviderModerator(client *providers.Client, model string, cfg config.ApprovalModeratorConfig) *ProviderModerator {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &ProviderModerator{Client: client, Model: strings.TrimSpace(model), Config: cfg, Timeout: timeout}
}

func (m *ProviderModerator) ModelIdentity() string {
	if m == nil {
		return ""
	}
	model := strings.TrimSpace(m.Model)
	if model == "" {
		return "moderator:unknown"
	}
	return "moderator:" + model
}

func (m *ProviderModerator) PolicyHash() string {
	sum := sha256.Sum256([]byte(builtinModeratorPolicy + "\n" + strings.TrimSpace(m.Config.UserPolicy)))
	return hex.EncodeToString(sum[:8])
}

func (m *ProviderModerator) ReviewApproval(ctx context.Context, input ModeratorReviewInput) (ModeratorReviewResult, error) {
	if m == nil || m.Client == nil {
		return ModeratorReviewResult{}, fmt.Errorf("moderator provider unavailable")
	}
	if hard, ok := deterministicHardDeny(input.SubjectType, subjectFromFacts(input)); ok {
		return hard, nil
	}
	prompt, err := buildModeratorUserPrompt(m.Config, input)
	if err != nil {
		return ModeratorReviewResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	model := strings.TrimSpace(m.Model)
	if model == "" {
		return ModeratorReviewResult{}, fmt.Errorf("moderator model not configured")
	}
	resp, err := m.Client.Chat(callCtx, providers.ChatCompletionRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: builtinModeratorPolicy},
			{Role: "user", Content: prompt},
		},
		Temperature: 0,
	})
	if err != nil {
		return ModeratorReviewResult{}, err
	}
	raw := extractChatContent(resp)
	parsed, err := parseModeratorResponse(raw)
	if err != nil {
		return ModeratorReviewResult{}, err
	}
	return enforceModeratorDecision(m.Config, parsed, input.SubjectType, subjectFromFacts(input), m.Config.UserPolicy), nil
}

func extractChatContent(resp providers.ChatCompletionResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	switch content := resp.Choices[0].Message.Content.(type) {
	case string:
		return content
	default:
		blob, _ := json.Marshal(content)
		return string(blob)
	}
}

func subjectFromFacts(input ModeratorReviewInput) any {
	switch input.SubjectType {
	case SubjectExec:
		argv, _ := input.SubjectFacts["argv"].([]string)
		if argv == nil {
			if raw, ok := input.SubjectFacts["argv"].([]any); ok {
				for _, item := range raw {
					if s, ok := item.(string); ok {
						argv = append(argv, s)
					}
				}
			}
		}
		exe, _ := input.SubjectFacts["executable"].(string)
		cwd, _ := input.SubjectFacts["working_dir"].(string)
		return ExecSubject{ExecutablePath: exe, Argv: argv, WorkingDir: cwd}
	case SubjectToolQuota:
		current, _ := input.SubjectFacts["current"].(int)
		limit, _ := input.SubjectFacts["limit"].(int)
		scope, _ := input.SubjectFacts["scope"].(string)
		limitName, _ := input.SubjectFacts["limit_name"].(string)
		toolName, _ := input.SubjectFacts["tool_name"].(string)
		return ToolQuotaSubject{Scope: scope, LimitName: limitName, ToolName: toolName, Current: current, Limit: limit}
	default:
		return input.SubjectFacts
	}
}

// FakeModerator supports deterministic tests.
type FakeModerator struct {
	Result ModeratorReviewResult
	Err    error
	Model  string
}

func (f *FakeModerator) ReviewApproval(ctx context.Context, input ModeratorReviewInput) (ModeratorReviewResult, error) {
	if f.Err != nil {
		return ModeratorReviewResult{}, f.Err
	}
	result := f.Result
	if result.Reason == "" {
		result.Reason = "fake moderator"
	}
	return result, nil
}

func (f *FakeModerator) ModelIdentity() string {
	if f != nil && strings.TrimSpace(f.Model) != "" {
		return f.Model
	}
	return "moderator:fake"
}

func (f *FakeModerator) PolicyHash() string { return "fake-policy" }
