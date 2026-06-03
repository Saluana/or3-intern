# Approval System

The approval broker evaluates every sensitive action and decides whether to allow, deny, or require operator approval.

## Evaluation pipeline

When a tool calls the broker, it follows this sequence (source: `internal/approval/evaluate.go:109-125`):

1. **Check existing token** - If the request includes an approval token, verify it matches the subject hash and host ID
2. **Check policy mode** - Apply the configured mode (trusted, deny, ask, or allowlist)
3. **Check allowlist** - See if the action matches a previously approved allowlist entry
4. **Require approval** - Create a new pending approval request

## Policy modes

Each subject type has its own mode (source: `internal/approval/evaluate.go:204-219`):

| Subject | Config key |
|---------|-----------|
| Exec | `security.approvals.exec.mode` |
| Skill execution | `security.approvals.skillExecution.mode` |
| Runner permission | `security.approvals.exec.mode` |
| Secret access | `security.approvals.secretAccess.mode` |
| Message send | `security.approvals.messageSend.mode` |

Available modes (source: `internal/approval/evaluate.go:138-161`):

- **trusted** - allow immediately, audit as "trusted"
- **deny** - block immediately, audit as "blocked"
- **ask** - require approval (default for balanced mode)
- **allowlist** - check allowlist, then require approval if no match

## Subject types

The broker handles these action types (source: `internal/approval/evaluate.go`):

- `SubjectExec` - command execution (program, argv, working dir, env)
- `SubjectSkillExec` - skill script execution (skill ID, version, plan hash)
- `SubjectSecretAccess` - secret store read/write
- `SubjectRunnerPermission` - filesystem read/write within skill runners
- `SubjectToolQuota` - rate limit checks

## Subject hashing

Each subject is serialized to JSON and hashed with SHA-256. This hash is used to:
- Match approval tokens to the same action
- Find existing pending requests for the same subject
- Index allowlist entries

Source: `internal/approval/tokens.go:115-122` (CanonicalSubjectHash)

## Approval tokens

When an operator approves a request, the broker issues a short-lived token (configured by `approvalTokenTTLSeconds`). The token format is `base64(payload).hex(hmac)`. Verification checks: token not expired, host ID matches, subject hash matches, token not already consumed, record not revoked.

Source: `internal/approval/tokens.go:22-62` (VerifyApprovalTokenClaims)
