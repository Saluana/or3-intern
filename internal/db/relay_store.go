package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type RelayHostRecord struct {
	HostIDHash             string
	AccountID              string
	HostPublicMetadataJSON string
	Status                 string
	LastSeenAt             int64
}

type RelayRouteRecord struct {
	RouteID      string
	AccountID    string
	HostIDHash   string
	DeviceIDHash string
	Status       string
	CreatedAt    int64
	ExpiresAt    int64
	Metadata     map[string]any
}

type RelayRendezvousRecord struct {
	RendezvousID     string
	AccountID        string
	HostIDHash       string
	SecretCommitment string
	Status           string
	CreatedAt        int64
	ExpiresAt        int64
	JoinedAt         int64
	ConsumedAt       int64
	JoinCount        int64
	Metadata         map[string]any
}

func (d *DB) UpsertRelayHost(ctx context.Context, rec RelayHostRecord) error {
	if strings.TrimSpace(rec.HostIDHash) == "" {
		return fmt.Errorf("host hash required")
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO relay_hosts(host_id_hash, account_id, host_public_metadata_json, status, last_seen_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(host_id_hash) DO UPDATE SET account_id=excluded.account_id, host_public_metadata_json=excluded.host_public_metadata_json, status=excluded.status, last_seen_at=excluded.last_seen_at`,
		rec.HostIDHash, rec.AccountID, rec.HostPublicMetadataJSON, rec.Status, rec.LastSeenAt)
	return err
}

func (d *DB) CreateRelayRoute(ctx context.Context, rec RelayRouteRecord) error {
	if strings.TrimSpace(rec.RouteID) == "" || strings.TrimSpace(rec.HostIDHash) == "" {
		return fmt.Errorf("route ID and host hash required")
	}
	if err := validateRelayMetadata(rec.Metadata); err != nil {
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO relay_routes(route_id, account_id, host_id_hash, device_id_hash, status, created_at, expires_at, metadata_json)
		VALUES(?,?,?,?,?,?,?,?)`, rec.RouteID, rec.AccountID, rec.HostIDHash, rec.DeviceIDHash, rec.Status, rec.CreatedAt, rec.ExpiresAt, mustJSONMap(rec.Metadata))
	return err
}

func (d *DB) GetRelayRoute(ctx context.Context, routeID string) (RelayRouteRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT route_id, account_id, host_id_hash, device_id_hash, status, created_at, expires_at, metadata_json FROM relay_routes WHERE route_id=?`, strings.TrimSpace(routeID))
	rec, err := scanRelayRoute(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RelayRouteRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) CreateRelayRendezvous(ctx context.Context, rec RelayRendezvousRecord) error {
	if strings.TrimSpace(rec.RendezvousID) == "" || strings.TrimSpace(rec.SecretCommitment) == "" {
		return fmt.Errorf("rendezvous ID and secret commitment required")
	}
	if err := validateRelayMetadata(rec.Metadata); err != nil {
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO relay_rendezvous(rendezvous_id, account_id, host_id_hash, secret_commitment, status, created_at, expires_at, joined_at, consumed_at, join_count, metadata_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, rec.RendezvousID, rec.AccountID, rec.HostIDHash, rec.SecretCommitment, rec.Status, rec.CreatedAt, rec.ExpiresAt, rec.JoinedAt, rec.ConsumedAt, rec.JoinCount, mustJSONMap(rec.Metadata))
	return err
}

func (d *DB) GetRelayRendezvous(ctx context.Context, rendezvousID string) (RelayRendezvousRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT rendezvous_id, account_id, host_id_hash, secret_commitment, status, created_at, expires_at, joined_at, consumed_at, join_count, metadata_json FROM relay_rendezvous WHERE rendezvous_id=?`, strings.TrimSpace(rendezvousID))
	rec, err := scanRelayRendezvous(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RelayRendezvousRecord{}, false, nil
	}
	return rec, err == nil, err
}

func (d *DB) JoinRelayRendezvous(ctx context.Context, rendezvousID string, nowMS int64, maxJoins int64) (RelayRendezvousRecord, error) {
	if maxJoins <= 0 {
		maxJoins = 3
	}
	res, err := d.SQL.ExecContext(ctx, `UPDATE relay_rendezvous SET status='joined', joined_at=?, join_count=join_count+1 WHERE rendezvous_id=? AND status IN ('created','joined') AND expires_at>? AND join_count<?`, nowMS, strings.TrimSpace(rendezvousID), nowMS, maxJoins)
	if err != nil {
		return RelayRendezvousRecord{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return RelayRendezvousRecord{}, err
	}
	if rows != 1 {
		return RelayRendezvousRecord{}, fmt.Errorf("rendezvous not joinable")
	}
	rec, ok, err := d.GetRelayRendezvous(ctx, rendezvousID)
	if err != nil {
		return RelayRendezvousRecord{}, err
	}
	if !ok {
		return RelayRendezvousRecord{}, fmt.Errorf("rendezvous not found")
	}
	return rec, nil
}

func (d *DB) ConsumeRelayRendezvous(ctx context.Context, rendezvousID string, nowMS int64) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE relay_rendezvous SET status='consumed', consumed_at=? WHERE rendezvous_id=? AND status IN ('created','joined') AND expires_at>?`, nowMS, strings.TrimSpace(rendezvousID), nowMS)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (d *DB) ExpireRelayRendezvous(ctx context.Context, nowMS int64) (int64, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE relay_rendezvous SET status='expired', consumed_at=? WHERE status IN ('created','joined') AND expires_at<=?`, nowMS, nowMS)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) RejectRelayRendezvous(ctx context.Context, rendezvousID string, nowMS int64) (bool, error) {
	res, err := d.SQL.ExecContext(ctx, `UPDATE relay_rendezvous SET status='rejected', consumed_at=? WHERE rendezvous_id=? AND status IN ('created','joined') AND expires_at>?`, nowMS, strings.TrimSpace(rendezvousID), nowMS)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func scanRelayRoute(row scanner) (RelayRouteRecord, error) {
	var rec RelayRouteRecord
	var metadataJSON string
	if err := row.Scan(&rec.RouteID, &rec.AccountID, &rec.HostIDHash, &rec.DeviceIDHash, &rec.Status, &rec.CreatedAt, &rec.ExpiresAt, &metadataJSON); err != nil {
		return RelayRouteRecord{}, err
	}
	rec.Metadata = decodeJSONMap(metadataJSON)
	return rec, nil
}

func scanRelayRendezvous(row scanner) (RelayRendezvousRecord, error) {
	var rec RelayRendezvousRecord
	var metadataJSON string
	if err := row.Scan(&rec.RendezvousID, &rec.AccountID, &rec.HostIDHash, &rec.SecretCommitment, &rec.Status, &rec.CreatedAt, &rec.ExpiresAt, &rec.JoinedAt, &rec.ConsumedAt, &rec.JoinCount, &metadataJSON); err != nil {
		return RelayRendezvousRecord{}, err
	}
	rec.Metadata = decodeJSONMap(metadataJSON)
	return rec, nil
}

func containsLikelySecret(metadata map[string]any) bool {
	for key, value := range metadata {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "plaintext") || strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "command") || strings.Contains(lowerKey, "terminal") {
			if strings.TrimSpace(fmt.Sprint(value)) != "" {
				return true
			}
		}
		switch typed := value.(type) {
		case map[string]any:
			if containsLikelySecret(typed) {
				return true
			}
		case []any:
			for _, item := range typed {
				if nested, ok := item.(map[string]any); ok && containsLikelySecret(nested) {
					return true
				}
			}
		}
	}
	return false
}

func validateRelayMetadata(metadata map[string]any) error {
	if containsLikelySecret(metadata) {
		return fmt.Errorf("relay metadata rejected possible plaintext secret")
	}
	for key, value := range metadata {
		key = strings.ToLower(strings.TrimSpace(key))
		switch key {
		case "kind", "protocol", "relay_origin":
		default:
			return fmt.Errorf("relay metadata rejected unsupported field %q", key)
		}
		switch value.(type) {
		case nil, string, bool, float64, int, int64:
		default:
			return fmt.Errorf("relay metadata rejected non-scalar field %q", key)
		}
	}
	return nil
}
