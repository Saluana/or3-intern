# Hardening Checklist

This checklist covers the security hardening measures available in OR3 Intern.

## Approval system

- [ ] Enable approvals (`security.approvals.enabled`)
- [ ] Set exec mode to "ask" or "deny" (not "trusted")
- [ ] Set skill execution mode to "ask"
- [ ] Set secret access mode to "ask"
- [ ] Set message send mode to "ask"
- [ ] Generate an approvals signing key
- [ ] Set pairing mode to "ask" (not "trusted" for hosted deployments)

Source: `internal/safetymode/safetymode.go:146-203`, `internal/approval/evaluate.go:138-161`

## Audit logging

- [ ] Enable audit logging (`security.audit.enabled`)
- [ ] Enable strict mode (`security.audit.strict`)
- [ ] Enable verify-on-start (`security.audit.verifyOnStart`)
- [ ] Generate an audit key file

Source: `internal/security/store.go:151-179`, `internal/doctor/engine_security.go:12-45`

## Secret store

- [ ] Enable secret store (`security.secretStore.enabled`)
- [ ] Set required mode (`security.secretStore.required`)
- [ ] Generate a secret store key file
- [ ] Migrate API keys from config to secret store (use `secret:` references)

Source: `internal/security/store.go:25-148`, `internal/doctor/engine_security.go:46-80`

## Network policy

- [ ] Enable network policy (`security.network.enabled`)
- [ ] Set default-deny (`security.network.defaultDeny`)
- [ ] Add allowed hosts explicitly
- [ ] Disable loopback access (for hosted deployments)
- [ ] Disable private network access (for hosted deployments)

Source: `internal/security/network.go:18-24`, `internal/safetymode/safetymode.go:128-130`

## Sandbox

- [ ] Enable sandbox (`hardening.sandbox.enabled`)
- [ ] Install bubblewrap (`bwrap`)
- [ ] Configure bubblewrap path if not in PATH
- [ ] Set writable paths as needed

Source: `internal/tools/sandbox.go:12-17`, `internal/safetymode/safetymode.go:166-168`

## Tool hardening

- [ ] Enable guarded tools (ask before risky actions)
- [ ] Disable privileged tools for non-admin users
- [ ] Disable shell execution (`hardening.enableExecShell = false`)
- [ ] Restrict tools to workspace (`tools.restrictToWorkspace`)
- [ ] Disable full file read (`tools.allowFullFileRead = false`)
- [ ] Enable access profiles for external ingress

Source: `internal/safetymode/safetymode.go:137-203`, `internal/doctor/engine_profiles.go`

## Service hardening

- [ ] Bind service to loopback (not 0.0.0.0)
- [ ] Generate a strong service secret
- [ ] Disable unauthenticated pairing for remote deployments
- [ ] Limit shared secret role to "service-client"
- [ ] Limit service max capability to "safe"
- [ ] Set session idle and absolute TTLs appropriately

Source: `internal/doctor/fix.go:60-77`

## Authentication

- [ ] Enable passkey auth
- [ ] Register at least one passkey
- [ ] Set reasonable session idle and absolute expiry
- [ ] Enable step-up for sensitive operations
- [ ] Register a second passkey as backup (prevents lockout)
- [ ] Review paired devices regularly

Source: `internal/auth/service.go:137-159`, `internal/auth/service.go:657-675`

## Channel security

- [ ] Set inbound policies for all enabled channels
- [ ] Use allowlist or pairing-based access (not open access)
- [ ] Assign access profiles to each channel
- [ ] Review allowed hosts and tools per profile

Source: `internal/doctor/fix.go:203-256`, `internal/doctor/engine_profiles.go:11-115`

## Monitoring

- [ ] Review audit events periodically
- [ ] Verify audit chain integrity
- [ ] Monitor approval requests
- [ ] Review paired devices
- [ ] Check doctor findings after config changes

Source: `internal/controlplane/controlplane.go:482-531`, `internal/controlplane/controlplane.go:214-226`
