package approval

import (
	"context"

	"or3-intern/internal/db"
)

func (b *Broker) ListApprovalRequests(ctx context.Context, status string, limit int) ([]db.ApprovalRequestRecord, error) {
	return b.ListApprovalRequestsFiltered(ctx, status, "", limit)
}

func (b *Broker) ListApprovalRequestsFiltered(ctx context.Context, status, approvalType string, limit int) ([]db.ApprovalRequestRecord, error) {
	return b.DB.ListApprovalRequestsFiltered(ctx, status, approvalType, limit)
}

func (b *Broker) ListPairingRequests(ctx context.Context, status string, limit int) ([]db.PairingRequestRecord, error) {
	return b.DB.ListPairingRequests(ctx, status, limit)
}

func (b *Broker) ListDevices(ctx context.Context, limit int) ([]db.PairedDeviceRecord, error) {
	return b.DB.ListPairedDevices(ctx, limit)
}
