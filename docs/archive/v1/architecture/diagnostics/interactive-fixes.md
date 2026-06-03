# Interactive Fixes

Interactive fixes require user choices and are available through `or3-intern doctor --fix --interactive`.

## How interactive fixes work

`ApplyInteractiveChoice` takes a finding, a user choice string, and optional values (like allowlist items). It returns whether the config was changed.

Source: `internal/doctor/fix.go:111-201`

## Available interactive fixes

### service.secret_missing / service.secret_weak

Choices:
- `generate` - generates a 32-byte random secret
- `disable` - disables the service

### webhook.secret_missing

Choices:
- `generate` - generates a random secret
- `disable` - disables the webhook

### service.public_bind

Choices:
- `loopback` - changes bind to `127.0.0.1:9100`

### webhook.public_bind

Choices:
- `loopback` - changes bind to `127.0.0.1:8765`

### security.secret_store_disabled_with_integrations

Choices:
- `enable` - enables secret store, sets required, generates key file
- `skip` - leaves as-is

### privileged-exec.sandbox_disabled

Choices:
- `disable_privileged` - turns off privileged tools
- `enable_sandbox` - turns on sandbox with default bubblewrap path
- `skip` - leaves as-is

### privileged-exec.bubblewrap_missing

Choices:
- `disable_privileged` - turns off both privileged tools and sandbox
- `set_path` - sets bubblewrap path from the provided value (requires allowlist argument)
- `skip` - leaves as-is

### channels.invalid_ingress

Choices (with optional allowlist values):
- `pairing` - restrict to paired devices
- `allowlist` - restrict to specific users/chats (values required)
- `open` - open access
- `deny` - block all inbound
- `disable` - disable the channel entirely

Source: `internal/doctor/fix.go:203-256` (applyChannelIngressChoice, applyChannelIngress)

Channel ingress fixes are applied per channel (telegram, slack, discord, whatsapp, email), setting the inbound policy, open access flag, and allowlist.

Source: `internal/doctor/fix.go:221-256`
