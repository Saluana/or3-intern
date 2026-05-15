package db

import (
	"context"
	"database/sql"
	"time"
)

type MCPToolCatalogRecord struct {
	ServerName   string
	RemoteName   string
	LocalName    string
	Status       string
	LastError    string
	DiscoveredAt int64
	UpdatedAt    int64
}

func (d *DB) UpsertMCPToolCatalog(ctx context.Context, records []MCPToolCatalogRecord) error {
	if d == nil || d.SQL == nil || len(records) == 0 {
		return nil
	}
	now := time.Now().Unix()
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO mcp_tool_catalog(server_name, remote_name, local_name, status, last_error, discovered_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(server_name, local_name) DO UPDATE SET
	remote_name=excluded.remote_name,
	status=excluded.status,
	last_error=excluded.last_error,
	updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, record := range records {
		discoveredAt := record.DiscoveredAt
		if discoveredAt <= 0 {
			discoveredAt = now
		}
		updatedAt := record.UpdatedAt
		if updatedAt <= 0 {
			updatedAt = now
		}
		if _, err := stmt.ExecContext(ctx, record.ServerName, record.RemoteName, record.LocalName, record.Status, record.LastError, discoveredAt, updatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) ListMCPToolCatalog(ctx context.Context) ([]MCPToolCatalogRecord, error) {
	if d == nil || d.SQL == nil {
		return nil, sql.ErrConnDone
	}
	rows, err := d.SQL.QueryContext(ctx, `SELECT server_name, remote_name, local_name, status, last_error, discovered_at, updated_at
FROM mcp_tool_catalog
ORDER BY server_name, local_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MCPToolCatalogRecord
	for rows.Next() {
		var record MCPToolCatalogRecord
		if err := rows.Scan(&record.ServerName, &record.RemoteName, &record.LocalName, &record.Status, &record.LastError, &record.DiscoveredAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}
