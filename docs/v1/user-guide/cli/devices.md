# Devices

`devices` manages devices that are already paired, plus pending pairing requests that now exist in the local store.

```bash
or3-intern devices list
or3-intern devices requests pending
```

## Use `devices` for management, not for starting pairing

- Use `or3-intern pair --auto` when you are at the computer and want to start the normal pairing flow.
- Use `or3-intern connect-device` only for the older manual computer-started flow.
- Use `or3-intern pairing approve-code <code>` when the app already shows a 6-digit pairing code.
- Use `devices` after pairing exists and you want to review, approve, deny, rotate, or revoke.
- Use the app's **Disconnect this app** action to forget the local saved token, then revoke from the computer when you want the host trust removed too.

## Subcommands

| Command                        | Description                                            |
| ------------------------------ | ------------------------------------------------------ |
| `list`                         | List paired devices                                    |
| `requests [status]`            | List pairing requests, optionally filtered by status   |
| `approve <pairing-request-id>` | Approve a pending pairing request                      |
| `deny <pairing-request-id>`    | Deny a pending pairing request                         |
| `rotate <device-id>`           | Rotate the paired-device token and print the new token |
| `revoke <device-id>`           | Revoke a paired device immediately                     |

## Examples

```bash
or3-intern devices list
or3-intern devices requests pending
or3-intern devices approve 12
or3-intern devices rotate dev_123
or3-intern devices revoke dev_123
```

## Device roles

Current pairing flows use these roles:

- `viewer` — chat-oriented, lowest access
- `operator` — normal app/device control-plane access
- `admin` — highest local control and management access

The role is chosen during pairing. To change access, re-pair the device or revoke and add it again.
