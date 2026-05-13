# Scope

`scope` links session keys to a shared history scope.

## Supported subcommands

| Command | Description |
| --- | --- |
| `link <session-key> <scope-key>` | Link a session key to a scope |
| `list <scope-key>` | List session keys attached to a scope |
| `resolve <session-key>` | Resolve the scope for a session key |

## Examples

```bash
or3-intern scope link session-a team-alpha
or3-intern scope list team-alpha
or3-intern scope resolve session-a
```

## When to use scopes

- continue related work across multiple sessions
- keep project-specific history grouped under one logical scope
- debug why two sessions do or do not share history

The key mental model is simple: pick a reusable `scope_key`, then link whichever session keys should share that scope.
