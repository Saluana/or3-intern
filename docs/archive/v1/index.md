# OR3 Intern

OR3 Intern is a personal AI agent. You can chat with it, ask it to run tasks, and set up automations. It works in many places:

- **CLI** — chat right in your terminal
- **Web service** — connect the OR3 App or any HTTP client
- **Telegram** — talk to your agent through a bot
- **Slack** — use it from your workspace
- **Discord** — invite it to your server
- **WhatsApp** — message it like a contact
- **Email** — send it tasks by email

## What can it do?

- Run commands and scripts on your machine
- Read, write, and search files
- Fetch web pages and search the internet
- Remember things between conversations
- Use tools and skills you give it
- Run on a schedule (cron, webhooks, file watchers)
- Spawn subagents for parallel work

## First step

Start with the [overview](getting-started/overview.md) to learn more. Then follow the [installation guide](getting-started/installation.md) to get it running.

## Main docs

- [CLI guide](user-guide/cli/overview.md) - local commands, setup, diagnostics, approvals, pairing, and skill management
- [App integration guide](user-guide/app-integration/overview.md) - authenticated `/internal/v1/*` service API usage
- [OR3 App connection guide](user-guide/app-integration/or3-app-connection-guide.md) - run the web, Electron, iOS, or Android app against `or3-intern service`
- [Architecture overview](architecture/overview.md) - how the core runtime, service API, storage, tools, channels, automation, and safety systems fit together
- [Operations guide](operations/overview.md) - Docker, production service mode, monitoring, backups, upgrades, and security checks
- [Reference](reference/command-reference.md) - commands, config, environment variables, tools, events, paths, permissions, errors, and tables
- [Documentation audit](documentation-audit.md) - coverage baseline and weak areas to keep an eye on
