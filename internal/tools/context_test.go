package tools

import (
	"context"
	"testing"

	"or3-intern/internal/approval"
)

func TestContextWithApprovalRequesterContextForSessionPreservesConversationSession(t *testing.T) {
	ctx := ContextWithSession(context.Background(), "cli:default")
	ctx = ContextWithDelivery(ctx, "telegram", "123")
	ctx = ContextWithDeliveryFrom(ctx, "456")
	ctx = ContextWithApprovalRequesterContextForSession(ctx, "telegram:123")

	requester := approval.RequesterContextFromContext(ctx)
	if requester.Channel != "telegram" || requester.SessionKey != "telegram:123" || requester.From != "456" || requester.ReplyTarget != "123" {
		t.Fatalf("unexpected requester context: %#v", requester)
	}
}
