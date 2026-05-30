# Thermo-Nuclear Code Quality Review: `internal/adminflow`

**Date:** 2026-05-28
**Scope:** `internal/adminflow/` — 10 files, 2303 lines total
**Reviewer:** Automated (thermo-nuclear skill)

---

## File Inventory

| File | Lines | Role |
|------|-------|------|
| `validator.go` | 417 | Plan staging, validation pipeline, value helpers, config cloning, reflection path resolver |
| `validator_test.go` | 389 | Tests for validator |
| `redaction_test.go` | 366 | Tests for redaction |
| `redaction.go` | 229 | Secret redaction, string regex redaction, env/JSON map redaction, prompt injection detection |
| `risk_test.go` | 268 | Tests for risk classification |
| `risk.go` | 217 | Risk classification with 6 escalation detectors |
| `types.go` | 126 | Shared type definitions |
| `provider.go` | 99 | Admin brain provider detection |
| `provider_test.go` | 100 | Tests for provider |
| `plan_normalize.go` | 92 | Plan change normalization |

---

## 1. Structural Regressions

### 1.1 `validator.go` is a 417-line grab bag — decompose it

`validator.go` conflates at least 5 distinct responsibilities:

- **Validation pipeline orchestration** (`Stage`)
- **Plan change application** (`applyPlanChange`, `validatePlanChangeValue`, `isProviderModelChange`)
- **Value coercion helpers** (`boolPlanValue`, `stringifyPlanValue`, `planValuesEqual`, `valuePresent`)
- **Reflection-based config path resolution** (`resolveConfigPathValue` — 47 lines of reflection)
- **Config cloning** (`cloneConfig`)
- **Doctor report summarization** (`summarizeDoctorBlocks`)
- **Risk authority checking** (`exceedsApprovedAuthority`)
- **Check naming** (`staleCheckName`, `applyCheckName`)
- **Live-reload key detection** (`planLiveReloadKeys`)
- **Plan decoration from metadata** (`decoratePlanFromMetadata`)

**Remedy:** Extract at minimum:
- Value coercion helpers (`boolPlanValue`, `stringifyPlanValue`, `planValuesEqual`, `valuePresent`) into a `plan_values.go` or similar.
- `resolveConfigPathValue` into its own file — it's a self-contained reflection walker with no dependency on validation logic.
- `applyPlanChange` and `validatePlanChangeValue` into `plan_apply.go`.

This would bring `validator.go` down to ~200 lines focused purely on the `Stage` orchestration pipeline.

### 1.2 `redaction.go` mixes secret redaction, text scrubbing, and prompt injection detection

`redaction.go` (229 lines) bundles:

- **Typed value redaction** (`RedactValue`, `redactSecret`) — used by plan construction
- **Regex-based string scrubbing** (`RedactString`) — used by log/output sanitization
- **Map redaction** (`RedactEnvMap`, `RedactJSON`) — used by diagnostic/AI pipelines
- **Prompt injection detection** (`IsPromptInjection`) — a security classifier, not a redaction function
- **AI sanitization** (`SanitizeForAI`) — a composition of the above

Prompt injection detection is a fundamentally different concern from secret redaction. It's a content classifier, not a redactor.

**Remedy:** Split into `redaction.go` (value/map/string redaction) and `injection.go` or `sanitize.go` (prompt injection detection and `SanitizeForAI`).

---

## 2. Missed Opportunities for Dramatic Simplification

### 2.1 `isTruthyValue` and `boolPlanValue` are near-duplicate functions

Both live in the same package and do overlapping work:

- `isTruthyValue` (`risk.go:203`) — checks `bool`, `string`, `int/int32/int64` for truthiness
- `boolPlanValue` (`validator.go:372`) — parses `bool` and `string` into a Go bool

The string parsing logic is nearly identical: both check `"true"`, `"1"`, `"yes"`, `"on"` (and their negatives). The only difference is `boolPlanValue` returns `(bool, bool)` while `isTruthyValue` returns `bool`.

**Code judo:** Collapse into a single `parseBoolish(value any) (bool, bool)` function. `isTruthyValue` becomes `func isTruthyValue(v any) bool { b, ok := parseBoolish(v); return ok && b }`. This eliminates the duplicated string-parsing switch and the subtle behavioral divergence.

### 2.2 Risk escalation is 6 copies of the same pattern — make it table-driven

`risk.go` repeats this exact structure 6 times:

```go
if hasXxxChange(plan.Changes) {
    decision.EscalationReasons = append(decision.EscalationReasons, "xxx change")
    if configmeta.RiskRank(decision.Level) < configmeta.RiskRank(configmeta.RiskXxx) {
        decision.Level = configmeta.RiskXxx
    }
}
```

Each `has*Change` function is itself a loop over changes with `strings.Contains` checks. The entire escalation system could be a single table:

```go
var escalationRules = []struct {
    reason   string
    minRisk  configmeta.RiskLevel
    match    func(SettingsPlanChange) bool
}{
    {"restart required", configmeta.RiskNotice, func(c SettingsPlanChange) bool { /* ... */ }},
    {"skill authentication change", configmeta.RiskWarning, hasSkillAuthChange},
    // ...
}
```

Then `ClassifyRisk` becomes a single loop over the table. This deletes ~80 lines of repetitive boilerplate and makes adding new escalation rules a one-line change instead of a 15-line function.

### 2.3 `decoratePlanFromMetadata` and `ClassifyRisk` overlap

`decoratePlanFromMetadata` (`validator.go:289`) iterates all changes, looks up metadata, and escalates `plan.RiskLevel`, `plan.RestartRequired`, `plan.RequiresApproval`, `plan.RequiresStepUpAuth`. Then `ClassifyRisk` runs immediately after, iterates all changes again, reads `change.MetadataRisk` (which `decoratePlanFromMetadata` just set), and re-derives the same flags.

The boundary between these two functions is unclear. `decoratePlanFromMetadata` sets plan-level flags from config metadata; `ClassifyRisk` escalates further based on content patterns. But both write to the same fields on the plan/decision.

**Remedy:** Merge the metadata-decoration step into `ClassifyRisk` itself, or make `ClassifyRisk` explicitly take the metadata-decorated plan as a precondition and document it. The current two-pass approach is confusing and fragile.

### 2.4 `staleCheckName` and `applyCheckName` are identical except for the prefix

```go
func staleCheckName(change SettingsPlanChange) string {
    if strings.TrimSpace(change.ConfigPath) != "" {
        return "stale." + change.ConfigPath
    }
    // ...
}

func applyCheckName(change SettingsPlanChange) string {
    if strings.TrimSpace(change.ConfigPath) != "" {
        return "apply." + change.ConfigPath
    }
    // ...
}
```

**Remedy:** One function: `func checkName(prefix string, change SettingsPlanChange) string`.

---

## 3. Spaghetti / Branching Complexity

### 3.1 `Stage` repeats the same error-return pattern 6 times

The `Stage` method has this pattern copy-pasted after every validation step:

```go
results = append(results, PlanValidationResult{Check: "...", Status: "fail", Message: err.Error()})
plan.ValidationResults = results
return ValidationState{...fields..., Validation: results}, someError
```

This is 3 lines × 6 occurrences = 18 lines of pure boilerplate. It also creates a maintenance hazard: if the return structure changes, every site must be updated.

**Remedy:** Extract a helper like `func failStage(results []PlanValidationResult, plan *SettingsChangePlan, state *ValidationState, err error) (ValidationState, error)` or restructure the pipeline to accumulate results and check errors at the end.

### 3.2 `applyPlanChange` has a subtle fallthrough path

```go
case "", "set":
    changed, err := configedit.ApplyFieldValue(...)
    if changed { return true, nil }
    if value, ok := boolPlanValue(...); ok {
        return configedit.SetToggleFieldValue(...), nil
    }
    return false, nil
```

The empty-string operation defaults to `"set"`, but then if `ApplyFieldValue` returns `changed=false`, it silently tries a boolean toggle path. This is a hidden branch that makes the `"set"` operation behave differently depending on the value type. The caller has no way to know which path was taken.

This is a design smell: the operation type should be explicit. If the plan normalizer already converts bool-valued sets to `"toggle"` (which `NormalizePlanChange` does at `plan_normalize.go:60-64`), then this fallback in `applyPlanChange` should be unreachable in practice. If it's unreachable, delete it. If it's reachable, document why.

---

## 4. Boundary / Abstraction / Type-Contract Problems

### 4.1 `PlanValidator` is a useless type

`PlanValidator` is an empty struct with a single method `Stage`. Every caller uses `(PlanValidator{}).Stage(...)` — constructing a zero-value literal just to call a method. This is a namespace pretending to be a type.

**Remedy:** Make `Stage` a package-level function: `func StagePlan(current config.Config, plan *SettingsChangePlan, opts ValidationOptions) (ValidationState, error)`. Delete the `PlanValidator` type entirely. If future state is needed, introduce it then.

### 4.2 `cloneConfig` silently returns the original on failure

```go
func cloneConfig(current config.Config) config.Config {
    data, err := json.Marshal(current)
    if err != nil {
        return current  // ← returns the LIVE config, not a copy
    }
    var next config.Config
    if err := json.Unmarshal(data, &next); err != nil {
        return current  // ← same problem
    }
    return next
}
```

If cloning fails, `Stage` proceeds to mutate the live config via `applyPlanChange`. This is a data-integrity risk. The caller has no way to detect that cloning failed.

**Remedy:** Return `(config.Config, error)` and fail the pipeline if cloning fails. Or panic — a JSON round-trip on a Go struct should never fail in practice, and if it does, something is deeply wrong.

### 4.3 `resolveConfigPathValue` is a bespoke reflection-based JSON path walker

This 47-line function (`validator.go:214-261`) walks a Go struct using reflection, matching JSON tags. It handles structs, maps, and pointer indirection. It's fragile (breaks if struct tags change), hard to test in isolation, and likely duplicates functionality in `configedit` or `configmeta`.

**Remedy:** Check whether `configmeta.GetByPath` or a `configedit` helper already provides this capability. If not, this should at minimum be extracted to its own file with dedicated tests, since it's a self-contained reflection utility.

### 4.4 `RedactedValue.Value` is `any` with no type contract

`RedactedValue.Value` is typed as `any`, and the package has 4 different functions that type-switch on it (`boolPlanValue`, `stringifyPlanValue`, `isTruthyValue`, `valuePresent`). Each handles a slightly different set of types. There's no invariant about what types `Value` can hold.

This isn't necessarily wrong for a config system that handles arbitrary values, but the scattered type-switches suggest a missing abstraction. Consider whether `RedactedValue` should carry a type discriminator or whether the value should be normalized to `string` earlier in the pipeline.

---

## 5. File-Size and Decomposition Concerns

### 5.1 `validator.go` at 417 lines is approaching the danger zone

Not yet at 1k, but it's the largest file in the package and growing. The `Stage` method alone is 76 lines. Combined with the 10+ helper functions, this file is doing too much. See §1.1 for decomposition plan.

### 5.2 Test files are large but acceptable

`validator_test.go` (389 lines) and `redaction_test.go` (366 lines) are large but contain well-structured table-driven tests. No action needed.

---

## 6. Modularity and Abstraction Issues

### 6.1 `RedactEnvMap` is dead code

Grep shows `RedactEnvMap` is only referenced in `redaction.go` (definition) and `redaction_test.go` (tests). No production code calls it.

**Remedy:** Delete `RedactEnvMap` and its tests. If it's needed later, it can be re-added from git history. Dead code is a maintenance burden.

### 6.2 Duplicated `sensitiveKeys` maps in `RedactEnvMap` and `RedactJSON`

Both functions define inline `sensitiveKeys` maps with nearly identical entries. `RedactJSON` has two extra keys (`refresh_token`, `access_token`). This is copy-paste with drift.

**Remedy:** Extract a single `var sensitiveKeyFragments = []string{...}` at package level and have both functions (if `RedactEnvMap` is kept) reference it. Or delete `RedactEnvMap` (§6.1) and just have one map.

### 6.3 `RedactString` recompiles 8 regexes on every call

```go
func RedactString(text string) string {
    result = regexp.MustCompile(`...`).ReplaceAllString(result, ...)
    result = regexp.MustCompile(`...`).ReplaceAllString(result, ...)
    // ... 6 more times
}
```

`regexp.MustCompile` compiles at call time. Since these patterns are constants, they should be package-level `var` declarations compiled once at init.

**Remedy:** Move all 8 regex patterns to package-level variables:

```go
var (
    reAPIKey  = regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*["']?([^\s"']+)["']?`)
    reToken   = regexp.MustCompile(`(?i)(token|secret|password|passwd)\s*[:=]\s*["']?([^\s"']+)["']?`)
    // ...
)
```

### 6.4 `IsPromptInjection` is a naive substring check with high false-positive risk

The patterns include `"system:"`, `"user:"`, `"assistant:"` — strings that appear routinely in legitimate log output, chat transcripts, and documentation. This function is called by `SanitizeForAI`, which prefixes `[UNTRUSTED CONTENT DETECTED]` to the text.

This means any log line containing `"system: boot complete"` or a chat message containing `"user: hello"` will be flagged as prompt injection.

**Remedy:** This is a design problem, not a code-quality problem. Flag it for product-level review. At minimum, consider requiring patterns to appear at word boundaries or in suspicious combinations rather than as bare substrings.

---

## 7. Legibility and Maintainability Concerns

### 7.1 `NormalizePlanChange` has tangled conditional logic

`plan_normalize.go:20-65` has nested conditionals for path resolution, metadata lookup, section/field fallback, channel extraction, and operation inference. The function tries to handle every normalization case in one pass.

The `if !ok && section != "" && field != ""` fallback, the `strings.Contains(metaPath, "*")` wildcard check, and the hardcoded `provider` fallback at line 51 all suggest this function is accumulating special cases.

**Remedy:** Consider splitting into `resolveFromPath`, `resolveFromSectionField`, and `resolveHardcodedFallback` functions. The current single-function approach is becoming a decision tree that's hard to follow.

### 7.2 `planLiveReloadKeys` uses string prefix matching on two different fields

```go
if strings.HasPrefix(change.ConfigPath, "modelRouting.") || strings.HasPrefix(change.Field, "routing_") {
```

This checks `ConfigPath` for one prefix and `Field` for a different prefix. The two conditions are conceptually the same check (is this a model routing change?) but expressed differently because the two fields use different naming conventions. This is fragile — if either naming convention changes, this check breaks silently.

### 7.3 `valuePresent` has a reflection fallback that could panic

```go
default:
    zero := reflect.Zero(reflect.TypeOf(value)).Interface()
    return !reflect.DeepEqual(value, zero)
```

If `value` is `nil`, `reflect.TypeOf(value)` returns `nil`, and `reflect.Zero(nil)` panics. The `nil` check above should catch this, but the type switch doesn't cover all cases (e.g., `[]int`, `map[string]int`), so the reflection fallback is reachable with non-nil values of unexpected types.

---

## Summary of Priority Actions

| Priority | Finding | Impact |
|----------|---------|--------|
| **P0** | `cloneConfig` silent failure returns live config | Data integrity risk |
| **P0** | `isTruthyValue` / `boolPlanValue` duplication | Conflicting truthiness semantics |
| **P1** | `validator.go` grab bag (417 lines, 5+ responsibilities) | Maintainability |
| **P1** | Risk escalation boilerplate (6 copies of same pattern) | Maintainability, extensibility |
| **P1** | `RedactString` recompiles 8 regexes per call | Performance |
| **P1** | `PlanValidator` empty struct | Unnecessary indirection |
| **P2** | `decoratePlanFromMetadata` + `ClassifyRisk` overlap | Confusing ownership of risk flags |
| **P2** | `resolveConfigPathValue` bespoke reflection walker | Fragile, likely duplicated |
| **P2** | `RedactEnvMap` dead code | Maintenance burden |
| **P2** | Duplicated `sensitiveKeys` maps | Drift risk |
| **P2** | `staleCheckName` / `applyCheckName` near-duplication | Minor cleanup |
| **P3** | `IsPromptInjection` false-positive risk | Product-level concern |
| **P3** | `NormalizePlanChange` accumulating special cases | Readability |
| **P3** | `RedactedValue.Value` as `any` with scattered type switches | Type contract weakness |

---

## Approval Assessment

**Not approved** under thermo-nuclear standards.

The package has solid test coverage and the individual functions generally work correctly. However, the structural issues — particularly the `validator.go` grab bag, the duplicated truthiness parsers, the silent `cloneConfig` failure, and the repetitive risk escalation pattern — represent meaningful maintainability debt that will compound as the package grows.

The "code judo" move here is the table-driven risk escalation (§2.2) combined with the `PlanValidator` deletion (§4.1) and the `isTruthyValue`/`boolPlanValue` unification (§2.1). Together, these three changes would delete ~120 lines of boilerplate, eliminate 2 near-duplicate abstractions, and make the risk classification system dramatically easier to extend.
