package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/agent"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

type serviceTurnRequest struct {
	SessionKey    string
	Message       string
	AllowedTools  []string
	RestrictTools bool
	Meta          map[string]any
	ProfileName   string
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
}

type serviceToolPolicyPayload struct {
	Mode              string   `json:"mode"`
	AllowedTools      []string `json:"allowed_tools"`
	AllowedToolsCamel []string `json:"allowedTools"`
	BlockedTools      []string `json:"blocked_tools"`
	BlockedToolsCamel []string `json:"blockedTools"`
}

type serviceTurnRequestPayload struct {
	SessionKey            string                    `json:"session_key"`
	InternSessionKey      string                    `json:"intern_session_key"`
	SessionKeyCamel       string                    `json:"sessionKey"`
	InternSessionKeyCamel string                    `json:"internSessionKey"`
	Message               string                    `json:"message"`
	AllowedTools          []string                  `json:"allowed_tools"`
	AllowedToolsCamel     []string                  `json:"allowedTools"`
	ToolPolicy            *serviceToolPolicyPayload `json:"tool_policy"`
	ToolPolicyCamel       *serviceToolPolicyPayload `json:"toolPolicy"`
	Meta                  map[string]any            `json:"meta"`
	ProfileName           string                    `json:"profile_name"`
	ProfileNameCamel      string                    `json:"profileName"`
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
}

func decodeServiceTurnRequest(body io.Reader, registry *tools.Registry) (serviceTurnRequest, error) {
	var payload serviceTurnRequestPayload
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceTurnRequest{}, err
	}
	allowedTools, restrictTools, err := agent.ResolveServiceToolAllowlist(
		registry,
		firstToolPolicy(payload.ToolPolicy, payload.ToolPolicyCamel),
		firstStringSlice(payload.AllowedTools, payload.AllowedToolsCamel),
	)
	if err != nil {
		return serviceTurnRequest{}, err
	}
	return serviceTurnRequest{
		SessionKey:    firstNonEmptyString(payload.SessionKey, payload.InternSessionKey, payload.SessionKeyCamel, payload.InternSessionKeyCamel),
		Message:       strings.TrimSpace(payload.Message),
		AllowedTools:  allowedTools,
		RestrictTools: restrictTools,
		Meta:          cloneMapOrEmpty(payload.Meta),
		ProfileName:   firstNonEmptyString(payload.ProfileName, payload.ProfileNameCamel),
	}, nil
}

func decodeServiceSubagentRequest(body io.Reader, registry *tools.Registry) (serviceSubagentRequest, error) {
	var payload serviceSubagentRequestPayload
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceSubagentRequest{}, err
	}
	allowedTools, restrictTools, err := agent.ResolveServiceToolAllowlist(
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
	}, nil
}

func decodeServiceRequestBody(body io.Reader, out any) error {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	return decoder.Decode(out)
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
