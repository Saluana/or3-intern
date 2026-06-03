# Plan tools

Native plan tools (`internal/agent/plan_tools.go`) let the agent maintain a structured work plan on the active **task card** for the current session. They are separate from Doctor settings plans (`doctor_create_plan`, `POST /internal/v1/doctor/plans`).

## Tools

| Tool | Purpose |
| --- | --- |
| `create_plan` | Create or replace the active plan (title, tasks, optional goal and next step) |
| `update_plan` | Change plan title, goal, tasks, statuses, or next-step note |
| `complete_plan_task` | Mark one task complete with a summary and updated next step |
| `remove_plan` | Clear the active plan when work is finished or no longer tracked |

All four tools are in the `plan` tool group and report **safe** capability (no elevated approval by themselves).

## Task card linkage

Plans are stored in session task-card metadata (`ActivePlanMetadata` on `TaskCard`). The prompt builder renders the active plan in the `active_task_card` context section so the model sees goal, tasks, and next step across turns.

Legacy string plan lines on the task card are kept in sync when tasks change (`syncLegacyPlanFromTasks`).

## Plan gate

When `context.taskCard.enforcePlan` is **true** and `context.taskCard.enabled` is **true**, the runtime sets `EnforceActivePlan` and blocks tools that need an established plan until `create_plan` has run:

- Write tools (`write_file`, `edit_file`, …)
- Execution tools (`exec`, skill runners, …)
- Web tools (`web_fetch`, `web_search`, …)
- MCP, service, and skill group tools
- `spawn_subagent`

Plan tools themselves are never blocked. Doctor tools (`doctor_*`) are excluded from this gate.

A small number of read-only **exploration** tools (`read_file`, `search_file`, `list_dir`, `read_artifact`, `memory_search`) may run before a plan exists; after four exploration calls in one turn, the gate applies to them as well.

Trusted tool access from context (for example elevated admin flows) bypasses the gate.

Default for new configs: `enforcePlan` is **false**, so local work can write files without calling `create_plan` first.

## Configuration

| JSON path | Configure field | Default |
| --- | --- | --- |
| `context.taskCard.enabled` | `context_task_card_enabled` | on (quality-oriented defaults) |
| `context.taskCard.enforcePlan` | `context_task_card_enforce_plan` | off |
| `context.taskCard.maxRefs` | `context_task_card_max_refs` | 12 |
| `context.taskCard.maxPlanItems` | `context_task_card_max_plan` | 8 |

Tune these under **Settings → Advanced → Context** in or3-app, or `or3-intern configure --section context`.

## Related docs

- [Task cards](../core-runtime/task-cards.md)
- [Tool reference](../../reference/tool-reference.md)
- [Configure API](../../user-guide/app-integration/configure-api.md)
