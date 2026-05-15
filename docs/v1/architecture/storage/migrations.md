# Database Migrations

OR3 Intern uses an additive migration approach: all schema changes are safe to run on an existing database because they use `IF NOT EXISTS` clauses and column existence checks.

Source: `internal/db/db.go`

## Migration Strategy

Migrations run every time the database opens, inside `migrate()` (`db.go:77-613`). The approach is:

1. **Core schema** — All `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS` statements run first. These are idempotent and safe on existing databases.

2. **Additive migrations** — Schema changes that add columns or restructure tables run second. Each checks whether the change is needed before applying it.

## Column Detection

`tableHasColumn()` (`db.go:977-996`) uses SQLite's `PRAGMA table_info()` to check if a column already exists:

```go
func (d *DB) tableHasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
    rows, err := d.SQL.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
    // ... scans for matching column name
}
```

This is used by additive migrations to avoid `ALTER TABLE ADD COLUMN` errors on re-runs.

## Additive Migrations

### migrateMemoryPinned (`db.go:619-648`)

Handles the transition from a `memory_pinned` table without `session_key` to one with it:
1. Checks if `session_key` column exists
2. If not: renames old table, creates new table, copies data (with global scope as default), drops old table

### ensureMemoryNotesSessionColumn (`db.go:650-662`)

Adds `session_key` to `memory_notes` if missing. Backfills existing rows with the global memory scope default.

### migrateLegacyGlobalMemoryScope (`db.go:664-692`)

Migrates data from a legacy global scope alias to the new global scope key. Copies pinned memory and updates memory_notes and memory_vec session keys.

### ensureMemoryNotesMetaColumns (`db.go:911-975`)

Adds lifecycle/ranking metadata columns to `memory_notes`:
- `embed_fingerprint`, `kind`, `status`, `importance`, `summary`, `source_artifact_id`, `confidence`, `updated_at`, `expires_at`, `supersedes_id`, `use_count`, `last_used_at`

After adding columns, it:
- Creates supporting indexes on `(session_key, kind, status, created_at)`, `kind`, `status`, `source_artifact_id`
- Backfills: rows tagged "consolidation" get `kind='summary'`
- Backfills: rows with `updated_at <= 0` get `updated_at = created_at`

### ensureMemoryVecMetaFingerprintColumn (`db.go:861-871`)

Adds `embed_fingerprint` to `memory_vec_meta`.

### ensureMemoryDocsEmbedFingerprintColumn (`db.go:873-883`)

Adds `embed_fingerprint` to `memory_docs`.

### ensureSkillRunPlanColumns (`db.go:885-906`)

Adds `stdin_nonce` and `stdin_sha256` to `skill_run_plans`.

### ensureMemoryVecIndexForExisting (`db.go:694-713`)

On startup, if `memory_vec_meta.dims` is 0, looks for existing embeddings in `memory_notes` to infer the dimension count. If found, initializes the vector index.

## Why This Approach

- **No version tracking table** — There is no `schema_version` or migration tracking table. All migrations are designed to be safe when re-run.
- **Idempotent** — Every migration checks current state before modifying. A database that has already been migrated will not be changed.
- **No destructive changes** — Columns are only added, never removed. Tables are never dropped (except in `migrateMemoryPinned` which renames → copies → drops the legacy table). The `memory_vec` virtual table is dropped and recreated during rebuilds, but that's a runtime operation, not a migration.
- **Safe on every open** — Running migrations on every open is cheap because `IF NOT EXISTS` and column checks short-circuit when the schema is current.
