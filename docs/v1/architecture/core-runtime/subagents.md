# Subagents

Subagents (`internal/agent/subagents.go`) are separate agent instances that work in parallel. The main agent can spawn subagents for independent tasks.

## How Subagents Work

- The main agent decides it needs help with a task
- It creates a subagent with its own instructions and tools
- The subagent runs independently
- The main agent waits for results or continues working
- Results are collected when all subagents finish

## When to Use Subagents

Subagents are useful for:
- **Research** — multiple subagents search different sources at the same time
- **Parallel processing** — process multiple files or data sets simultaneously
- **Multi-step tasks** — each subagent handles one part of a complex task
- **Background checks** — verify facts while the main agent continues

## Limitations

Subagents share the same provider quota. Running many subagents at once can hit rate limits. There is a configurable maximum number of concurrent subagents.

## State Management

Subagent state is stored in the database. Each subagent has an ID, a parent session, its status, and its results. You can check on subagents even after the main conversation ends.
