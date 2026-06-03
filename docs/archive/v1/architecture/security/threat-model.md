# Threat Model

The security system addresses these threats:

## Unauthorized command execution

An AI agent running arbitrary shell commands or programs. The approval broker evaluates every exec using policy modes, allowlists, and operator-issued tokens.

Source: `internal/approval/evaluate.go:11-29` (EvaluateExec)

## Secret exfiltration

Secrets stored in plaintext config or environment leaks. The SecretManager encrypts all secrets with AES-256-GCM using a derived key from the secret store key file.

Source: `internal/security/store.go:298-325` (encryptBlob/decryptBlob)

## Network access to internal hosts

Outbound HTTP requests to loopback, private IPs, or the cloud metadata endpoint. HostPolicy blocks these by default with configurable lists.

Source: `internal/security/network.go:142-156` (validateAddr)

## Audit log tampering

Someone deleting or modifying audit records to cover tracks. The AuditLogger uses an HMAC chain to make the log tamper-evident.

Source: `internal/security/store.go:151-179` (AuditLogger)

## Unauthorized device access

A rogue device connecting to the OR3 Intern service. Device pairing requires a 6-digit code approved by the operator, and device tokens are stored as hashes.

Source: `internal/approval/pairing.go:14-68` (CreatePairingRequest)

## Prompt injection via tool metadata

MCP tools with suspicious names or descriptions that try to override system instructions. The metadata scanner detects patterns like "ignore previous instructions" and can block the tool.

Source: `internal/tools/metadata_scanner.go:22-30` (scanner patterns)

## Privilege escalation through shells

Shell execution bypasses program-level controls. The exec tool separates program+args (guarded) from legacy shell commands (privileged), and hosted-service profiles disable shells entirely.

Source: `internal/tools/exec.go:77-81` (CapabilityForParams)
