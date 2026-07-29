# OR3 Connect

Connect a macOS or Linux computer to OR3 Cloud:

```sh
npx or3 connect
```

The bootstrap downloads checksum-verified `or3-intern` and `cloudflared`
release assets into `~/.or3/bin`, then opens OR3's browser device authorization.
It never asks for an API token or puts tunnel credentials in process arguments.

Remote access requires an OR3 Cloud account. Local/offline OR3 remains
account-free.
