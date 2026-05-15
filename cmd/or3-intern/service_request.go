package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"or3-intern/internal/agent"
	"or3-intern/internal/app"
	"or3-intern/internal/compat"
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
	Warnings       []string
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
	Warnings         []string
}

type serviceAgentRunRequest struct {
	ParentSessionKey string
	RunnerID         string
	Task             string
	TimeoutSeconds   int
	Cwd              string
	Model            string
	Mode             string
	Isolation        string
	MaxTurns         int
	Meta             map[string]any
	Warnings         []string
}

type serviceAgentRunRequestPayload struct {
	ParentSessionKey      string         `json:"parent_session_key"`
	ParentSessionKeyCamel string         `json:"parentSessionKey"`
	RunnerID              string         `json:"runner_id"`
	RunnerIDCamel         string         `json:"runnerId"`
	Task                  string         `json:"task"`
	TimeoutSeconds        json.Number    `json:"timeout_seconds"`
	TimeoutSecondsCamel   json.Number    `json:"timeoutSeconds"`
	Cwd                   string         `json:"cwd"`
	Model                 string         `json:"model"`
	Mode                  string         `json:"mode"`
	Isolation             string         `json:"isolation"`
	MaxTurns              json.Number    `json:"max_turns"`
	MaxTurnsCamel         json.Number    `json:"maxTurns"`
	Meta                  map[string]any `json:"meta"`
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
	fields, err := decodeServiceRequestPayload(body, &payload)
	if err != nil {
		return serviceTurnRequest{}, err
	}
	warnings := serviceRequestConflictWarnings(fields,
		serviceRequestFieldPair{"session_key", "sessionKey"},
		serviceRequestFieldPair{"intern_session_key", "internSessionKey"},
		serviceRequestFieldPair{"allowed_tools", "allowedTools"},
		serviceRequestFieldPair{"tool_policy", "toolPolicy"},
		serviceRequestFieldPair{"profile_name", "profileName"},
		serviceRequestFieldPair{"approval_token", "approvalToken"},
		serviceRequestFieldPair{"replay_tool_call", "replayToolCall"},
	)
	allowedTools, restrictTools, err := app.ResolveToolPolicy(
		registry,
		firstToolPolicy(payload.ToolPolicy, payload.ToolPolicyCamel),
		compat.FirstStringSlice(payload.AllowedTools, payload.AllowedToolsCamel),
	)
	if err != nil {
		return serviceTurnRequest{}, err
	}
	replayToolCall, err := firstReplayToolCall(payload.ReplayToolCall, payload.ReplayToolCallCamel)
	if err != nil {
		return serviceTurnRequest{}, err
	}
	return serviceTurnRequest{
		SessionKey:     compat.FirstString(payload.SessionKey, payload.InternSessionKey, payload.SessionKeyCamel, payload.InternSessionKeyCamel),
		Message:        strings.TrimSpace(payload.Message),
		AllowedTools:   allowedTools,
		RestrictTools:  restrictTools,
		Meta:           cloneMapOrEmpty(payload.Meta),
		ProfileName:    compat.FirstString(payload.ProfileName, payload.ProfileNameCamel),
		ApprovalToken:  compat.FirstString(payload.ApprovalToken, payload.ApprovalTokenCamel),
		ReplayToolCall: replayToolCall,
		Warnings:       warnings,
	}, nil
}

func decodeServiceSubagentRequest(body io.Reader, registry *tools.Registry) (serviceSubagentRequest, error) {
	var payload serviceSubagentRequestPayload
	fields, err := decodeServiceRequestPayload(body, &payload)
	if err != nil {
		return serviceSubagentRequest{}, err
	}
	warnings := serviceRequestConflictWarnings(fields,
		serviceRequestFieldPair{"parent_session_key", "parentSessionKey"},
		serviceRequestFieldPair{"session_key", "sessionKey"},
		serviceRequestFieldPair{"intern_session_key", "internSessionKey"},
		serviceRequestFieldPair{"prompt_snapshot", "promptSnapshot"},
		serviceRequestFieldPair{"allowed_tools", "allowedTools"},
		serviceRequestFieldPair{"tool_policy", "toolPolicy"},
		serviceRequestFieldPair{"timeout_seconds", "timeoutSeconds"},
		serviceRequestFieldPair{"profile_name", "profileName"},
		serviceRequestFieldPair{"reply_to", "replyTo"},
		serviceRequestFieldPair{"approval_token", "approvalToken"},
	)
	allowedTools, restrictTools, err := app.ResolveToolPolicy(
		registry,
		firstToolPolicy(payload.ToolPolicy, payload.ToolPolicyCamel),
		compat.FirstStringSlice(payload.AllowedTools, payload.AllowedToolsCamel),
	)
	if err != nil {
		return serviceSubagentRequest{}, err
	}
	timeoutSeconds, err := firstPositiveInt(payload.TimeoutSeconds, payload.TimeoutSecondsCamel, payload.Timeout)
	if err != nil {
		return serviceSubagentRequest{}, err
	}
	return serviceSubagentRequest{
		ParentSessionKey: compat.FirstString(
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
		ProfileName:    compat.FirstString(payload.ProfileName, payload.ProfileNameCamel),
		Channel:        strings.TrimSpace(payload.Channel),
		ReplyTo:        compat.FirstString(payload.ReplyTo, payload.ReplyToCamel),
		ApprovalToken:  compat.FirstString(payload.ApprovalToken, payload.ApprovalTokenCamel),
		Warnings:       warnings,
	}, nil
}

func decodeServiceAgentRunRequest(body io.Reader) (serviceAgentRunRequest, error) {
	var payload serviceAgentRunRequestPayload
	fields, err := decodeServiceRequestPayload(body, &payload)
	if err != nil {
		return serviceAgentRunRequest{}, err
	}
	warnings := serviceRequestConflictWarnings(fields,
		serviceRequestFieldPair{"parent_session_key", "parentSessionKey"},
		serviceRequestFieldPair{"runner_id", "runnerId"},
		serviceRequestFieldPair{"timeout_seconds", "timeoutSeconds"},
		serviceRequestFieldPair{"max_turns", "maxTurns"},
	)

	parentSessionKey := compat.FirstString(payload.ParentSessionKey, payload.ParentSessionKeyCamel)
	if parentSessionKey == "" {
		return serviceAgentRunRequest{}, errors.New("parent_session_key is required")
	}

	runnerID := compat.FirstString(payload.RunnerID, payload.RunnerIDCamel)
	if runnerID == "" {
		return serviceAgentRunRequest{}, errors.New("runner_id is required")
	}

	task := strings.TrimSpace(payload.Task)
	if task == "" {
		return serviceAgentRunRequest{}, errors.New("task is required")
	}

	timeoutSeconds := 0
	if ts := serviceFirstJSONNumber(payload.TimeoutSeconds, payload.TimeoutSecondsCamel); strings.TrimSpace(ts.String()) != "" {
		n, err := ts.Int64()
		if err != nil {
			return serviceAgentRunRequest{}, fmt.Errorf("invalid timeout_seconds: %w", err)
		}
		timeoutSeconds = int(n)
	}

	maxTurns := 0
	if mt := serviceFirstJSONNumber(payload.MaxTurns, payload.MaxTurnsCamel); strings.TrimSpace(mt.String()) != "" {
		n, err := mt.Int64()
		if err != nil {
			return serviceAgentRunRequest{}, fmt.Errorf("invalid max_turns: %w", err)
		}
		maxTurns = int(n)
	}

	cwd := strings.TrimSpace(payload.Cwd)
	model := strings.TrimSpace(payload.Model)
	mode := strings.TrimSpace(payload.Mode)
	isolation := strings.TrimSpace(payload.Isolation)

	return serviceAgentRunRequest{
		ParentSessionKey: parentSessionKey,
		RunnerID:         runnerID,
		Task:             task,
		TimeoutSeconds:   timeoutSeconds,
		Cwd:              cwd,
		Model:            model,
		Mode:             mode,
		Isolation:        isolation,
		MaxTurns:         maxTurns,
		Meta:             cloneMapOrEmpty(payload.Meta),
		Warnings:         warnings,
	}, nil
}

func decodeServiceRequestPayload(body io.Reader, out any) (map[string]json.RawMessage, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if err := decodeServiceRequestBody(bytes.NewReader(raw), out); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return fields, nil
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

type serviceRequestFieldPair struct {
	Canonical string
	Alias     string
}

func serviceRequestConflictWarnings(fields map[string]json.RawMessage, pairs ...serviceRequestFieldPair) []string {
	warnings := make([]string, 0)
	for _, pair := range pairs {
		canonical, hasCanonical := fields[pair.Canonical]
		alias, hasAlias := fields[pair.Alias]
		if !hasCanonical || !hasAlias || rawJSONEqual(canonical, alias) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("conflicting request fields %s and %s; %s wins", pair.Canonical, pair.Alias, pair.Canonical))
	}
	return warnings
}

func rawJSONEqual(left, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right))
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right))
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func firstToolPolicy(values ...*serviceToolPolicyPayload) *agent.ServiceToolPolicy {
	for _, value := range values {
		if value == nil {
			continue
		}
		return &agent.ServiceToolPolicy{
			Mode:         strings.TrimSpace(value.Mode),
			AllowedTools: compat.FirstStringSlice(value.AllowedTools, value.AllowedToolsCamel),
			BlockedTools: compat.FirstStringSlice(value.BlockedTools, value.BlockedToolsCamel),
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
		argsJSON := strings.TrimSpace(compat.FirstString(value.ArgumentsJSON, value.ArgumentsJSONCamel))
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

func firstNonEmptyString(values ...string) string {
	return compat.FirstString("", values...)
}

func serviceFirstJSONNumber(values ...json.Number) json.Number {
	for _, value := range values {
		if strings.TrimSpace(value.String()) != "" {
			return value
		}
	}
	return ""
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
