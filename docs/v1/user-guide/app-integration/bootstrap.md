# Bootstrap

Bootstrap is the first-time app-to-service connection flow.

## 1. Start the service

```bash
or3-intern service
```

## 2. Discover auth requirements

Before opening protected routes, an app can call:

```http
GET /internal/v1/auth/capabilities
```

That lets the app decide whether it needs pairing only, an auth session, passkeys, or step-up.

## 3. Pair the device

The pairing bootstrap routes are:

- `POST /internal/v1/pairing/requests`
- `POST /internal/v1/pairing/exchange`

For the human approval step, the easiest desktop flow is still:

```bash
or3-intern pairing approve-code 123456
```

That corresponds to the 6-digit code shown in the app.

The app can also consume a request that was created by the CLI. Run:

```bash
or3-intern pair --auto
```

Then enter the printed code in the app's CLI-code pairing section on `/settings/pair`. Use `or3-intern pair --manual` if you need the older request-ID flow.

See [OR3 App Connection Guide](or3-app-connection-guide.md) for the complete web, Electron, iOS, Android, pairing, and disconnect flow.

## 4. Establish session auth if required

After pairing, the app may still need an authenticated session flow such as:

- `POST /internal/v1/auth/session`
- `POST /internal/v1/auth/passkeys/registration`
- `POST /internal/v1/auth/passkeys/login`
- `POST /internal/v1/auth/step-up`

Whether these are required depends on the current auth posture.

## 5. Load bootstrap state

Once authenticated, call:

```http
GET /internal/v1/app/bootstrap
```

This returns the host overview used by OR3 App: pairing/auth status, counts, warnings, and action summaries.

## Useful checks

- `or3-intern devices list` — confirm paired devices exist
- `or3-intern health` — quick readiness check
- `or3-intern status` — safety and access posture summary
- `or3-intern doctor` — deeper readiness troubleshooting
