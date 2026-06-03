# File paths

Important file and directory locations.

## Default data directory

`~/.or3-intern/`

This is where config and data live unless you change the path.

## Files

| Path | What it is |
|---|---|
| `~/.or3-intern/config.json` | Main configuration file |
| `~/.or3-intern/or3-intern.sqlite` | SQLite database with all state |
| `~/.or3-intern/secrets/` | Encrypted secret store directory |

## Project files

| Path | What it is |
|---|---|
| `.env` | Local environment variables (not committed) |
| `.env.example` | Example environment variables |
| `.or3/` | Runtime state directory |
| `bin/or3-intern` | Prebuilt binary |

## Config file command

```bash
or3-intern config-path
```

Prints the full path to your config file.
