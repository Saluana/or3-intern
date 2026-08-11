# Remote access with OR3 Connect

## Managed Cloud status

Remote Connect is currently **withheld** from the managed Cloud launch. The
central `https://or3.chat` device endpoint has not passed the required public
staging smoke, so this guide does not provide a bare `npx @or3/connect`
command. Local/offline Intern remains account-free.

Read [Connect release status](connect-release-status.md) before choosing a
package version. Install local Intern with
`npx @or3/connect@0.1.2 intern`, from this checkout with
`./scripts/install-cli.sh`, or use `go run ./cmd/or3-intern ...` for a one-off
source run.

## Advanced staging or self-hosted path

An operator who has independently verified a staging or self-hosted Cloud
endpoint can opt into the existing device flow explicitly:

```bash
or3-intern connect --cloud-url https://staging.example.test
```

For an external runtime, use the same explicit endpoint:

```sh
or3-intern connect openclaw --cloud-url https://staging.example.test
or3-intern connect hermes --cloud-url https://staging.example.test
```

These commands reuse the device approval, named tunnel, service, status,
doctor, and disconnect lifecycle. They configure only the selected runtime's
loopback API; finish model/provider onboarding in the runtime first. The
endpoint must be HTTPS (except exact loopback development URLs), and the
command validates verification URLs before accepting credentials.

The advanced flow opens a browser, shows a matching phrase in both places, and
asks once before installing the background service. It does not require a
pasted access token, VPN, Cloudflare account, or QR scan when the selected
endpoint has the complete device flow configured.

## Commands

```bash
or3-intern connect status
or3-intern connect doctor
or3-intern connect disconnect
or3-intern connect uninstall
```

Useful setup flags:

- `--name "Studio Mac"` sets the name shown in OR3.
- `--no-service` runs only until the terminal closes.
- `--no-browser` prints the URL for a headless computer.
- `--cloud-url` is required for setup/openclaw/hermes and selects a staging or self-hosted OR3 Cloud. It is intentionally never defaulted to `https://or3.chat`.

## Files and services

Connection state lives in `~/.or3-intern/connect` with owner-only permissions.
The tunnel token has its own `0600` file and is passed to cloudflared with
`--token-file`.

macOS installs `/Library/LaunchDaemons/chat.or3.connect.plist`. Linux installs
`/etc/systemd/system/or3-connect.service`. Both run OR3 as the invoking user;
administrator access is used only to add/remove the service definition.

`disconnect` revokes the cloud tunnel and removes local connection state while
leaving the local OR3 workspace and agent configuration untouched.

## Troubleshooting

Run:

```bash
or3-intern connect doctor
```

It checks saved state, the tunnel client, and authenticated remote health
without printing credentials. On macOS, recent service diagnostics are
size-rotated, owner-only, redacted, and shown in bounded form when a check
fails.
