# Docker

The Dockerfile uses a multi-stage build.

## Build stages

**Stage 1: Build**

Uses `golang:1.25-bookworm` to compile the binary. CGO is enabled for SQLite support.

**Stage 2: Run**

Uses `debian:bookworm-slim` as the runtime image. It's small and secure.

## Running

```bash
docker build -t or3-intern .
docker run -p 9100:9100 or3-intern
```

## Important details

- Exposes port 9100
- Default command is `or3-intern service`
- Expects config at `/config/config.json`
- Works from the `/workspace` directory
- Mount your config and data directories as volumes

## Example with volumes

```bash
docker run -p 9100:9100 \
  -v ~/.or3-intern:/config \
  -v $(pwd):/workspace \
  or3-intern
```
