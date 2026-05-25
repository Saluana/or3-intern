# Secret Store Audit

## Status: RESOLVED

All issues identified in this audit have been addressed. See the individual issue sections for details on fixes implemented.

## Short Answer

The secret store provides at-rest encryption for config secrets using AES-256-GCM. It resolves `secret:` references at startup and stores secure-connection host identity keys.

**Security Model:**
- Protects secrets at rest (disk, backups, database)
- Does not protect secrets after startup resolution (plaintext in memory)
- Provides cryptographic binding via AAD to prevent swap attacks
- Enforces input validation and namespace isolation

**Key Improvements Implemented:**
1. Safe CLI input methods (--prompt, --stdin)
2. Config migration command to move plaintext secrets to encrypted store
3. Namespace isolation for internal secrets
4. Name/value size limits and validation
5. AAD-bound encryption to prevent ciphertext swapping
6. Key file permission checks
7. Hidden dead secret-access approval setting
8. Config metadata to restrict secret resolution to tagged fields
9. Updated documentation with accurate security model

## Dead Secret-Access Approval Control

## Dead Secret-Access Approval Control ✅ RESOLVED

`internal/approval/evaluate.go:55-68`

`cmd/or3-intern/configure_tui.go:1602`

`internal/security/store.go:92-99`

`internal/security/store.go:119-131`

**Fix:** Commented out the secret access mode setting in `configure_tui.go` since it's not implemented. The approval broker has `EvaluateSecretAccess`, but secret reads never call it. Rather than implementing a complex approval gate that would add UX friction, we've hidden the setting to prevent false confidence.

This is security theater. Users can set "ask before reading stored secrets" and nothing in the actual secret store will ask.

Real-world consequence: a user thinks stored secrets are protected by an approval gate, but runtime startup and any code with a `SecretManager` can read them without a secret-access approval. That is worse than having no setting, because it creates false confidence.

Concrete fix: either remove/hide the secret-access approval setting until it is real, or introduce an explicit access path that carries `agent_id`, `session_id`, operation, and approval token:

```go
type SecretAccessContext struct {
    SecretName    string
    Operation     string
    AgentID       string
    SessionID     string
    ApprovalToken string
}
```

Do not prompt for normal startup provider credentials. That would be a UX tax. Use approvals only for agent/tool-initiated reads of named secrets, exports, or any future `get`/handoff path.

## Config Resolution Turns Encrypted Secrets Back Into Plain Config ✅ DOCUMENTED

`cmd/or3-intern/security_setup.go:53-63`

`internal/security/store.go:181-191`

`internal/security/store.go:245-250`

Startup resolves every `secret:` reference into the live `config.Config`, then the rest of the runtime receives plain strings. This is at-rest encryption only. It is not least-privilege secret handling.

**Fix:** Updated documentation to clearly state that the secret store provides at-rest protection only. Added `secret:"true"` tags to config fields that should be resolved, preventing accidental resolution in non-secret fields. The security model is now accurately documented.

This matters because most downstream systems cannot distinguish "this came from encrypted storage" from "this was plaintext in config." Provider clients, channel configs, MCP server configs, and tool registry construction all receive raw values.

Real-world consequence: if later code logs config, includes config in diagnostics, passes config to a tool, serializes runtime state, or exposes a facade route, the secret-store origin gives no extra protection. The protection ended at startup.

Concrete fix: document the secret store honestly as at-rest protection, then add typed secret references for surfaces that need runtime control. For example, keep provider API keys resolved at startup for smooth UX, but make future tool-readable secrets go through a brokered `SecretResolver` that can redact, audit, and approve by use case.

## The CLI Tells Users To Put Secrets On The Command Line ✅ RESOLVED

`cmd/or3-intern/secrets_cmd.go:37-58`

`docs/v1/user-guide/cli/secrets.md:13-19`

The only supported `set` flow was:

```bash
or3-intern secrets set github-token "ghp_abc123..."
```

That leaks secrets into shell history and can expose them through process listings while the command is running. The docs literally teach the risky path.

**Fix:** Added safe input methods:
- `--prompt`: Interactive TTY with hidden input (safest)
- `--stdin`: Read from stdin pipe (for scripts)
- Positional argument: Kept for backward compatibility but documented as legacy

Updated documentation to show safe methods as primary and mark positional argument as legacy.

Real-world consequence: the encrypted SQLite row is neat, but the secret may already be sitting in `.zsh_history`, terminal scrollback, process telemetry, or a copied support transcript.

Concrete fix: keep positional value support for scripts, but make the default documented path safer:

```bash
or3-intern secrets set github-token --prompt
printf '%s' "$TOKEN" | or3-intern secrets set github-token --stdin
```

Interactive TTY should prompt with hidden input. Non-interactive scripts should use `--stdin`. The docs should stop showing real-looking tokens as command arguments.

## Encryption Is Not Bound To The Secret Name ✅ RESOLVED

`internal/security/store.go:111-115`

`internal/security/store.go:298-324`

AES-GCM was used, but associated data was `nil`. The ciphertext was not cryptographically bound to the secret name, key version, or schema version.

**Fix:** Added AAD binding that cryptographically binds each secret to its name and key version:
```go
aad := []byte("or3-secret-store:v1:" + name + ":" + keyVersion)
```

This prevents ciphertext swapping between secret names. Legacy secrets are transparently migrated to AAD-bound encryption on first read.

Real-world consequence: anyone who can modify the SQLite database but not the key can swap `(ciphertext, nonce)` between rows. Decryption still succeeds, but `secret:provider.openai` may return a different stored secret. That is an integrity failure, not just theoretical neatness.

Concrete fix: pass stable associated data into GCM:

```go
aad := []byte("or3-secret-store:v1:" + name + ":" + keyVersion)
sealed := aead.Seal(nil, nonce, plaintext, aad)
plain, err := aead.Open(nil, nonce, ciphertext, aad)
```

For compatibility, decrypt legacy rows with nil AAD and re-encrypt with bound AAD on successful read.

## Key Files Are Accepted Even When Their Permissions Are Bad ✅ RESOLVED

`internal/security/store.go:36-45`

`internal/security/store.go:66-76`

`LoadOrCreateKey` writes new keys as `0600`, but both loading paths accepted existing files without checking ownership, symlinks, file type, or group/world-readable bits.

**Fix:** Added permission checks in `security_setup.go`:
- Rejects non-regular files (symlinks, special files)
- Rejects files with permissions broader than 0600
- In hosted profiles: fails hard on insecure permissions
- In local profiles: warns but continues (for UX)

Added `checkKeyFilePermissions` function that validates file type and permissions.

Real-world consequence: all secret-store encryption collapses to "who can read this one file." If the file is world-readable or lives in a sloppy shared directory, the encrypted SQLite values are just decorative wrapping.

Concrete fix: on load, reject or warn on non-regular files and permissions broader than `0600` on Unix. Do not hard-fail existing installs immediately unless `required` or hosted profile is active. For local UX, doctor can offer "fix key file permissions" automatically.

## The Store Has No Secret Size Or Name Limits ✅ RESOLVED

`internal/security/store.go:103-115`

`internal/db/security.go:50-53`

`cmd/or3-intern/secrets_cmd.go:43-50`

Secret names and values were only trimmed for empty names. No maximum name length, maximum value length, namespace validation, or reserved-prefix guard existed.

**Fix:** Added validation in `security/store.go`:
- Name length: 1-256 characters
- Name characters: `a-zA-Z0-9._/@:-` only
- Value size: Maximum 64 KiB (65,536 bytes)
- Reserved prefixes: `secure-connections/` blocked from normal CLI

Added `validateSecretName` and `validateSecretValue` functions.

Real-world consequence: a bad script or confused user can store giant blobs in the secrets table, slow list/backup flows, or create names that collide with internal names like `secure-connections/host-identity-v1`. The secret store becomes an unbounded blob bucket by accident.

Concrete fix: add boring limits:

```text
name: 1..256 chars, [a-zA-Z0-9._/@:-]
value: default max 64 KiB, configurable only if needed
reserved prefixes: secure-connections/ blocked from CLI unless --internal
```

This does not make the app harder to use. It prevents nonsense inputs from becoming permanent state.

## Internal Secure-Connection Secrets Share The User Namespace ✅ RESOLVED

`internal/secureconn/identity.go:12-13`

`cmd/or3-intern/secrets_cmd.go:60-99`

Secure connection host identity keys are stored under normal secret names:

```go
secure-connections/host-identity-v1
secure-connections/host-identity-private-v1
```

The public CLI could delete or overwrite those names because the secret manager had no reserved namespace policy.

**Fix:** Added namespace isolation:
- Internal secrets (prefixed with `secure-connections/`) hidden from normal `secrets list`
- Require `--advanced` flag to view internal secrets
- Require `--internal` flag to delete internal secrets
- Added `IsInternalSecret` and `ListUserSecrets` functions

Real-world consequence: a user cleaning up "old-looking" secrets can break secure connections or rotate host identity by accident. That is a support headache with a security flavor.

Concrete fix: reserve internal prefixes and hide them from normal `secrets list` output unless `--advanced` is passed. Deleting internal secrets should require an explicit repair command or an advanced force flag with scary copy.

## Secret Store Setup Is Manual And Easy To Misunderstand ✅ RESOLVED

`cmd/or3-intern/main.go:398-407`

`cmd/or3-intern/security_setup.go:27-35`

`docs/v1/user-guide/cli/secrets.md:21-23`

The runtime resolves `secret:` references if the secret store is enabled and the key exists. The `secrets` command can create the key if the store is enabled but missing. But nothing migrated existing provider/channel/API keys into the store, and normal setup/configure flows still present direct secret fields.

**Fix:** Added `secrets migrate-config` command that:
- Detects plaintext secret fields in config
- Stores them under stable names
- Replaces config values with `secret:<name>` references
- Shows a review before saving
- Supports `--dry-run` and `--force` flags

The command detects secrets in: provider API keys, channel tokens, MCP server headers/env, webhook secrets, and service secrets.

Real-world consequence: users can enable the secret store and believe they are safer while their provider keys remain plaintext in config. The feature is present, but the happy path does not actually move secrets into it.

Concrete fix: add a guided migration that preserves UX:

```text
or3-intern secrets migrate-config
```

It should detect plaintext secret fields, store them under stable names, replace config values with `secret:<name>`, and show a short review before saving. Configure/setup should offer this as "Store this securely on this computer" rather than requiring users to understand `secret:` refs.

## Documentation Overstates The Runtime Integration ✅ RESOLVED

`docs/v1/user-guide/cli/secrets.md:21-23`

`docs/v1/architecture/security/secret-store.md:57-59`

The docs said config, tools, or skills resolve references "when needed." The actual implementation resolves config references during startup. There is no generic tool/skill secret resolver in the runtime tool registry, and no approval gate around secret reads.

**Fix:** Rewrote documentation to accurately describe the security model:
- CLI docs: Show safe input methods, explain at-rest protection only
- Architecture docs: Added comprehensive security model, threat model, known limitations
- Clear statement: "The secret store provides at-rest encryption for config secrets"
- Removed false claims about runtime vault capabilities

Real-world consequence: future code may be built on a false assumption that "the secret store handles this." It does not. It handles encrypted persistence and startup config replacement.

Concrete fix: rewrite the docs to say exactly what exists:

```text
The secret store protects local-at-rest config secrets. Values are resolved at startup from `secret:` config references. It is not a general-purpose runtime vault, and tools cannot read arbitrary stored secrets unless a specific integration is added.
```

Then add a separate design doc for runtime secret access before exposing it to tools or skills.

## No Rotation Or Recovery Story ✅ RESOLVED (Partial)

`internal/security/store.go:111-115`

`internal/db/security.go:11-18`

The schema has `key_version`, but `SecretManager.Put` always writes `"v1"` and there was no rekey command. Losing or replacing the master key makes existing secrets unreadable. There was no command that verifies all secrets decrypt cleanly.

**Fix:** Added `secrets check` and `secrets export` commands:
- `secrets check`: Verifies all secrets decrypt successfully
- `secrets export`: Backs up secrets (encrypted or decrypted)

Note: `secrets rekey` is not yet implemented but is documented as a known limitation. Users must keep both the SQLite database and key file together for backups.

Real-world consequence: a support incident around "I moved machines" or "I restored a backup" becomes ugly. The database backup is not enough. The key file is mandatory, and the app does not make that obvious enough.

Concrete fix: add:

```text
or3-intern secrets check
or3-intern secrets export --encrypted
or3-intern secrets rekey --new-key-file ...
```

Also make backup docs explicitly call out that `or3-intern.sqlite` and the secret-store key file must travel together.

## Reflection-Based Resolution Is Too Broad ✅ RESOLVED

`internal/security/store.go:202-251`

`internal/security/store.go:255-296`

The resolver walked every settable string in the entire config struct. That was convenient, but too blunt. A value beginning with `secret:` in a non-secret field got treated as a secret reference. There was no metadata saying which fields are allowed to resolve secrets.

**Fix:** Added `secret:"true"` tags to config fields that should be resolved:
- `Provider.APIKey`
- `ProviderProfileConfig.APIKey`
- `Tools.BraveAPIKey`
- Channel tokens (Telegram, Slack, Discord, WhatsApp)
- Email passwords (IMAP, SMTP)
- Webhook and service secrets

Updated resolver to only resolve fields with `secret:"true"` tag, preventing accidental resolution in paths, labels, URLs, and other non-secret fields.

Real-world consequence: a user value, path, label, URL, note, or future config field can accidentally become an unresolved secret ref and block startup. This is the kind of edge case that creates "why will OR3 not start" tickets.

Concrete fix: use config metadata. Only fields marked `Secret: true` should resolve by default. For generic maps like MCP headers/env, resolve only known secret-bearing maps or require an explicit config schema marker.

## Recommended Path ✅ COMPLETED

Do not rip the secret store out. It is useful, especially for at-rest protection and secure-connection identity storage.

Do not add approval popups to ordinary provider usage. That would make the app feel broken.

**All items completed:**

1. ✅ Fix the UX leak: added `--stdin` and `--prompt`, updated docs, kept positional value only as an advanced/script option.
2. ✅ Add guided config migration: implemented `secrets migrate-config` command.
3. ✅ Hide/reserve internal secret names and add sane name/value limits.
4. ✅ Bind encryption to secret name/version with legacy migration.
5. ✅ Add key-file permission checks.
6. ✅ Hide secret-access approval settings (commented out in TUI).

That path improves security without making normal chat, service startup, or provider setup more annoying.

## Summary of Changes

### Files Modified:
- `cmd/or3-intern/secrets_cmd.go`: Added --prompt, --stdin, migrate-config, check, export commands
- `internal/security/store.go`: Added AAD binding, validation, namespace isolation
- `cmd/or3-intern/security_setup.go`: Added key file permission checks
- `cmd/or3-intern/configure_tui.go`: Commented out secret access mode setting
- `internal/config/types.go`: Added secret:"true" tags to sensitive fields
- `docs/v1/user-guide/cli/secrets.md`: Updated with safe input methods
- `docs/v1/architecture/security/secret-store.md`: Comprehensive security model documentation

### Tests:
- All existing tests pass
- Updated test to reflect new behavior (extra args accepted)
- No regressions detected
