# Context Management

Context management (`internal/agent/context_manager.go`) decides what goes into the prompt. It is the gatekeeper for the agent's attention.

## What Context Includes

- Recent conversation history (last N messages)
- Relevant memory items (from vector similarity search)
- Pinned context items (things the user wants the agent to always remember)
- Workspace context (files and folders in the current workspace)
- Tool results from the current turn

## How It Decides

The context manager scores each potential item by relevance. Items related to the current query get higher scores. Old or unrelated items get lower scores.

It also tracks token budgets. If the context is too large, it drops the least important items first.

## Pinned Context

Users can pin items to keep them in context. Pinned items always stay, regardless of relevance scores. This is useful for project rules, important documents, or ongoing tasks.

## Workspace Context

The workspace context tells the agent about files in the current directory. It includes file names, types, and sometimes file contents (for small files).

## Tool Results

When the agent calls a tool, the result goes into context for the next turn. This lets the agent use tool results in its reasoning.
