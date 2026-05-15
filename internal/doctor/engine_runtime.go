package doctor

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"or3-intern/internal/config"

	_ "modernc.org/sqlite"
)

func runtimeProfileFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	p := cfg.RuntimeProfile
	if p == "" {
		findings = append(findings, Finding{
			ID:       "runtime-profile.unset",
			Area:     "runtime-profile",
			Severity: SeverityWarn,
			Summary:  "runtimeProfile is not set; consider setting it to one of: local-dev, single-user-hardened, hosted-service, hosted-no-exec, hosted-remote-sandbox-only",
		})
		return findings
	}
	if config.IsHostedProfile(p) {
		if !cfg.Security.SecretStore.Enabled {
			findings = append(findings, Finding{
				ID:       "runtime-profile.secret_store_disabled",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.secretStore.enabled",
			})
		}
		if !cfg.Security.SecretStore.Required {
			findings = append(findings, Finding{
				ID:       "runtime-profile.secret_store_not_required",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.secretStore.required",
			})
		}
		if !cfg.Security.Audit.Enabled {
			findings = append(findings, Finding{
				ID:       "runtime-profile.audit_disabled",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.audit.enabled",
			})
		}
		if !cfg.Security.Audit.Strict {
			findings = append(findings, Finding{
				ID:       "runtime-profile.audit_not_strict",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.audit.strict",
			})
		}
		if !cfg.Security.Audit.VerifyOnStart {
			findings = append(findings, Finding{
				ID:       "runtime-profile.audit_no_verify_on_start",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile requires security.audit.verifyOnStart",
			})
		}
		if !cfg.Security.Network.Enabled && !cfg.Security.Network.DefaultDeny {
			findings = append(findings, Finding{
				ID:       "runtime-profile.network_missing",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted profile should configure security.network outbound policy",
			})
		}
	}
	if p == config.ProfileHostedNoExec {
		if cfg.Hardening.EnableExecShell {
			findings = append(findings, Finding{
				ID:       "runtime-profile.hosted_no_exec_shell",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted-no-exec profile: enableExecShell should be false",
			})
		}
		if cfg.Hardening.PrivilegedTools {
			findings = append(findings, Finding{
				ID:       "runtime-profile.hosted_no_exec_privileged",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted-no-exec profile: privilegedTools should be false",
			})
		}
	}
	if p == config.ProfileHostedRemoteSandbox {
		if cfg.Hardening.EnableExecShell && !cfg.Hardening.Sandbox.Enabled {
			findings = append(findings, Finding{
				ID:       "runtime-profile.remote_sandbox_missing",
				Area:     "runtime-profile",
				Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
				Summary:  "hosted-remote-sandbox-only profile: exec requires sandbox to be enabled",
			})
		}
	}
	return findings
}

func probeFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if strings.TrimSpace(cfg.DBPath) != "" {
		if err := probeSQLiteDatabase(context.Background(), cfg.DBPath); err != nil {
			findings = append(findings, Finding{
				ID:       "probe.sqlite_open_failed",
				Area:     "runtime",
				Severity: SeverityError,
				Summary:  "SQLite database could not be opened",
				Detail:   err.Error(),
				FixMode:  FixModeManual,
			})
		}
	}
	return findings
}

func probeSQLiteDatabase(ctx context.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(1000)", path)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer database.Close()
	return database.PingContext(ctx)
}
