# Deepen `doctor` security diagnostics

## Overview

`doctor` in `cmd/or3-intern` already catches a useful first layer of unsafe configuration, but it still treats most settings as isolated booleans. The next step is to make it reason across inbound exposure, profiles, tool capability, outbound network posture, and script execution posture so operators can see when a configuration is technically valid but still risky.

This plan keeps the implementation small and local:

- extend `cmd/or3-intern/doctor.go`
- reuse existing config/runtime/security semantics already defined elsewhere
- avoid new persistence, migrations, or service layers
- cover every new finding with deterministic config-local tests

## Design

### Goals

- Preserve the current `doctor` CLI shape and `--strict` behavior.
- Add higher-signal warnings for missing or contradictory hardening controls.
- Reuse existing repo semantics from config validation, runtime profile enforcement, network policy, and tool registration.
- Keep diagnostics deterministic and cheap: pure config inspection only, with no live network or filesystem probing beyond existing config values.

### Affected areas

- `cmd/or3-intern/doctor.go`
  - Main implementation target.
  - Refactor into helper functions so diagnostics stay readable as coverage expands.
- `cmd/or3-intern/doctor_test.go`
  - Add focused regression cases for every new warning family.
- `internal/config/config.go`
  - Existing source of truth for config defaults and validation semantics.
  - `doctor` should mirror these semantics, not invent conflicting ones.
- `cmd/or3-intern/security_setup.go`
  - Source of actual security wiring for audit, secret store, outbound endpoint validation, and MCP behavior.
- `internal/agent/runtime.go`
  - Source of truth for profile selection and tool enforcement.
- `internal/tools/exec.go`
  - Source of truth for shell-vs-program execution posture.
- `internal/tools/skill_exec.go`
  - Source of truth for skill execution enablement and approval enforcement.
- `internal/security/network.go`
  - Source of truth for host allowlist semantics.

### Diagnostic groups

#### 1. Security posture findings

Add checks for:

- `security.audit.enabled == false`
  - Warn when audit logging is disabled entirely.
- `security.audit.enabled == true` but `strict == false`
  - Warn that privileged tool auditing is enabled but non-fatal on failure.
- `security.audit.enabled == true` but `verifyOnStart == false`
  - Warn that tamper verification is not performed at startup.
- `security.secretStore.enabled == false`
  - Warn when secret persistence is disabled.
- `security.secretStore.enabled == true` but `required == false`
  - Warn in higher-risk setups where channels, webhooks, or MCP servers are enabled but secret store failures would still be tolerated.

Rationale:
These settings already matter operationally in `setupSecurity`, but `doctor` does not tell operators when they have opted out of them.

#### 2. Access profile coverage findings

Add checks for:

- `security.profiles.enabled == false` while any open-access/public ingress is enabled.
- Profiles enabled but no effective default/channel/trigger mapping exists for enabled ingress paths.
- Webhook enabled with no trigger-specific profile mapping and no non-empty default profile.
- Channel enabled with `openAccess == true` while profiles are disabled.
- Channel enabled with `openAccess == true` while the resolved/default profile allows privileged capability.

Profile resolution should mirror runtime behavior:

- trigger-specific profile first
- then channel-specific profile
- then default profile

Doctor does not need to simulate events, but it should use the same precedence model when evaluating exposure.

#### 3. MCP posture findings

Add checks for each enabled MCP server:

- `transport == stdio` with an empty server-level `childEnvAllowlist`
  - Warn because subprocess MCP inherits a wider environment than intended.
- `transport == stdio` with a broad inherited environment posture because global `hardening.childEnvAllowlist` is also empty.
- `transport == sse` or `streamablehttp` with `allowInsecureHttp == true`
  - Warn even though config validation allows loopback HTTP, because it is still intentionally weaker than HTTPS.
- HTTP MCP enabled while `security.network.enabled/defaultDeny` posture is weak or host rules are broad.

Important nuance:

- `internal/config/config.go` already rejects non-loopback insecure HTTP unless explicitly allowed.
- `doctor` should not duplicate hard validation errors.
- It should warn on configurations that are allowed but still weaker than recommended.

#### 4. Public-channel privileged-tool exposure

Add cross-surface checks that look at both ingress and tool posture.

Warn when any public ingress exists and the runtime could still reach:

- privileged tools (`hardening.privilegedTools == true`)
- guarded tools (`hardening.guardedTools == true`) without meaningful profile restriction
- `run_skill_script` via profiles that allow privileged capability
- shell-based `exec` usage via profiles that allow `exec`

This should consider:

- `channels.*.openAccess`
- enabled webhook ingress
- whether profiles are enabled
- whether the effective profile capability is `privileged`
- whether the effective profile has an explicit `allowedTools` list

The goal is not to prove exploitation, only to identify obviously unsafe reachability.

#### 5. Webhook + profile permissiveness

Add checks for webhook-specific risk combinations:

- webhook enabled and bound safely, but no profile enforcement exists
- webhook enabled with a resolved/default profile whose `maxCapability` is `privileged`
- webhook enabled with a profile that allows subagents
- webhook enabled with a profile that grants broad `allowedHosts`
- webhook enabled with a profile that grants writable paths

This is a high-signal area because webhook events are autonomous and already treated specially in the runtime prompt path.

#### 6. Broad host allowlist findings

Add host allowlist warnings for both:

- global `security.network.allowedHosts`
- per-profile `allowedHosts`

Flag patterns such as:

- literal `*`
- empty policy with `defaultDeny == false` when networked features are enabled
- too many domains in a single allowlist
- very broad wildcard patterns like `*.example.com` where the profile or global policy is otherwise acting as a security boundary

Implementation note:

- Keep heuristics simple and deterministic.
- A reasonable threshold such as “more than 10 domains” is enough for an operator warning.
- This is advisory only; do not move validation rules.

#### 7. Exec shell posture findings

Add checks for:

- shell execution effectively available because `exec` supports `command` and `hardening.privilegedTools == true`
- weak `execAllowedPrograms` posture when privileged access is enabled
- empty child env allowlist on exec-capable setups
- profiles or defaults that allow `exec` while public ingress exists

Important repo-specific nuance:

- program execution is allowlisted through `AllowedPrograms`
- shell command execution does not use that allowlist in `internal/tools/exec.go`
- `doctor` should explicitly call this out as a weaker posture when `exec` is reachable

#### 8. Skill execution + quarantine findings

Add checks for:

- `skills.enableExec == true` and `skills.policy.quarantineByDefault == false`
- public ingress + profiles that allow privileged capability + skill execution enabled
- skill execution enabled with empty child env allowlist

Rationale:
The runtime already blocks non-approved skills at execution time, but disabling quarantine-by-default removes the operator approval barrier for newly discovered skill bundles.

### Implementation shape

Refactor `doctor.go` into small helpers, for example:

- `securityFindings(cfg config.Config) []doctorFinding`
- `profileFindings(cfg config.Config) []doctorFinding`
- `mcpFindings(cfg config.Config) []doctorFinding`
- `networkFindings(cfg config.Config) []doctorFinding`
- `execFindings(cfg config.Config) []doctorFinding`
- `skillFindings(cfg config.Config) []doctorFinding`
- `channelExposureFindings(cfg config.Config) []doctorFinding`

Add small internal helpers for shared reasoning:

- `hasPublicIngress(cfg config.Config) bool`
- `enabledChannelNames(cfg config.Config) []string`
- `resolvedExposureProfiles(cfg config.Config) []string` or equivalent targeted helpers
- `isBroadHostPattern(pattern string) bool`
- `hostListTooBroad(hosts []string) bool`
- `profileAllowsPrivileged(profile config.AccessProfileConfig) bool`
- `profileHasMeaningfulToolRestriction(profile config.AccessProfileConfig) bool`

Keep the output model unchanged:

- same `doctorFinding`
- same sorted output style
- same `--strict` failure rule

### Safeguards and boundaries

- No DB or migration changes.
- No config format changes.
- No runtime behavior changes in this planning slice; this is diagnostic-only work.
- Avoid duplicate warnings when one higher-level warning already explains the same problem.
- Keep findings actionable and specific to a config knob or relationship.

### Follow-up hardening note

During repo review, one concrete implementation gap surfaced:

- shell execution through `exec`’s `command` path in `internal/tools/exec.go` is not constrained by `hardening.execAllowedPrograms`

That is not required for the diagnostic pass, but the warning text should make the posture clear, and a later hardening follow-up should decide whether shell mode should be narrowed, separately gated, or denied in profile-constrained/public contexts.

## Task list

### 1. Refactor doctor structure

- [x] Split `doctorFindings` in `cmd/or3-intern/doctor.go` into focused helper groups.
- [x] Keep current findings intact and preserve stable output sorting.
- [x] Keep `runDoctorCommand` and `--strict` semantics unchanged.

### 2. Add core security posture warnings

- [x] Warn when `security.audit.enabled` is false.
- [x] Warn when audit is enabled but `strict` is false.
- [x] Warn when audit is enabled but `verifyOnStart` is false.
- [x] Warn when `security.secretStore.enabled` is false.
- [x] Warn when secret store failures are tolerated in channel/webhook/MCP-enabled setups.

### 3. Add profile coverage diagnostics

- [x] Detect public ingress with `security.profiles.enabled == false`.
- [x] Detect enabled ingress with no effective default/channel/trigger profile path.
- [x] Add webhook-specific profile coverage warnings.
- [x] Add public-channel warnings when the effective/default profile still permits privileged capability.
- [x] Add warnings when profile mappings exist but do not meaningfully restrict tools.

### 4. Add MCP posture diagnostics

- [x] Warn on enabled stdio MCP servers with empty server-level `childEnvAllowlist`.
- [x] Warn when stdio MCP posture is also broad at the global child environment level.
- [x] Warn on HTTP MCP servers using `allowInsecureHttp`.
- [x] Warn when HTTP MCP is enabled but network policy is weak or broadly allowlisted.

### 5. Add ingress-to-tool exposure diagnostics

- [x] Add a helper that identifies whether any public ingress path exists.
- [x] Warn when public ingress can reach privileged tools.
- [x] Warn when public ingress can reach guarded tools without a profile boundary.
- [x] Warn when public ingress can reach `exec` or `run_skill_script` through permissive profiles.

### 6. Add webhook permissiveness diagnostics

- [x] Warn when webhook ingress relies on no profile or only a permissive default profile.
- [x] Warn when the webhook-resolved profile allows privileged capability.
- [x] Warn when the webhook-resolved profile allows subagents.
- [x] Warn when the webhook-resolved profile grants writable paths or broad allowed hosts.

### 7. Add host allowlist heuristics

- [x] Add shared host-breadth helpers in `cmd/or3-intern/doctor.go`.
- [x] Warn on literal `*` in global network policy hosts.
- [x] Warn on literal `*` or overly broad wildcard entries in per-profile hosts.
- [x] Warn when allowlists exceed a fixed advisory threshold.
- [x] Warn when networked features are enabled without a meaningful deny-by-default posture.

### 8. Add exec posture diagnostics

- [x] Warn when privileged `exec` shell mode is reachable in public or webhook-facing setups.
- [x] Warn when `hardening.execAllowedPrograms` is empty or effectively too broad for the deployment posture.
- [x] Warn when child env allowlist is empty in exec-capable setups.
- [x] Make the warning text explicit that shell `command` execution is broader than program allowlisting.

### 9. Add skill execution diagnostics

- [x] Warn when `skills.enableExec` is true and `skills.policy.quarantineByDefault` is false.
- [x] Warn when skill execution is reachable from public ingress through permissive profiles.
- [x] Warn when skill execution is enabled with an empty child env allowlist.

### 10. Add tests

- [x] Convert `cmd/or3-intern/doctor_test.go` to table-driven coverage where practical.
- [x] Add one targeted test for each new warning family.
- [x] Add at least one “safe baseline” config test that emits no warnings.
- [x] Add regression coverage for output grouping and strict-mode failure behavior after the refactor.

### 11. Optional follow-up after diagnostics land

- [x] Decide whether `exec` shell mode should receive its own hardening flag or stronger gating. Decision: keep schema unchanged in this slice and surface the risk through `doctor`; revisit a dedicated flag or stronger runtime gating in a later hardening pass.
- [x] Decide whether public ingress should automatically require access profiles in config validation rather than only warning in `doctor`. Decision: keep this as a `doctor` warning for now to avoid breaking existing configs that are valid but weakly hardened.
- [x] Decide whether broad host allowlists should stay advisory or become validation errors for specific ingress/profile combinations. Decision: keep them advisory in `doctor` until real operator usage shows which combinations are safe to hard-fail.

## Out of scope

- Changing config schema.
- Adding new runtime enforcement in this slice.
- Adding database tables, migrations, or persisted doctor state.
- Performing live environment probes or outbound connectivity tests.
