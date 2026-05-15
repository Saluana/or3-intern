# System Map

Here is how data flows through OR3 Intern:

```
User Input (CLI / App / Channel)
        |
        v
   Command Dispatcher / Service Router / Channel Adapter
        |
        +--> Runtime Builder (config, storage, security, integrations)
        +--> Control Plane + ServiceApp (typed app facade)
        |
        v
   Agent Runtime (prompt -> context -> tool calls -> response)
        |
        +--> Tools (exec, files, web, memory, skills, MCP)
        +--> Memory (vector search, FTS, consolidation)
        +--> Subagents (parallel processing)
        +--> Jobs (background tasks)
        |
        v
   Output (streaming response, tool results, job status, channel delivery)
```

## What Each Part Does

- **Command Dispatcher** — routes user input to the right handler
- **Service Router** — routes authenticated `/internal/v1/*` app requests to handler families
- **Control Plane** — exposes typed health, readiness, capabilities, approval, device, embedding, audit, scope, and job operations
- **ServiceApp** — wraps runtime execution for turns, subagents, approvals, agent runs, runner chat, auth, and aborts
- **Runtime Builder** — loads everything the agent needs to run
- **Agent Runtime** — the main loop that processes messages
- **Tools** — things the agent can do (run commands, read files, search the web)
- **Memory** — stores and retrieves past conversations and documents
- **Subagents** — extra agent instances that work in parallel
- **Jobs** — tasks that run in the background
- **Event Bus** — transports channel, cron, webhook, filewatch, and heartbeat events inside the process

The runtime is the central hub. Everything flows through it.
