package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"or3-intern/internal/config"
	intdoctor "or3-intern/internal/doctor"
)

var startupWarningWriter io.Writer = os.Stderr

func validateStartupCommand(cmd string, cfg config.Config, unsafeDev bool) error {
	return validateStartupCommandWithOptions(cmd, cfg, unsafeDev, true)
}

func validateStartupCommandWithOptions(cmd string, cfg config.Config, unsafeDev bool, emitWarnings bool) error {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	if unsafeDev {
		return nil
	}
	if err := validateReadinessForStartup(cmd, cfg); err != nil {
		return err
	}
	mode := startupDoctorMode(cmd)
	if mode == "" {
		return nil
	}
	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: mode})
	blockers := report.BlockingFindings()
	warnings := startupNonBlockingWarnings(blockers)
	if emitWarnings && len(warnings) > 0 {
		emitStartupWarnings(cmd, warnings)
	}
	blockers = startupBlockingFindings(blockers)
	if len(blockers) == 0 {
		return nil
	}
	top := intdoctor.TopFindings(blockers, 3)
	parts := make([]string, 0, len(top))
	for _, finding := range top {
		parts = append(parts, finding.Area+": "+finding.Summary)
	}
	message := strings.Join(parts, "; ")
	if len(blockers) > len(top) {
		message += fmt.Sprintf(" (%d more blocking finding(s))", len(blockers)-len(top))
	}
	return startupRefusal(cmd, message, blockers)
}

func validateReadinessForStartup(cmd string, cfg config.Config) error {
	report := config.EvaluateReadiness(cfg, config.ReadinessOptions{Command: cmd})
	switch report.State {
	case config.ReadinessReady, config.ReadinessAdvancedCustom:
		return nil
	default:
		return readinessStartupRefusal(cmd, report)
	}
}

func startupBlockingFindings(findings []intdoctor.Finding) []intdoctor.Finding {
	filtered := make([]intdoctor.Finding, 0, len(findings))
	for _, finding := range findings {
		if startupWarningOnlyFinding(finding) {
			continue
		}
		filtered = append(filtered, finding)
	}
	return filtered
}

func startupNonBlockingWarnings(findings []intdoctor.Finding) []intdoctor.Finding {
	warnings := make([]intdoctor.Finding, 0, 1)
	for _, finding := range findings {
		if startupWarningOnlyFinding(finding) {
			warnings = append(warnings, finding)
		}
	}
	return warnings
}

func startupWarningOnlyFinding(finding intdoctor.Finding) bool {
	switch finding.ID {
	case "privileged-exec.sandbox_disabled":
		return true
	default:
		return false
	}
}

func emitStartupWarnings(cmd string, warnings []intdoctor.Finding) {
	if startupWarningWriter == nil || len(warnings) == 0 {
		return
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		cmd = "startup"
	}
	for _, warning := range warnings {
		message := strings.TrimSpace(warning.Summary)
		if detail := strings.TrimSpace(warning.Detail); detail != "" {
			message += ": " + detail
		}
		guidance := strings.TrimSpace(warning.FixHint)
		if guidance == "" {
			guidance = "Disable privileged tools or enable sandboxing before using this in a less-trusted environment."
		}
		fmt.Fprintf(startupWarningWriter, "warning: %s startup continuing without sandbox protection: %s; %s\n", cmd, message, guidance)
	}
}

func startupDoctorMode(cmd string) intdoctor.Mode {
	switch cmd {
	case "chat":
		return intdoctor.ModeStartupChat
	case "serve":
		return intdoctor.ModeStartupServe
	case "service":
		return intdoctor.ModeStartupService
	default:
		return ""
	}
}

func startupRefusal(cmd, message string, blockers []intdoctor.Finding) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		cmd = "startup"
	}
	guidance := startupFixGuidance(blockers)
	return fmt.Errorf("%s startup refused: %s; %s", cmd, message, guidance)
}

func readinessStartupRefusal(cmd string, report config.ReadinessReport) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		cmd = "startup"
	}
	top := report.Issues
	if len(top) > 3 {
		top = top[:3]
	}
	parts := make([]string, 0, len(top))
	for _, issue := range top {
		message := strings.TrimSpace(issue.Title)
		if fix := strings.TrimSpace(issue.Fix); fix != "" {
			message += " — " + fix
		}
		parts = append(parts, message)
	}
	if len(parts) == 0 {
		parts = append(parts, "setup is not complete")
	}
	if extra := len(report.Issues) - len(top); extra > 0 {
		parts = append(parts, fmt.Sprintf("%d more issue(s)", extra))
	}
	return fmt.Errorf("%s startup refused: setup state is %s: %s", cmd, report.State, strings.Join(parts, "; "))
}

func startupFixGuidance(blockers []intdoctor.Finding) string {
	hasAutomatic := false
	hasInteractive := false
	for _, finding := range blockers {
		switch finding.FixMode {
		case intdoctor.FixModeAutomatic:
			hasAutomatic = true
		case intdoctor.FixModeInteractive:
			hasInteractive = true
		}
	}
	switch {
	case hasAutomatic && hasInteractive:
		return "run `or3-intern doctor --fix` for safe repairs, then `or3-intern doctor --fix --interactive` for guided ones, or fix the configuration manually"
	case hasAutomatic:
		return "run `or3-intern doctor --fix` or fix the configuration manually"
	case hasInteractive:
		return "run `or3-intern doctor --fix --interactive` or fix the configuration manually"
	default:
		return "fix the configuration manually"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
