# Service restart script

The `restart-service.sh` script manages the OR3 Intern service process.

## Usage

```bash
./scripts/restart-service.sh restart
```

## Commands

| Command | What it does |
|---|---|
| `restart` | Stop and start the service |
| `start` | Start the service |
| `stop` | Stop the service |
| `status` | Check if the service is running |

## Options

| Option | What it does |
|---|---|
| `--foreground` | Run in the foreground (no daemon) |
| `--rebuild` | Rebuild the binary before starting |

The `--rebuild` option is useful during development. It detects source file changes and rebuilds automatically.

## Logs

When running in the background, logs are captured by the script. Use Docker for more robust log management in production.
