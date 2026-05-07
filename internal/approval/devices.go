package approval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"or3-intern/internal/db"
)

func (b *Broker) AuthenticateDeviceToken(ctx context.Context, rawToken string, allowedRoles ...string) (db.PairedDeviceRecord, error) {
	device, ok, err := b.DB.FindPairedDeviceByToken(ctx, rawToken)
	if err != nil {
		return db.PairedDeviceRecord{}, err
	}
	if !ok {
		return db.PairedDeviceRecord{}, fmt.Errorf("device token not found")
	}
	if device.Status != StatusActive || device.RevokedAt > 0 {
		return db.PairedDeviceRecord{}, fmt.Errorf("device token revoked")
	}
	if len(allowedRoles) > 0 && !slices.Contains(allowedRoles, device.Role) {
		return db.PairedDeviceRecord{}, fmt.Errorf("device role not allowed")
	}
	_ = b.DB.TouchPairedDevice(ctx, device.DeviceID, b.now().UnixMilli())
	return device, nil
}

func (b *Broker) RevokeDevice(ctx context.Context, deviceID string, actor string) error {
	device, err := b.DB.GetPairedDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	device.Status = StatusRevoked
	device.RevokedAt = b.now().UnixMilli()
	device.LastSeenAt = device.RevokedAt
	if _, err := b.DB.UpsertPairedDevice(ctx, device); err != nil {
		return err
	}
	_ = b.audit(ctx, "device.revoked", map[string]any{"device_id": deviceID, "actor": actor, "host_id": b.hostID(), "outcome": "revoked"})
	return nil
}

func (b *Broker) RotateDeviceToken(ctx context.Context, deviceID, role, displayName string, metadata map[string]any) (db.PairedDeviceRecord, string, error) {
	rawToken, err := randomHex(24)
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	nowMS := b.now().UnixMilli()
	device, err := b.DB.UpsertPairedDevice(ctx, db.PairedDeviceRecord{DeviceID: strings.TrimSpace(deviceID), Role: normalizeRole(role), DisplayName: strings.TrimSpace(displayName), TokenHash: hashBytes(rawToken), Status: StatusActive, CreatedAt: nowMS, LastSeenAt: nowMS, Metadata: cloneMap(metadata)})
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	_ = b.audit(ctx, "device.rotated", map[string]any{"device_id": device.DeviceID, "role": device.Role, "host_id": b.hostID(), "outcome": "rotated"})
	return device, rawToken, nil
}

func (b *Broker) RotatePairedDeviceToken(ctx context.Context, deviceID string) (db.PairedDeviceRecord, string, error) {
	device, err := b.DB.GetPairedDevice(ctx, deviceID)
	if err != nil {
		return db.PairedDeviceRecord{}, "", err
	}
	if device.Status != StatusActive || device.RevokedAt > 0 {
		return db.PairedDeviceRecord{}, "", fmt.Errorf("device token revoked")
	}
	return b.RotateDeviceToken(ctx, device.DeviceID, device.Role, device.DisplayName, cloneMap(device.Metadata))
}

func (b *Broker) IsPairedChannelIdentity(ctx context.Context, channel, identity string) (bool, error) {
	if b == nil || b.DB == nil {
		return false, nil
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	identity = strings.TrimSpace(identity)
	if channel == "" || identity == "" {
		return false, nil
	}

	for _, deviceID := range []string{channel + ":" + identity, identity} {
		rec, err := b.DB.GetPairedDevice(ctx, deviceID)
		if err == nil {
			return rec.Status == StatusActive && rec.RevokedAt == 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}

	if found, err := b.DB.FindActivePairedDeviceByChannelIdentity(ctx, channel, identity); err != nil || found {
		return found, err
	}

	for offset := 0; ; offset += defaultPageSize {
		items, err := b.DB.ListPairedDevicesPage(ctx, defaultPageSize, offset)
		if err != nil {
			return false, err
		}
		for _, item := range items {
			if item.Status != StatusActive || item.RevokedAt > 0 {
				continue
			}
			if pairedMetadataMatches(item.Metadata, channel, identity) {
				return true, nil
			}
		}
		if len(items) < defaultPageSize {
			break
		}
	}
	return false, nil
}

func pairedMetadataMatches(metadata map[string]any, channel, identity string) bool {
	if len(metadata) == 0 {
		return false
	}
	compare := func(key, want string) bool {
		if want == "" {
			return false
		}
		value := metadataString(metadata, key)
		return value != "" && strings.EqualFold(value, want)
	}
	if compare("channel", channel) && (compare("identity", identity) || compare("sender", identity) || compare("user_id", identity) || compare("chat_id", identity) || compare("from", identity)) {
		return true
	}
	if compare(channel+"_identity", identity) || compare(channel+"_user_id", identity) || compare(channel+"_chat_id", identity) || compare(channel+"_from", identity) {
		return true
	}
	return false
}

func metadataString(metadata map[string]any, key string) string {
	raw, ok := metadata[key]
	if !ok {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
