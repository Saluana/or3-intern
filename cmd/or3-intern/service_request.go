package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

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
}

type serviceRunnerRunRequestPayload struct {
	ParentSessionKey string         `json:"parent_session_key"`
	RunnerID         string         `json:"runner_id"`
	Task             string         `json:"task"`
	TimeoutSeconds   json.Number    `json:"timeout_seconds"`
	Cwd              string         `json:"cwd"`
	Model            string         `json:"model"`
	Mode             string         `json:"mode"`
	Isolation        string         `json:"isolation"`
	MaxTurns         json.Number    `json:"max_turns"`
	Meta             map[string]any `json:"meta"`
}

func decodeServiceRunnerRunRequest(body io.Reader) (serviceRunnerRunRequest, error) {
	var payload serviceRunnerRunRequestPayload
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceRunnerRunRequest{}, err
	}
	parentSessionKey := strings.TrimSpace(payload.ParentSessionKey)
	if parentSessionKey == "" {
		return serviceRunnerRunRequest{}, errors.New("parent_session_key is required")
	}

	runnerID := strings.TrimSpace(payload.RunnerID)
	if runnerID == "" {
		return serviceRunnerRunRequest{}, errors.New("runner_id is required")
	}

	task := strings.TrimSpace(payload.Task)
	if task == "" {
		return serviceRunnerRunRequest{}, errors.New("task is required")
	}

	timeoutSeconds := 0
	if strings.TrimSpace(payload.TimeoutSeconds.String()) != "" {
		ts := payload.TimeoutSeconds
		n, err := ts.Int64()
		if err != nil {
			return serviceRunnerRunRequest{}, fmt.Errorf("invalid timeout_seconds: %w", err)
		}
		timeoutSeconds = int(n)
	}

	maxTurns := 0
	if strings.TrimSpace(payload.MaxTurns.String()) != "" {
		mt := payload.MaxTurns
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func serviceFirstJSONNumber(values ...json.Number) json.Number {
	for _, value := range values {
		if strings.TrimSpace(value.String()) != "" {
			return value
		}
	}
	return ""
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
