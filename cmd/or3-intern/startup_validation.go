package main

import (
	"fmt"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	intdoctor "or3-intern/internal/doctor"
)

func validateStartupCommand(cmd string, cfg config.Config, unsafeDev bool) error {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	if unsafeDev {
		return nil
	}
	if err := validateStrictServicePosture(cmd, cfg); err != nil {
		return err
	}
	mode := startupDoctorMode(cmd)
	if mode == "" {
		return nil
	}
	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: mode})
	if !report.HasBlockingFindings() {
		return nil
	}
	blockers := report.BlockingFindings()
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

func validateStrictServicePosture(cmd string, cfg config.Config) error {
	if cmd != "service" {
		return nil
	}
	if cfg.Service.AllowUnauthenticatedPairing && !serviceListenIsLoopback(cfg.Service.Listen) {
		return fmt.Errorf("service startup refused: unauthenticated pairing requires a loopback listen address; rerun with --unsafe-dev only for local development")
	}
	role := strings.ToLower(strings.TrimSpace(cfg.Service.SharedSecretRole))
	switch role {
	case "", approval.RoleViewer, approval.RoleServiceClient:
	default:
		return fmt.Errorf("service startup refused: service.sharedSecretRole must be viewer or service-client; rerun with --unsafe-dev only for local development")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Service.MaxCapability)) {
	case "", "safe":
	default:
		return fmt.Errorf("service startup refused: service.maxCapability must remain safe; rerun with --unsafe-dev only for local development")
	}
	return nil
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
