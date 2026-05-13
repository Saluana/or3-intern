# Automatic Fixes

Automatic fixes are applied by the doctor without user input when running `or3-intern doctor --fix`.

## What gets fixed automatically

Source: `internal/doctor/fix.go:15-101` (ApplyAutomaticFixes)

| Finding ID | Fix |
|-----------|-----|
| `filesystem.db_parent_missing` | Creates the database parent directory |
| `filesystem.artifacts_dir_missing` | Creates the artifacts directory |
| `security.audit.key_missing` | Generates a new audit key file |
| `security.secret_store.key_missing` | Generates a new secret-store key file |
| `approvals.key_missing` / `approvals.key_path_missing` | Generates a new approvals key file |
| `quotas.unset` | Restores default hardening quotas |
| `privileged-exec.bubblewrap_path_empty` | Sets bubblewrapPath to default "bwrap" |
| `service.public_bind` | Changes service bind to `127.0.0.1:9100` (hosted profiles only) |
| `service.unauthenticated_pairing_remote` | Disables unauthenticated remote pairing |
| `service.shared_secret_role_unsafe` | Limits shared-secret role to "service-client" |
| `service.max_capability_unsafe` | Limits service capability to "safe" |
| `webhook.public_bind` | Changes webhook bind to `127.0.0.1:8765` (hosted profiles only) |
| `channels.invalid_ingress` | Sets channel inbound policy to "deny" |

## Config mutation

When a fix changes config values (not just filesystem), the updated config is saved back to the config file. If no config path is provided and a config change is needed, the operation fails.

Source: `internal/doctor/fix.go:92-99`

## Key file generation

Key files are generated using `security.LoadOrCreateKey` which:
1. Tries to load the existing key file
2. If missing, generates a 32-byte random key
3. Creates the parent directory with 0o700 permissions
4. Writes the key as base64 with 0o600 permissions

Source: `internal/security/store.go:31-58` (LoadOrCreateKey)

## Secret generation

`GenerateSecret` creates a 32-byte random value encoded as base64 URL-safe. Used for service secrets and webhook secrets.

Source: `internal/doctor/fix.go:103-109`

## Fix listing

After applying fixes, the report includes an `FixesApplied` array with the finding ID and a summary of what was done.

Source: `internal/doctor/report.go:59-62` (AppliedFix)
