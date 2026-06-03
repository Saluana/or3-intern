# Installation

Two ways to get OR3 Intern: download a prebuilt binary or build from source.

## Option 1: Install script

Run the install script from the repo root:

```bash
./scripts/install-cli.sh
```

This builds the binary and installs it to `$GOPATH/bin`. A symlink is also created in `/usr/local/bin` so you can run `or3-intern` from anywhere.

## Option 2: Build from source

You need Go 1.25 or later with CGO enabled.

```bash
CGO_ENABLED=1 go build -o ./or3-intern ./cmd/or3-intern
```

This produces a binary called `or3-intern` in the current directory.

## Docker

Use Docker Compose:

```bash
docker compose up
```

Or build with the Dockerfile:

```bash
docker build -t or3-intern .
docker run or3-intern
```

## Prebuilt binary

A prebuilt binary is available at `bin/or3-intern` in the repo. You can copy it to somewhere on your PATH.

## Verify

Run this to check the install worked:

```bash
or3-intern version
```

## Next step

Run the [first-run setup](first-run-setup.md) to configure your agent.
