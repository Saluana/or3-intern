package skilldiag

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/configmeta"
)

type fakeRunner struct {
	result CommandResult
	err    error
}

func (f fakeRunner) Run(context.Context, CommandSpec) (CommandResult, error) {
	return f.result, f.err
}

func TestEvaluateManifest_GeneratesWarningPlanForStaleManagedReference(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "skill.diagnostic.yaml")
	if err := os.WriteFile(manifestPath, []byte(`version: 1
redactions:
  env_keys: [TOKEN]
checks:
  - id: stale_managed_reference
    kind: config
    label: Managed reference
    summary: Stale managed reference is still configured
    severity: warning
    config_key: managed_reference
    require_absent: true
known_fixes:
  - id: clear_managed_reference
    summary: Clear the stale managed reference
    match_check: stale_managed_reference
    match_status: fail
    risk: warning
    restart_required: true
    change:
      type: config
      key: managed_reference
      clear: true
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifest, path, ok, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !ok || path != manifestPath {
		t.Fatalf("expected manifest discovery, got ok=%t path=%q", ok, path)
	}
	result := EvaluateManifest(context.Background(), manifest, path, Options{Entry: SkillEntryState{SkillKey: "demo", Config: map[string]any{"managed_reference": "managed://cred-1"}}})
	if result.Status != "issues" {
		t.Fatalf("expected issues status, got %#v", result)
	}
	if len(result.SuggestedPlans) != 1 {
		t.Fatalf("expected one suggested plan, got %#v", result)
	}
	plan := result.SuggestedPlans[0]
	if plan.RiskLevel != configmeta.RiskWarning {
		t.Fatalf("expected warning risk plan, got %#v", plan)
	}
	if !plan.RestartRequired {
		t.Fatalf("expected restart-required plan, got %#v", plan)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].ConfigPath != "skills.entries.demo.config.managed_reference" {
		t.Fatalf("unexpected plan changes: %#v", plan.Changes)
	}
	if plan.Changes[0].NewValue.Value != "clear" {
		t.Fatalf("expected clear sentinel for config removal, got %#v", plan.Changes[0].NewValue)
	}
}

func TestEvaluateManifest_RedactsSensitiveSuggestedPlanValues(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		KnownFixes: []KnownFixSpec{{
			ID:          "clear_api_token",
			Summary:     "Clear stored API token",
			MatchCheck:  "config-token",
			MatchStatus: "fail",
			Risk:        configmeta.RiskWarning,
			Change:      SkillEntryChangeSpec{Type: "config", Key: "api_token", Clear: true},
		}},
		Checks: []CheckSpec{{ID: "config-token", Kind: "config", Label: "Token", Summary: "Token config", ConfigKey: "api_token", RequireAbsent: true}},
	}
	result := EvaluateManifest(context.Background(), manifest, t.TempDir(), Options{Entry: SkillEntryState{SkillKey: "demo", Config: map[string]any{"api_token": "super-secret-token"}}})
	if len(result.SuggestedPlans) != 1 {
		t.Fatalf("expected one suggested plan, got %#v", result)
	}
	change := result.SuggestedPlans[0].Changes[0]
	if !change.OldValue.Redacted || !change.OldValue.Present {
		t.Fatalf("expected secret old value to be redacted, got %#v", change.OldValue)
	}
	if change.OldValue.Value != nil {
		t.Fatalf("expected no raw secret value, got %#v", change.OldValue)
	}
	if change.NewValue.Value != "clear" {
		t.Fatalf("expected clear new value, got %#v", change.NewValue)
	}
}

func TestEvaluateManifest_GeneratesMultiChangeKnownPatternPlan(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		Checks: []CheckSpec{{
			ID:            "credential-source",
			Kind:          "config",
			Label:         "Credential source",
			Summary:       "Credential source needs OR3-managed correction",
			ConfigKey:     "external_credential_path",
			RequireAbsent: true,
		}},
		KnownFixes: []KnownFixSpec{{
			ID:              "repair_credential_source",
			Summary:         "Repair the OR3-managed credential source",
			MatchCheck:      "credential-source",
			MatchStatus:     "fail",
			Risk:            configmeta.RiskWarning,
			RestartRequired: true,
			Changes: []SkillEntryChangeSpec{
				{Type: "config", Key: "credential_path", Value: "managed://credentials/github"},
				{Type: "config", Key: "external_credential_path", Clear: true},
				{Type: "config", Key: "managed_reference", Clear: true},
			},
		}},
	}

	result := EvaluateManifest(context.Background(), manifest, t.TempDir(), Options{Entry: SkillEntryState{
		SkillKey: "github",
		Config: map[string]any{
			"external_credential_path": "/Users/demo/.config/github/token.json",
			"managed_reference":        "managed://stale/github",
		},
	}})
	if len(result.SuggestedPlans) != 1 {
		t.Fatalf("expected one suggested plan, got %#v", result)
	}
	plan := result.SuggestedPlans[0]
	if plan.RiskLevel != configmeta.RiskWarning || !plan.RestartRequired {
		t.Fatalf("unexpected plan risk/restart: %#v", plan)
	}
	if len(plan.Changes) != 3 {
		t.Fatalf("expected multi-change plan, got %#v", plan.Changes)
	}
	wantPaths := []string{
		"skills.entries.github.config.credential_path",
		"skills.entries.github.config.external_credential_path",
		"skills.entries.github.config.managed_reference",
	}
	for i, want := range wantPaths {
		if plan.Changes[i].ConfigPath != want {
			t.Fatalf("change %d path = %q, want %q", i, plan.Changes[i].ConfigPath, want)
		}
	}
}

func TestEvaluateManifest_RedactsSensitiveEnvAndIdentityFields(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "account.json")
	if err := os.WriteFile(jsonPath, []byte(`{"email":"person@example.com"}`), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	manifest := Manifest{
		Version:    1,
		Redactions: RedactionRules{EnvKeys: []string{"TOKEN"}},
		Checks: []CheckSpec{
			{ID: "env-token", Kind: "env", Label: "Token", Summary: "Token env", EnvKey: "TOKEN"},
			{ID: "identity-json", Kind: "json_file", Label: "Identity", Summary: "Identity JSON", File: "account.json", IdentityField: "email"},
		},
	}
	result := EvaluateManifest(context.Background(), manifest, filepath.Join(dir, "skill.diagnostic.yaml"), Options{Entry: SkillEntryState{SkillKey: "demo", Env: map[string]string{"TOKEN": "super-secret-token"}}})
	if len(result.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %#v", result)
	}
	envEvidence := result.Findings[0].Evidence["value"]
	if envEvidence == "super-secret-token" {
		t.Fatalf("expected env value to be redacted, got %#v", envEvidence)
	}
	identityEvidence := result.Findings[1].Evidence["identity"]
	if identityEvidence == "person@example.com" {
		t.Fatalf("expected identity field to be redacted, got %#v", identityEvidence)
	}
}

func TestEvaluateManifest_CommandCheckUsesRunner(t *testing.T) {
	manifest := Manifest{Version: 1, Checks: []CheckSpec{{ID: "cmd", Kind: "command", Label: "Command", Summary: "Command check", Command: []string{"demo", "status"}, ExpectContains: "ok"}}}
	result := EvaluateManifest(context.Background(), manifest, t.TempDir(), Options{Entry: SkillEntryState{SkillKey: "demo"}, Runner: fakeRunner{result: CommandResult{ExitCode: 0, Stdout: "ok\n"}}})
	if len(result.Findings) != 1 || result.Findings[0].Status != "pass" {
		t.Fatalf("expected passing command finding, got %#v", result)
	}
}

func TestEvaluateManifest_GenericInstallableSkillFixture(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, "credential.json")
	if err := os.WriteFile(credPath, []byte(`{"email":"person@example.com","client_secret":"shh-secret","credential_path":"/Users/demo/.config/skill/creds.json"}`), 0o644); err != nil {
		t.Fatalf("write credential json: %v", err)
	}
	manifest := Manifest{
		Version:    1,
		Redactions: RedactionRules{ConfigKeys: []string{"client_secret"}, PathFields: []string{"credential_path"}},
		Checks: []CheckSpec{
			{ID: "binary", Kind: "binary", Label: "Binary", Binary: "sh"},
			{ID: "api-key", Kind: "api_key", Label: "API Key", Summary: "Stored API key"},
			{ID: "credential-json", Kind: "json_config_file", Label: "Credential JSON", Summary: "Credential JSON is valid", ConfigKey: "credential_path", IdentityField: "email"},
			{ID: "capability-status", Kind: "command", Label: "Capability", Summary: "Capability check", Command: []string{"demo", "status"}, ExpectContains: "ok"},
			{ID: "managed-reference", Kind: "config", Label: "Managed reference", Summary: "Managed reference should be cleared", ConfigKey: "managed_reference", RequireAbsent: true, RestartRequired: true},
		},
	}
	result := EvaluateManifest(context.Background(), manifest, dir, Options{
		Entry: SkillEntryState{
			SkillKey: "demo",
			APIKey:   "skill-secret-token",
			Config: map[string]any{
				"credential_path":   credPath,
				"managed_reference": "managed://credential/demo",
			},
		},
		Runner: fakeRunner{result: CommandResult{ExitCode: 0, Stdout: "ok\n"}},
	})
	if len(result.Findings) != 5 {
		t.Fatalf("expected generic fixture findings, got %#v", result)
	}
	if result.Findings[1].Evidence["value"] == "skill-secret-token" {
		t.Fatalf("expected api key finding to redact secret values, got %#v", result.Findings[1].Evidence)
	}
	jsonEvidence, ok := result.Findings[2].Evidence["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected redacted json payload evidence, got %#v", result.Findings[2].Evidence)
	}
	if jsonEvidence["client_secret"] == "shh-secret" {
		t.Fatalf("expected nested credential JSON to be redacted, got %#v", jsonEvidence)
	}
	if jsonEvidence["credential_path"] == credPath {
		t.Fatalf("expected credential path to be redacted, got %#v", jsonEvidence)
	}
	if result.Findings[4].Status != "fail" || !result.RestartRequired {
		t.Fatalf("expected stale managed reference to fail and mark restart required, got %#v", result.Findings[4])
	}
}
