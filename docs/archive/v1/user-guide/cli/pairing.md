# Pairing

`pairing` manages first-class pairing workflows, including app/device pairing and channel-bound identities.

```bash
or3-intern pairing approve-code 123456
```

## The easiest app flow

When the OR3 App shows a 6-digit pairing code:

1. Open the app and tap the action that shows the code.
2. On the computer, run:

    ```bash
    or3-intern pairing approve-code 123456
    ```

3. Go back to the app. It should finish connecting by itself.

This is the main approval flow for app-generated pairing requests.

## When the CLI starts pairing

When you start at the computer instead, run:

```bash
or3-intern pair --auto
```

The command checks readiness, applies safe fixes when possible, then prints a formatted code such as `123-456`. Open OR3 App, go to `/settings/pair`, and enter the code. The app accepts the code with or without the hyphen.

Useful options:

- `--name <name>` sets the device display name.
- `--role viewer|operator|admin` chooses the access level.
- `--json` emits structured output for scripts.
- `--manual` falls back to the older `connect-device` flow.

## Advanced subcommands

| Command                        | Description                                                |
| ------------------------------ | ---------------------------------------------------------- |
| `list [status]`                | List pairing requests                                      |
| `request [flags]`              | Create a pairing request manually                          |
| `approve-code <6-digit-code>`  | Approve the waiting device using the code shown in the app |
| `approve <request-id>`         | Approve a pairing request by request ID                    |
| `deny <request-id>`            | Deny a pairing request                                     |
| `exchange <request-id> <code>` | Exchange an approved pairing code for a device token       |

## Request flags

`pairing request` supports:

- `--role <role>`
- `--name <text>`
- `--origin <text>`
- `--device <id>`
- `--channel <name>` with `--identity <id>`

That advanced path is useful for channel-bound identities such as Slack or other non-app pairing flows.

## Related commands

- `pair --auto` starts the normal pairing flow from this computer.
- `connect-device` remains available for the older manual computer-started flow.
- [OR3 App Connection Guide](../app-integration/or3-app-connection-guide.md) covers the full web, Electron, iOS, Android, pairing, and disconnect flow.
- `devices` manages already paired devices and stored requests.
