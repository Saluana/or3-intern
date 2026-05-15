# Upgrade guide

Upgrading OR3 Intern to a new version is straightforward.

## From source

```bash
git pull
CGO_ENABLED=1 go build -o ./or3-intern ./cmd/or3-intern
./scripts/restart-service.sh restart
```

## With Docker

```bash
docker compose pull
docker compose up -d
```

This pulls the latest image and restarts the container.

## With Docker (build locally)

```bash
git pull
docker compose build
docker compose up -d
```

## Major version upgrades

Check for migration notes when upgrading major versions. These will be posted in the release notes. Some upgrades may require database migrations, which run automatically on first start.

## Verifying the upgrade

```bash
or3-intern version
```

Check that the version matches what you expect.
