# Safety Modes

Safety modes are preset security configurations that can be applied with a single setting. They control approvals, audit, sandboxing, network policy, and tool hardening.

## Modes

Four modes exist (source: `internal/safetymode/safetymode.go:23-28`):

- **relaxed** - minimal security, no approvals, no sandbox, no network policy
- **balanced** (default) - approvals with "ask" mode, sandbox off, network policy off
- **locked-down** - max security, exec denied, sandbox on, network default-deny, audit strict
- **custom** - none of the standard modes match the current config

## Mode settings

### Relaxed mode

- Guarded tools off
- Audit disabled, not strict, no startup verify
- Approvals disabled, all modes set to "trusted"
- Network policy disabled
- Sandbox disabled
- Runtime profile: local-dev

Source: `internal/safetymode/safetymode.go:146-161`

### Balanced mode (default)

- Guarded tools on
- Audit enabled, not strict, no startup verify
- Approvals enabled, all modes "ask"
- Network policy disabled
- Sandbox disabled
- Runtime profile: single-user-hardened

Source: `internal/safetymode/safetymode.go:186-203`

### Locked-down mode

- Guarded tools on
- Privileged tools off
- Shell execution off
- Sandbox enabled
- Secret store enabled and required
- Audit enabled, strict, verify on start
- Approvals enabled: exec denied, others "ask"
- Network policy enabled, default deny
- Runtime profile: hosted-no-exec

Source: `internal/safetymode/safetymode.go:162-185`

## Deployment scenarios

Scenarios help choose the right mode and additional settings:

- **solo-computer** - single trusted user, local use
- **phone-companion** - personal use with a paired phone
- **private-server** - self-hosted for a trusted group
- **hosted-service** - internet-facing service
- **advanced** - manual configuration

Each scenario applies specific settings beyond the safety mode (e.g., the hosted-service scenario enables audit strict and network default-deny).

Source: `internal/safetymode/safetymode.go:30-135` (ApplyScenario)

## Mode inference

`Infer` compares the current config against each standard mode and finds the closest match (fewest differences). If an exact match is found, confidence is "exact." Otherwise, it returns "custom" mode with drift (list of differences from the closest match) and "closest-match" confidence.

Source: `internal/safetymode/safetymode.go:206-221` (Infer)

## Drift detection

`Drift` compares the current config against a baseline for the given mode. It checks 11 settings: workspace boundary, file read access, guarded tools, audit enabled/strict, approvals enabled, exec mode, network enabled/default-deny, sandbox enabled, shell execution, and privileged tools.

Source: `internal/safetymode/safetymode.go:223-264` (Drift)

## Scenario inference

`InferScenario` guesses the deployment scenario from config: hosted-service/no-exec profiles map to hosted scenarios; single-user-hardened with service enabled maps to phone-companion; otherwise solo-computer.

Source: `internal/safetymode/safetymode.go:266-283` (InferScenario)
