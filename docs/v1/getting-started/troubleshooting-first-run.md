# Troubleshooting first run

Here are common issues and how to fix them.

## CGO_ENABLED=1 required

If you get an error about CGO, you need to install Go with CGO support. On macOS, this means having Xcode Command Line Tools installed. On Linux, install `gcc` or `build-essential`.

```bash
CGO_ENABLED=1 go build -o ./or3-intern ./cmd/or3-intern
```

## Provider not configured

The agent needs an AI provider to work. Run the setup wizard:

```bash
or3-intern setup
```

Or configure a provider manually:

```bash
or3-intern configure
```

## Port already in use

If port 9100 is taken, change it in your config:

```json
{
  "service": {
    "port": 9101
  }
}
```

## Channel not connecting

Check your bot tokens and API keys in the config. Make sure they are correct. For Telegram, check the bot was created with BotFather. For Slack, verify the bot token has the right scopes.

## Run a diagnostic

```bash
or3-intern doctor
```

This checks your setup and reports any problems. It looks at config, provider connectivity, and channel status.
