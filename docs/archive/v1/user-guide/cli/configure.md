# Configure

`configure` is the advanced configuration editor behind the friendlier `settings` flow.

```bash
or3-intern configure
or3-intern configure --section provider --section service
```

## When to use it

Use `configure` when you want one of these advanced workflows:

- target specific config sections from a script
- open the full Bubble Tea editor in a terminal
- make changes that do not fit the simplified `settings` task list
- work directly with provider, service, channels, tools, doc index, or hardening settings

For everyday configuration changes, prefer `or3-intern settings`.

## Interactive behavior

When stdin and stdout are terminals, `configure` opens the terminal UI.

- Arrow keys move through items
- `Enter` selects
- `Space` toggles when a field supports it
- `s` saves
- `q` quits

When stdin or stdout is non-interactive, `configure` falls back to the plain-text prompt flow so redirected input and scripts still work.

## Section targeting

The command accepts repeatable `--section` flags. Common values include:

- `provider`
- `storage`
- `workspace`
- `web` or `service`
- `channels`

The full advanced editor also exposes runtime, context, tools, doc index, skills, security, hardening, session, automation, and service-related fields.

## Example patterns

```bash
or3-intern configure
or3-intern configure --section provider
or3-intern configure --section channels --section service
```

## Related guides

- [settings](settings.md) for the canonical daily configuration flow
- [setup-init](setup-init.md) for first-run setup
