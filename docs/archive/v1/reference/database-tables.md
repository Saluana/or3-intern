# Database tables

OR3 Intern uses SQLite for storage. Here are the main tables.

| Table | What it stores |
|---|---|
| `sessions` | Chat sessions and conversations |
| `messages` | Individual messages in sessions |
| `approvals` | Pending and completed approval requests |
| `auth_sessions` | Authentication sessions for paired devices |
| `jobs` | Background job records |
| `subagents` | Subagent instances and their results |
| `skills` | Registered skill definitions |
| `runner_chat` | Runner-level chat history |
| `mcp_servers` | MCP server connections |
| `task_states` | State tracking for tasks |
| `artifacts` | Files and data produced by the agent |
| `audit_log` | Tamper-evident audit chain entries |
| `secrets` | Encrypted secret storage |
| `devices` | Paired device records |
| `pairing_requests` | Pending pairing requests |
| `allowlist` | Allowed items (tools, paths, etc.) |

The database file is at `~/.or3-intern/or3-intern.sqlite` by default.
