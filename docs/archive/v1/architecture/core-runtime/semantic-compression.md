# Semantic Compression

When the context is too large, semantic compression (`internal/agent/semantic_compression.go`) kicks in. It shrinks the context while keeping the important information.

## How Compression Works

1. **Identify** — less important messages are flagged for compression
2. **Summarize** — each message is summarized into a shorter version
3. **Replace** — the summary replaces the original in context

The result is a shorter prompt that still has the key information.

## What Gets Compressed First

Old messages are compressed before new ones. General chit-chat is compressed before task-specific messages. Messages that do not relate to the current turn are the best candidates.

## Quality Trade-Off

Compression loses some detail. A long technical discussion might become a one-line summary. The agent will still understand the gist, but it might miss nuances.

The system tries to balance context size against information quality. It only compresses as much as needed to fit within the token budget.

## When Compression Is Used

- After memory search returns too many results
- When conversation history is very long
- When tool results are large
- When the prompt exceeds the provider's limit
