package doctor

import (
	"strings"

	"or3-intern/internal/config"
)

func configValidationFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if validationErr := strings.TrimSpace(opts.ValidationError); validationErr != "" {
		findings = append(findings, Finding{
			ID:       "config.validation.load",
			Area:     "config",
			Severity: severityForConfigureOrStartup(opts.Mode, SeverityError),
			Summary:  validationErr,
			Detail:   "The config was loaded in repair mode because normal validation failed. Fix the reported fields before startup.",
			FixMode:  FixModeInteractive,
			FixHint:  "Run `or3-intern doctor --fix --interactive` or `or3-intern configure`.",
		})
	}
	if err := config.ValidateProfile(cfg); err != nil {
		findings = append(findings, Finding{
			ID:       "runtime-profile.validation",
			Area:     "runtime-profile",
			Severity: severityForConfigureOrStartup(opts.Mode, SeverityError),
			Summary:  err.Error(),
			Detail:   "The selected runtime profile contradicts the rest of the configuration.",
			FixMode:  FixModeManual,
			FixHint:  "Adjust the runtime profile or the dependent security settings.",
		})
	}
	if strings.TrimSpace(opts.ValidationError) == "" {
		if err := validateConfigSnapshot(cfg); err != nil {
			findings = append(findings, Finding{
				ID:       "config.validation.snapshot",
				Area:     "config",
				Severity: severityForConfigureOrStartup(opts.Mode, SeverityError),
				Summary:  err.Error(),
				Detail:   "The current in-memory config cannot pass full validation.",
				FixMode:  FixModeInteractive,
				FixHint:  "Repair the invalid config fields before startup.",
			})
		}
	}
	return findings
}

func validateConfigSnapshot(cfg config.Config) error {
	return config.ValidateSnapshot(cfg)
}
