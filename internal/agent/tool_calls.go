package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

type ToolCallSource string

const (
	ToolCallSourceProvider ToolCallSource = "provider"
	ToolCallSourceMarkup   ToolCallSource = "markup"
)

type NormalizedToolCall struct {
	ID            string
	ProviderID    string
	Index         int
	Name          string
	ArgumentsJSON string
	Source        ToolCallSource
	Raw           map[string]any
}

func normalizeProviderToolCalls(calls []providers.ToolCall, source ToolCallSource, idPrefix string) []NormalizedToolCall {
	out := make([]NormalizedToolCall, 0, len(calls))
	seen := map[string]struct{}{}
	for i, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		args := strings.TrimSpace(call.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		id := strings.TrimSpace(call.ID)
		providerID := id
		if id == "" {
			id = stableToolCallID(idPrefix, i, name, args)
		}
		normalized := NormalizedToolCall{
			ID:            id,
			ProviderID:    providerID,
			Index:         len(out),
			Name:          name,
			ArgumentsJSON: args,
			Source:        source,
			Raw: map[string]any{
				"provider_id": providerID,
				"index":       call.Index,
				"type":        call.Type,
			},
		}
		key := normalized.dedupeKey()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func availableNormalizedToolCalls(calls []NormalizedToolCall, reg *tools.Registry) []NormalizedToolCall {
	if len(calls) == 0 || reg == nil {
		return nil
	}
	out := make([]NormalizedToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "search" && reg.Get("web_search") != nil {
			name = "web_search"
		}
		if name == "" || reg.Get(name) == nil {
			continue
		}
		call.Name = name
		call.Index = len(out)
		out = append(out, call)
	}
	return out
}

func normalizedToProviderToolCalls(calls []NormalizedToolCall) []providers.ToolCall {
	out := make([]providers.ToolCall, 0, len(calls))
	for _, call := range calls {
		tc := providers.ToolCall{ID: call.ID, Index: call.Index, Type: "function"}
		tc.Function.Name = call.Name
		tc.Function.Arguments = call.ArgumentsJSON
		out = append(out, tc)
	}
	return out
}

func unavailableNormalizedToolCallPrompt(calls []NormalizedToolCall, reg *tools.Registry) string {
	names := make([]string, 0, len(calls))
	seen := map[string]struct{}{}
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			name = "<blank>"
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	available := []string{}
	if reg != nil {
		available = reg.Names()
	}
	return fmt.Sprintf(
		"The previous assistant response attempted unavailable tool call(s): %s. Continue by answering directly or by using only currently advertised tool names: %s.",
		strings.Join(names, ", "),
		strings.Join(available, ", "),
	)
}

func (c NormalizedToolCall) dedupeKey() string {
	if c.ProviderID != "" {
		return "provider:" + c.ProviderID
	}
	return fmt.Sprintf("local:%s:%s", c.Name, canonicalJSON(c.ArgumentsJSON))
}

func stableToolCallID(prefix string, index int, name, args string) string {
	if strings.TrimSpace(prefix) == "" {
		prefix = "call"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s:%s", index, name, canonicalJSON(args))))
	return fmt.Sprintf("%s_%d_%08x", prefix, index+1, h.Sum32())
}

func canonicalJSON(raw string) string {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return strings.TrimSpace(raw)
	}
	b, err := json.Marshal(decoded)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return string(b)
}

func emitToolCallStarted(ctx context.Context, observer ConversationObserver, call NormalizedToolCall) {
	if observer == nil {
		return
	}
	if lifecycle, ok := observer.(ToolLifecycleObserver); ok {
		lifecycle.OnToolLifecycle(ctx, ToolLifecycleEvent{
			ToolCallID:       call.ID,
			Name:             call.Name,
			Status:           "running",
			Arguments:        call.ArgumentsJSON,
			ArgumentsPreview: eventPreview(call.ArgumentsJSON, 500),
		})
		return
	}
	observer.OnToolCall(ctx, call.Name, call.ArgumentsJSON)
}

func emitToolCallFinished(ctx context.Context, observer ConversationObserver, call NormalizedToolCall, result string, artifactID string, err error) {
	if observer == nil {
		return
	}
	status := "completed"
	if err != nil {
		status = "failed"
	}
	if lifecycle, ok := observer.(ToolLifecycleObserver); ok {
		event := ToolLifecycleEvent{
			ToolCallID:       call.ID,
			Name:             call.Name,
			Status:           status,
			Arguments:        call.ArgumentsJSON,
			ArgumentsPreview: eventPreview(call.ArgumentsJSON, 500),
			Result:           result,
			ResultPreview:    eventPreview(result, 700),
			ArtifactID:       artifactID,
			PublicCode:       PublicErrorCode(err),
		}
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) {
			event.ApprovalID = approvalErr.RequestID
		}
		lifecycle.OnToolLifecycle(ctx, event)
		return
	}
	observer.OnToolResult(ctx, call.Name, result, err)
}

func eventPreview(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
