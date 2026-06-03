# Approval Workflow

Approvals happen when a turn, job, terminal flow, or runner action needs explicit permission.

## 1. Work hits a guarded action

Typical triggers include:

- exec or shell actions
- network access
- restricted file operations
- skill execution with guarded policy

## 2. OR3 Intern creates an approval request

The request becomes visible through the local CLI, OR3 App, or the service API.

When `security.approvals.moderator.enabled` is on and a provider is configured, a fast model may review the request first:

| Moderator outcome | What happens next |
| --- | --- |
| Auto-approve (low/medium risk) | OR3 issues the same one-shot approval token as a human approval and continues |
| Escalate (high/extreme or failure) | The request stays pending and follows the normal CLI/app/channel flow |
| Auto-deny (policy or extreme risk) | The request is resolved as denied with a short reason and safe alternative when available |

Built-in hard denials (secret exfiltration, destructive commands, security weakening) always win over user policy text. Review failures fail closed using `security.approvals.moderator.failureAction` (default: escalate).

## 3. The request is surfaced

Common places it appears:

| Surface | Typical handling |
| --- | --- |
| Local CLI | inline approval prompt or follow-up command workflow |
| OR3 App | approval inbox / action prompt |
| Service API | `approval_required` response, approval list polling, or related streamed events |

## 4. Resolve it

API and operator routes:

- `GET /internal/v1/approvals`
- `GET /internal/v1/approvals/{id}`
- `POST /internal/v1/approvals/{id}/approve`
- `POST /internal/v1/approvals/{id}/deny`
- `POST /internal/v1/approvals/{id}/cancel`

Local CLI equivalents live under `or3-intern approvals ...`.

## 5. Watch for resume information

Approving may return:

- `token`
- `resume_job_id`
- `plan_id` or `plan_ids`
- `warnings`

That means resolution is not only “mark approved”; it can also restart or resume waiting work.

## Related flows

- Device pairing approvals are separate and live under `pairing`, not `approvals`.
- Persistent approval bypasses use allowlists under `/internal/v1/approvals/allowlists` or `or3-intern approvals allowlist ...`.
