# UX State and Copy

The UX state/copy packages translate internal config, doctor findings, approvals, and errors into stable user-facing summaries. They support the CLI settings/status experience and app-facing bootstrap payloads.

## Packages

| Package | Role |
| --- | --- |
| `internal/uxstate` | Builds structured views for status, settings home, access dashboard, problems, devices, and approval prompts |
| `internal/uxcopy` | Central copy labels, hints, problem explanations, safety-mode labels, and friendly error translations |
| `internal/uxformat` | Small rendering helpers for CLI error blocks, loading states, and empty states |

## Status View

`BuildStatusView` combines config, doctor report, device count, and pending approval count into:

- headline
- safety label and summary
- workspace/commands/internet/devices/activity summaries
- access dashboard sections
- problem views derived from doctor findings

This is the layer that turns technical findings like `service.secret_missing` into actionable text.

## Settings View

`BuildSettingsHomeView` defines the main settings sections:

- provider
- workspace
- devices
- safety
- channels
- tools
- memory
- context
- advanced

Each section has a summary and command/action hint.

## Approval Prompt View

`BuildApprovalPrompt` classifies approval requests by subject type, including exec, skill execution, secret access, message delivery, and file transfers. It produces:

- title
- action summary
- risk label
- risk explanation
- choice hints
- advanced details

Approval UI should prefer this shape over reconstructing risk text from raw approval JSON.

## Error Translation

`uxcopy.TranslateError` maps common technical failures to:

- title
- what happened
- fix
- suggested command
- advanced detail

This keeps routine recovery copy consistent across status, settings, and service-facing flows.
