# Approval Moderator Neckbead Review

All findings below are addressed. Verification:

```bash
go test ./internal/config ./internal/db ./internal/approval ./internal/tools ./cmd/or3-intern
```

---

## [x] Prompt truncation cuts off the actual request

**Fixed in** `internal/approval/moderator_prompt.go` — section-budgeted user prompt; built-in policy stays in the system message only; output contract preserved when truncating.

## [x] The same policy is sent twice

**Fixed in** `internal/approval/moderator_provider.go` — uses `buildModeratorUserPrompt`; system message carries `builtinModeratorPolicy` only.

## [x] Requester metadata is sent unredacted

**Fixed in** `internal/approval/moderator_redact.go` (`redactRequesterContext`) and `moderator_prompt.go` — allowlisted/hashed requester summary only.

## [x] Workspace detection is fake

**Fixed in** `internal/approval/workspace_path.go`, `moderator_facts.go`, `moderator_broker.go` — `workspaceRelation` uses broker workspace and `pathWithinRoot`.

## [x] Partial action overrides ignore the selected preset

**Fixed in** `internal/config/moderator.go` — missing action fields fall back to `actionsForApprovalModeratorPreset(cfg.Preset)`.

## [x] Redaction stats are computed and then thrown away

**Fixed in** `moderator_facts.go`, `types.go`, `moderator_broker.go` — `Redactions` on `ModeratorReviewInput`; audit events include counts.

## [x] Audit events are too thin to debug decisions

**Fixed in** `moderator_broker.go` (`moderatorAuditPayload`) — common payload with model, policy_hash, latency, risk, action, type, redaction stats.

## [x] Metadata write failures are ignored before making safety decisions

**Fixed in** `moderator_broker.go` — approve/deny paths return error when metadata persist fails; `approval.moderator.metadata_failed` audit on escalate path.

## [x] Secret-exfiltration hard-deny is basically two string checks

**Fixed in** `moderator_hard.go` — `hasNetworkSink` + `hasSecretSource` conservative detector.

## [x] The security-weakening detector is brittle string soup

**Fixed in** `moderator_hard.go` — expanded markers, JSON `enabled: false` pattern, config-file edit heuristics.

## [x] The configure TUI cannot edit per-risk actions

**Fixed in** `cmd/or3-intern/configure_tui.go` — four choice fields for low/medium/high/extreme actions.

## [x] Message-send approvals are still not wired into the broker

**Fixed in** `evaluate.go` (`EvaluateMessageSend`), `tools/message.go`, `cmd/or3-intern/main.go` — send_message calls broker before delivery.

## [x] Subject fact extraction does not cover the promised domains

**Fixed in** `moderator_facts.go` — explicit facts for `SubjectMessageSend` and `SubjectFileTransfer`.

## [x] Redaction is shallow

**Fixed in** `moderator_redact.go` — recursive redaction for nested maps and `[]any`.

## [x] Tests assert happy paths, not the dangerous boundaries

**Fixed in** `moderator_boundaries_test.go`, `moderator_test.go`, `config_test.go`, `tools/message_approval_test.go` — prompt budget, requester redaction, workspace relation, preset fallback, exfil/weakening variants, audit payload, message-send approval.
