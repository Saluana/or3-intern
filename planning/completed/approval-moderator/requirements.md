# Approval Moderator Requirements

## Overview

This plan defines an AI-assisted approval moderator for `or3-intern`. The moderator evaluates approval requests that the existing broker would otherwise send to the user, classifies their risk, and either approves, denies, or escalates them based on user policy.

The goal is to reduce approval fatigue during long-running agent work without weakening the existing safety posture. The moderator does not replace sandboxing, access profiles, command allowlists, quotas, or human approval for dangerous actions. It only decides what happens after an action has already crossed an approval boundary.

Assumptions:

- The first implementation targets `or3-intern` service/CLI approval requests, not Codex itself.
- The moderator should be enabled by default only when `security.approvals.enabled` is enabled and the approval broker can issue tokens.
- Default behavior should auto-approve low and medium risk, escalate high and extreme risk, and deny requests that match explicit deny rules or cannot be reviewed safely.
- User policy text can append to the built-in moderator policy, but built-in hard denials take precedence.
- The moderator model should be configurable through existing provider/profile/model routing patterns.

## Requirements

### 1. Moderator Policy Configuration

**Engineering objective:** Add explicit configuration for AI-assisted approval review.

Acceptance criteria:

1. WHEN config loads without moderator fields THEN defaults SHALL preserve existing approval behavior unless moderator defaults are intentionally enabled by the selected runtime/safety profile.
2. WHEN the moderator is enabled THEN config SHALL include provider/model, timeout, risk thresholds, action mapping, and optional user policy text.
3. WHEN a user configures unsupported risk levels, actions, or policy values THEN config validation SHALL fail with a precise field path.
4. WHEN managed or future hardening profiles set stricter values THEN local user policy SHALL not be able to weaken built-in hard-deny classes.

### 2. Risk Classification

**Engineering objective:** Classify every reviewed request into a small, stable risk taxonomy.

Acceptance criteria:

1. WHEN an approval request is reviewed THEN the moderator SHALL return exactly one risk level: `low`, `medium`, `high`, or `extreme`.
2. WHEN a request involves likely secret exfiltration, credential probing, broad persistent security weakening, or destructive irreversible damage THEN it SHALL classify as `extreme` or return a hard denial.
3. WHEN a request is ordinary bounded development work inside the configured workspace, such as running tests or reading non-secret files, THEN it MAY classify as `low` or `medium`.
4. WHEN a request would fetch large uncached external content, access broad network surfaces, run shell syntax, mutate external services, or exceed quotas materially THEN it SHALL classify at least `high` unless user policy explicitly allows that pattern.
5. WHEN the moderator cannot confidently classify a request THEN it SHALL fail closed by escalating or denying according to config.

### 3. Decision Actions

**User story:** As a user, I want to choose which risk levels require my attention, so that the agent can keep working while still stopping for dangerous actions.

Acceptance criteria:

1. WHEN a risk level maps to `approve` THEN the broker SHALL issue a normal one-shot approval token and audit that it was moderator-approved.
2. WHEN a risk level maps to `escalate` THEN the request SHALL remain pending and surface through the existing CLI, service API, app, and channel approval flows.
3. WHEN a risk level maps to `deny` THEN the request SHALL resolve as denied with a concise moderator reason and safe retry advice for the agent.
4. WHEN a moderator decision is malformed, times out, or hits provider failure THEN the action SHALL not run.
5. WHEN the user explicitly approves a previously escalated request THEN the normal existing approval/resume path SHALL continue to work unchanged.

### 4. Prompt and Policy Safety

**Engineering objective:** Make the review prompt deterministic, inspectable, and resistant to request-content prompt injection.

Acceptance criteria:

1. WHEN building a moderator prompt THEN it SHALL separate system policy, user policy, and request facts into clearly labeled sections.
2. WHEN request fields contain instructions, logs, command output, filenames, or tool arguments THEN the prompt SHALL treat them as untrusted data.
3. WHEN subject JSON contains secrets or likely credentials THEN the prompt SHALL redact or summarize before sending to the provider.
4. WHEN user policy says "never use grep" or "send network-heavy pulls to me" THEN the moderator SHALL apply it as policy and return deny/escalate with actionable alternatives where appropriate.
5. WHEN the policy is updated THEN the effective policy summary SHALL be visible in diagnostics without leaking secrets.

### 5. Provider and Model Selection

**User story:** As a user, I want to choose the moderator model, so that I can trade latency, cost, and judgment quality.

Acceptance criteria:

1. WHEN no moderator model is configured THEN the system SHALL use a fast default model from existing provider/model routing config.
2. WHEN a moderator provider profile is configured THEN the review call SHALL use that provider profile and timeout, independent of the main chat model.
3. WHEN the moderator provider is unavailable THEN the review SHALL fail closed and surface the approval to the user or deny according to config.
4. WHEN moderator calls are made THEN they SHALL use bounded input size and bounded output parsing.

### 6. Auditability and Observability

**Engineering objective:** Store enough information to debug and tune moderator decisions without storing sensitive prompt payloads.

Acceptance criteria:

1. WHEN a moderator reviews a request THEN audit logs SHALL include request ID, subject hash, subject type, risk level, action, model identity, policy version/hash, latency, and a short reason.
2. WHEN a request is listed through CLI/API/app surfaces THEN it MAY include moderator risk/action metadata if present.
3. WHEN a request is auto-approved or auto-denied THEN the event SHALL be distinguishable from human approval/denial.
4. WHEN the moderator redacts request data THEN audit logs SHALL record redaction count/category, not raw redacted content.

### 7. Persistence

**Engineering objective:** Persist moderator metadata in SQLite without breaking existing approval data.

Acceptance criteria:

1. WHEN migrations run on existing databases THEN all existing approval request rows SHALL remain valid.
2. WHEN moderator metadata is stored THEN it SHALL not change the subject hash used for approval token matching.
3. WHEN old binaries read the database THEN the migration SHALL avoid destructive schema changes.
4. WHEN moderator metadata is absent THEN existing approval list/show behavior SHALL continue.

### 8. Existing Approval Flow Compatibility

**Engineering objective:** Preserve current CLI, OR3 App, service API, Telegram, Slack, Discord, WhatsApp, and email approval behavior.

Acceptance criteria:

1. WHEN the moderator escalates a request THEN existing pending approval routing SHALL behave exactly as it does today.
2. WHEN the moderator approves a request THEN existing token replay/resume behavior SHALL be used instead of a separate execution path.
3. WHEN the moderator denies a request THEN channel and app surfaces SHALL receive a safe, understandable terminal state.
4. WHEN approvals are disabled or broker signing key is unavailable THEN the moderator SHALL not create an unsafe bypass.

### 9. User-Tunable Defaults

**User story:** As a user, I want simple presets, so that I do not need to write policy text for common autonomy levels.

Acceptance criteria:

1. WHEN the user selects `balanced` THEN low and medium risk SHALL auto-approve, high and extreme SHALL escalate or deny according to built-in policy.
2. WHEN the user selects `cautious` THEN low MAY auto-approve and medium/high/extreme SHALL escalate or deny.
3. WHEN the user selects `hands_off` THEN low/medium/high MAY auto-approve only if built-in policy has no deny match; extreme SHALL escalate or deny.
4. WHEN the user selects `manual` THEN all reviewed requests SHALL route to the user.
5. WHEN the user customizes per-level actions THEN custom settings SHALL override the preset within built-in safety bounds.

### 10. Agent Feedback

**User story:** As an agent, I need a useful denial or escalation reason, so that I can adapt without repeatedly asking for the same blocked action.

Acceptance criteria:

1. WHEN a request is denied because of user policy THEN the tool error SHALL include a short reason and one safe alternative when available.
2. WHEN a request is escalated THEN the tool error SHALL continue to indicate approval required and include the request ID.
3. WHEN an action is denied by hard policy THEN the agent SHALL not receive sensitive details that could help bypass the policy.

## Non-functional constraints

- Moderator review must be bounded: short timeout, bounded prompt size, bounded response size, no tool calls, no recursive approval.
- Moderator failure must fail closed.
- Existing sandbox, access profile, network policy, allowlist, quota, and approval-token checks remain authoritative.
- SQLite migrations must be additive and compatible with single-process deterministic operation.
- Prompt construction must avoid sending secrets, raw environment variables, approval tokens, signing keys, or long command output.
- The review path must avoid high RAM use and must not block unrelated approval requests indefinitely.
- Audit and diagnostics should support tuning, but should store summaries and hashes rather than raw sensitive payloads.
