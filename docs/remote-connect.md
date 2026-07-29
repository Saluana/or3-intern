# Remote access with OR3 Connect

Connect this computer to an OR3 Cloud account:

```bash
npx or3 connect
```

No access token, VPN, Cloudflare account, or QR scan is required. The command
opens a browser, shows a matching phrase in both places, then asks once before
installing the background service.

Local/offline use remains account-free.

## Commands

```bash
or3-intern connect
or3-intern connect status
or3-intern connect doctor
or3-intern connect disconnect
or3-intern connect uninstall
```

Useful setup flags:

- `--name "Studio Mac"` sets the name shown in OR3.
- `--no-service` runs only until the terminal closes.
- `--no-browser` prints the URL for a headless computer.
- `--cloud-url` selects a staging or self-hosted OR3 Cloud.

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
without printing credentials. Logs are stored beside the connection state.
