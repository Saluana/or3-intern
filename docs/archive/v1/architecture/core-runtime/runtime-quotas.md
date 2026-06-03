# Runtime Quotas

Runtime quotas prevent the agent from using too many resources. They are safety limits for the system.

## What Is Quota'd

- **Max tokens per turn** — how big the prompt can be (default: provider's limit)
- **Max tool calls per turn** — how many tools the agent can call in one turn (default: 25)
- **Max subagent count** — how many subagents can run at once (default: 5)
- **Max job concurrency** — how many background jobs can run at once (default: 3)
- **Rate limits** — how fast the agent can call the provider (varies by provider)

## What Happens at the Limit

- **Token limit** — context is compressed or the oldest messages are dropped
- **Tool call limit** — the agent is told no more tools are available this turn
- **Subagent limit** — new subagents wait in a queue
- **Job concurrency** — new jobs wait in a pending state
- **Rate limits** — calls are queued and delayed

## Configuring Quotas

Quotas are in the config file:

```json
{
  "runtime": {
    "max_tokens_per_turn": 32000,
    "max_tool_calls_per_turn": 25,
    "max_subagents": 5,
    "max_jobs": 3
  }
}
```

## Why Quotas Matter

Quotas prevent runaway resource usage. Without them, a bad prompt could burn through your API budget. A bug in the agent could spawn unlimited subagents. Quotas keep things under control.
