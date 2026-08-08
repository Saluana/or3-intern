# OR3 Connect

This package contains the advanced OR3 Connect bootstrap and the local Intern
launcher. Remote Connect is **not** a managed-Cloud launch feature yet: the
old `https://or3.chat` default is intentionally disabled. See the [Connect
release status](https://github.com/Saluana/or3-intern/blob/main/docs/connect-release-status.md)
page for the immutable package and binary-asset contract.

When the matching package and Intern release are published, start local OR3
Intern without Go or a PATH edit:

```sh
npx @or3/connect intern
```

The source tree currently targets `@or3/connect@0.1.1`, while npm serves only
the immutable `0.1.0` package. Do not claim the `intern` subcommand is
available from npm until the exact package and matching `or3-intern` release
asset resolve.

For a source checkout today, use `go run ./cmd/or3-intern setup` and
`go run ./cmd/or3-intern chat` instead.

To connect an installed external agent, choose its runtime and an explicitly
verified staging or self-hosted endpoint:

```sh
or3-intern connect openclaw --cloud-url https://staging.example.test
or3-intern connect hermes --cloud-url https://staging.example.test
```

The command checks the runtime, asks before installing or changing anything,
opens the normal OR3 browser approval, configures the loopback API, and keeps
the named Cloudflare tunnel running as a background service. Runtime-owned
model/provider onboarding still happens in OpenClaw or Hermes first.

With Bun:

```sh
bunx @or3/connect
```

The bootstrap downloads checksum-verified `or3-intern` and, for remote Connect,
`cloudflared` release assets into `~/.or3/bin`, then opens OR3's browser device
authorization.
Downloads are streamed, deadline-bounded, and installed atomically under a
shared lock. A valid cached install continues to work offline.

Manage the connection from any fresh terminal:

```sh
npx @or3/connect status
npx @or3/connect doctor
npx @or3/connect disconnect
```

The bootstrap never asks for an API token or puts tunnel credentials in process
arguments.

Those management commands operate on a previously configured advanced
connection. Local/offline OR3 remains account-free; managed Cloud Connect is
withheld until its public staging flow is proved.
