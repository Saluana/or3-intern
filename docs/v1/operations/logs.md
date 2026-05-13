# Logs

OR3 Intern logs to stdout by default.

## CLI mode

In chat mode, logs go to the terminal. You'll see the agent's responses and any errors.

## Service mode

When running `or3-intern service`, logs go to stdout. The restart script captures these if you use `--foreground`.

## Docker

```bash
docker compose logs -f
```

The `-f` flag follows new log entries as they appear.

## Log levels

Set the log level in your config:

```json
{
  "logging": {
    "level": "debug"
  }
}
```

Levels: debug, info, warn, error. Debug shows the most detail.
