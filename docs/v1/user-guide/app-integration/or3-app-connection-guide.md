# OR3 App Connection Guide

Use this guide when you want to run `or3-intern` on a computer and connect to it from OR3 App on web, Electron, iOS, or Android.

## What needs to be running

On the computer you want to control, install or run the intern CLI from the `or3-intern` repo:

```bash
./scripts/install-cli.sh
or3-intern version
```

Run setup once if this is a fresh checkout:

```bash
or3-intern setup
```

Start the authenticated service API:

```bash
or3-intern service
```

The default service address is `http://127.0.0.1:9100`. That only works when the service is actually listening on localhost on the same device as the app. If the service logs `or3-intern service listening on 100.x.x.x:9100`, a LAN IP, or a Tailscale name, enter that exact address in OR3 App instead.

## Run OR3 App

From `or3-app`, install dependencies once:

```bash
bun install
```

For the web app during development:

```bash
bun run dev
```

Open `http://127.0.0.1:3060` on the same computer, or `http://<app-host>:3060` from another device on the private network.

For Electron development, keep the Nuxt dev server running and start Electron in a second terminal:

```bash
bun run electron:dev
```

For packaged/static Electron:

```bash
bun run electron
```

For Capacitor mobile builds:

```bash
bun run build:web
bun run cap:sync
bun run cap:open:ios
```

or:

```bash
bun run build:web
bun run cap:sync
bun run cap:open:android
```

When the app runs on a real phone or simulator, do not use `127.0.0.1` for the intern service unless the service is also running inside that same device environment. Use the computer's LAN or Tailscale address instead.

## Pairing path A: app shows the code

This is the easiest path when you are already in OR3 App.

1. Open OR3 App and go to `/settings/pair`.
2. Enter the computer address, for example `http://127.0.0.1:9100`, `http://192.168.1.23:9100`, or a Tailscale address.
3. Enter friendly names for the computer and this device.
4. Press **Get legacy pairing code**.
5. On the computer, approve the code the app shows:

    ```bash
    or3-intern pairing approve-code 123456
    ```

6. Leave the app screen open. It polls the exchange endpoint and should connect automatically. If needed, press **Try now**.

After this succeeds, the app stores a paired-device token locally. The Settings pages now distinguish between a saved pairing and a live connection: **Connected** means the service was reachable and authenticated; **Unavailable** means the token exists but the host is offline, blocked, or unauthorized.

## Pairing path B: CLI prints the code

Use this path when you are at the computer first and want the CLI to create the request.

On the computer:

```bash
or3-intern pair --auto
```

Choose the access level and device name. The CLI checks readiness, applies safe fixes when possible, and prints a formatted six-digit code, for example:

```text
123-456
```

In OR3 App:

1. Go to `/settings/pair`.
2. Enter the computer address and device names at the top of the card.
3. In the CLI-code pairing area, enter the code printed by `or3-intern pair --auto`.
4. Press **Connect**.

The app accepts either `123456` or `123-456` and exchanges the approved request for a device token.

Do not run `or3-intern pairing approve-code` for this path. `pair --auto` already created the waiting request; the app connects by exchanging the code directly. If you need the older request-ID flow, use `or3-intern pair --manual`.

## Secure QR upgrade

The short-code flow is the bootstrap path. Once it works, use the **Connect with QR** card on `/settings/pair` to enroll the app as a signed secure device record.

1. Keep the existing app connection active.
2. Show or copy a fresh secure QR payload from the connected computer flow.
3. In OR3 App, press **Scan** or paste the QR text and press **Use text**.
4. Confirm that the card reports secure enrollment for the computer.

QR enrollment is an upgrade after legacy connectivity. If the app has no active host address yet, the QR card intentionally asks you to finish the one-time connection first.

## Disconnect and revoke

To make the app forget its local pairing, open OR3 App and use **Disconnect this app** on `/settings` or `/settings/pair`. This removes the saved host and token from the app only.

To revoke the device on the computer, list devices and disconnect the device ID:

```bash
or3-intern pair list
or3-intern connect-device disconnect <device-id>
```

The lower-level device command also works:

```bash
or3-intern devices list
or3-intern devices revoke <device-id>
```

Use both sides when you want a clean reset: disconnect in the app, then revoke from the computer.

## Troubleshooting

If the app says **Unavailable**, the saved token exists but the app cannot currently verify the host. Check that `or3-intern service` is still running, the address in Settings is reachable from that device, the service port is `9100`, and LAN/Tailscale/firewall rules allow the connection.

If the browser console shows `ERR_CONNECTION_REFUSED` for `http://127.0.0.1:9100`, nothing is listening on localhost. Use the address from the service log instead, for example `http://100.x.x.x:9100` for Tailscale or `http://192.168.x.x:9100` for LAN.

If the browser or Electron app cannot reach a LAN or Tailscale host, check the app CSP. The current app policy allows `connect-src 'self' http: https: ws: wss:` so private HTTP service calls and WebSocket/SSE-style flows can work in development and Electron.

If the screen goes blank after a build, check worker CSP first. OR3 App needs `worker-src 'self' blob:` for client-side workers used by the Nuxt/Vite runtime and app dependencies.

If icons disappear, verify that the local Nuxt Icon bundle is enabled and the local Iconify packages are installed. The app intentionally uses `@nuxt/icon` with `provider: "none"`; icons should come from the local client bundle, not `api.iconify.design`. A good production build prints a line like:

```text
Nuxt Icon client bundle consist of 161 icons
```

If a phone cannot use `http://127.0.0.1:9100`, that is expected. `127.0.0.1` points at the phone itself. Use the computer's private network address instead.

## Verification commands

Use these before handing a build to someone else:

```bash
cd or3-app
bun vitest run tests/unit/pairing.test.ts tests/unit/or3-api.test.ts tests/unit/configure-settings.test.ts tests/unit/approvals-composable.test.ts tests/unit/secure-connections.test.ts tests/unit/use-secure-connection-session.test.ts tests/unit/electron-security.test.ts
bun run typecheck
bun run build:web
```

```bash
cd or3-intern
go test ./internal/approval ./cmd/or3-intern ./internal/secureconn ./internal/db
```
