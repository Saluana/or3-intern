package approval

import (
	"context"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func (b *Broker) createApprovalRequest(ctx context.Context, subjectType SubjectType, subject any, sh SubjectHash, scope AllowlistScope, mode config.ApprovalMode) (db.ApprovalRequestRecord, bool, error) {
	nowMS := b.now().UnixMilli()
	return b.DB.CreateOrGetPendingApprovalRequest(ctx, db.ApprovalRequestRecord{
		Type: string(subjectType), SubjectHash: sh.Hash, SubjectJSON: sh.JSON,
		RequesterAgentID: scope.Agent, RequesterSessionID: extractSessionID(subject),
		RequesterContextJSON: MarshalRequesterContext(RequesterContextFromContext(ctx)),
		ExecutionHostID:      b.hostID(), Status: StatusPending, PolicyMode: string(mode),
		RequestedAt: nowMS, ExpiresAt: nowMS + int64(b.Config.PendingTTLSeconds*1000),
	}, nowMS)
}
