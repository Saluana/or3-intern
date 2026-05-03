# Using or3-intern Service With Tailscale

This guide is for the setup where:

- `or3-intern service` runs on one machine
- your browser or OR3 app reaches it over your Tailscale tailnet
- you want the setup to feel predictable instead of magical

The short version is:

1. Tailscale gives the machines private `100.x.x.x` addresses.
2. `or3-intern service` must listen on a Tailscale-reachable address, not just `127.0.0.1`.
3. The browser app origin must be explicitly allowlisted.
4. The remote client IP or CIDR must also be explicitly allowlisted.
5. Authentication still happens with shared-secret or paired-device bearer tokens.

If one of those pieces is wrong, it usually looks like a CORS problem, a network timeout, or a `401`/`403` error.

## Mental model

Think of this as three separate checks:

1. Network reachability: can the browser machine reach the service machine on Tailscale?
2. Browser allowlist: does `Origin` exactly match `OR3_SERVICE_TRUSTED_BROWSER_ORIGINS`?
3. Client IP allowlist: does the remote Tailscale IP match `OR3_SERVICE_TRUSTED_BROWSER_CIDRS`?

You need all three.

## The most common working setup

Example:

- Mac mini runs `or3-intern service`
- Mac mini Tailscale IP is `100.64.0.42`
- OR3 app is opened from `http://100.64.0.42:3060`
- service listens on `100.64.0.42:9100`

Use environment like this:

```bash
OR3_SERVICE_ENABLED=true
OR3_SERVICE_LISTEN=100.64.0.42:9100
OR3_SERVICE_SECRET=replace-this-with-a-long-random-secret
OR3_SERVICE_TRUSTED_BROWSER_ORIGINS=http://100.64.0.42:3060
OR3_SERVICE_TRUSTED_BROWSER_CIDRS=100.64.0.0/10
```

Then start the service:

```bash
or3-intern service
```

Why this works:

- the service is reachable on the host's Tailscale IP
- the browser origin exactly matches the app URL
- the client IP range allows Tailscale callers

## Before you start

On the machine running `or3-intern service`:

```bash
tailscale status
tailscale ip -4
```

On the machine opening the OR3 app:

```bash
tailscale status
tailscale ip -4
```

You want both devices online in the same tailnet.

If `tailscale ip -4` does not print a `100.x.x.x` address, stop and fix Tailscale first.

## Step 1: Pick the correct service address

If you leave this at loopback:

```bash
OR3_SERVICE_LISTEN=127.0.0.1:9100
```

then only the same machine can reach it.

For Tailscale access, bind to the service machine's Tailscale IP instead:

```bash
OR3_SERVICE_LISTEN=100.64.0.42:9100
```

Use the actual IP returned by `tailscale ip -4` on that machine.

If you change networks or re-install Tailscale and the IP changes, update this value too.

## Step 2: Match the browser origin exactly

`OR3_SERVICE_TRUSTED_BROWSER_ORIGINS` is strict. It is not a hostname hint. It must match the browser's origin exactly, including scheme and port.

Examples:

- `http://100.64.0.42:3060`
- `http://app.local:3060`
- `https://or3.example.com`

Non-matches:

- service is `http://100.64.0.42:9100` but origin is set to `http://100.64.0.42`
- app is opened on `http://100.64.0.42:3060` but origin is set to `http://127.0.0.1:3060`
- app is opened on `https://...` but origin is allowlisted as `http://...`

If you use more than one app URL, separate them with commas:

```bash
OR3_SERVICE_TRUSTED_BROWSER_ORIGINS=http://100.64.0.42:3060,http://app.local:3060
```

## Step 3: Allow the remote client IP range

`OR3_SERVICE_TRUSTED_BROWSER_CIDRS` controls which remote client IPs may use those trusted origins.

For a Tailscale-only setup, this is the broad but convenient option:

```bash
OR3_SERVICE_TRUSTED_BROWSER_CIDRS=100.64.0.0/10
```

That covers normal Tailscale IPv4 addresses.

If you want to be tighter, you can allow a single device instead:

```bash
OR3_SERVICE_TRUSTED_BROWSER_CIDRS=100.88.12.34
```

The broad `/10` is easier while you are getting the system working. A single-IP allowlist is better once the devices are stable.

## Step 4: Keep auth enabled

Tailscale is private networking, not application auth.

The service still expects:

- a shared-secret bearer token, or
- a paired-device token with the `operator` role

The unauthenticated exceptions are only the pairing bootstrap routes:

- `POST /internal/v1/pairing/requests`
- `POST /internal/v1/pairing/exchange`

That means a working Tailscale connection does not remove the need for:

- `OR3_SERVICE_SECRET`
- device pairing when you want operator access from the app

## Step 5: Pair devices in the least confusing way

If you just want the simple guided path, use:

```bash
or3-intern connect-device
```

That flow checks prerequisites, repairs missing safe defaults when possible, creates a pairing code, and guides you through the access level.

If you want the lower-level commands:

```bash
or3-intern devices requests
or3-intern devices approve <pairing-request-id>
or3-intern devices list
```

Useful rule:

- use `connect-device` when you want the happy path
- use `devices ...` when you are inspecting or repairing state

## Recommended copy-paste setup

On the service machine:

```bash
export OR3_SERVICE_ENABLED=true
export OR3_SERVICE_LISTEN="$(tailscale ip -4 | head -n 1):9100"
export OR3_SERVICE_SECRET="replace-this-with-a-long-random-secret"
export OR3_SERVICE_TRUSTED_BROWSER_ORIGINS="http://$(tailscale ip -4 | head -n 1):3060"
export OR3_SERVICE_TRUSTED_BROWSER_CIDRS="100.64.0.0/10"

or3-intern doctor --strict
or3-intern service
```

This is convenient if the OR3 app is served from the same machine as the service.

If the app runs on a different machine, do not reuse the service machine IP in `OR3_SERVICE_TRUSTED_BROWSER_ORIGINS`. Put the app machine's actual origin there instead.

## Two common layouts

### Layout A: App and service on the same Tailscale host

- app origin: `http://100.64.0.42:3060`
- service: `100.64.0.42:9100`
- trusted origins: `http://100.64.0.42:3060`
- trusted CIDRs: `100.64.0.0/10`

This is the simplest setup.

### Layout B: App and service on different Tailscale hosts

Example:

- service host IP: `100.64.0.42`
- app host IP: `100.72.1.8`
- app origin: `http://100.72.1.8:3060`
- service bind: `100.64.0.42:9100`

Use:

```bash
OR3_SERVICE_LISTEN=100.64.0.42:9100
OR3_SERVICE_TRUSTED_BROWSER_ORIGINS=http://100.72.1.8:3060
OR3_SERVICE_TRUSTED_BROWSER_CIDRS=100.64.0.0/10
```

This is where people often get tripped up: the trusted browser origin belongs to the app URL, not the service URL.

## Fast troubleshooting

### Symptom: page says network error or never connects

Check:

```bash
tailscale ping 100.64.0.42
curl http://100.64.0.42:9100/health
```

If `curl` cannot connect, the problem is reachability or bind address, not auth.

### Symptom: browser shows a CORS error

Usually one of these is wrong:

- `OR3_SERVICE_TRUSTED_BROWSER_ORIGINS`
- `OR3_SERVICE_TRUSTED_BROWSER_CIDRS`
- the app is being opened from a different host/port than you think

Re-check the browser URL exactly.

### Symptom: `401 unauthorized`

The network path works, but auth is missing or invalid.

Check:

- `OR3_SERVICE_SECRET` is set on the service machine
- device pairing completed successfully
- you are not using a stale paired-device token

### Symptom: `403 forbidden`

The caller authenticated, but it does not have the needed role.

For app/operator control, use a paired device token with the `operator` role.

### Symptom: pairing works locally but not over Tailscale

Check all of these together:

- service bound to Tailscale IP, not `127.0.0.1`
- app origin is exact
- client IP is covered by `OR3_SERVICE_TRUSTED_BROWSER_CIDRS`

## A simple decision tree

1. Can the client machine `tailscale ping` the service machine?
2. Can the client machine `curl http://SERVICE_IP:9100/health`?
3. Does the browser URL exactly match `OR3_SERVICE_TRUSTED_BROWSER_ORIGINS`?
4. Does the client Tailscale IP fall inside `OR3_SERVICE_TRUSTED_BROWSER_CIDRS`?
5. Is auth configured with `OR3_SERVICE_SECRET` or a valid paired-device token?

If you answer those in order, the failure point is usually obvious.

## My recommended default

When you are setting this up from scratch, start with this:

```bash
OR3_SERVICE_ENABLED=true
OR3_SERVICE_LISTEN=<service tailscale ip>:9100
OR3_SERVICE_SECRET=<long random secret>
OR3_SERVICE_TRUSTED_BROWSER_ORIGINS=http://<app tailscale ip>:3060
OR3_SERVICE_TRUSTED_BROWSER_CIDRS=100.64.0.0/10
```

Get that working first.

Only after it works should you tighten the CIDR from `100.64.0.0/10` down to a specific device IP.

## Related docs

- [Getting started](getting-started.md)
- [Configuration reference](configuration-reference.md)
- [Internal service REST / HTTP API reference](api-reference.md)
- [CLI reference](cli-reference.md)
