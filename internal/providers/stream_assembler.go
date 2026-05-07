package providers

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ProviderStreamWarning struct {
	Code    string
	Preview string
}

type NormalizedToolCallDelta struct {
	ToolCall ToolCall
}

type StreamAssemblyEvent struct {
	TextDelta string
	ToolCalls []NormalizedToolCallDelta
	Warning   *ProviderStreamWarning
}

type ProviderAssistantMessage struct {
	Role      string
	Content   string
	ToolCalls []ToolCall
	Warnings  []ProviderStreamWarning
}

type StreamAssembler struct {
	Profile          ProviderProfile
	content          strings.Builder
	previousSnapshot string
	toolCalls        toolCallAccumulator
	sawData          bool
	sawVisibleOutput bool
	warnings         []ProviderStreamWarning
}

type ProviderStreamError struct {
	Code      string
	Message   string
	Retryable bool
	Warnings  []ProviderStreamWarning
}

func (e ProviderStreamError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "provider stream error"
}

func (a *StreamAssembler) ApplyChunk(chunk ChatStreamChunk) []StreamAssemblyEvent {
	a.sawData = true
	events := []StreamAssemblyEvent{}
	if len(chunk.Choices) == 0 {
		return events
	}
	for _, choice := range chunk.Choices {
		delta := choice.Delta
		if delta.Content != "" {
			text := a.textDelta(delta.Content)
			if text != "" {
				a.content.WriteString(text)
				a.sawVisibleOutput = true
				events = append(events, StreamAssemblyEvent{TextDelta: text})
			}
		}
		if len(delta.ToolCalls) > 0 {
			accumulated := a.toolCalls.Apply(delta.ToolCalls)
			callEvents := make([]NormalizedToolCallDelta, 0, len(accumulated))
			for _, call := range accumulated {
				callEvents = append(callEvents, NormalizedToolCallDelta{ToolCall: call})
			}
			events = append(events, StreamAssemblyEvent{ToolCalls: callEvents})
		}
	}
	return events
}

func (a *StreamAssembler) RecordMalformed(raw string) StreamAssemblyEvent {
	a.sawData = true
	warning := ProviderStreamWarning{Code: "malformed_chunk", Preview: trimToRunes(strings.TrimSpace(raw), 240)}
	a.warnings = append(a.warnings, warning)
	return StreamAssemblyEvent{Warning: &warning}
}

func (a *StreamAssembler) Finalize() (ProviderAssistantMessage, error) {
	profile := a.profile()
	if !a.sawData {
		return ProviderAssistantMessage{}, ProviderStreamError{
			Code:      "empty_stream",
			Message:   "provider stream returned no data events",
			Retryable: profile.Retry.RetryEmptyStream,
			Warnings:  a.warnings,
		}
	}
	if len(a.warnings) > 0 && !a.sawVisibleOutput && profile.Retry.RetryMalformedBeforeOutput {
		return ProviderAssistantMessage{}, ProviderStreamError{
			Code:      "malformed_stream_before_output",
			Message:   "provider stream contained malformed chunks before any visible output",
			Retryable: true,
			Warnings:  a.warnings,
		}
	}
	finalToolCalls := a.toolCalls.Finalize()
	for _, call := range finalToolCalls {
		args := strings.TrimSpace(call.Function.Arguments)
		if args == "" {
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(args), &decoded); err != nil {
			warning := ProviderStreamWarning{Code: "incomplete_tool_arguments", Preview: trimToRunes(args, 240)}
			a.warnings = append(a.warnings, warning)
			if !a.sawVisibleOutput {
				return ProviderAssistantMessage{}, ProviderStreamError{
					Code:      "incomplete_tool_arguments",
					Message:   "provider stream ended with incomplete tool-call JSON",
					Retryable: profile.Retry.RetryMalformedBeforeOutput,
					Warnings:  a.warnings,
				}
			}
		}
	}
	return ProviderAssistantMessage{
		Role:      "assistant",
		Content:   a.content.String(),
		ToolCalls: finalToolCalls,
		Warnings:  a.warnings,
	}, nil
}

func (a *StreamAssembler) profile() ProviderProfile {
	if a.Profile.Name == "" {
		return OpenAICompatibleProfile()
	}
	return a.Profile
}

func (a *StreamAssembler) textDelta(in string) string {
	if a.profile().Streaming.TextMode == StreamTextModeDelta {
		return in
	}
	if a.previousSnapshot == "" {
		a.previousSnapshot = in
		return in
	}
	if in == a.previousSnapshot {
		return ""
	}
	if strings.HasPrefix(in, a.previousSnapshot) {
		delta := strings.TrimPrefix(in, a.previousSnapshot)
		a.previousSnapshot = in
		return delta
	}
	current := a.content.String()
	if strings.HasPrefix(in, current) {
		delta := strings.TrimPrefix(in, current)
		a.previousSnapshot = in
		return delta
	}
	if overlap := suffixPrefixOverlap(current, in); overlap > 0 {
		a.previousSnapshot = in
		return in[overlap:]
	}
	a.previousSnapshot = current + in
	return in
}

func suffixPrefixOverlap(left, right string) int {
	max := len(left)
	if len(right) < max {
		max = len(right)
	}
	for n := max; n > 0; n-- {
		if strings.HasSuffix(left, right[:n]) {
			return n
		}
	}
	return 0
}

type toolCallAccumulator struct {
	calls []ToolCall
}

func (a *toolCallAccumulator) Apply(delta []ToolCall) []ToolCall {
	for _, d := range delta {
		idx := d.Index
		if idx < 0 {
			idx = len(a.calls)
		}
		for len(a.calls) <= idx {
			a.calls = append(a.calls, ToolCall{Index: len(a.calls), Type: "function"})
		}
		call := &a.calls[idx]
		call.Index = idx
		if d.ID != "" {
			call.ID = d.ID
		}
		if d.Type != "" {
			call.Type = d.Type
		}
		if d.Function.Name != "" {
			call.Function.Name += d.Function.Name
		}
		if d.Function.Arguments != "" {
			call.Function.Arguments += d.Function.Arguments
		}
	}
	return a.Finalize()
}

func (a *toolCallAccumulator) Finalize() []ToolCall {
	out := make([]ToolCall, 0, len(a.calls))
	for i, call := range a.calls {
		if call.Type == "" {
			call.Type = "function"
		}
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", i+1)
		}
		call.Index = i
		out = append(out, call)
	}
	return out
}
