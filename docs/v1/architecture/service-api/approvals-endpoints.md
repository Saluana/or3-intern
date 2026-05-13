# Approvals Endpoints

Endpoints for the approval workflow. Apps use these to let users review and respond to approval requests.

## Get Pending Approvals

`GET /api/v1/approvals/pending`

Returns all approval requests waiting for user action.

```json
{
  "approvals": [
    {
      "id": "apr_123",
      "tool": "exec",
      "args": {"command": "rm -rf /tmp/test"},
      "reason": "Agent wants to run a shell command",
      "created_at": "2026-05-12T10:00:00Z"
    }
  ]
}
```

## Approve or Deny

`POST /api/v1/approvals/:id`

```json
{
  "action": "approve"
}
```

Or:

```json
{
  "action": "deny",
  "reason": "That command looks dangerous"
}
```

## Approval History

`GET /api/v1/approvals/history`

Returns past approvals and denials. Useful for audit and review.

## How Approvals Work

When the agent tries to use a tool that needs approval, a request is created. The request waits for user action. The user can approve (tool runs) or deny (tool is rejected). The agent gets the result and continues.

Approvals can time out. If the user does not respond, the tool is denied after a configurable timeout.
