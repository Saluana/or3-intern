package main

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

func approvalRequiredContinuationPrompt(ctx context.Context, broker *approval.Broker, fallbackReq db.ApprovalRequestRecord, approvalErr *tools.ApprovalRequiredError) (db.ApprovalRequestRecord, string) {
	req := fallbackReq
	requestID := int64(0)
	toolName := "tool"
	if approvalErr != nil {
		requestID = approvalErr.RequestID
		if strings.TrimSpace(approvalErr.ToolName) != "" {
			toolName = strings.TrimSpace(approvalErr.ToolName)
		}
	}
	if requestID > 0 && broker != nil && broker.DB != nil {
		if rec, err := broker.DB.GetApprovalRequest(ctx, requestID); err == nil {
			req = rec
		}
	}
	if requestID <= 0 {
		return req, "One more approval is needed before I can continue. Please review it in the OR3 app."
	}
	preview := strings.TrimSpace(approval.SafeSubjectPreview(req.Type, req.SubjectJSON))
	if preview == "" {
		preview = toolName
	}
	return req, strings.Join([]string{
		"One more approval is needed before I can continue.",
		"",
		fmt.Sprintf("Request #%d: %s", requestID, preview),
		"",
		fmt.Sprintf("Reply `/approve %d` to continue or `/deny %d` to stop. You can also review this request in the OR3 app.", requestID, requestID),
	}, "\n")
}
