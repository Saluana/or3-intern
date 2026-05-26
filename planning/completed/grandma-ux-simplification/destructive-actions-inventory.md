# Destructive Action Inventory

Implemented for task 8.

## App Actions

- Disconnect/unpair this app from a saved computer:
  - `or3-app/app/components/app/HostConnectionCard.vue`
  - `or3-app/app/pages/settings/index.vue`
  - `or3-app/app/pages/settings/advanced.vue`
- Revoke/remove paired devices:
  - `or3-app/app/components/app/DeviceManagementCard.vue`
  - `or3-app/app/components/electron/TrustedDevicesPanel.vue`
- Revoke passkeys:
  - `or3-app/app/pages/settings/passkeys.vue`
- Sign out of the active passkey session:
  - `or3-app/app/pages/settings/security.vue`
- Delete scheduled tasks:
  - `or3-app/app/pages/scheduled.vue`
- Remove integration/add-on style configuration:
  - `or3-app/app/components/settings/ProviderManagerControl.vue`

All listed app paths use `DestructiveActionConfirmModal`, which shows the item name, consequence, and undo availability before the existing backend call runs.

## CLI Actions

- `or3-intern connect-device disconnect <device-id> [--force]`
- `or3-intern devices revoke <device-id> [--force]`
- `or3-intern approvals allowlist remove <id> [--force]`
- `or3-intern secrets delete <name> [--force]`
- `or3-intern skills remove <name> [--force]`
- TUI MCP server removal already had an interactive `y/n` gate in `configure_tui_mcp.go`.

CLI confirmations only prompt when stdin and stdout are terminals. Non-interactive/scripted execution preserves existing behavior. `--force` explicitly skips the prompt.
