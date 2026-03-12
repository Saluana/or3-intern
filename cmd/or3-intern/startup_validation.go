package main

import (
	"fmt"
	"sort"
	"strings"

	"or3-intern/internal/config"
)

func validateStartupCommand(cmd string, cfg config.Config) error {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	switch cmd {
	case "chat", "serve", "service":
	default:
		return nil
	}
	if err := config.ValidateProfile(cfg); err != nil {
		return startupRefusal(cmd, err.Error())
	}
	if !requiresHostedStrictStartup(cmd, cfg) {
		return nil
	}
	findings := strictStartupFindings(cmd, cfg)
	if len(findings) == 0 {
		return nil
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Area == findings[j].Area {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Area < findings[j].Area
	})
	messages := make([]string, 0, minInt(3, len(findings)))
	for i := 0; i < len(findings) && i < 3; i++ {
		messages = append(messages, findings[i].Area+": "+findings[i].Message)
	}
	msg := strings.Join(messages, "; ")
	if len(findings) > 3 {
		msg += fmt.Sprintf(" (%d more blocking finding(s))", len(findings)-3)
	}
	return startupRefusal(cmd, msg)
}

func requiresHostedStrictStartup(cmd string, cfg config.Config) bool {
	if !config.IsHostedProfile(cfg.RuntimeProfile) {
		return false
	}
	if hasRemoteHTTPMCP(cfg) {
		return true
	}
	switch cmd {
	case "service":
		return true
	case "serve":
		return anyEnabledChannels(cfg) ||
			cfg.Triggers.Webhook.Enabled ||
			cfg.Triggers.FileWatch.Enabled ||
			cfg.Heartbeat.Enabled ||
			cfg.Cron.Enabled
	default:
		return false
	}
}

func strictStartupFindings(cmd string, cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	findings = append(findings, privilegedExecStartupFindings(cfg)...)
	if hasRemoteHTTPMCP(cfg) {
		findings = append(findings, mcpFindings(cfg)...)
		findings = append(findings, networkFindings(cfg)...)
	}
	switch cmd {
	case "service":
		if secret := strings.TrimSpace(cfg.Service.Secret); secret == "" {
			findings = append(findings, doctorFinding{Level: "warn", Area: "service", Message: "service mode is enabled without a shared secret"})
		} else if len(secret) < 24 {
			findings = append(findings, doctorFinding{Level: "warn", Area: "service", Message: "service mode is enabled with a weak shared secret"})
		}
		if !isLoopbackAddr(cfg.Service.Listen) {
			findings = append(findings, doctorFinding{Level: "warn", Area: "service", Message: "service bind address is not loopback-only"})
		}
	case "serve":
		if cfg.Triggers.Webhook.Enabled {
			findings = append(findings, webhookFindings(cfg)...)
		}
		if anyEnabledChannels(cfg) || cfg.Triggers.Webhook.Enabled {
			findings = append(findings, profileFindings(cfg)...)
			findings = append(findings, execFindings(cfg)...)
			findings = append(findings, skillFindings(cfg)...)
			findings = append(findings, channelExposureFindings(cfg)...)
		}
	}
	return findings
}

func privilegedExecStartupFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	if cfg.Hardening.PrivilegedTools && !cfg.Hardening.Sandbox.Enabled {
		findings = append(findings, doctorFinding{
			Level:   "warn",
			Area:    "privileged-exec",
			Message: "privileged tools are enabled without Bubblewrap sandboxing",
		})
	}
	if (cfg.Hardening.PrivilegedTools || cfg.Hardening.GuardedTools) && len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		findings = append(findings, doctorFinding{
			Level:   "warn",
			Area:    "exec",
			Message: "exec is enabled without an exec allowlist",
		})
	}
	return findings
}

func startupRefusal(cmd, message string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		cmd = "startup"
	}
	return fmt.Errorf("%s startup refused: %s; move risky execution to or3-sandbox or fix the profile configuration", cmd, message)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
