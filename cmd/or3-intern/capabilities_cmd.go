package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
)

func runCapabilitiesCommand(cfg config.Config, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("capabilities", flag.ContinueOnError)
	fs.SetOutput(stderr)
	channelFilter := fs.String("channel", "", "filter to a specific channel")
	triggerFilter := fs.String("trigger", "", "filter to a specific trigger")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report := controlplane.CollectCapabilitiesReport(cfg, broker, strings.TrimSpace(*channelFilter), strings.TrimSpace(*triggerFilter))
	if *asJSON {
		blob, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, string(blob))
		return nil
	}
	printCapabilitiesReport(stdout, report)
	return nil
}

func printCapabilitiesReport(w io.Writer, report controlplane.CapabilitiesReport) {
	_, _ = fmt.Fprintf(w, "runtime_profile: %s\n", report.RuntimeProfile)
	_, _ = fmt.Fprintf(w, "host_id: %s\n", report.HostID)
	_, _ = fmt.Fprintf(w, "hosted: %t\n", report.Hosted)
	_, _ = fmt.Fprintf(w, "approval_broker: enabled=%v required=%v available=%v can_issue_token=%v\n", report.ApprovalBroker["enabled"], report.ApprovalBroker["required"], report.ApprovalBroker["available"], report.ApprovalBroker["canIssueToken"])
	_, _ = fmt.Fprintf(w, "exec_available: %t\n", report.ExecAvailable)
	_, _ = fmt.Fprintf(w, "shell_mode_available: %t\n", report.ShellModeAvailable)
	_, _ = fmt.Fprintf(w, "skill_exec_enabled: %t\n", report.SkillExecEnabled)
	_, _ = fmt.Fprintf(w, "subagents_enabled: %t\n", report.SubagentsEnabled)
	_, _ = fmt.Fprintf(w, "sandbox: enabled=%t required=%t\n", report.SandboxEnabled, report.SandboxRequired)
	if len(report.EnabledMCPServers) == 0 {
		_, _ = fmt.Fprintln(w, "mcp_servers: none")
	} else {
		_, _ = fmt.Fprintf(w, "mcp_servers: %s\n", strings.Join(report.EnabledMCPServers, ", "))
	}
	_, _ = fmt.Fprintln(w, "approvals:")
	for _, key := range []string{"pairing", "exec", "skillExecution", "secretAccess", "messageSend"} {
		_, _ = fmt.Fprintf(w, "  %s: %s\n", key, report.Approvals[key])
	}
	_, _ = fmt.Fprintln(w, "channels:")
	for _, item := range report.Channels {
		_, _ = fmt.Fprintf(w, "  - %s enabled=%t inbound=%s", item.Name, item.Enabled, item.InboundPolicy)
		if item.Profile != nil {
			_, _ = fmt.Fprintf(w, " profile=%s max=%s subagents=%t", item.Profile.Name, item.Profile.MaxCapability, item.Profile.AllowSubagents)
			if len(item.Profile.AllowedTools) > 0 {
				_, _ = fmt.Fprintf(w, " tools=%s", strings.Join(item.Profile.AllowedTools, ","))
			}
			if len(item.Profile.AllowedHosts) > 0 {
				_, _ = fmt.Fprintf(w, " hosts=%s", strings.Join(item.Profile.AllowedHosts, ","))
			}
		}
		_, _ = fmt.Fprintln(w)
	}
	_, _ = fmt.Fprintln(w, "triggers:")
	for _, item := range report.Triggers {
		_, _ = fmt.Fprintf(w, "  - %s enabled=%t", item.Name, item.Enabled)
		if item.Profile != nil {
			_, _ = fmt.Fprintf(w, " profile=%s max=%s subagents=%t", item.Profile.Name, item.Profile.MaxCapability, item.Profile.AllowSubagents)
			if len(item.Profile.AllowedTools) > 0 {
				_, _ = fmt.Fprintf(w, " tools=%s", strings.Join(item.Profile.AllowedTools, ","))
			}
			if len(item.Profile.AllowedHosts) > 0 {
				_, _ = fmt.Fprintf(w, " hosts=%s", strings.Join(item.Profile.AllowedHosts, ","))
			}
		}
		_, _ = fmt.Fprintln(w)
	}
}
