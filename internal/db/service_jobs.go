package db

import (
	"context"
	"database/sql"
)

type ServiceJobSummary struct {
	ID         string
	Kind       string
	Status     string
	EventsJSON string
	CreatedAt  int64
	UpdatedAt  int64
}

func (d *DB) UpsertServiceJobSummary(ctx context.Context, summary ServiceJobSummary) error {
	if d == nil || d.SQL == nil {
		return sql.ErrConnDone
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO service_jobs(id, kind, status, events_json, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	kind=excluded.kind,
	status=excluded.status,
	events_json=excluded.events_json,
	updated_at=excluded.updated_at`,
		summary.ID, summary.Kind, summary.Status, summary.EventsJSON, summary.CreatedAt, summary.UpdatedAt)
	return err
}

func (d *DB) GetServiceJobSummary(ctx context.Context, id string) (ServiceJobSummary, error) {
	if d == nil || d.SQL == nil {
		return ServiceJobSummary{}, sql.ErrConnDone
	}
	var summary ServiceJobSummary
	err := d.SQL.QueryRowContext(ctx, `SELECT id, kind, status, events_json, created_at, updated_at
FROM service_jobs WHERE id = ?`, id).Scan(&summary.ID, &summary.Kind, &summary.Status, &summary.EventsJSON, &summary.CreatedAt, &summary.UpdatedAt)
	return summary, err
}
