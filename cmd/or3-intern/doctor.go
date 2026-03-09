package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"

	"or3-intern/internal/config"
)

type doctorFinding struct {
	Level   string
	Area    string
	Message string
}

func runDoctorCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	strict := fs.Bool("strict", false, "exit non-zero when warnings are found")
	if err := fs.Parse(args); err != nil {
		return err
	}
	findings := doctorFindings(cfg)
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(stdout, "[ok] configuration looks safe")
		return nil
	}
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "[%s] %s: %s\n", finding.Level, finding.Area, finding.Message)
	}
	if *strict && hasDoctorWarnings(findings) {
		return fmt.Errorf("doctor found warnings")
	}
	return nil
}

func doctorFindings(cfg config.Config) []doctorFinding {
	findings := make([]doctorFinding, 0, 16)
	add := func(level, area, message string) {
		findings = append(findings, doctorFinding{Level: level, Area: area, Message: message})
	}
	if !cfg.Tools.RestrictToWorkspace {
		add("warn", "filesystem", "workspace restriction is disabled")
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		add("warn", "filesystem", "workspace restriction is enabled but workspaceDir is empty")
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		add("warn", "env", "child process environment allowlist is empty")
	}
	if !cfg.Hardening.Quotas.Enabled {
		add("warn", "quotas", "tool quotas are disabled")
	}
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 || cfg.Hardening.Quotas.MaxExecCalls <= 0 || cfg.Hardening.Quotas.MaxWebCalls <= 0 || cfg.Hardening.Quotas.MaxSubagentCalls <= 0 {
		add("warn", "quotas", "one or more quota limits are unset")
	}
	if cfg.Hardening.PrivilegedTools && !cfg.Hardening.Sandbox.Enabled {
		add("warn", "privileged-exec", "privileged tools are enabled without Bubblewrap sandboxing")
	}
	if cfg.Hardening.Sandbox.Enabled && strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
		add("warn", "privileged-exec", "Bubblewrap sandbox is enabled without a bubblewrapPath")
	}
	if cfg.Triggers.Webhook.Enabled {
		if strings.TrimSpace(cfg.Triggers.Webhook.Secret) == "" {
			add("warn", "webhook", "webhook is enabled without a secret")
		}
		if !isLoopbackAddr(cfg.Triggers.Webhook.Addr) {
			add("warn", "webhook", "webhook bind address is not loopback-only")
		}
	}
	for _, finding := range channelExposureFindings(cfg) {
		findings = append(findings, finding)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Area == findings[j].Area {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Area < findings[j].Area
	})
	return findings
}

func channelExposureFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	add := func(area, message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: area, Message: message})
	}
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.OpenAccess {
		add("telegram", "channel is open to any sender")
	}
	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.OpenAccess {
		add("slack", "channel is open to any sender")
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.OpenAccess {
		add("discord", "channel is open to any sender")
	}
	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.OpenAccess {
		add("whatsapp", "channel is open to any sender")
	}
	if cfg.Channels.Email.Enabled && cfg.Channels.Email.OpenAccess {
		add("email", "channel is open to any sender")
	}
	return findings
}

func hasDoctorWarnings(findings []doctorFinding) bool {
	for _, finding := range findings {
		if strings.EqualFold(finding.Level, "warn") || strings.EqualFold(finding.Level, "error") {
			return true
		}
	}
	return false
}

func isLoopbackAddr(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}
