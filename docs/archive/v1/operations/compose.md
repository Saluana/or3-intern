# Docker Compose

The `compose.yaml` file sets up OR3 Intern with all the right mounts and environment variables.

## What it mounts

- `./config` — your config file
- `./data` — SQLite database and other data
- The repo root — as the working directory

## Environment

Use a `.env` file for secrets. The compose file reads from it automatically.

## Service secret

The default service secret in the compose file is:

```
change-me-local-compose-secret
```

**Change this for production.** Set `OR3_SERVICE_SECRET` in your `.env` file.

## Running

```bash
docker compose up
```

For background mode:

```bash
docker compose up -d
```

## Stopping

```bash
docker compose down
```
