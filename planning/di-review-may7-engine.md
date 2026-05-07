# Code Review: `internal/doctor/engine.go`

**Date:** May 7, 2026
**File:** `internal/doctor/engine.go` (1369 lines, ~50,766 chars)
**Package:** `internal/doctor/` (6 files, 2016 lines total)

---

## Grade: **D+** (one step above "rewrite it")

This file is the poster child for "it started small and nobody stopped adding to it." It compiles, it works, but it has 45 functions in one file spanning 15 different security domains with zero separation of concerns.

## Review Validation Update

The main critique holds: `engine.go` is too large, channel handling is duplicated in multiple places, and `webhookFindings` is doing enough work to deserve helper functions. The highest-value change is still a channel enumeration helper because it removes repeated Telegram/Slack/Discord/WhatsApp/Email blocks and prevents future channel drift.

Some cleanup recommendations were too aggressive:

- `profileAllowsGuarded` is not dead weight. It encodes a small but meaningful policy distinction used by `profileHasMeaningfulToolRestriction`.
- `hasNonEmpty` is tiny, but it is not harmful. It should probably be absorbed into channel enumeration rather than deleted as its own project.
- `Options.ConfigPath` is unused by `Evaluate`, but it is part of the doctor command API today. Removing it is a broader API cleanup, not an engine refactor prerequisite.
- Moving `TopFindings` to `report.go` is reasonable, but purely cosmetic.
- Finding builders and config-package methods may help later, but they should not outrank channel enumeration, webhook decomposition, or tests.

The action plan has been updated to prioritize drift prevention and testable refactors over line-count-only cleanup.

---

## 1. Structural Problems

### 1.1. File Size is Absurd

1369 lines for a single Go file violates every sensible guideline. The Go standard library rarely exceeds 500 lines per file. This file is the entire evaluation engine + all helpers + all domain-specific finding generators + all utility predicates in one flat namespace.

**Impact:** Any developer touching this file must hold 45 function signatures in their head. Code navigation is painful. Merge conflicts are guaranteed if two people touch different sections.

### 1.2. Does Not Follow Package-Oriented Design

Other files in `doctor/` are cleanly scoped:

- `report.go` (232 lines) -- Report, Finding, Summary types
- `fix.go` (255 lines) -- Fix/repair logic
- `render.go` (88 lines) -- Output rendering

Then `engine.go` just dumps everything else. The package is 68% one file.

### 1.3. Recommended Split

At minimum, split into these files (keeping the existing `doctor` package flat):

| File                   | Lines | Contents                                                                                                                                                                                         |
| ---------------------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `engine.go`            | ~60   | `Evaluate()`, `Options`, `severityFor()`, `severityForConfigureOrStartup()`                                                                                                                      |
| `engine_config.go`     | ~55   | `configValidationFindings()`, `validateConfigSnapshot()`                                                                                                                                         |
| `engine_filesystem.go` | ~80   | `filesystemFindings()`                                                                                                                                                                           |
| `engine_hardening.go`  | ~75   | `hardeningFindings()`, `isHostedOrStartupMode()`, `fixModeForBind()`                                                                                                                             |
| `engine_security.go`   | ~90   | `securityFindings()`, `keyFileFinding()`                                                                                                                                                         |
| `engine_approvals.go`  | ~50   | `approvalFindings()`, `approvalBrokerRequired()`                                                                                                                                                 |
| `engine_webhook.go`    | ~120  | `webhookFindings()`                                                                                                                                                                              |
| `engine_service.go`    | ~45   | `serviceFindings()`                                                                                                                                                                              |
| `engine_mcp.go`        | ~60   | `mcpFindings()`                                                                                                                                                                                  |
| `engine_network.go`    | ~80   | `networkFindings()`, `hostListContainsLiteralStar()`, `hostListTooBroad()`, `isInsecureHTTPURL()`, `hasRemoteHTTPMCP()`, `isLoopbackAddr()`                                                      |
| `engine_profiles.go`   | ~120  | `profileFindings()`, `resolveEffectiveProfile()`, `profileAllowsPrivileged()`, `profileAllowsGuarded()`, `profileAllowsTool()`, `profileHasMeaningfulToolRestriction()`, `profileCanReachExec()` |
| `engine_exec.go`       | ~50   | `execFindings()`, `publicIngressCanReachExec()`, `webhookCanReachExec()`                                                                                                                         |
| `engine_skills.go`     | ~65   | `skillFindings()`, `publicIngressCanReachSkillExec()`                                                                                                                                            |
| `engine_channels.go`   | ~210  | `channelExposureFindings()`, `publicChannelExposureFindings()`, `channelIngressFindings()`, `openAccessChannelNames()`, `hasPublicIngress()`, `requiresChannelAllowlist()`, `hasNonEmpty()`      |
| `engine_runtime.go`    | ~110  | `runtimeProfileFindings()`, `probeFindings()`, `probeSQLiteDatabase()`                                                                                                                           |
| `engine_predicates.go` | ~50   | `hasExternalIntegrations()`, `anyEnabledChannels()`, `anyEnabledMCPServers()`, `TopFindings()`                                                                                                   |

This yields ~1400 lines across 16 files. Slightly more than the current 1369 due to package declarations, but each file is focused on ONE domain. A developer fixing a webhook validation bug opens `engine_webhook.go` and sees only webhook logic.

---

## 2. Questionable / Low-Priority Code

### 2.1. `profileAllowsGuarded()` Is Small But Legitimate

**Line 1175-1178.** Used by exactly one function (`profileHasMeaningfulToolRestriction`, line 1194), which is itself used exactly once (`publicChannelExposureFindings`, line 881) to produce one finding (`channels.open_access_no_tool_boundary`).

This is only one call chain, but it represents a real policy distinction: a profile with `guarded` capability and no explicit tool allowlist is materially different from a profile that cannot reach guarded tools.

**Recommendation:** Keep it unless the surrounding profile helpers are moved wholesale. The helper costs almost nothing and makes `profileHasMeaningfulToolRestriction` readable.

### 2.2. `hasNonEmpty()` -- A Function That Wraps a 3-Line For Loop

**Line 1330-1337.** A 7-line function (with comments/whitespace) whose entire body is:

```go
for _, item := range items {
    if strings.TrimSpace(item) != "" {
        return true
    }
}
return false
```

Used 5 times in `channelIngressFindings` via a closure (lines 928-932). The helper is small and harmless, but the repeated channel blocks around it are the real problem.

**Recommendation:** Fold this into `enumerateChannels()`/`collectChannels()` when the channel metadata helper is introduced. Do not spend a standalone diff deleting it.

### 2.3. `Options.ConfigPath` -- Declared but Never Read in This File

**Line 22.** The `ConfigPath` field on `Options` is never accessed by `Evaluate`. It is still passed through the doctor command surfaces today, so removing it means changing the public-ish doctor options shape and tests.

**Recommendation:** Leave it alone unless `Options` is split into separate evaluation and fix option structs. This is not a priority inside the engine cleanup.

---

## 3. Redundancies and DRY Violations

### 3.1. Five Identical Blocks for Five Channels (Pattern Repeated 3 Times)

The pattern `cfg.Channels.X.Enabled && cfg.Channels.X.OpenAccess` appears in 3 separate functions with hardcoded channel names:

1. **`channelExposureFindings` (lines 820-834):** 5 if-blocks, each 3 lines, calling `publicChannelExposureFindings`
2. **`openAccessChannelNames` (lines 1132-1146):** 5 identical if-blocks, building a string slice
3. **`channelIngressFindings` (lines 928-932):** 5 calls to closure `add()`, each with different field accessors

That's 15 hardcoded channel blocks across 3 functions. Adding a new channel (e.g., "Signal") requires touching all 3 locations + the `anyEnabledChannels` helper.

**Fix:** Channels should be iterable. Either:

- Add a method to `config.Config` that returns `[]ChannelInfo` (name, enabled, openAccess, inboundPolicy, allowlist fields)
- Or build the list once in `Evaluate()` and pass it around

Example refactor pattern:

```go
type channelConfig struct {
    name         string
    enabled      bool
    openAccess   bool
    inboundPolicy config.InboundPolicy
    hasAllowlist  bool
}

func enumerateChannels(cfg config.Config) []channelConfig { ... }
```

Then `channelExposureFindings`, `openAccessChannelNames`, and `channelIngressFindings` all iterate over the same list.

### 3.2. `strings.TrimSpace()` Called Repeatedly on Same Fields

In `hardeningFindings`, `cfg.Hardening.Sandbox.BubblewrapPath` is trimmed 4 separate times (lines 228, 240, 247, 249). In `filesystemFindings`, `cfg.WorkspaceDir` is trimmed 3 times. Every finding generator trims config fields before comparing.

**Fix:** Trim once at the top of each function and use a local variable.

### 3.3. `strings.TrimSpace()` + `strings.ToLower()` on `profile.MaxCapability`

Called in both `profileAllowsPrivileged` (line 1171) and `profileAllowsGuarded` (line 1176) independently. Same field, same normalization.

**Fix:** Normalize `MaxCapability` once during config loading in `config/` package, or at least share the normalized value locally.

### 3.4. `strings.ToLower` on Already-Lowercase Constants

In `requiresChannelAllowlist` (line 1340):

```go
switch strings.ToLower(strings.TrimSpace(string(policy))) {
case string(config.InboundPolicyAllowlist):
```

`config.InboundPolicyAllowlist` is `"allowlist"` -- already lowercase. This is defensive but wasteful. Either trust the config types or add a `Normalize()` method on the type.

### 3.5. Finding Construction is Boilerplate Heaven

Every finding is constructed with 6-8 fields set explicitly. Across 1369 lines there are ~100 finding constructions. Many follow the same pattern of: ID, Area, Severity from `severityFor()`, Summary, sometimes Detail, FixMode, FixHint.

**Possible fix:** Add builder or constructor helpers:

```go
func newFinding(id, area string, sev Severity, summary string) Finding { ... }
func newFixableFinding(id, area string, sev Severity, summary, detail string, mode FixMode, hint string) Finding { ... }
```

This could cut boilerplate, but it also hides explicit fields that are useful in a security diagnostic engine. Treat this as a later readability experiment after the domain split, not a phase-1 goal.

---

## 4. Simplicity Improvements

### 4.1. `severityFor()` and `severityForConfigureOrStartup()` Are Near-Duplicates

**Lines 53-65.** `severityFor` takes a `blockOnStartup bool`, while `severityForConfigureOrStartup` hardcodes the same logic. `severityForConfigureOrStartup` is strictly a special case of `severityFor` where `blockOnStartup` is true.

```go
// severityForConfigureOrStartup is equivalent to:
severityFor(mode, advisory, true)
```

...for the modes it handles. The diff:

- `severityFor` checks: `ModeStartupChat || ModeStartupServe || ModeStartupService`
- `severityForConfigureOrStartup` checks: `ModeConfigurePostSave || ModeStartupChat || ModeStartupServe || ModeStartupService`

**Recommendation:** Merge this only if the replacement stays obvious. A helper like `severityForModes(mode, advisory, blockModes...)` is clearer than silently adding configure behavior to `severityFor`.

### 4.2. `webhookFindings` is a 113-Line Function with Deep Nesting

**Lines 371-484.** This function has 4 levels of nesting at its deepest point (line 475). It checks:

1. Is webhook enabled?
2. Is secret set?
3. Is bind loopback?
4. Is profile resolved? -> If not, check 3 sub-conditions (privileged tools, guarded tools, skill exec)
5. If profile resolved -> check 6 conditions (privileged, subagents, writable paths, broad hosts, exec shell, skill exec)

**Recommendation:** Extract the "profile-resolved" section into `webhookProfileFindings()` and the "profile-not-resolved" section into `webhookNoProfileFindings()`. Cut the function to ~40 lines.

### 4.3. The Closure `add()` in `channelIngressFindings` Should Become Iteration

**Lines 910-927.** Defining a 6-parameter closure just to call it 5 times is a sign that the channel data wants to be enumerable. This should be a proper helper or, better, a loop over channel snapshots.

**Recommendation:** Convert to iterating over `enumerateChannels()` (see 3.1).

### 4.4. `TopFindings()` Doesn't Belong in This File

**Lines 1364-1369.** This is a report utility, not an engine concern. It should live in `report.go` next to `NewReport()` and `Filter()`.

**Recommendation:** Move to `report.go` only if already touching report utilities. It is active code and the move is cosmetic.

---

## 5. Bad Patterns / Code Smells

### 5.1. Tuple Return from `resolveEffectiveProfile()`

**Line 1150:** `func resolveEffectiveProfile(...) (string, config.AccessProfileConfig, bool)`

Three return values, one of which (`string`) is the profile name that could be derived from the `AccessProfileConfig` if needed. The `bool` for "found" makes callers write:

```go
profileName, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
if !ok { ... }
```

**Fix:** Either return `(*AccessProfileConfig, bool)` and include the name in the config, or define a `ResolvedProfile` struct:

```go
type ResolvedProfile struct {
    Name    string
    Profile config.AccessProfileConfig
    Found   bool
}
```

### 5.2. Profile Policy Helpers May Belong Near Config, But Not Necessarily As Methods

Every profile inspection (`profileAllowsPrivileged`, `profileAllowsGuarded`, `profileAllowsTool`, `profileCanReachExec`) is a standalone function taking the config as a parameter. That is not inherently wrong, but the logic is domain policy and may become useful outside doctor.

**Possible fix:** Add methods on `config.AccessProfileConfig`:

```go
func (p AccessProfileConfig) AllowsPrivileged() bool { ... }
func (p AccessProfileConfig) AllowsTool(name string) bool { ... }
func (p AccessProfileConfig) CanReachExec() bool { ... }
```

Move these to `config` only if the same semantics are needed by runtime enforcement or other packages. If they remain doctor-only heuristics, keeping them in `engine_profiles.go` is fine.

### 5.3. `validateConfigSnapshot()` is a Destructive Side-Effect Trap

**Lines 1072-1088.** Creates a temp file, writes config, reads it back, deletes it. If the temp file creation fails, it returns an error. If loading fails, it returns an error. If `defer os.Remove(path)` fails... it's silently ignored.

This is a round-trip serialization test. It's clever but fragile. If someone changes `config.Save` or `config.Load` to be stateful, this breaks.

**Recommendation:** Consider `config.ValidateRoundTrip(cfg)` as a method in the config package that uses bytes.Buffer instead of a temp file.

### 5.4. Severity Escalation Logic is Sprinkled Everywhere

The `severityFor(opts.Mode, X, condition)` pattern is called 30+ times across the file. Each call requires the author to decide: "should this block startup?" The answer is embedded in every call site instead of being centralized.

Example inconsistency:

- `filesystem.workspace_dir_empty` (line 125): `severityFor(opts.Mode, SeverityError, false)` -- NEVER blocks
- `filesystem.artifacts_dir_empty` (line 163): `severityFor(opts.Mode, SeverityError, false)` -- NEVER blocks
- `service.secret_missing` (line 496): `severityFor(opts.Mode, SeverityWarn, opts.Mode == ModeStartupService && config.IsHostedProfile(cfg.RuntimeProfile))` -- complex inline condition

These decisions are made ad-hoc at each call site with no pattern. Two different findings with the same conceptual severity could behave differently at startup because the third argument to `severityFor` was chosen differently.

**Recommendation:** The blocking severity should be declared on the `Finding` ID itself (a property of the finding type), not computed ad-hoc at each call site. Consider a `FindingDefinition` registry:

```go
var findingDefs = map[string]FindingMeta{
    "filesystem.workspace_dir_empty": {Advisory: SeverityError, BlockOnStartup: true},
    "filesystem.db_parent_missing":   {Advisory: SeverityWarn,  BlockOnStartup: false},
    ...
}
```

### 5.5. Network Utility Functions Are Better As A Small Network Section

`isLoopbackAddr` and `isInsecureHTTPURL` are small wrappers around common checks. They are not a serious smell; they just do not belong in a giant all-purpose `engine.go`.

**Recommendation:** Keep them, but move them to `engine_network.go` with the network findings.

### 5.6. `hostListTooBroad` Has Dubious Heuristics

**Lines 1210-1227.** Checks:

1. `len(hosts) > 10` → "too broad"
2. Contains `*` → "too broad"
3. Contains `*` anywhere → "too broad"
4. Starts with `*.` → "too broad"

Conditions 3 and 4 overlap. If a host starts with `*.`, it's caught by both checks. The logic is: "any wildcard is too broad" -- which could just be:

```go
func hostListTooBroad(hosts []string) bool {
    if len(hosts) > 10 { return true }
    for _, h := range hosts {
        if strings.Contains(h, "*") { return true }
    }
    return false
}
```

---

## 6. What Should Stay / What's Actually Good

1. **The overall architecture of `Evaluate()` composing findings from sources is sound.** The function at lines 27-51 is clean and easy to follow.

2. **Finding generator functions are independently testable.** Each `*Findings()` function takes `(cfg, opts)` and returns `[]Finding` -- pure functions with no side effects (except `probeFindings`). This is good functional design.

3. **`keyFileFinding()` helper (line 1090)** is well-designed. Small, focused, returns nil when not applicable.

4. **The `Finding` type itself (in `report.go`)** is well-structured with all needed fields.

5. **Domain coverage is thorough.** The file catches all the edge cases it should -- the problem is HOW it's organized, not WHAT it covers.

---

## 7. Action Plan (Prioritized)

### Phase 1: Drift Prevention (Do First)

1. **Add `collectChannels()` / `enumerateChannels()` inside `doctor`** with channel name, display name, enabled, open access, inbound policy, and allowlist-present state.
2. **Rewrite `channelExposureFindings`, `openAccessChannelNames`, `channelIngressFindings`, `anyEnabledChannels`, and public-ingress helpers** to iterate over that channel snapshot.
3. **Add focused tests for channel exposure and invalid ingress** so adding a sixth channel requires updating one helper and one test table, not several handwritten branches.

### Phase 2: Readability Without Behavior Change

4. **Split `webhookFindings` into unresolved-profile and resolved-profile helpers** to reduce nesting and make webhook policy tests easier to write.
5. **Trim repeated config fields once per function** where it improves clarity (`workspaceDir`, `artifactsDir`, `bubblewrapPath`, service/webhook secrets).
6. **Merge `severityForConfigureOrStartup` only if the replacement stays obvious**. A helper like `severityForModes(mode, advisory, blockModes...)` is clearer than widening `severityFor` with hidden configure behavior.
7. **Move `TopFindings` to `report.go`** only if a report utility cleanup is already underway.

### Phase 3: Structural Split

8. **Split into domain files** per the table in section 1.3, starting with `engine_channels.go`, `engine_webhook.go`, `engine_network.go`, and `engine_profiles.go`.
9. **Consider moving profile policy helpers to `config`** only after another package needs exactly the same semantics.
10. **Consider a `FindingDefinition` registry** for centralized severity escalation rules after the file split makes existing finding IDs easier to audit.
11. **Move `validateConfigSnapshot` to config package** as `ValidateRoundTrip()` if config grows a buffer-based save/load validation path.

---

## 8. Summary

| Metric                    | Score                                               |
| ------------------------- | --------------------------------------------------- |
| File size appropriateness | **F** -- 1369 lines is too large                    |
| Separation of concerns    | **D** -- 15 domains in one file                     |
| Code duplication          | **D** -- Channel handling repeated 3x               |
| Test coverage             | **F** -- 27 lines of tests for 1369 lines of engine |
| Readability / navigation  | **D** -- 45 functions, flat namespace               |
| Correctness               | **B+** -- Logic appears sound, no obvious bugs      |
| Functional design         | **B** -- Pure functions pattern is good             |
| **Overall**               | **D+**                                              |

**Verdict:** The code works, but it's an unmaintainable monolith. The channel duplication is the most urgent DRY violation because it can create real drift when another channel is added. Webhook decomposition and domain-file splits come next. The smaller removals (`TopFindings`, `hasNonEmpty`, `profileAllowsGuarded`) are optional cleanup, not the plan.

---

## 9. Quick Win: Channel Snapshot

```go
// Add to config/types.go or doctor package:
type ChannelSnapshot struct {
    Name          string
    Enabled       bool
    OpenAccess    bool
    InboundPolicy config.InboundPolicy
    HasAllowlist  bool
}

func collectChannels(cfg config.Config) []ChannelSnapshot {
    return []ChannelSnapshot{
        {"telegram", cfg.Channels.Telegram.Enabled, cfg.Channels.Telegram.OpenAccess,
         cfg.Channels.Telegram.InboundPolicy, hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs)},
        {"slack",    cfg.Channels.Slack.Enabled,    cfg.Channels.Slack.OpenAccess,
         cfg.Channels.Slack.InboundPolicy,    hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs)},
        {"discord",  cfg.Channels.Discord.Enabled,  cfg.Channels.Discord.OpenAccess,
         cfg.Channels.Discord.InboundPolicy,  hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs)},
        {"whatsapp", cfg.Channels.WhatsApp.Enabled, cfg.Channels.WhatsApp.OpenAccess,
         cfg.Channels.WhatsApp.InboundPolicy, hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom)},
        {"email",    cfg.Channels.Email.Enabled,    cfg.Channels.Email.OpenAccess,
         cfg.Channels.Email.InboundPolicy,    hasNonEmpty(cfg.Channels.Email.AllowedSenders)},
    }
}
```

Then rewrite `channelExposureFindings`, `openAccessChannelNames`, `channelIngressFindings`, and `anyEnabledChannels` to iterate over this list. This single change eliminates ~80 lines of duplicated channel-checking logic and ensures future channels only need ONE registration point.
