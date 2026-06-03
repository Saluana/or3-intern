# Secrets

`secrets` manages encrypted secret references stored in SQLite.

## Supported subcommands

| Command | Description |
| --- | --- |
| `set <name> [--prompt \| --stdin \| <value>]` | Store or replace a secret |
| `delete <name> [--force] [--internal]` | Remove a stored secret |
| `list [--advanced]` | List stored secret names |
| `check` | Verify stored secrets decrypt successfully |
| `export [--encrypted \| --plaintext] [--force]` | Export encrypted secret records by default |
| `migrate-config [--dry-run] [--force]` | Move plaintext config secrets into the encrypted store |

## Safe Secret Input Methods

**Recommended: Use `--prompt` for interactive use**

```bash
# Interactive prompt with hidden input (safest for terminals)
or3-intern secrets set github-token --prompt
```

**For scripts and automation: Use `--stdin`**

```bash
# Read secret from stdin pipe
printf '%s' "$TOKEN" | or3-intern secrets set github-token --stdin

# Or from a file
cat ~/.token | or3-intern secrets set github-token --stdin
```

**Legacy: Positional argument (not recommended)**

```bash
# This leaks secrets into shell history - avoid when possible
or3-intern secrets set github-token "ghp_abc123..."
```

## Examples

```bash
# Store a secret safely
or3-intern secrets set github-token --prompt

# List only user secrets (hides internal secure-connection keys)
or3-intern secrets list

# List all secrets including internal ones
or3-intern secrets list --advanced

# Delete a secret (with confirmation)
or3-intern secrets delete old-token

# Force delete without confirmation
or3-intern secrets delete old-token --force

# Export encrypted records for backup
or3-intern secrets export

# Explicitly export plaintext values
or3-intern secrets export --plaintext

# Preview config secret migration
or3-intern secrets migrate-config --dry-run
```

## How secrets are used

The practical pattern is to store a secret under a stable name and let config resolve that secret reference at startup. The CLI does not expose a `get` command for reading the value back out.

## Security Model

The secret store provides **at-rest encryption** for config secrets:

- Values are encrypted with AES-256-GCM before storage in SQLite
- Secrets are resolved at startup from `secret:` config references
- After startup, secrets become plaintext in memory like any other config
- The store is **not** a general-purpose runtime vault
- Tools cannot read arbitrary stored secrets unless a specific integration is added

## Secret Name Requirements

- Length: 1-256 characters
- Allowed characters: `a-zA-Z0-9._/@:-`
- Internal secrets (starting with `secure-connections/`) are hidden by default
- Use descriptive names like `github-token`, `openai-api-key`, etc.

## Value Limits

- Maximum size: 64 KiB (65,536 bytes)
- Larger values should be stored in files and referenced by path

## Notes

- `list` shows user secrets by default; use `--advanced` to include internal secrets
- Secret names should be stable and descriptive
- Deleting a secret can break integrations that depend on that reference
- Internal secrets (like secure-connection keys) require `--internal` flag to delete
- Backup requires both the SQLite database and the secret-store key file
