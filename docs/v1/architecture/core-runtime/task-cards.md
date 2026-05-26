# Task cards

Task cards (`internal/agent/task_card.go`) are per-session structured state: current goal, active plan, decisions, references, open questions, and active files. They are persisted in SQLite and injected into prompts under the `active_task_card` budget.

## What a task card holds

- **Goal** — what the session is trying to accomplish
- **Status** — active or completed for the card as a whole
- **Metadata** — title, current user request, next step, completion notes
- **Active plan** — titled plan with tasks (id, title, description, status, completion note)
- **Legacy plan lines** — older string plan items kept in sync with structured tasks
- **Decisions, refs, files** — bounded lists used for continuity across long turns

## Plan tools vs passive updates

When the task card is enabled, the agent can use native plan tools (see [plan tools](../tools/plan-tools.md)):

- `create_plan` — establish or replace the active plan
- `update_plan` — revise tasks or next step
- `complete_plan_task` — finish a step with notes
- `remove_plan` — clear tracking when done

The optional **context manager** may also propose conservative `task_card_updates`; that path is separate from the plan tools.

## Plan enforcement

`context.taskCard.enforcePlan` controls whether write/exec/web-style tools require an active plan first. It only takes effect when the task card is enabled (`context.taskCard.enabled`). See [plan tools](../tools/plan-tools.md#plan-gate) for the full gate rules.

For everyday local use, leave **Require plan before writes** off so simple file edits do not fail with `active plan required`.

## Configuration

| Field | Meaning |
| --- | --- |
| `context.taskCard.enabled` | Persist and inject task-card state |
| `context.taskCard.enforcePlan` | Require `create_plan` before gated tools |
| `context.taskCard.maxRefs` | Max source refs on the card |
| `context.taskCard.maxPlanItems` | Max legacy plan lines |
| `context.sections.activeTaskCard` | Prompt token budget for the section |

Configure via `or3-intern configure --section context`, or **Settings → Advanced → Context** in or3-app.

## Storage

Task card JSON lives in the task state store. See [task state store](../storage/task-state-store.md).

## Example (prompt view)

```
Goal: ship the audit fix
Plan: Fix plan gate · Update docs · Verify in app
Next: document enforcePlan in config reference
Tasks:
  - t1 Fix plan gate [completed]
  - t2 Update docs [in_progress]
  - t3 Verify in app [pending]
```
