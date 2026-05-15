# Setup and Init

`setup` and `init` are both first-run helpers, but they serve different audiences.

## `setup`

```bash
or3-intern setup
```

`setup` is the friendliest first-run flow. It asks plain-language questions such as:

- where you plan to use OR3
- how careful it should be
- which folder it should stay inside

Those answers map onto the underlying runtime profile, approvals, audit, service, and hardening settings.

## `init`

```bash
or3-intern init
```

`init` is the compatibility alias for the original first-run wizard. It runs the older targeted configuration flow for:

- provider
- storage
- workspace
- web/service-related setup

It is mainly useful for existing scripts, older docs, or users who already know the advanced configuration model.

## Which one should you use?

- Use `setup` if you are new to OR3 Intern.
- Use `init` if you want the older compatibility flow.
- Use `settings` after first run for normal changes.
- Use `configure` for advanced targeted edits.
