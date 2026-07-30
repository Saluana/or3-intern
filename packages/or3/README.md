# OR3 Connect

Connect a macOS or Linux computer to OR3 Cloud:

```sh
npx or3 connect
```

With Bun:

```sh
bunx or3 connect
```

The bootstrap downloads checksum-verified `or3-intern` and `cloudflared`
release assets into `~/.or3/bin`, then opens OR3's browser device authorization.
Downloads are streamed, deadline-bounded, and installed atomically under a
shared lock. A valid cached install continues to work offline.

Manage the connection from any fresh terminal:

```sh
npx or3 connect status
npx or3 connect doctor
npx or3 connect disconnect
```

The bootstrap never asks for an API token or puts tunnel credentials in process
arguments.

Remote access requires an OR3 Cloud account. Local/offline OR3 remains
account-free.
