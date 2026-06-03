# Approvals Endpoints

Endpoints for the approval workflow. Apps use these to let users review and respond to approval requests.

## List Approvals

`GET /internal/v1/approvals`

Query parameters can filter by `status`, `type`, and `limit`.

```json
{
  "items": [
    {
      "id": 123,
      "status": "pending",
      "type": "exec",
      "subject_json": "{}"
    }
  ]
}
```

## Read One Approval

`GET /internal/v1/approvals/{id}`

Returns one approval request as `item`.

## Approve

`POST /internal/v1/approvals/{id}/approve`

```json
{
  "allowlist": true,
  "note": "Allow this exact command for this workspace"
}
```

Approval responses can include:

- `token`
- `allowlist_id`
- `session_key`
- `plan_id` / `plan_ids`
- `resume_job_id`
- `warnings`

## Deny or Cancel

```http
POST /internal/v1/approvals/{id}/deny
POST /internal/v1/approvals/{id}/cancel
```

Both accept an optional JSON body with `note`.

## Expire Stale Requests

`POST /internal/v1/approvals/expire`

Returns the number of expired pending requests.

## Allowlists

| Route | Purpose |
| --- | --- |
| `GET /internal/v1/approvals/allowlists` | List active allowlist rules |
| `POST /internal/v1/approvals/allowlists` | Add a rule |
| `POST /internal/v1/approvals/allowlists/{id}/remove` | Remove a rule |

## How Approvals Work

When the agent tries to use a tool that needs approval, a request is created. The request waits for user action. The user can approve (tool runs) or deny (tool is rejected). The agent gets the result and continues.

Approvals can time out. If the user does not respond, the tool is denied after a configurable timeout.
