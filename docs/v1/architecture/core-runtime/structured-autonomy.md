# Structured Autonomy

Structured autonomy lets the agent work on complex tasks with multiple steps. It goes beyond simple question-and-answer.

## What It Does

The agent can:
1. Break a task into smaller sub-steps
2. Track progress across steps
3. Use different tools for different steps
4. Adapt the plan as it goes

## How It Works

When given a complex task, the agent can call `create_plan` to store a titled plan with tasks on the session task card, then drive work with `update_plan` and `complete_plan_task`. See [plan tools](../tools/plan-tools.md).

With `context.taskCard.enforcePlan` enabled, write, exec, and web tools stay blocked until that plan exists. With enforcement off (the default), the agent may still use plan tools for long jobs but can write files without a planning step first.

The agent works through steps one at a time, adapts the plan when needed, and clears it with `remove_plan` when finished.

## Example

User: "Research our top 3 competitors and write a comparison report."

The agent might:
1. Search the web for competitor A
2. Search the web for competitor B
3. Search the web for competitor C
4. Compare the findings
5. Write a report file
6. Show the report to the user

Each step uses different tools (web search, file write, etc.). The agent tracks which steps are done and what comes next.

## Benefits

Structured autonomy makes the agent more useful for real work. It can handle tasks that take many steps. It does not need the user to guide every step.
