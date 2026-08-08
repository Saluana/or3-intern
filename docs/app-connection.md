# Connect OR3 App to a local Intern service

This is the supported local app path. It does not use OR3 Connect, a central
Cloud endpoint, a VPN, or a manually edited `.env` file.

## Start the service

From a configured Intern installation:

```bash
or3-intern health
or3-intern service
```

The service listens on its configured loopback address and requires its normal
service authentication. Keep this terminal open while pairing, or use your
usual process manager for a local development machine.

## Pair the app

For the shortest local flow, create a one-time device token:

```bash
or3-intern devices create
```

Copy the displayed token into OR3 Chat's add-host/device screen. The token is
shown once; use `or3-intern devices list` to review or revoke devices later.

For an approval-based flow instead:

```bash
or3-intern pairing request
```

Approve the request with the displayed code, then finish the app's pairing
screen. Keep the approval code private and use a fresh request if it expires.

The service API and authentication boundaries are documented in
[api-reference.md](api-reference.md). For a deliberately advanced tailnet
deployment, see [tailscale-service-guide.md](tailscale-service-guide.md).

## Disconnect or recover

Revoke a device from the app or with the corresponding `devices revoke`
command. If the service is unreachable, run `or3-intern health` locally first;
the command reports configuration and permission problems without printing
credentials.

Remote Connect is currently withheld from managed Cloud. Do not substitute a
bare `npx @or3/connect` command for this local pairing flow; see
[Connect release status](connect-release-status.md) for the current decision.

