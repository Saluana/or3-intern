package approval

import (
	"context"
	"strings"

	"or3-intern/internal/db"
)

func (b *Broker) ListApprovalRequests(ctx context.Context, status string, limit int) ([]db.ApprovalRequestRecord, error) {
	return b.ListApprovalRequestsFiltered(ctx, status, "", limit)
}

func (b *Broker) ListApprovalRequestsFiltered(ctx context.Context, status, approvalType string, limit int) ([]db.ApprovalRequestRecord, error) {
	return b.DB.ListApprovalRequestsFiltered(ctx, status, approvalType, limit)
}

func (b *Broker) CountApprovalRequests(ctx context.Context, status, approvalType string) (int64, error) {
	return b.DB.CountApprovalRequests(ctx, status, approvalType)
}

func (b *Broker) ListPairingRequests(ctx context.Context, status string, limit int) ([]db.PairingRequestRecord, error) {
	if _, err := b.ExpirePairingRequests(ctx, "system"); err != nil {
		return nil, err
	}
	records, err := b.DB.ListPairingRequests(ctx, status, limit, b.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (b *Broker) ExpirePairingRequests(ctx context.Context, actor string) (int64, error) {
	if b == nil || b.DB == nil {
		return 0, nil
	}
	nowMS := b.now().UnixMilli()
	ids, count, err := b.DB.ExpirePairingRequestsReturning(ctx, nowMS, strings.TrimSpace(actor))
	if err != nil {
		return 0, err
	}
	_ = ids
	if count > 0 {
		_ = b.audit(ctx, "pairing.expired", map[string]any{"count": count, "actor": actor, "host_id": b.hostID()})
	}
	return count, nil
}

func (b *Broker) ListDevices(ctx context.Context, limit int) ([]db.PairedDeviceRecord, error) {
	return b.DB.ListPairedDevices(ctx, limit)
}
