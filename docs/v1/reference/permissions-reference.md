# Permissions reference

The agent can request permissions to do different things. These are managed by the safety system.

## Permission types

| Permission | What it allows |
|---|---|
| `exec` | Run commands and scripts on the system |
| `files` | Read, write, and edit files |
| `network` | Access the internet (web fetch, search) |
| `memory` | Read and write to long-term memory |
| `admin` | Change configuration and settings |

## How permissions work

When the agent wants to do something that needs a permission, the approval system checks:

1. Is the tool allowed in the config?
2. Does the action match a blocked pattern?
3. What is the current approval mode?

## Approval modes

| Mode | Behavior |
|---|---|
| `relaxed` | Auto-approve all tool calls |
| `normal` | Ask for approval on sensitive tools (exec, files, admin) |
| `strict` | Ask for approval on every tool call |

## Denied actions

If the agent tries something it's not allowed to do, the tool call is blocked. The agent gets a message explaining why and can ask for a different approach.

## Changing permissions

Update the `safety` section in your config file. Or use the `configure` command to change settings interactively.
