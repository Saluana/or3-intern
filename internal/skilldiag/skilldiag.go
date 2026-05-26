package skilldiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/configmeta"

	"gopkg.in/yaml.v3"
)

var manifestNames = []string{
	"skill.diagnostic.yaml",
	"skill.diagnostic.yml",
	"skill.diagnostic.json",
	"diagnostic.yaml",
	"diagnostic.yml",
	"diagnostic.json",
}

type Manifest struct {
	Version    int            `json:"version" yaml:"version"`
	Checks     []CheckSpec    `json:"checks" yaml:"checks"`
	KnownFixes []KnownFixSpec `json:"known_fixes" yaml:"known_fixes"`
	Redactions RedactionRules `json:"redactions" yaml:"redactions"`
}

type CheckSpec struct {
	ID              string   `json:"id" yaml:"id"`
	Kind            string   `json:"kind" yaml:"kind"`
	Label           string   `json:"label" yaml:"label"`
	Summary         string   `json:"summary,omitempty" yaml:"summary,omitempty"`
	Severity        string   `json:"severity,omitempty" yaml:"severity,omitempty"`
	Hint            string   `json:"hint,omitempty" yaml:"hint,omitempty"`
	Binary          string   `json:"binary,omitempty" yaml:"binary,omitempty"`
	EnvKey          string   `json:"env_key,omitempty" yaml:"env_key,omitempty"`
	ConfigKey       string   `json:"config_key,omitempty" yaml:"config_key,omitempty"`
	File            string   `json:"file,omitempty" yaml:"file,omitempty"`
	IdentityField   string   `json:"identity_field,omitempty" yaml:"identity_field,omitempty"`
	Command         []string `json:"command,omitempty" yaml:"command,omitempty"`
	ExpectContains  string   `json:"expect_contains,omitempty" yaml:"expect_contains,omitempty"`
	RequireAbsent   bool     `json:"require_absent,omitempty" yaml:"require_absent,omitempty"`
	RestartRequired bool     `json:"restart_required,omitempty" yaml:"restart_required,omitempty"`
}

type KnownFixSpec struct {
	ID              string                 `json:"id" yaml:"id"`
	Summary         string                 `json:"summary" yaml:"summary"`
	MatchCheck      string                 `json:"match_check" yaml:"match_check"`
	MatchStatus     string                 `json:"match_status,omitempty" yaml:"match_status,omitempty"`
	MatchContains   string                 `json:"match_contains,omitempty" yaml:"match_contains,omitempty"`
	Risk            configmeta.RiskLevel   `json:"risk,omitempty" yaml:"risk,omitempty"`
	RestartRequired bool                   `json:"restart_required,omitempty" yaml:"restart_required,omitempty"`
	Change          SkillEntryChangeSpec   `json:"change" yaml:"change"`
	Changes         []SkillEntryChangeSpec `json:"changes,omitempty" yaml:"changes,omitempty"`
}

type SkillEntryChangeSpec struct {
	Type  string `json:"type" yaml:"type"`
	Key   string `json:"key,omitempty" yaml:"key,omitempty"`
	Value any    `json:"value,omitempty" yaml:"value,omitempty"`
	Clear bool   `json:"clear,omitempty" yaml:"clear,omitempty"`
}

type RedactionRules struct {
	EnvKeys    []string `json:"env_keys,omitempty" yaml:"env_keys,omitempty"`
	ConfigKeys []string `json:"config_keys,omitempty" yaml:"config_keys,omitempty"`
	PathFields []string `json:"path_fields,omitempty" yaml:"path_fields,omitempty"`
}

type SkillEntryState struct {
	SkillKey string            `json:"skill_key"`
	Enabled  *bool             `json:"enabled,omitempty"`
	APIKey   string            `json:"api_key,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Config   map[string]any    `json:"config,omitempty"`
}

type Options struct {
	Entry              SkillEntryState
	Runner             Runner
	MaxCommandRuntime  time.Duration
	SkipCommandChecks  bool
}

type Result struct {
	Available       bool            `json:"available"`
	ManifestPath    string          `json:"manifest_path,omitempty"`
	Status          string          `json:"status"`
	Findings        []Finding       `json:"findings,omitempty"`
	SuggestedPlans  []SuggestedPlan `json:"suggested_plans,omitempty"`
	RestartRequired bool            `json:"restart_required,omitempty"`
}

type SuggestedPlan struct {
	ID                string               `json:"id"`
	Title             string               `json:"title"`
	Summary           string               `json:"summary"`
	RiskLevel         configmeta.RiskLevel `json:"risk_level"`
	RestartRequired   bool                 `json:"restart_required,omitempty"`
	Changes           []SuggestedChange    `json:"changes,omitempty"`
	PostApplyChecks   []SuggestedCheck     `json:"post_apply_checks,omitempty"`
	UserFacingSummary string               `json:"user_facing_summary,omitempty"`
}

type SuggestedValue struct {
	Value    any    `json:"value,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
	Present  bool   `json:"present,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

type SuggestedChange struct {
	ConfigPath string         `json:"config_path"`
	Section    string         `json:"section"`
	Channel    string         `json:"channel,omitempty"`
	Field      string         `json:"field"`
	Operation  string         `json:"operation"`
	OldValue   SuggestedValue `json:"old_value,omitempty"`
	NewValue   SuggestedValue `json:"new_value,omitempty"`
}

type SuggestedCheck struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Timeout     int    `json:"timeout_seconds,omitempty"`
}

type Finding struct {
	CheckID         string         `json:"check_id"`
	Label           string         `json:"label,omitempty"`
	Severity        string         `json:"severity"`
	Status          string         `json:"status"`
	Summary         string         `json:"summary"`
	Detail          string         `json:"detail,omitempty"`
	Hint            string         `json:"hint,omitempty"`
	RestartRequired bool           `json:"restart_required,omitempty"`
	Evidence        map[string]any `json:"evidence,omitempty"`
}

type Runner interface {
	Run(ctx context.Context, spec CommandSpec) (CommandResult, error)
}

type CommandSpec struct {
	Dir     string
	Command []string
	Timeout time.Duration
}

type CommandResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, spec CommandSpec) (CommandResult, error) {
	if len(spec.Command) == 0 {
		return CommandResult{}, fmt.Errorf("command is required")
	}
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	cmd.Dir = spec.Dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	return result, err
}

func LoadManifest(dir string) (Manifest, string, bool, error) {
	for _, name := range manifestNames {
		path := filepath.Join(strings.TrimSpace(dir), name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return Manifest{}, "", false, fmt.Errorf("diagnostic manifest must be a regular file")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return Manifest{}, "", false, err
		}
		var manifest Manifest
		if strings.HasSuffix(name, ".json") {
			if err := json.Unmarshal(body, &manifest); err != nil {
				return Manifest{}, path, true, fmt.Errorf("invalid diagnostic manifest: %w", err)
			}
		} else {
			if err := yaml.Unmarshal(body, &manifest); err != nil {
				return Manifest{}, path, true, fmt.Errorf("invalid diagnostic manifest: %w", err)
			}
		}
		if manifest.Version == 0 {
			manifest.Version = 1
		}
		if err := validateManifest(manifest); err != nil {
			return Manifest{}, path, true, err
		}
		return manifest, path, true, nil
	}
	return Manifest{}, "", false, nil
}

func Evaluate(ctx context.Context, dir string, opts Options) (Result, error) {
	manifest, path, ok, err := LoadManifest(dir)
	if err != nil {
		return Result{Available: true, ManifestPath: path, Status: "invalid"}, err
	}
	if !ok {
		return Result{Available: false, Status: "unavailable"}, nil
	}
	return EvaluateManifest(ctx, manifest, path, opts), nil
}

func EvaluateManifest(ctx context.Context, manifest Manifest, manifestPath string, opts Options) Result {
	result := Result{Available: true, ManifestPath: manifestPath, Status: "ok"}
	baseDir := manifestPathOrDir(manifestPath, opts.Entry.SkillKey)
	for _, check := range manifest.Checks {
		finding := runCheck(ctx, baseDir, check, manifest.Redactions, opts)
		if finding.Status != "pass" {
			result.Status = "issues"
			result.RestartRequired = result.RestartRequired || finding.RestartRequired
		}
		result.Findings = append(result.Findings, finding)
	}
	result.SuggestedPlans = buildSuggestedPlans(manifest, result.Findings, opts.Entry)
	if len(result.SuggestedPlans) > 0 && result.Status == "ok" {
		result.Status = "issues"
	}
	for _, plan := range result.SuggestedPlans {
		if plan.RestartRequired {
			result.RestartRequired = true
		}
	}
	return result
}

func validateManifest(manifest Manifest) error {
	seen := map[string]struct{}{}
	for _, check := range manifest.Checks {
		id := strings.TrimSpace(check.ID)
		if id == "" {
			return fmt.Errorf("diagnostic manifest check id is required")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate diagnostic check id %q", id)
		}
		seen[id] = struct{}{}
		switch strings.ToLower(strings.TrimSpace(check.Kind)) {
		case "binary", "env", "config", "api_key", "json_file", "json_config_file", "command":
		default:
			return fmt.Errorf("unsupported diagnostic check kind %q", check.Kind)
		}
	}
	return nil
}

func runCheck(ctx context.Context, baseDir string, check CheckSpec, redactions RedactionRules, opts Options) Finding {
	finding := Finding{
		CheckID:         strings.TrimSpace(check.ID),
		Label:           strings.TrimSpace(check.Label),
		Severity:        firstNonEmpty(strings.TrimSpace(check.Severity), "warning"),
		Status:          "pass",
		Summary:         firstNonEmpty(strings.TrimSpace(check.Summary), strings.TrimSpace(check.Label), strings.TrimSpace(check.ID)),
		Hint:            strings.TrimSpace(check.Hint),
		RestartRequired: check.RestartRequired,
	}
	setFailure := func(detail string, evidence map[string]any) Finding {
		finding.Status = "fail"
		finding.Detail = strings.TrimSpace(detail)
		finding.Evidence = evidence
		return finding
	}
	setPass := func(detail string, evidence map[string]any) Finding {
		finding.Detail = strings.TrimSpace(detail)
		finding.Evidence = evidence
		return finding
	}
	switch strings.ToLower(strings.TrimSpace(check.Kind)) {
	case "binary":
		path, err := exec.LookPath(strings.TrimSpace(check.Binary))
		if err != nil {
			return setFailure("required binary is not available", map[string]any{"binary": check.Binary})
		}
		return setPass("required binary is present", map[string]any{"binary": check.Binary, "path": redactPath(path)})
	case "env":
		value := strings.TrimSpace(opts.Entry.Env[strings.TrimSpace(check.EnvKey)])
		if value == "" {
			return setFailure("required environment value is not configured", map[string]any{"env_key": check.EnvKey})
		}
		return setPass("environment value is configured", map[string]any{"env_key": check.EnvKey, "value": redactValue(check.EnvKey, value, redactions)})
	case "config":
		value, ok := opts.Entry.Config[strings.TrimSpace(check.ConfigKey)]
		if check.RequireAbsent {
			if !ok || isEmptyValue(value) {
				return setPass("stale config value is absent", map[string]any{"config_key": check.ConfigKey})
			}
			return setFailure("stale config value is still present", map[string]any{"config_key": check.ConfigKey, "value": redactAny(check.ConfigKey, value, redactions)})
		}
		if !ok || isEmptyValue(value) {
			return setFailure("required config value is missing", map[string]any{"config_key": check.ConfigKey})
		}
		return setPass("config value is present", map[string]any{"config_key": check.ConfigKey, "value": redactAny(check.ConfigKey, value, redactions)})
	case "api_key":
		value := strings.TrimSpace(opts.Entry.APIKey)
		if check.RequireAbsent {
			if value == "" {
				return setPass("stored api key is absent", map[string]any{"field": "api_key"})
			}
			return setFailure("stored api key is still configured", map[string]any{"field": "api_key", "value": redactValue("api_key", value, redactions)})
		}
		if value == "" {
			return setFailure("required api key is missing", map[string]any{"field": "api_key"})
		}
		return setPass("stored api key is configured", map[string]any{"field": "api_key", "value": redactValue("api_key", value, redactions)})
	case "json_file":
		path := resolveManifestPath(baseDir, check.File)
		return runJSONFileCheck(path, check, redactions, setFailure, setPass)
	case "json_config_file":
		pathValue, ok := opts.Entry.Config[strings.TrimSpace(check.ConfigKey)]
		if !ok || isEmptyValue(pathValue) {
			return setFailure("json file path is missing from config", map[string]any{"config_key": check.ConfigKey})
		}
		path, ok := pathValue.(string)
		if !ok || strings.TrimSpace(path) == "" {
			return setFailure("json file path config value must be a string", map[string]any{"config_key": check.ConfigKey})
		}
		finding := runJSONFileCheck(resolveManifestPath(baseDir, path), check, redactions, setFailure, setPass)
		if finding.Evidence == nil {
			finding.Evidence = map[string]any{}
		}
		finding.Evidence["config_key"] = check.ConfigKey
		return finding
	case "command":
		if opts.SkipCommandChecks {
			return setPass("command check skipped in read-only Doctor chat", map[string]any{"skipped": true, "kind": "command"})
		}
		runner := opts.Runner
		if runner == nil {
			runner = ExecRunner{}
		}
		result, err := runner.Run(ctx, CommandSpec{Dir: baseDir, Command: append([]string{}, check.Command...), Timeout: effectiveTimeout(opts.MaxCommandRuntime)})
		evidence := map[string]any{"command": append([]string{}, check.Command...), "stdout": redactOutput(result.Stdout), "stderr": redactOutput(result.Stderr), "exit_code": result.ExitCode}
		if err != nil {
			return setFailure("diagnostic command failed", evidence)
		}
		if expected := strings.TrimSpace(check.ExpectContains); expected != "" {
			joined := result.Stdout + "\n" + result.Stderr
			if !strings.Contains(joined, expected) {
				return setFailure("diagnostic command output did not match expectation", evidence)
			}
		}
		return setPass("diagnostic command succeeded", evidence)
	default:
		return setFailure("unsupported diagnostic check kind", map[string]any{"kind": check.Kind})
	}
}

func buildSuggestedPlans(manifest Manifest, findings []Finding, entry SkillEntryState) []SuggestedPlan {
	plans := make([]SuggestedPlan, 0, len(manifest.KnownFixes))
	for _, fix := range manifest.KnownFixes {
		for _, finding := range findings {
			if !matchesKnownFix(fix, finding) {
				continue
			}
			changes := buildPlanChanges(entry, fix)
			if len(changes) == 0 {
				continue
			}
			plan := SuggestedPlan{
				ID:                "skilldiag_" + strings.TrimSpace(fix.ID),
				Title:             strings.TrimSpace(fix.Summary),
				Summary:           strings.TrimSpace(fix.Summary),
				RiskLevel:         coalesceRisk(fix.Risk, configmeta.RiskWarning),
				RestartRequired:   fix.RestartRequired,
				Changes:           changes,
				PostApplyChecks:   []SuggestedCheck{{ID: "skilldiag." + strings.TrimSpace(fix.ID), Description: "Re-run skill diagnostics", Timeout: 10}},
				UserFacingSummary: strings.TrimSpace(fix.Summary),
			}
			plans = append(plans, plan)
			break
		}
	}
	return plans
}

func matchesKnownFix(fix KnownFixSpec, finding Finding) bool {
	if !strings.EqualFold(strings.TrimSpace(fix.MatchCheck), strings.TrimSpace(finding.CheckID)) {
		return false
	}
	if status := strings.TrimSpace(fix.MatchStatus); status != "" && !strings.EqualFold(status, finding.Status) {
		return false
	}
	if contains := strings.TrimSpace(fix.MatchContains); contains != "" && !strings.Contains(strings.ToLower(finding.Detail), strings.ToLower(contains)) {
		return false
	}
	return true
}

func buildPlanChanges(entry SkillEntryState, fix KnownFixSpec) []SuggestedChange {
	specs := append([]SkillEntryChangeSpec{}, fix.Changes...)
	if len(specs) == 0 && strings.TrimSpace(fix.Change.Type) != "" {
		specs = append(specs, fix.Change)
	}
	changes := make([]SuggestedChange, 0, len(specs))
	for _, spec := range specs {
		change, ok := buildPlanChange(entry, spec)
		if ok {
			changes = append(changes, change)
		}
	}
	return changes
}

func buildPlanChange(entry SkillEntryState, change SkillEntryChangeSpec) (SuggestedChange, bool) {
	skillKey := strings.TrimSpace(entry.SkillKey)
	if skillKey == "" {
		return SuggestedChange{}, false
	}
	switch strings.ToLower(strings.TrimSpace(change.Type)) {
	case "enabled":
		newValue, ok := boolValue(change.Value)
		if !ok {
			return SuggestedChange{}, false
		}
		oldValue := false
		if entry.Enabled != nil {
			oldValue = *entry.Enabled
		}
		return SuggestedChange{
			ConfigPath: "skills.entries." + skillKey + ".enabled",
			Section:    "skills_entry",
			Channel:    skillKey,
			Field:      "enabled",
			Operation:  "toggle",
			OldValue:   plainSuggestedValue(oldValue),
			NewValue:   plainSuggestedValue(newValue),
		}, true
	case "env":
		key := strings.TrimSpace(change.Key)
		oldValue := ""
		if entry.Env != nil {
			oldValue = entry.Env[key]
		}
		sensitive := shouldRedactKey(key, RedactionRules{})
		newValue := fmt.Sprint(change.Value)
		if change.Clear {
			newValue = "clear"
		}
		return SuggestedChange{
			ConfigPath: "skills.entries." + skillKey + ".env." + key,
			Section:    "skills_entry",
			Channel:    skillKey,
			Field:      "env." + key,
			Operation:  "set",
			OldValue:   suggestedValue(oldValue, sensitive, "configured"),
			NewValue:   suggestedNewValue(newValue, sensitive),
		}, true
	case "config":
		key := strings.TrimSpace(change.Key)
		oldValue := any(nil)
		if entry.Config != nil {
			oldValue = entry.Config[key]
		}
		sensitive := shouldRedactKey(key, RedactionRules{})
		newValue := change.Value
		if change.Clear {
			newValue = "clear"
		}
		return SuggestedChange{
			ConfigPath: "skills.entries." + skillKey + ".config." + key,
			Section:    "skills_entry",
			Channel:    skillKey,
			Field:      "config." + key,
			Operation:  "set",
			OldValue:   suggestedValue(oldValue, sensitive, "configured"),
			NewValue:   suggestedNewValue(newValue, sensitive),
		}, true
	case "api_key":
		newValue := fmt.Sprint(change.Value)
		if change.Clear {
			newValue = "clear"
		}
		return SuggestedChange{
			ConfigPath: "skills.entries." + skillKey + ".apiKey",
			Section:    "skills_entry",
			Channel:    skillKey,
			Field:      "api_key",
			Operation:  "set",
			OldValue:   suggestedValue(entry.APIKey, true, "configured"),
			NewValue:   suggestedNewValue(newValue, true),
		}, true
	default:
		return SuggestedChange{}, false
	}
}

func plainSuggestedValue(value any) SuggestedValue {
	return SuggestedValue{Value: value, Present: !isEmptyValue(value)}
}

func suggestedValue(value any, redacted bool, presentSummary string) SuggestedValue {
	if !redacted {
		return plainSuggestedValue(value)
	}
	present := !isEmptyValue(value)
	item := SuggestedValue{Redacted: true, Present: present}
	if present {
		item.Summary = strings.TrimSpace(presentSummary)
		if item.Summary == "" {
			item.Summary = "configured"
		}
		return item
	}
	item.Summary = "not set"
	return item
}

func suggestedNewValue(value any, redacted bool) SuggestedValue {
	if typed, ok := value.(string); ok && strings.EqualFold(strings.TrimSpace(typed), "clear") {
		return plainSuggestedValue("clear")
	}
	if !redacted {
		return plainSuggestedValue(value)
	}
	return suggestedValue(value, true, "proposed")
}

func RedactForAI(result Result) Result {
	redacted := result
	for i := range redacted.Findings {
		for key, value := range redacted.Findings[i].Evidence {
			redacted.Findings[i].Evidence[key] = redactAny(key, value, RedactionRules{})
		}
	}
	return redacted
}

func redactAny(key string, value any, rules RedactionRules) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			redacted[nestedKey] = redactAny(nestedKey, nestedValue, rules)
		}
		return redacted
	case map[string]string:
		redacted := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			redacted[nestedKey] = redactAny(nestedKey, nestedValue, rules)
		}
		return redacted
	case []any:
		redacted := make([]any, 0, len(typed))
		for _, item := range typed {
			redacted = append(redacted, redactAny(key, item, rules))
		}
		return redacted
	case []string:
		redacted := make([]any, 0, len(typed))
		for _, item := range typed {
			redacted = append(redacted, redactAny(key, item, rules))
		}
		return redacted
	case string:
		return redactValue(key, typed, rules)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "[redacted]"
		}
		return redactValue(key, string(encoded), rules)
	}
}

func redactValue(key, value string, rules RedactionRules) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	keyLower := strings.ToLower(strings.TrimSpace(key))
	if shouldRedactPath(keyLower, rules) || strings.Contains(trimmed, string(filepath.Separator)) {
		return redactPath(trimmed)
	}
	if shouldMaskIdentity(keyLower) {
		if strings.Contains(trimmed, "@") {
			return maskEmail(trimmed)
		}
		return "[identity]"
	}
	if shouldRedactKey(keyLower, rules) {
		return "[redacted]"
	}
	if strings.Contains(trimmed, "@") {
		return maskEmail(trimmed)
	}
	if len(trimmed) > 12 {
		return trimmed[:3] + "...[redacted]"
	}
	return "[redacted]"
}

func redactPath(path string) string {
	base := filepath.Base(strings.TrimSpace(path))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "[path]"
	}
	return "[path]/" + base
}

func redactOutput(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return "[redacted output]"
}

func shouldRedactKey(key string, rules RedactionRules) bool {
	for _, candidate := range append(append([]string{}, rules.EnvKeys...), rules.ConfigKeys...) {
		if strings.EqualFold(strings.TrimSpace(candidate), key) {
			return true
		}
	}
	sensitiveHints := []string{"token", "secret", "password", "key", "credential", "oauth", "auth", "client_id", "clientid", "refresh", "access"}
	for _, hint := range sensitiveHints {
		if strings.Contains(key, hint) {
			return true
		}
	}
	return false
}

func shouldRedactPath(key string, rules RedactionRules) bool {
	for _, candidate := range rules.PathFields {
		if strings.EqualFold(strings.TrimSpace(candidate), key) {
			return true
		}
	}
	return strings.Contains(key, "path") || strings.Contains(key, "dir") || strings.Contains(key, "file")
}

func shouldMaskIdentity(key string) bool {
	identityHints := []string{"email", "account", "identity", "user"}
	for _, hint := range identityHints {
		if strings.Contains(key, hint) {
			return true
		}
	}
	return false
}

func runJSONFileCheck(path string, check CheckSpec, redactions RedactionRules, setFailure func(string, map[string]any) Finding, setPass func(string, map[string]any) Finding) Finding {
	body, err := os.ReadFile(path)
	if err != nil {
		return setFailure("json file could not be read", map[string]any{"file": redactPath(path)})
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return setFailure("json file is invalid", map[string]any{"file": redactPath(path)})
	}
	if field := strings.TrimSpace(check.IdentityField); field != "" {
		identity, _ := payload[field]
		if isEmptyValue(identity) {
			return setFailure("identity field is missing from json file", map[string]any{"file": redactPath(path), "identity_field": field})
		}
		return setPass("json file is valid", map[string]any{"file": redactPath(path), "identity_field": field, "identity": redactAny(field, identity, redactions), "payload": redactAny("payload", payload, redactions)})
	}
	return setPass("json file is valid", map[string]any{"file": redactPath(path), "payload": redactAny("payload", payload, redactions)})
}

func maskEmail(value string) string {
	parts := strings.SplitN(strings.TrimSpace(value), "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "[redacted]"
	}
	local := parts[0]
	if len(local) > 1 {
		local = local[:1] + "***"
	} else {
		local = "***"
	}
	return local + "@" + parts[1]
}

func manifestPathOrDir(path, fallback string) string {
	if strings.TrimSpace(path) == "" {
		return fallback
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func resolveManifestPath(dir, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(dir, strings.TrimSpace(value))
}

func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 5 * time.Second
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
	}
	return false, false
}

func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	}
	return false
}

func coalesceRisk(primary, fallback configmeta.RiskLevel) configmeta.RiskLevel {
	if primary != "" {
		return primary
	}
	return fallback
}
