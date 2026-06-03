package serviceerrors

import (
	"context"
	"errors"
	"testing"

	"or3-intern/internal/tools"
)

func TestPublicErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "abort", err: context.Canceled, want: PublicErrorAbort},
		{name: "approval", err: &tools.ApprovalRequiredError{ToolName: "exec", RequestID: 7}, want: PublicErrorApproval},
		{name: "validation", err: errors.New("validation failed"), want: PublicErrorValidation},
		{name: "policy", err: errors.New("policy denied"), want: PublicErrorPolicy},
		{name: "unknown", err: errors.New("boom"), want: PublicErrorUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PublicErrorCode(tt.err); got != tt.want {
				t.Fatalf("PublicErrorCode() = %q, want %q", got, tt.want)
			}
		})
	}
}
