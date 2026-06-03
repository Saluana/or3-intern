# Running serve mode

Serve mode is the "always-on" mode. It runs all enabled connected apps and automation.

## Start the server

```bash
or3-intern serve
```

This starts everything you've configured:

- Telegram bots
- Slack listeners
- Discord bots
- WhatsApp listeners
- Email handlers
- Cron jobs
- Webhook endpoints
- File watchers

## When to use serve mode

Use serve mode when you want your agent to be available all the time. It listens through connected apps and runs scheduled tasks in the background.

## Connected app setup

Each connected app needs its own setup. For example, a Telegram bot needs a bot token from BotFather. Set these in your config file or environment variables.

## Automation

Serve mode runs cron jobs on schedule. It also starts webhook listeners and file watchers. The agent will respond to triggers as they happen.

## Stopping serve mode

Press Ctrl+C to stop. Or use the restart script:

```bash
./scripts/restart-service.sh stop
```

## Next step

See [troubleshooting](troubleshooting-first-run.md) if something goes wrong.
