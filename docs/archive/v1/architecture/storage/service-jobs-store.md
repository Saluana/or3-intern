# Service Jobs Store

A minimal store for tracking background service job summaries. Unlike subagent jobs or agent CLI runs, this table stores high-level status summaries, not job queues.

Source: `internal/db/service_jobs.go`

## Data Model

### ServiceJobSummary (`service_jobs.go:8-15`)

```go
type ServiceJobSummary struct {
    ID         string
    Kind       string
    Status     string
    EventsJSON string
    CreatedAt  int64
    UpdatedAt  int64
}
```

- **ID** — Primary key, unique job identifier
- **Kind** — Job type classification
- **Status** — Current status string
- **EventsJSON** — JSON array of job events
- **CreatedAt / UpdatedAt** — Timestamps in milliseconds

## Operations

### UpsertServiceJobSummary (`service_jobs.go:17-30`)

Inserts or updates a job summary. Uses `ON CONFLICT(id) DO UPDATE` to merge status and events.

### GetServiceJobSummary (`service_jobs.go:32-40`)

Retrieves a job summary by ID. Returns `sql.ErrConnDone` if the DB connection is nil.
