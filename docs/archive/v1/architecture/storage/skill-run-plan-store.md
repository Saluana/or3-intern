# Skill Run Plan Store

The skill run plan store manages the lifecycle of skill execution plans — structured records that track a skill invocation from planning through approval to execution and completion.

Source: `internal/db/skill_run_plan_store.go`

## Status Lifecycle

Skills progress through these normalized statuses (`skill_run_plan_store.go:17-35`):

```
planned → pending_approval → approved → running → succeeded
                                               → failed
                                               → timed_out
                                               → cancelled
```

Pre-run failure paths:
```
planned → preflight_failed
planned → blocked_by_policy
planned → denied
planned → expired
planned → stale_plan
```

Legacy statuses are normalized:
- `prepared` → `planned`
- `awaiting_resume` → `approved`
- `blocked` → `blocked_by_policy`

## Data Model

### SkillRunPlanRecord (`skill_run_plan_store.go:76-104`)

```go
type SkillRunPlanRecord struct {
    ID                 string   // unique plan ID (auto-generated: "srp_" + hex)
    SkillID            string   // skill identifier
    Version            string   // skill version
    Origin             string   // origin of the skill
    TrustState         string   // trust evaluation state
    SkillDir           string   // skill directory path
    RelativePath       string   // relative path within skill dir
    Entrypoint         string   // entrypoint command
    ArgsJSON           string   // JSON array of arguments
    StdinText          string   // stdin content
    StdinNonce         []byte   // random nonce for stdin (cleared after use)
    StdinSHA256        string   // SHA256 of stdin (for integrity)
    TimeoutSeconds     int      // execution timeout
    CommandJSON        string   // JSON array of resolved command
    ScriptHash         string   // hash of the script content
    EnvBindingHash     string   // hash of environment bindings
    PlanHash           string   // composite hash for deduplication
    SubjectHash        string   // hash of the approval subject
    RequesterAgentID   string   // requesting agent
    RequesterSessionID string   // requesting session
    ExecutionHostID    string   // target execution host
    ApprovalRequestID  int64    // FK to approval_requests
    Status             string   // current status
    ResultJSON         string   // execution result
    LastError          string   // last error message
    CreatedAt          int64
    UpdatedAt          int64
}
```

## Deduplication

An active plan is identified by `(requester_session_id, plan_hash)`. The `skill_run_plans` table has a partial unique index:

```sql
CREATE UNIQUE INDEX skill_run_plans_active_session_plan_hash
ON skill_run_plans(requester_session_id, plan_hash)
WHERE status IN ('prepared','planned','pending_approval','awaiting_resume','approved','running')
```

## Operations

| Function | Purpose |
|----------|---------|
| `NormalizeSkillRunStatus()` | Normalizes legacy statuses to current ones |
| `IsTerminalSkillRunStatus()` | Returns true for ended statuses |
| `CreateSkillRunPlan()` | Creates a new plan. Auto-generates ID if empty |
| `GetSkillRunPlan()` | Retrieves by ID |
| `ListSkillRunPlansByApprovalRequest()` | Lists plans linked to an approval request |
| `FindActiveSkillRunPlan()` | Finds an active plan by session + plan hash |
| `GetOrCreateActiveSkillRunPlan()` | Idempotent create: finds existing or creates new. Handles unique constraint races by re-querying |
| `ClaimSkillRunPlan()` | Transitions to running. Only from claimable statuses (planned, pending_approval, approved, + legacy) |
| `UpdateSkillRunPlansByApprovalRequest()` | Bulk updates plans linked to an approval request (e.g., when approved/denied) |
| `UpdateSkillRunPlanApproval()` | Links a plan to an approval request |
| `UpdateSkillRunPlanResult()` | Sets terminal status with result |
| `ClearSkillRunPlanStdin()` | Clears stdin content after execution |
| `TouchSkillRunPlan()` | Updates updated_at for running plans (heartbeat) |

## Active vs Claimable Statuses

Two status groups control transitions (`skill_run_plan_store.go:37-52`):

- **Active statuses** — `planned`, `pending_approval`, `approved`, `running` (+ legacy `prepared`, `awaiting_resume`)
- **Claimable statuses** — Active statuses minus `running`

`FindActiveSkillRunPlan()` and the unique index use active statuses. `ClaimSkillRunPlan()` uses claimable statuses (you can't claim a running plan).

## Key Design Patterns

- **Deduplication by hash** — The `plan_hash` prevents duplicate plan creation for the same skill + session combination.
- **Idempotent create** — `GetOrCreateActiveSkillRunPlan()` handles concurrent creation by catching the unique constraint error and re-querying.
- **Status-gated transitions** — All mutations check the current status via `WHERE status IN (...)` to prevent invalid transitions.
- **Stdin clearing** — After execution, stdin text, nonce, and hash are cleared from the database.
- **Auto-generated IDs** — Plan IDs use `crypto/rand` to generate 10 random bytes, hex-encoded with a `srp_` prefix.
