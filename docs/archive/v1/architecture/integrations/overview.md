# Integrations Overview

OR3 Intern connects to external systems through integrations. These are the components that let OR3 use external tools, skills, AI providers, and storage.

## Integration Types

### Skills (`internal/skills/`)
Skills are markdown-defined capabilities with optional executable entrypoints. They are discovered from local directories and installed from the ClawHub registry. Skills declare their permissions, runtime requirements, and dependencies in YAML front matter within `SKILL.md` files.

### ClawHub (`internal/clawhub/`)
ClawHub is a remote registry for skills. The client handles search, download, installation, and integrity verification of managed skills. Installed skills are fingerprinted and scanned for security issues.

### MCP (`internal/mcp/`)
MCP (Model Context Protocol) connects to external servers that expose tools. Servers can use stdio, SSE, or streamable HTTP transports. Discovered tools are registered in OR3's tool registry and can be called during agent turns.

### Providers (`internal/providers/`)
The provider client wraps OpenAI-compatible chat completion and embedding APIs. It handles streaming, fallback, schema sanitization, and host policy enforcement.

### Artifacts (`internal/artifacts/`)
Artifacts persist binary attachments (images, audio, files) from conversations. They are stored on disk and tracked in the database with session-based access control.

### Integrations State (`internal/integrations/`)
A shared state model used by MCP and other integrations to report connection status.

## Integration State

Defined in `internal/integrations/state.go`. States are:

| State | Meaning |
|-------|---------|
| `off` | Integration is disabled |
| `needs setup` | Enabled but not yet connected |
| `connected` | Active and working |
| `degraded` | Connected but with errors or partial success |
| `disabled for safety` | Blocked by security policy |

State is derived from three values: `enabled` (config), `connected` (session active), and `lastError` (failure message).
