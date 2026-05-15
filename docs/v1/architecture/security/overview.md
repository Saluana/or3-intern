# Security Architecture Overview

The OR3 Intern security system has several layers that work together:

1. **Approval Broker** - evaluates every sensitive action before it runs
2. **Secret Store** - encrypts secrets at rest using AES-256-GCM
3. **Audit Logger** - appends tamper-evident records for every security decision
4. **Network Policy** - controls which outbound hosts can be contacted
5. **Sandboxing** - isolates command execution with bubblewrap
6. **Safety Modes** - preset security postures from relaxed to locked-down
7. **Device Pairing** - authenticates devices through 6-digit codes
8. **Auth Sessions** - passkey-based authentication with WebAuthn
9. **Access Profiles** - limits what tools and hosts each ingress point can use

## Source files

- `internal/security/store.go` - SecretManager and AuditLogger
- `internal/security/network.go` - HostPolicy for outbound connections
- `internal/approval/evaluate.go` - evaluation pipeline
- `internal/approval/broker.go` - broker helpers (host ID, clock)
- `internal/safetymode/safetymode.go` - safety mode presets
- `internal/auth/service.go` - WebAuthn auth service

## How they connect

When a tool requests a guarded action (like exec, web fetch, or skill run):

1. The tool calls the **Approval Broker** to evaluate the action
2. The broker checks in order: existing tokens → policy mode → allowlists → create approval request
3. If denied by policy (deny mode), the action is blocked immediately
4. If the action needs approval, a pending request is created and the tool returns `ApprovalRequiredError`
5. An operator approves the request, which issues a short-lived token
6. The tool retries with the token, which the broker verifies and consumes
