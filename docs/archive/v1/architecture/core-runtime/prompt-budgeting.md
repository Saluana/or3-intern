# Prompt Budgeting

The prompt budget (`internal/agent/prompt_budget.go`) tracks token usage. It makes sure the prompt does not get too big for the provider.

## How Token Tracking Works

The budget estimates tokens for each message. It uses a simple formula: roughly 4 characters per token. This is an estimate, not exact.

It monitors total usage against the provider's limits. Each model has a different limit (e.g., GPT-4o has 128K tokens, some models have 32K).

## What Happens When Over Budget

When the prompt goes over budget, the system needs to reduce size. It does this in order:

1. Drop less important memory items
2. Compress old conversation history
3. Use semantic compression on remaining items

If compression is not enough, the request is rejected with a "context too large" error.

## Per-Provider Limits

Different providers have different limits:
- OpenAI: up to 128K tokens (varies by model)
- Anthropic: up to 200K tokens
- Local models: typically 4K to 32K tokens

The budget system knows the limit for the current provider and adjusts accordingly.
