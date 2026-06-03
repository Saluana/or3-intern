# Encrypted Secret Storage

The secret store provides **at-rest encryption** for config secrets using AES-256-GCM.

## Security Model

The secret store protects secrets when they are:
- Stored in the SQLite database
- Backed up or copied
- At rest on disk

The secret store does **not** protect secrets when they are:
- Resolved at startup into config (become plaintext in memory)
- Passed to providers, channels, or tools
- Logged in diagnostics or error messages
- Visible in process memory

## SecretManager

The `SecretManager` handles encrypting, storing, decrypting, and deleting secrets. It requires:
- A database connection (`DB`)
- An encryption key (32 bytes)

Source: `internal/security/store.go:25-28` (SecretManager struct)

## Key management

### Creating or loading a key

`LoadOrCreateKey` loads a key from a file path. If the file doesn't exist, it generates a new 32-byte random key and writes it (base64-encoded) to the file with permissions 0o600. The file's parent directory is created with permissions 0o700.

The function tries to decode the file as base64 first. If the decoded value is at least 32 bytes, the first 32 bytes are used. If the file isn't valid base64 but is at least 32 bytes raw, those raw bytes are used.

Source: `internal/security/store.go:31-58`

### Loading existing key

`LoadExistingKey` only loads an existing key file and returns an error if it doesn't exist. Used when keys must already be configured.

Source: `internal/security/store.go:61-77`

### Key file permissions

Key files are checked for secure permissions:
- Must be regular files (not symlinks or special files)
- Must have permissions 0600 (owner read/write only)
- Insecure permissions trigger warnings or errors in hosted profiles

Source: `cmd/or3-intern/security_setup.go` (checkKeyFilePermissions)

## Encryption

Secrets are encrypted with AES-256-GCM:
1. The master key is derived with HMAC-SHA256 using the label "secrets" to produce a 32-byte AES key
2. A random nonce (12 bytes for GCM) is generated
3. Associated data (AAD) binds the ciphertext to the secret name and key version
4. The plaintext is encrypted with AES-GCM Seal
5. Both ciphertext and nonce are stored

### AAD Binding

Each secret is cryptographically bound to its name and key version using Associated Additional Data (AAD):

```
AAD = "or3-secret-store:v1:<name>:<keyVersion>"
```

This prevents:
- Swapping ciphertext between secret names
- Cross-version replay attacks
- Integrity violations where modification goes undetected

Legacy secrets without AAD are transparently migrated on first read.

Source: `internal/security/store.go` (encryptBlob, decryptBlob, deriveKey)

## Storing secrets

`Put` encrypts the value and stores it in the database with:
- Name (the secret identifier) - validated for length and character set
- Ciphertext (encrypted bytes)
- Nonce (for decryption)
- Version marker ("v1")

### Name validation
- Length: 1-256 characters
- Allowed characters: `a-zA-Z0-9._/@:-`
- Reserved prefixes: `secure-connections/` (internal use)

### Value validation
- Maximum size: 64 KiB (65,536 bytes)

Source: `internal/security/store.go` (Put, validateSecretName, validateSecretValue)

## Retrieving secrets

`Get` fetches the record by name and decrypts the ciphertext:
1. Try decryption with AAD-bound encryption (new format)
2. If that fails, try legacy nil AAD decryption (backward compatibility)
3. On successful legacy decryption, re-encrypt with AAD for future reads

Source: `internal/security/store.go` (Get)

## Secret references in config

Config values can reference secrets using the `secret:` prefix. For example, `secret:brave-api-key` tells the system to resolve the value from the secret store.

### Config metadata

Only fields tagged with `secret:"true"` in the config struct are resolved. This prevents accidental resolution in non-secret fields like paths, labels, or URLs.

Source: `internal/config/types.go` (config structs with secret tags)

### Resolution

`ResolveConfigSecrets` walks the config struct and resolves `secret:` references in tagged fields. `ValidateNoSecretRefs` checks that no unresolved references remain.

**Important**: Secrets are resolved at startup and become plaintext in memory. The secret store is at-rest protection only.

Source: `internal/security/store.go` (ResolveConfigSecrets, ValidateNoSecretRefs)

## Namespace isolation

Internal secrets (like secure-connection keys) are prefixed with `secure-connections/` and:
- Hidden from normal `secrets list` output
- Require `--advanced` flag to view
- Require `--internal` flag to delete
- Cannot be overwritten by normal CLI operations

Source: `internal/security/store.go` (IsInternalSecret, ListUserSecrets)

## Deleting secrets

`Delete` removes a secret by name from the database. Internal secrets require the `--internal` flag.

Source: `internal/security/store.go` (Delete)

## Listing secrets

`List` returns only the secret names, never the values. `ListUserSecrets` filters out internal secrets for normal CLI output.

Source: `internal/security/store.go` (List, ListUserSecrets)

## Audit logging

The `AuditLogger` provides tamper-evident audit records for secret operations:
- Secret creation/update (`secret.set`)
- Secret deletion (`secret.delete`)
- Config migration (`secret.migrate`)

Source: `internal/security/store.go` (AuditLogger)

## CLI commands

The `secrets` command provides:
- `set`: Store secrets safely with `--prompt` or `--stdin` options
- `delete`: Remove secrets with confirmation
- `list`: View user secrets (or all with `--advanced`)
- `check`: Verify all secrets decrypt successfully
- `export`: Back up encrypted secret records by default; decrypted plaintext export requires `--plaintext`
- `migrate-config`: Move plaintext config secrets to encrypted store

Source: `cmd/or3-intern/secrets_cmd.go`

## Known limitations

1. **At-rest only**: Secrets become plaintext in memory after startup resolution
2. **No runtime access control**: No approval gate for secret reads (setting hidden)
3. **No key rotation**: `secrets rekey` not yet implemented
4. **Single key file**: Losing the key makes all secrets unreadable
5. **Backup requirement**: Both SQLite database and key file must travel together

## Threat model

**Defends against:**
- Disk theft or unauthorized file access
- Database backup exposure
- Accidental secret leakage in config files
- Swap attacks between secret names (via AAD binding)

**Does not defend against:**
- Runtime memory inspection
- Process listing while secrets are in memory
- Logging of resolved config values
- Compromised runtime environment
- Key file theft (with database)
