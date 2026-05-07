package agent

import (
	"context"
	"errors"
	"strings"

	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

const (
	PublicErrorProvider      = "provider_error"
	PublicErrorStream        = "stream_error"
	PublicErrorValidation    = "validation_error"
	PublicErrorPolicy        = "policy_error"
	PublicErrorApproval      = "approval_required"
	PublicErrorToolExecution = "tool_execution_error"
	PublicErrorLoopLimit     = "tool_loop_limit"
	PublicErrorAbort         = "aborted"
	PublicErrorUnknown       = "unknown_error"
)

func PublicErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return PublicErrorAbort
	}
	var approvalErr *tools.ApprovalRequiredError
	if errors.As(err, &approvalErr) {
		return PublicErrorApproval
	}
	var streamErr providers.ProviderStreamError
	if errors.As(err, &streamErr) {
		return PublicErrorStream
	}
	var providerErr providers.ProviderError
	if errors.As(err, &providerErr) {
		return PublicErrorProvider
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "validation"):
		return PublicErrorValidation
	case strings.Contains(msg, "denied") || strings.Contains(msg, "policy") || strings.Contains(msg, "capability") || strings.Contains(msg, "not available"):
		return PublicErrorPolicy
	case strings.Contains(msg, "loop"):
		return PublicErrorLoopLimit
	default:
		return PublicErrorUnknown
	}
}
