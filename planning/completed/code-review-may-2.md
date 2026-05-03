## Verdict

**BLOCKER**

The architecture is going in the right direction, but I would not merge this yet. The current `SkillRunPlan` implementation still has approval-provenance, allowlist, concurrency, and persistence issues that can make approved skill runs non-auditable, broader than intended, or executed more than once.

## Executive Summary

The good parts:

- `run_skill` is the right abstraction.
- Freezing `PlanHash`, `ScriptHash`, command, args, stdin, timeout, and environment binding is the right direction.
- Plan-id resume and drift checks address the original “model must retry exact argv” failure mode.
- Structured `ToolResult` fields are useful for chat/UI flows.

The blocking issues:

1. Approved-token resume can erase the plan’s `approval_request_id`.
2. Skill allowlists do not bind to the frozen plan, so an allowlist for one invocation can allow different args/stdin/env.
3. Plan execution is not atomically claimed, so concurrent resume/retry can execute a skill twice.
4. Raw `stdin` is stored in SQLite as plaintext.
5. Approval API/CLI can commit approval and then fail while adding optional plan metadata.
6. Statuses are not yet a complete, centralized state machine.

## Findings

### 1. BLOCKER — Approved-token resume clears approval linkage

In broker.go, verified approval tokens return a successful `Decision` with `RequestID == 0`.

Then skill_run.go unconditionally overwrites `prepared.plan.ApprovalRequestID` with that zero value.

Finally, skill_run_plan_store.go converts `approvalRequestID <= 0` to `NULL` and updates the row.

Impact:

- The plan loses its approval provenance after successful resume.
- `ListSkillRunPlansByApprovalRequest()` stops finding the plan.
- CLI/service plan-id UX becomes inconsistent.
- Audit/recovery/debug flows cannot reliably answer “which approval authorized this run?”

Required fix:

- Preserve the existing `ApprovalRequestID` when `decision.RequestID == 0`.
- Better: make `VerifyApprovalToken()` return token claims, including request id, and populate `Decision.RequestID`.
- Make `UpdateSkillRunPlanApproval()` unable to clear `approval_request_id` accidentally. Use a separate explicit clear method if clearing is ever needed.

Required test:

- Extend `TestRunSkill_PersistsPendingPlanAndResumesByPlanID` so after approved-token resume:
    - stored `ApprovalRequestID` still equals the original request id
    - `ListSkillRunPlansByApprovalRequest(originalRequestID)` still returns the plan

---

### 2. BLOCKER — Skill allowlists are broader than frozen plans

The frozen plan includes args, stdin, command, script hash, and environment binding in the plan hash around skill_run.go.

But `SkillAllowlistMatcher` only includes skill/version/origin/trust/script/timeout in broker.go. `EvaluateSkillExec()` passes only those fields into the allowlist matcher in broker.go, and allowlists created from approval requests do the same in broker.go.

Impact:

- A user may approve/allowlist one safe frozen invocation.
- A later invocation with different args, stdin, or env can match the same allowlist.
- This weakens the frozen-plan security boundary.

Required fix:

- Add `PlanHash` and `EnvBindingHash` to `SkillAllowlistMatcher`.
- Allowlist entries created from an approved `SkillExecutionSubject` should bind to `PlanHash` by default.
- If broad skill-level allowlists are desired, require an explicit separate CLI/API path with clear wording.
- Add `ToolName` to `SkillEvaluation` / `SkillExecutionSubject`; stop hardcoding `"run_skill_script"` for both tools in broker.go.

Required tests:

- Approve/allowlist args `["safe"]`, then retry same skill with args `["danger"]`; it must require a new approval.
- Same for changed `stdin`.
- Same for changed environment binding.
- Keep a separate test for intentionally broad manual allowlists if that feature remains.

---

### 3. BLOCKER — Plan resume has no atomic execution claim

`executeNamed()` does a non-atomic find-then-create flow in skill_run.go. Two concurrent callers can both miss the active plan and create separate rows.

`FindActiveSkillRunPlan()` also treats `running` as reusable in skill_run_plan_store.go. Then `runPreparedSkillRun()` simply marks the plan `running` in skill_run.go without a compare-and-swap claim.

Impact:

- Concurrent resume of the same approved `plan_id` can run the command twice.
- Replaying identical args while a plan is already `running` can run again.
- Persistence errors are ignored in the execution path, so a command can execute even if the state transition to `running` failed.

Required fix:

- Add an atomic `ClaimSkillRunPlan()` store method:
    - conditional update from resumable states to `running`
    - returns whether the caller won the claim
    - execution must not start unless the claim succeeds
- Add a partial unique index or transaction for active `(requester_session_id, plan_hash)` reuse.
- Treat `running` as already in flight, not reusable.
- Add explicit stale-running recovery using timeout/heartbeat/restart policy.
- Stop ignoring persistence errors before execution; failure to persist `running` should block execution.

Required tests:

- Concurrent resume of the same `plan_id` executes exactly once.
- Concurrent identical trusted-mode `run_skill` calls create/reuse one active plan.
- A `running` plan returns a structured `running` result instead of executing again.
- A stale `running` plan is explicitly reconciled and tested.

---

### 4. HIGH — Raw stdin is persisted as plaintext

The schema stores `stdin_text` directly in db.go. `prepareSkillRun()` writes raw user input into that field in skill_run.go, and execution reads it back in skill_run.go.

Impact:

- Skill stdin may contain API tokens, email contents, customer data, shell snippets, or other sensitive material.
- The database becomes a plaintext secret store.
- This conflicts with the stated persistence requirement unless intentionally encrypted/redacted.

Required fix:

Choose one explicit policy:

- Encrypt persisted stdin blobs at rest and wipe after terminal status, or
- Store only a hash/reference and require stdin to be re-supplied for resume, or
- Make stdin persistence opt-in with clear warnings.

Do not include raw stdin in plan JSON, result JSON, audit events, debug logs, or CLI output.

Required tests:

- A secret passed as `stdin` does not appear in plaintext in SQLite after plan creation.
- Terminal cleanup removes or redacts persisted stdin if wipe-on-completion is chosen.
- Plan hash still changes when stdin changes.

---

### 5. MEDIUM — Approval response can fail after approval has already committed

The service approves first in service.go, then performs optional plan lookup in service.go. If that lookup fails, the API returns an error even though approval/token issuance already happened.

The CLI has the same shape: it prints the token in approvals_cmd.go, then can still return an error from plan lookup in approvals_cmd.go.

Impact:

- Service clients can lose the token because the response fails after mutation.
- Retrying approval may now fail because the request is no longer pending.
- CLI may show a token but return non-zero, causing automation to treat approval as failed.

Required fix:

- Query plan ids before mutation, or make post-approval plan lookup best-effort.
- If lookup fails after approval, still return/print the token and include a warning.
- Never let optional UX metadata make a committed approval appear failed.

Required tests:

- Simulate plan lookup failure after approval succeeds.
- Service still returns HTTP 200 with token plus warning.
- CLI exits successfully after printing token and warning.

---

### 6. MEDIUM — Status taxonomy is incomplete and stringly typed

Current terminal statuses are only `"succeeded"`, `"failed"`, `"blocked"`, and `"preflight_failed"` in skill_run.go.

The implementation also uses statuses like `"prepared"`, `"pending_approval"`, `"running"`, and `"blocked"` directly as string literals across skill_run.go.

Missing or mismatched states from the requested model include:

- `planned`
- `denied`
- `approved`
- `blocked_by_policy`
- `timed_out`
- `cancelled`
- `expired`
- `stale_plan`

Impact:

- The agent/UI cannot distinguish policy denial from preflight failure.
- Timeout is reported as generic `failed`.
- Script/env drift is reported as generic `preflight_failed` instead of `stale_plan`.
- Approval denial/cancel/expiry does not appear to update associated plans into durable terminal states.

Required fix:

- Define typed constants for all skill run statuses.
- Add a small state-machine helper for allowed transitions.
- Map:
    - policy deny → `blocked_by_policy`
    - approval deny → `denied`
    - approval cancel → `cancelled`
    - approval expiry → `expired`
    - script/env/metadata drift → `stale_plan`
    - context deadline → `timed_out`
- Update docs/tests to match the actual status names.

Required tests:

- Approval denied does not execute and plan becomes `denied`.
- Approval expired does not execute and plan becomes `expired`.
- Script hash drift becomes `stale_plan`.
- Context deadline becomes `timed_out`.
- Policy deny becomes `blocked_by_policy`.

## Deletion / Simplification Recommendations

- Keep `run_skill_script` as a compatibility wrapper, but make `run_skill` the only source of new semantics.
- Centralize tool-name constants for `run_skill` and `run_skill_script`; avoid repeated string comparisons.
- Centralize skill-run status constants and transition logic.
- Remove or justify the extra `"awaiting_resume"` status if it is not part of the final state machine.
- Avoid broad documentation reformatting while this security-sensitive change is still under review.

## Required Implementation Plan

1. **Fix approval provenance first**
    - Return request id from token verification or preserve the existing plan request id.
    - Make accidental clearing of `approval_request_id` impossible.
    - Add regression coverage.

2. **Make allowlists plan-aware**
    - Add `PlanHash` and `EnvBindingHash` to skill allowlist matching.
    - Default approval-created allowlists to exact frozen plan matching.
    - Add explicit broad allowlist path only if needed.

3. **Add atomic execution claiming**
    - Add DB transaction/unique active plan handling.
    - Add `ClaimSkillRunPlan()` CAS update.
    - Do not execute unless the `running` claim is durably persisted.
    - Treat already-running plans as in-flight.

4. **Protect stdin persistence**
    - Encrypt, redact, wipe, or stop persisting raw stdin.
    - Ensure result/audit/log paths never expose stdin.

5. **Make approval response metadata best-effort**
    - Ensure service/CLI never lose tokens due to optional plan lookup failure.

6. **Formalize status model**
    - Add constants and state transitions.
    - Implement missing statuses.
    - Align docs and tests with the final taxonomy.

## Merge Checklist

- [ ] Approved-token resume preserves `approval_request_id`.
- [ ] `ListSkillRunPlansByApprovalRequest()` still finds completed resumed plans.
- [ ] Skill allowlists bind to `PlanHash`/`EnvBindingHash` by default.
- [ ] Changed args/stdin/env require a new approval unless an explicit broad allowlist exists.
- [ ] Same `plan_id` cannot execute twice under concurrent resume.
- [ ] Identical active plan creation is atomic.
- [ ] `running` plans are not re-executed unless explicitly reclaimed as stale.
- [ ] Raw stdin is not stored in plaintext.
- [ ] Approval service returns token even if plan-id enrichment fails.
- [ ] CLI approval exits successfully after token issuance even if plan-id lookup fails.
- [ ] Status constants cover `planned`, `preflight_failed`, `pending_approval`, `denied`, `approved`, `running`, `succeeded`, `failed`, `blocked_by_policy`, `timed_out`, `cancelled`, `expired`, and `stale_plan`.
- [ ] Tests cover denied/expired/cancelled approvals, stale plan drift, timeout, env ordering, default timeout behavior, and restart/stale-running reconciliation.
