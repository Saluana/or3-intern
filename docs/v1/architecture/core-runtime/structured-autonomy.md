# Structured Autonomy

Structured autonomy lets the agent work on complex tasks with multiple steps. It goes beyond simple question-and-answer.

## What It Does

The agent can:
1. Break a task into smaller sub-steps
2. Track progress across steps
3. Use different tools for different steps
4. Adapt the plan as it goes

## How It Works

When given a complex task, the agent creates a plan. The plan has steps. Each step has a goal, the tools needed, and success criteria.

The agent works through the steps one at a time. It can change the plan if needed. If a step fails, it tries a different approach.

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
