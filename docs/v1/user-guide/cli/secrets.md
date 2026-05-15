# Secrets

`secrets` manages encrypted secret references stored in SQLite.

## Supported subcommands

| Command | Description |
| --- | --- |
| `set <name> <value>` | Store or replace a secret |
| `delete <name>` | Remove a stored secret |
| `list` | List stored secret names |

## Examples

```bash
or3-intern secrets set github-token "ghp_abc123..."
or3-intern secrets list
or3-intern secrets delete old-token
```

## How secrets are used

The practical pattern is to store a secret under a stable name and let config, tools, or skills resolve that secret reference when needed. The CLI does not expose a `get` command for reading the value back out.

## Notes

- `list` shows names only, never values
- secret names should be stable and descriptive
- deleting a secret can break skills or configs that depend on that reference
