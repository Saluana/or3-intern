# Tool reference

These are the built-in tools the agent can use.

| Tool | What it does |
|---|---|
| `Exec` | Run a command or script |
| `ReadFile` | Read a file's contents |
| `SearchFile` | Search within files |
| `WriteFile` | Write content to a file |
| `EditFile` | Make targeted edits to a file |
| `ListDir` | List directory contents |
| `WebFetch` | Fetch a web page as HTML |
| `WebFetchMarkdown` | Fetch a web page as Markdown |
| `WebSearch` | Search the internet |
| `MemorySet` | Save something to memory |
| `MemoryGet` | Retrieve something from memory |
| `MemorySearch` | Search memory by similarity |
| `MemoryRecent` | Get recent memory entries |
| `MemoryPinned` | Get pinned memory entries |
| `SendMessage` | Send a message through a channel |
| `ReadSkill` | Read a skill's instructions |
| `RunSkill` | Execute a skill |
| `RunSkillScript` | Run a skill's script |
| `CronTool` | Manage scheduled tasks |
| `SpawnSubagent` | Start a subagent for parallel work |

Each tool can be allowed or blocked in the config's safety settings.
