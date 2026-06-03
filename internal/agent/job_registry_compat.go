package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"or3-intern/internal/jobs"
	"or3-intern/internal/tools"
)

type JobEvent = jobs.Event
type JobSnapshot = jobs.Snapshot
type JobRegistry = jobs.Registry

func NewJobRegistry(retention time.Duration, maxTracked int) *JobRegistry {
	return jobs.NewRegistry(retention, maxTracked)
}

type JobObserver struct {
	registry *jobs.Registry
	jobID    string
}

func JobObserverForRegistry(registry *jobs.Registry, jobID string) ConversationObserver {
	return JobObserver{registry: registry, jobID: jobID}
}

func (o JobObserver) OnTextDelta(_ context.Context, text string) {
	if o.registry == nil || text == "" {
		return
	}
	o.registry.Publish(o.jobID, "text_delta", map[string]any{"content": text})
}

func (o JobObserver) OnToolCall(_ context.Context, name string, arguments string) {
	if o.registry == nil {
		return
	}
	o.registry.Publish(o.jobID, "tool_call", map[string]any{"name": name, "arguments": arguments, "status": "running", "arguments_preview": boundedEventPreview(arguments, 500)})
}

func (o JobObserver) OnToolResult(_ context.Context, name string, result string, err error) {
	if o.registry == nil {
		return
	}
	data := map[string]any{"name": name, "result": result}
	if err != nil {
		data["error"] = err.Error()
		data["public_code"] = PublicErrorCode(err)
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) {
			data["code"] = "approval_required"
			data["request_id"] = approvalErr.RequestID
			data["approval_id"] = approvalErr.RequestID
		}
	}
	data["status"] = "completed"
	if err != nil {
		data["status"] = "failed"
	}
	data["result_preview"] = boundedEventPreview(result, 700)
	o.registry.Publish(o.jobID, "tool_result", data)
}

func (o JobObserver) OnToolLifecycle(_ context.Context, event ToolLifecycleEvent) {
	if o.registry == nil {
		return
	}
	eventType := "tool_call"
	if event.Status == "completed" || event.Status == "failed" || event.Result != "" || event.ResultPreview != "" {
		eventType = "tool_result"
	}
	data := map[string]any{
		"name":              event.Name,
		"status":            event.Status,
		"tool_call_id":      event.ToolCallID,
		"arguments":         event.Arguments,
		"arguments_preview": firstNonEmpty(event.ArgumentsPreview, boundedEventPreview(event.Arguments, 500)),
	}
	if event.Result != "" || event.ResultPreview != "" {
		data["result"] = event.Result
		data["result_preview"] = firstNonEmpty(event.ResultPreview, boundedEventPreview(event.Result, 700))
	}
	if event.ArtifactID != "" {
		data["artifact_id"] = event.ArtifactID
	}
	if event.ApprovalID > 0 {
		data["approval_id"] = event.ApprovalID
		data["request_id"] = event.ApprovalID
		data["code"] = PublicErrorApproval
	}
	if event.PublicCode != "" {
		data["public_code"] = event.PublicCode
	}
	if event.Status == "failed" {
		data["error"] = firstNonEmptyString(event.ResultPreview, event.Result, event.PublicCode, "tool failed")
	}
	o.registry.Publish(o.jobID, eventType, data)
}

func (o JobObserver) OnCompletion(_ context.Context, finalText string, streamed bool) {
	if o.registry == nil {
		return
	}
	o.registry.Publish(o.jobID, "assistant", map[string]any{"content": finalText, "streamed": streamed})
}

func (o JobObserver) OnError(_ context.Context, err error) {
	if o.registry == nil || err == nil {
		return
	}
	o.registry.Publish(o.jobID, "runtime_error", map[string]any{"message": err.Error(), "public_code": PublicErrorCode(err)})
}

func boundedEventPreview(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
