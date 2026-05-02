package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/agent"
	"or3-intern/internal/app"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

type serviceTurnRequest struct {
	SessionKey     string
	Message        string
	AllowedTools   []string
	RestrictTools  bool
	Meta           map[string]any
	ProfileName    string
	ApprovalToken  string
	ReplayToolCall *serviceReplayToolCall
}

type serviceSubagentRequest struct {
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	AllowedTools     []string
	RestrictTools    bool
	TimeoutSeconds   int
	Meta             map[string]any
	ProfileName      string
	Channel          string
	ReplyTo          string
	ApprovalToken    string
}

type serviceToolPolicyPayload struct {
	Mode              string   `json:"mode"`
	AllowedTools      []string `json:"allowed_tools"`
	AllowedToolsCamel []string `json:"allowedTools"`
	BlockedTools      []string `json:"blocked_tools"`
	BlockedToolsCamel []string `json:"blockedTools"`
}

type serviceReplayToolCall struct {
	Name          string
	ArgumentsJSON string
}

type serviceReplayToolCallPayload struct {
	Name               string          `json:"name"`
	Arguments          json.RawMessage `json:"arguments"`
	ArgumentsCamel     json.RawMessage `json:"argumentsCamel"`
	ArgumentsJSON      string          `json:"arguments_json"`
	ArgumentsJSONCamel string          `json:"argumentsJson"`
}

type serviceTurnRequestPayload struct {
	SessionKey            string                        `json:"session_key"`
	InternSessionKey      string                        `json:"intern_session_key"`
	SessionKeyCamel       string                        `json:"sessionKey"`
	InternSessionKeyCamel string                        `json:"internSessionKey"`
	PlatformSessionRef    map[string]any                `json:"platform_session_ref"`
	Message               string                        `json:"message"`
	AllowedTools          []string                      `json:"allowed_tools"`
	AllowedToolsCamel     []string                      `json:"allowedTools"`
	ToolPolicy            *serviceToolPolicyPayload     `json:"tool_policy"`
	ToolPolicyCamel       *serviceToolPolicyPayload     `json:"toolPolicy"`
	Meta                  map[string]any                `json:"meta"`
	ProfileName           string                        `json:"profile_name"`
	ProfileNameCamel      string                        `json:"profileName"`
	ApprovalToken         string                        `json:"approval_token"`
	ApprovalTokenCamel    string                        `json:"approvalToken"`
	ReplayToolCall        *serviceReplayToolCallPayload `json:"replay_tool_call"`
	ReplayToolCallCamel   *serviceReplayToolCallPayload `json:"replayToolCall"`
}

type serviceSubagentRequestPayload struct {
	ParentSessionKey      string                    `json:"parent_session_key"`
	ParentSessionKeyCamel string                    `json:"parentSessionKey"`
	SessionKey            string                    `json:"session_key"`
	InternSessionKey      string                    `json:"intern_session_key"`
	SessionKeyCamel       string                    `json:"sessionKey"`
	InternSessionKeyCamel string                    `json:"internSessionKey"`
	Task                  string                    `json:"task"`
	PromptSnapshot        []providers.ChatMessage   `json:"prompt_snapshot"`
	PromptSnapshotCamel   []providers.ChatMessage   `json:"promptSnapshot"`
	AllowedTools          []string                  `json:"allowed_tools"`
	AllowedToolsCamel     []string                  `json:"allowedTools"`
	ToolPolicy            *serviceToolPolicyPayload `json:"tool_policy"`
	ToolPolicyCamel       *serviceToolPolicyPayload `json:"toolPolicy"`
	TimeoutSeconds        json.Number               `json:"timeout_seconds"`
	TimeoutSecondsCamel   json.Number               `json:"timeoutSeconds"`
	Timeout               json.Number               `json:"timeout"`
	Meta                  map[string]any            `json:"meta"`
	ProfileName           string                    `json:"profile_name"`
	ProfileNameCamel      string                    `json:"profileName"`
	Channel               string                    `json:"channel"`
	ReplyTo               string                    `json:"reply_to"`
	ReplyToCamel          string                    `json:"replyTo"`
	ApprovalToken         string                    `json:"approval_token"`
	ApprovalTokenCamel    string                    `json:"approvalToken"`
}

func decodeServiceTurnRequest(body io.Reader, registry *tools.Registry) (serviceTurnRequest, error) {
	var payload serviceTurnRequestPayload
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceTurnRequest{}, err
	}
	allowedTools, restrictTools, err := app.ResolveToolPolicy(
		registry,
		firstToolPolicy(payload.ToolPolicy, payload.ToolPolicyCamel),
		firstStringSlice(payload.AllowedTools, payload.AllowedToolsCamel),
	)
	if err != nil {
		return serviceTurnRequest{}, err
	}
	replayToolCall, err := firstReplayToolCall(payload.ReplayToolCall, payload.ReplayToolCallCamel)
	if err != nil {
		return serviceTurnRequest{}, err
	}
	return serviceTurnRequest{
		SessionKey:     firstNonEmptyString(payload.SessionKey, payload.InternSessionKey, payload.SessionKeyCamel, payload.InternSessionKeyCamel),
		Message:        strings.TrimSpace(payload.Message),
		AllowedTools:   allowedTools,
		RestrictTools:  restrictTools,
		Meta:           cloneMapOrEmpty(payload.Meta),
		ProfileName:    firstNonEmptyString(payload.ProfileName, payload.ProfileNameCamel),
		ApprovalToken:  firstNonEmptyString(payload.ApprovalToken, payload.ApprovalTokenCamel),
		ReplayToolCall: replayToolCall,
	}, nil
}

func decodeServiceSubagentRequest(body io.Reader, registry *tools.Registry) (serviceSubagentRequest, error) {
	var payload serviceSubagentRequestPayload
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceSubagentRequest{}, err
	}
	allowedTools, restrictTools, err := app.ResolveToolPolicy(
		registry,
		firstToolPolicy(payload.ToolPolicy, payload.ToolPolicyCamel),
		firstStringSlice(payload.AllowedTools, payload.AllowedToolsCamel),
	)
	if err != nil {
		return serviceSubagentRequest{}, err
	}
	timeoutSeconds, err := firstPositiveInt(payload.TimeoutSeconds, payload.TimeoutSecondsCamel, payload.Timeout)
	if err != nil {
		return serviceSubagentRequest{}, err
	}
	return serviceSubagentRequest{
		ParentSessionKey: firstNonEmptyString(
			payload.ParentSessionKey,
			payload.ParentSessionKeyCamel,
			payload.SessionKey,
			payload.InternSessionKey,
			payload.SessionKeyCamel,
			payload.InternSessionKeyCamel,
		),
		Task:           strings.TrimSpace(payload.Task),
		PromptSnapshot: firstPromptSnapshot(payload.PromptSnapshot, payload.PromptSnapshotCamel),
		AllowedTools:   allowedTools,
		RestrictTools:  restrictTools,
		TimeoutSeconds: timeoutSeconds,
		Meta:           cloneMapOrEmpty(payload.Meta),
		ProfileName:    firstNonEmptyString(payload.ProfileName, payload.ProfileNameCamel),
		Channel:        strings.TrimSpace(payload.Channel),
		ReplyTo:        firstNonEmptyString(payload.ReplyTo, payload.ReplyToCamel),
		ApprovalToken:  firstNonEmptyString(payload.ApprovalToken, payload.ApprovalTokenCamel),
	}, nil
}

func decodeServiceRequestBody(body io.Reader, out any) error {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}

func firstToolPolicy(values ...*serviceToolPolicyPayload) *agent.ServiceToolPolicy {
	for _, value := range values {
		if value == nil {
			continue
		}
		return &agent.ServiceToolPolicy{
			Mode:         strings.TrimSpace(value.Mode),
			AllowedTools: firstStringSlice(value.AllowedTools, value.AllowedToolsCamel),
			BlockedTools: firstStringSlice(value.BlockedTools, value.BlockedToolsCamel),
		}
	}
	return nil
}

func firstReplayToolCall(values ...*serviceReplayToolCallPayload) (*serviceReplayToolCall, error) {
	for _, value := range values {
		if value == nil {
			continue
		}
		name := strings.TrimSpace(value.Name)
		if name == "" {
			return nil, errors.New("replay_tool_call.name is required")
		}
		argsJSON := strings.TrimSpace(firstNonEmptyString(value.ArgumentsJSON, value.ArgumentsJSONCamel))
		if argsJSON == "" {
			raw := value.Arguments
			if len(raw) == 0 {
				raw = value.ArgumentsCamel
			}
			argsJSON = strings.TrimSpace(string(raw))
		}
		if argsJSON == "" {
			argsJSON = "{}"
		}
		var params map[string]any
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return nil, fmt.Errorf("invalid replay_tool_call arguments: %w", err)
		}
		return &serviceReplayToolCall{Name: name, ArgumentsJSON: argsJSON}, nil
	}
	return nil, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		out := make([]string, len(value))
		copy(out, value)
		return out
	}
	return nil
}

func firstPromptSnapshot(values ...[]providers.ChatMessage) []providers.ChatMessage {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		out := make([]providers.ChatMessage, len(value))
		copy(out, value)
		return out
	}
	return nil
}

func firstPositiveInt(values ...json.Number) (int, error) {
	for _, value := range values {
		raw := strings.TrimSpace(value.String())
		if raw == "" {
			continue
		}
		n, err := value.Int64()
		if err != nil {
			return 0, fmt.Errorf("invalid timeout")
		}
		if n <= 0 {
			continue
		}
		return int(n), nil
	}
	return 0, nil
}

func cloneMapOrEmpty(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func backgroundToolsRegistry(manager *agent.SubagentManager) *tools.Registry {
	if manager == nil {
		return tools.NewRegistry()
	}
	if manager.BackgroundTools != nil {
		return manager.BackgroundTools()
	}
	return tools.NewRegistry()
}
