# Backup and restore

OR3 Intern stores all its data in a SQLite database. Regular backups protect against data loss.

## What to back up

- **SQLite database** — contains sessions, messages, memory, and all state
- **config.json** — your configuration

Both are at `~/.or3-intern/` by default. You can check the exact path with:

```bash
or3-intern config-path
```

## Backup

Copy the files while the service is stopped. If you back up while running, the database might be inconsistent.

```bash
cp ~/.or3-intern/or3-intern.sqlite ~/backups/
cp ~/.or3-intern/config.json ~/backups/
```

## Restore

1. Stop the service
2. Replace the database file
3. Restart the service

```bash
cp ~/backups/or3-intern.sqlite ~/.or3-intern/
./scripts/restart-service.sh restart
```

## Automate it

Add a cron job to back up regularly. Use `sqlite3` for hot backups if you can't stop the service.
