package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestApprovalModeratorMetadataMigrationAndCRUD(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "approval-moderator.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	ctx := context.Background()
	rec, err := d.CreateApprovalRequest(ctx, ApprovalRequestRecord{
		Type: "exec", SubjectHash: "hash-1", SubjectJSON: `{"type":"exec"}`,
		ExecutionHostID: "local", Status: "pending", PolicyMode: "ask",
		RequestedAt: 1, ExpiresAt: 2,
	})
	if err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}
	meta := ApprovalModeratorMetadata{
		Status: "reviewed", Risk: "low", Action: "approve",
		Reason: "bounded test command", Model: "moderator:test",
		PolicyHash: "policy-abc", ReviewedAt: 10, LatencyMS: 25,
	}
	if err := d.UpdateApprovalRequestModeratorMetadata(ctx, rec.ID, meta); err != nil {
		t.Fatalf("UpdateApprovalRequestModeratorMetadata: %v", err)
	}
	got, err := d.GetApprovalRequest(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if got.ModeratorRisk != "low" || got.ModeratorAction != "approve" || got.ModeratorModel != "moderator:test" {
		t.Fatalf("unexpected moderator metadata: %#v", got)
	}
}
