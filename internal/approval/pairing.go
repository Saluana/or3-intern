package approval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func (b *Broker) CreatePairingRequest(ctx context.Context, input PairingRequestInput) (db.PairingRequestRecord, string, error) {
	role := normalizeRole(input.Role)
	if role == "" {
		return db.PairingRequestRecord{}, "", fmt.Errorf("invalid pairing role")
	}
	code, err := randomDigits(6)
	if err != nil {
		return db.PairingRequestRecord{}, "", err
	}
	deviceID := strings.TrimSpace(input.DeviceID)
	if deviceID == "" {
		deviceID, err = randomHex(12)
		if err != nil {
			return db.PairingRequestRecord{}, "", err
		}
	}
	nowMS := b.now().UnixMilli()
	status := StatusPending
	approverID := ""
	approvedAt := int64(0)
	switch b.pairingMode() {
	case config.ApprovalModeDeny:
		_ = b.audit(ctx, "pairing.blocked", map[string]any{"device_id": deviceID, "role": role, "host_id": b.hostID(), "outcome": "blocked", "reason": "deny"})
		return db.PairingRequestRecord{}, "", fmt.Errorf("pairing denied by policy")
	case config.ApprovalModeTrusted:
		if auditAuthKindFromContext(ctx) == "unauthenticated" {
			status = StatusPending
		} else {
			status = StatusApproved
			approverID = "policy:trusted"
			approvedAt = nowMS
		}
	case config.ApprovalModeAllowlist:
		allowed, allowErr := b.pairingAllowlistMatches(ctx, deviceID, role)
		if allowErr != nil {
			return db.PairingRequestRecord{}, "", allowErr
		}
		if !allowed {
			_ = b.audit(ctx, "pairing.blocked", map[string]any{"device_id": deviceID, "role": role, "host_id": b.hostID(), "outcome": "blocked", "reason": "allowlist"})
			return db.PairingRequestRecord{}, "", fmt.Errorf("pairing denied by policy")
		}
		if auditAuthKindFromContext(ctx) == "unauthenticated" {
			status = StatusPending
		} else {
			status = StatusApproved
			approverID = "policy:allowlist"
			approvedAt = nowMS
		}
	}
	req, err := b.DB.CreatePairingRequest(ctx, db.PairingRequestRecord{DeviceID: deviceID, Role: role, DisplayName: strings.TrimSpace(input.DisplayName), Origin: strings.TrimSpace(input.Origin), PairingCodeHash: b.hashPairingCode(code), RequestedAt: nowMS, ExpiresAt: nowMS + int64(b.Config.PairingCodeTTLSeconds*1000), Status: status, ApproverID: approverID, ApprovedAt: approvedAt, Metadata: cloneMap(input.Metadata)})
	if err != nil {
		return db.PairingRequestRecord{}, "", err
	}
	_ = b.audit(ctx, "pairing.requested", map[string]any{"pairing_request_id": req.ID, "device_id": req.DeviceID, "role": req.Role, "host_id": b.hostID(), "outcome": req.Status})
	return req, code, nil
}

func (b *Broker) resolvePairingRequest(ctx context.Context, id int64, actor string, targetStatus string) (db.PairingRequestRecord, int64, error) {
	req, err := b.DB.GetPairingRequest(ctx, id)
	if err != nil {
		return db.PairingRequestRecord{}, 0, err
	}
	if req.Status != StatusPending {
		return db.PairingRequestRecord{}, 0, fmt.Errorf("pairing request is not pending")
	}
	nowMS := b.now().UnixMilli()
	approvedAt := int64(0)
	deniedAt := int64(0)
	if targetStatus == StatusApproved {
		if req.ExpiresAt > 0 && req.ExpiresAt < nowMS {
			_, _ = b.DB.ResolvePairingRequestStatus(ctx, id, StatusPending, StatusExpired, "system", 0, 0, req.Metadata)
			return db.PairingRequestRecord{}, 0, fmt.Errorf("pairing request expired")
		}
		approvedAt = nowMS
	}
	if targetStatus == StatusDenied {
		deniedAt = nowMS
	}
	resolved, err := b.DB.ResolvePairingRequestStatus(ctx, id, StatusPending, targetStatus, strings.TrimSpace(actor), approvedAt, deniedAt, req.Metadata)
	if err != nil {
		return db.PairingRequestRecord{}, 0, err
	}
	if !resolved {
		return db.PairingRequestRecord{}, 0, fmt.Errorf("pairing request is not pending")
	}
	return req, nowMS, nil
}

func (b *Broker) ApprovePairingRequest(ctx context.Context, id int64, actor string) (db.PairingRequestRecord, error) {
	req, _, err := b.resolvePairingRequest(ctx, id, actor, StatusApproved)
	if err != nil {
		return db.PairingRequestRecord{}, err
	}
	updated, err := b.DB.GetPairingRequest(ctx, id)
	if err != nil {
		return db.PairingRequestRecord{}, err
	}
	_ = b.audit(ctx, "pairing.resolved", map[string]any{"pairing_request_id": id, "device_id": req.DeviceID, "outcome": "approved", "actor": actor, "host_id": b.hostID()})
	return updated, nil
}

func (b *Broker) ApprovePairingRequestByCode(ctx context.Context, code string, actor string) (db.PairingRequestRecord, error) {
	code = strings.TrimSpace(strings.ReplaceAll(code, "-", ""))
	if code == "" {
		return db.PairingRequestRecord{}, fmt.Errorf("pairing code required")
	}
	nowMS := b.now().UnixMilli()
	active, err := b.DB.FindPairingRequestsByCodeHash(ctx, b.hashPairingCode(code), StatusPending, nowMS, 2)
	if err != nil {
		return db.PairingRequestRecord{}, err
	}
	if len(active) == 0 {
		return db.PairingRequestRecord{}, fmt.Errorf("could not find a waiting device with that code. In the app, tap Get pairing code again and use the fresh 6-digit code")
	}
	if len(active) > 1 {
		return db.PairingRequestRecord{}, fmt.Errorf("more than one waiting device has that code. Run `or3-intern pairing list pending` and approve by request ID")
	}
	return b.ApprovePairingRequest(ctx, active[0].ID, actor)
}

func (b *Broker) DenyPairingRequest(ctx context.Context, id int64, actor string) error {
	req, _, err := b.resolvePairingRequest(ctx, id, actor, StatusDenied)
	if err != nil {
		return err
	}
	_ = b.audit(ctx, "pairing.resolved", map[string]any{"pairing_request_id": id, "device_id": req.DeviceID, "outcome": "denied", "actor": actor, "host_id": b.hostID()})
	return nil
}

func (b *Broker) ExchangePairingCode(ctx context.Context, input PairingExchangeInput) (db.PairedDeviceRecord, string, error) {
	code := strings.TrimSpace(strings.ReplaceAll(input.Code, "-", ""))
	req, ok, err := b.DB.FindPairingRequestByCodeHash(ctx, input.RequestID, b.hashPairingCode(code))
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	if !ok {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request not found")
	}
	nowMS := b.now().UnixMilli()
	if req.ExpiresAt > 0 && req.ExpiresAt < nowMS {
		_, _ = b.DB.ResolvePairingRequestStatus(ctx, req.ID, StatusPending, StatusExpired, "system", 0, 0, req.Metadata)
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request expired")
	}
	if req.Status != StatusApproved {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request is not approved")
	}
	claimed, err := b.DB.CompareAndSwapPairingRequestStatus(ctx, req.ID, StatusApproved, StatusExchanged)
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	if !claimed {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("pairing request is not approved")
	}
	device, token, err := b.RotateDeviceToken(ctx, req.DeviceID, req.Role, req.DisplayName, cloneMap(req.Metadata))
	if err != nil {
		_, _ = b.DB.CompareAndSwapPairingRequestStatus(ctx, req.ID, StatusExchanged, StatusApproved)
		return db.PairedDeviceRecord{}, "", err
	}
	_ = b.audit(ctx, "pairing.exchanged", map[string]any{"pairing_request_id": req.ID, "device_id": device.DeviceID, "role": device.Role, "host_id": b.hostID(), "outcome": "paired"})
	return device, token, nil
}

func (b *Broker) pairingMode() config.ApprovalMode {
	if b == nil {
		return config.ApprovalModeDeny
	}
	return b.Config.Pairing.Mode
}

func (b *Broker) pairingAllowlistMatches(ctx context.Context, deviceID, role string) (bool, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || b == nil || b.DB == nil {
		return false, nil
	}
	device, err := b.DB.GetPairedDevice(ctx, deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return device.Status == StatusActive && device.RevokedAt == 0 && device.Role == role, nil
}
