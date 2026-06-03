# Installing the CLI

The `install-cli.sh` script builds and installs OR3 Intern in one step.

## How it works

Run from the repo root:

```bash
./scripts/install-cli.sh
```

The script does three things:

1. Builds the binary with `go build`
2. Installs it to `$GOPATH/bin`
3. Creates a symlink in `/usr/local/bin` so it's on your PATH

## Requirements

- Go 1.25+ with CGO enabled
- `$GOPATH/bin` should be in your PATH

## Verify

```bash
or3-intern version
```

If you see the version number, it worked.
