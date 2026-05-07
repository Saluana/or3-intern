package doctor

import (
	"os"
	"strings"

	"or3-intern/internal/config"
)

func securityFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Security.Audit.Enabled {
		findings = append(findings, Finding{
			ID:       "security.audit_disabled",
			Area:     "security",
			Severity: SeverityWarn,
			Summary:  "audit logging is disabled",
			Detail:   "Sensitive operations are not written to the append-only audit chain.",
			FixMode:  FixModeManual,
		})
	} else {
		if !cfg.Security.Audit.Strict {
			findings = append(findings, Finding{
				ID:       "security.audit_not_strict",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "audit logging is enabled but strict mode is off",
				Detail:   "Audit write failures will not fail closed.",
				FixMode:  FixModeManual,
			})
		}
		if !cfg.Security.Audit.VerifyOnStart {
			findings = append(findings, Finding{
				ID:       "security.audit_no_verify_on_start",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "audit logging is enabled but verifyOnStart is off",
				Detail:   "The audit chain is not verified automatically at startup.",
				FixMode:  FixModeManual,
			})
		}
		if keyFinding := keyFileFinding("security.audit.key_missing", "security", cfg.Security.Audit.KeyFile, "audit key file is missing", FixModeAutomatic); keyFinding != nil {
			findings = append(findings, *keyFinding)
		}
	}
	if !cfg.Security.SecretStore.Enabled {
		findings = append(findings, Finding{
			ID:       "security.secret_store_disabled",
			Area:     "security",
			Severity: SeverityWarn,
			Summary:  "secret store is disabled",
			Detail:   "Provider and integration secrets cannot be stored in encrypted local secret storage.",
			FixMode:  FixModeManual,
		})
		if hasExternalIntegrations(cfg) {
			findings = append(findings, Finding{
				ID:       "security.secret_store_disabled_with_integrations",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "secret store is disabled while external integrations are enabled",
				Detail:   "External integrations increase the value of local secret protection.",
				FixMode:  FixModeInteractive,
				FixHint:  "Enable the secret store and generate a key file.",
			})
		}
	} else {
		if !cfg.Security.SecretStore.Required && hasExternalIntegrations(cfg) {
			findings = append(findings, Finding{
				ID:       "security.secret_store_not_required",
				Area:     "security",
				Severity: SeverityWarn,
				Summary:  "secret store failures are tolerated while external integrations are enabled",
				Detail:   "The runtime may continue with weaker secret handling if the secret store becomes unavailable.",
				FixMode:  FixModeManual,
			})
		}
		if keyFinding := keyFileFinding("security.secret_store.key_missing", "security", cfg.Security.SecretStore.KeyFile, "secret-store key file is missing", FixModeAutomatic); keyFinding != nil {
			findings = append(findings, *keyFinding)
		}
	}
	if !cfg.Security.Profiles.Enabled {
		findings = append(findings, Finding{
			ID:       "security.profiles_disabled",
			Area:     "security",
			Severity: SeverityWarn,
			Summary:  "access profiles are disabled",
			Detail:   "External ingress and automation run without a profile boundary.",
			FixMode:  FixModeManual,
		})
	}
	return findings
}

func keyFileFinding(id, area, path, summary string, fixMode FixMode) *Finding {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return &Finding{
		ID:       id,
		Area:     area,
		Severity: SeverityWarn,
		Summary:  summary,
		Detail:   path,
		FixMode:  fixMode,
		FixHint:  "Generate the missing key file.",
	}
}
