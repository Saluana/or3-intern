package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type SecretRecord struct {
	Name       string
	Ciphertext []byte
	Nonce      []byte
	Version    int
	KeyVersion string
	UpdatedAt  int64
}

type AuditEvent struct {
	ID          int64
	EventType   string
	SessionKey  string
	Actor       string
	PayloadJSON string
	PrevHash    []byte
	RecordHash  []byte
	CreatedAt   int64
}

type AuditEventInput struct {
	EventType  string
	SessionKey string
	Actor      string
	Payload    any
}

func (d *DB) PutSecret(ctx context.Context, name string, ciphertext, nonce []byte, version int, keyVersion string) error {
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO secrets(name, ciphertext, nonce, version, key_version, updated_at) VALUES(?,?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext, nonce=excluded.nonce, version=excluded.version, key_version=excluded.key_version, updated_at=excluded.updated_at`,
		strings.TrimSpace(name), ciphertext, nonce, version, strings.TrimSpace(keyVersion), NowMS())
	return err
}

func (d *DB) GetSecret(ctx context.Context, name string) (SecretRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT name, ciphertext, nonce, version, key_version, updated_at FROM secrets WHERE name=?`,
		strings.TrimSpace(name))
	var record SecretRecord
	if err := row.Scan(&record.Name, &record.Ciphertext, &record.Nonce, &record.Version, &record.KeyVersion, &record.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return SecretRecord{}, false, nil
		}
		return SecretRecord{}, false, err
	}
	return record, true, nil
}

func (d *DB) DeleteSecret(ctx context.Context, name string) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM secrets WHERE name=?`, strings.TrimSpace(name))
	return err
}

func (d *DB) ListSecretNames(ctx context.Context) ([]string, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT name FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (d *DB) AppendAuditEvent(ctx context.Context, input AuditEventInput, key []byte) error {
	payloadBytes, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}
	payload := truncateAuditPayload(string(payloadBytes))
	prevHash := []byte{}
	row := d.SQL.QueryRowContext(ctx, `SELECT record_hash FROM audit_events ORDER BY id DESC LIMIT 1`)
	var previous []byte
	if err := row.Scan(&previous); err == nil {
		prevHash = append([]byte{}, previous...)
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}
	createdAt := NowMS()
	recordHash := computeAuditHash(key, input.EventType, input.SessionKey, input.Actor, payload, prevHash, createdAt)
	_, err = d.SQL.ExecContext(ctx,
		`INSERT INTO audit_events(event_type, session_key, actor, payload_json, prev_hash, record_hash, created_at) VALUES(?,?,?,?,?,?,?)`,
		strings.TrimSpace(input.EventType), strings.TrimSpace(input.SessionKey), strings.TrimSpace(input.Actor), payload, prevHash, recordHash, createdAt)
	return err
}

func (d *DB) VerifyAuditChain(ctx context.Context, key []byte) error {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, event_type, session_key, actor, payload_json, prev_hash, record_hash, created_at FROM audit_events ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var prev []byte
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.EventType, &event.SessionKey, &event.Actor, &event.PayloadJSON, &event.PrevHash, &event.RecordHash, &event.CreatedAt); err != nil {
			return err
		}
		if !hmac.Equal(event.PrevHash, prev) {
			return fmt.Errorf("audit chain mismatch at id=%d", event.ID)
		}
		expected := computeAuditHash(key, event.EventType, event.SessionKey, event.Actor, event.PayloadJSON, event.PrevHash, event.CreatedAt)
		if !hmac.Equal(expected, event.RecordHash) {
			return fmt.Errorf("audit hash mismatch at id=%d", event.ID)
		}
		prev = append([]byte{}, event.RecordHash...)
	}
	return rows.Err()
}

func computeAuditHash(key []byte, eventType, sessionKey, actor, payload string, prevHash []byte, createdAt int64) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(strings.TrimSpace(eventType)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(sessionKey)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(actor)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(payload))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(prevHash)
	_, _ = mac.Write([]byte(fmt.Sprint(createdAt)))
	return mac.Sum(nil)
}

func truncateAuditPayload(payload string) string {
	payload = strings.TrimSpace(payload)
	if len(payload) <= 2048 {
		return payload
	}
	return payload[:2048] + "...[truncated]"
}
