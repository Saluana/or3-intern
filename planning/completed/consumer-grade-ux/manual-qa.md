# Consumer UX manual QA

Use this checklist to validate the simple command surface and product-facing UX introduced in phases 1 through 5.

## Evidence to capture

- command run
- whether you used a fresh or existing config
- exact user-visible output for any failure or confusing wording
- screenshots or terminal captures for setup review, status summary, approval detail, and connect-device code display

## 1. First run under two minutes

- [ ] Run `or3-intern setup` on a fresh config.
- [ ] Choose a provider, workspace folder, scenario, and safety mode.
- [ ] Confirm setup prints a review for files, commands, internet, devices, and activity log.
- [ ] Confirm setup offers to start chat next.
- [ ] Confirm the full flow can be completed in under two minutes without needing raw config keys.

## 2. Safety-mode change

- [ ] Re-run `or3-intern setup` or `or3-intern settings`.
- [ ] Change the scenario or safety mode.
- [ ] Run `or3-intern status`.
- [ ] Confirm the reported safety label and summary match the new selection.

## 3. Fix-problems summary

- [ ] Intentionally create or use a config with at least one doctor finding.
- [ ] Run `or3-intern status`.
- [ ] Confirm each finding is rendered as a human problem title, why-it-matters text, and a recommended fix.
- [ ] Run `or3-intern status --advanced` and confirm the internal finding IDs appear only in advanced output.

## 4. Approval decision wording

- [ ] Trigger a pending approval request.
- [ ] Run `or3-intern approvals list pending`.
- [ ] Confirm the list includes a human summary of the requested action.
- [ ] Run `or3-intern approvals show <id>`.
- [ ] Confirm the output answers what OR3 wants to do, why approval is needed, and the risk level.

## 5. Connect-device flow

- [ ] Run `or3-intern connect-device`.
- [ ] Confirm missing prerequisites are repaired automatically when safe.
- [ ] Confirm the code is shown in short-code format and the selected access level is described in plain language.
- [ ] Run `or3-intern connect-device list` and confirm paired devices show friendly role labels.
- [ ] Run `or3-intern connect-device disconnect <device-id>` and confirm the device is revoked cleanly.

## 6. Access dashboard sanity

- [ ] Run `or3-intern status`.
- [ ] Confirm a normal user can answer “Can OR3 see my whole computer?” from the Files line.
- [ ] Confirm Commands, Internet, Devices, and Activity log lines are understandable without internal terms.

## Pass criteria

- setup, status, approvals, and connect-device stay understandable without requiring knowledge of runtime profiles, matcher JSON, or allowlists
- advanced details remain available when explicitly requested
- beginner docs match the actual command names and flow order