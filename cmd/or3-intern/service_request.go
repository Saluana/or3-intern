package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"or3-intern/internal/compat"
	"or3-intern/internal/turns"
)

type serviceRunnerRunRequest struct {
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

type serviceRunnerRunRequestPayload struct {
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

func decodeServiceRunnerRunRequest(body io.Reader) (serviceRunnerRunRequest, error) {
	var payload serviceRunnerRunRequestPayload
	fields, err := decodeServiceRequestPayload(body, &payload)
	if err != nil {
		return serviceRunnerRunRequest{}, err
	}
	warnings := serviceRequestConflictWarnings(fields,
		serviceRequestFieldPair{"parent_session_key", "parentSessionKey"},
		serviceRequestFieldPair{"runner_id", "runnerId"},
		serviceRequestFieldPair{"timeout_seconds", "timeoutSeconds"},
		serviceRequestFieldPair{"max_turns", "maxTurns"},
	)

	parentSessionKey := compat.FirstString(payload.ParentSessionKey, payload.ParentSessionKeyCamel)
	if parentSessionKey == "" {
		return serviceRunnerRunRequest{}, errors.New("parent_session_key is required")
	}

	runnerID := compat.FirstString(payload.RunnerID, payload.RunnerIDCamel)
	if runnerID == "" {
		return serviceRunnerRunRequest{}, errors.New("runner_id is required")
	}

	task := strings.TrimSpace(payload.Task)
	if task == "" {
		return serviceRunnerRunRequest{}, errors.New("task is required")
	}

	timeoutSeconds := 0
	if ts := serviceFirstJSONNumber(payload.TimeoutSeconds, payload.TimeoutSecondsCamel); strings.TrimSpace(ts.String()) != "" {
		n, err := ts.Int64()
		if err != nil {
			return serviceRunnerRunRequest{}, fmt.Errorf("invalid timeout_seconds: %w", err)
		}
		timeoutSeconds = int(n)
	}

	maxTurns := 0
	if mt := serviceFirstJSONNumber(payload.MaxTurns, payload.MaxTurnsCamel); strings.TrimSpace(mt.String()) != "" {
		n, err := mt.Int64()
		if err != nil {
			return serviceRunnerRunRequest{}, fmt.Errorf("invalid max_turns: %w", err)
		}
		maxTurns = int(n)
	}

	cwd := strings.TrimSpace(payload.Cwd)
	model := strings.TrimSpace(payload.Model)
	mode := strings.TrimSpace(payload.Mode)
	isolation := strings.TrimSpace(payload.Isolation)

	return serviceRunnerRunRequest{
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

func decodeServiceAttachments(raw []map[string]any) []turns.Attachment {
	if len(raw) == 0 {
		return nil
	}
	items := make([]any, 0, len(raw))
	for _, item := range raw {
		items = append(items, item)
	}
	return turns.DecodeAttachments(items)
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
