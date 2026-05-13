# First-run setup

After installing, run the setup wizard:

```bash
or3-intern setup
```

This walks you through the basic configuration. It will ask about:

**AI provider**

Choose a provider like OpenAI, Anthropic, or another supported service. You need an API key. The wizard will prompt you for it.

**Safety preferences**

Set your approval mode. Relaxed mode auto-approves tool calls. Normal mode asks for approval on sensitive actions. Strict mode requires approval for everything.

**Device pairing**

If you plan to use the OR3 App, the wizard will help you pair your device. This uses passkey authentication for secure approval requests.

**Storage location**

Pick where config and data files go. The default is `~/.or3-intern/`.

## Lighter option

If you prefer to configure things yourself, run:

```bash
or3-intern init
```

This creates a default config file you can edit manually.

## Change settings later

Run this anytime to update your configuration:

```bash
or3-intern configure
```

## Next step

Learn about the [configuration file](configuration-basics.md) and how it works.
