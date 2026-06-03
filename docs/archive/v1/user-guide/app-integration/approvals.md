# Approvals

Approval requests are managed under `/internal/v1/approvals/*`.

## Main routes

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/approvals` | List approval requests |
| `GET /internal/v1/approvals/{id}` | Read one approval request |
| `POST /internal/v1/approvals/{id}/approve` | Approve a request |
| `POST /internal/v1/approvals/{id}/deny` | Deny a request |
| `POST /internal/v1/approvals/{id}/cancel` | Cancel a request without executing it |
| `POST /internal/v1/approvals/expire` | Expire stale pending requests |
| `GET /internal/v1/approvals/allowlists` | List allowlist rules |
| `POST /internal/v1/approvals/allowlists` | Add an allowlist rule |
| `POST /internal/v1/approvals/allowlists/{id}/remove` | Remove an allowlist rule |

## Approval responses matter

Approving a request can return more than a simple OK. Current approval responses may include:

- `token`
- `allowlist_id`
- `session_key`
- `plan_id` or `plan_ids`
- `resume_job_id`
- `warnings`

That means approval UI should be prepared to both update the visible approval state and resume waiting work.

## When approvals appear

Approvals can surface during:

- normal turns
- background jobs
- terminal flows
- runner chat or agent-runner actions

Clients should treat `approval_required` style failures or streamed approval events as a first-class part of the control-plane contract.
