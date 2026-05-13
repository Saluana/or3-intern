# Task Cards

Task cards (`internal/agent/task_card.go`) are structured task definitions. They help the agent manage complex, multi-step work.

## What a Task Card Contains

- **Title** — what the task is called
- **Description** — what needs to be done
- **Steps** — ordered list of sub-tasks
- **Status** — pending, running, completed, failed, cancelled
- **Results** — what each step produced

## How Task Cards Work

The agent creates a task card when it starts a multi-step task. Each step in the card has its own status and result. The agent marks steps as done when they complete.

If a step fails, the agent can retry or adjust the plan. The task card tracks all of this.

## User Visibility

Task cards are stored in the database. Users can see them through the CLI or API. This gives visibility into what the agent is doing and how far along it is.

## Example

```
Task: Research Competitors
  Step 1: Search for Competitor A [done]
  Step 2: Search for Competitor B [done]
  Step 3: Search for Competitor C [running]
  Step 4: Write comparison [pending]
  Step 5: Save to file [pending]
```
