# OR3 Connect

Connect a macOS or Linux computer to OR3 Cloud:

```sh
npx @or3/connect
```

To run OR3 Intern locally without installing Go or changing your shell PATH:

```sh
npx @or3/connect intern
```

That downloads the verified OR3 Intern release and opens its guided local
setup. Add an Intern command after `intern` when needed, for example
`npx @or3/connect intern chat`.

To connect an installed external agent with the guided setup, choose its
runtime explicitly:

```sh
npx @or3/connect openclaw
npx @or3/connect hermes
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

Remote access requires an OR3 Cloud account. Local/offline OR3 remains
account-free.
