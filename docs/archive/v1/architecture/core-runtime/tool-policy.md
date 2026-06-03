# Tool Policy

Tool policy (`internal/agent/tool_policy.go`) controls what tools the agent can use. It is the safety gate for tool access.

## Policy Types

- **Allowlist** — tools the agent can always use without restrictions
- **Blocklist** — tools the agent is never allowed to use
- **Approval required** — tools that need user approval before running
- **Role-based** — different rules for different contexts (chat, automation, channels)

## How Policies Are Applied

Each tool call goes through policy check. The system checks:
1. Is this tool in the allowlist? If yes, it can run.
2. Is this tool in the blocklist? If yes, it is rejected.
3. Does this tool need approval? If yes, the user must approve.
4. Does the current context allow this tool? If not, it is rejected.

## Default Policy

By default, most tools are allowed. Dangerous tools (like shell exec with arbitrary commands) need approval. The defaults are safe for most users but can be customized.

## Customizing Policy

You can change the policy in the config file. Add tools to the blocklist to disable them. Add tools to the approval list to require confirmation. The config section looks like this:

```json
{
  "tools": {
    "allowlist": ["read_file", "search_files", "web_search"],
    "blocklist": ["dangerous_command"],
    "approval_required": ["exec", "write_file"]
  }
}
```
