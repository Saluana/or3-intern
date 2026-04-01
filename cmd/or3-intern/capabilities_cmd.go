package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

type capabilitiesProfileSummary struct {
	Name           string   `json:"name,omitempty"`
	MaxCapability  string   `json:"maxCapability,omitempty"`
	AllowedTools   []string `json:"allowedTools,omitempty"`
	AllowedHosts   []string `json:"allowedHosts,omitempty"`
	WritablePaths  []string `json:"writablePaths,omitempty"`
	AllowSubagents bool     `json:"allowSubagents"`
}

type capabilitiesIngressSummary struct {
	Name          string                      `json:"name"`
	Enabled       bool                        `json:"enabled"`
	InboundPolicy string                      `json:"inboundPolicy,omitempty"`
	Profile       *capabilitiesProfileSummary `json:"effectiveProfile,omitempty"`
}

type capabilitiesReport struct {
	RuntimeProfile     string                       `json:"runtimeProfile"`
	Hosted             bool                         `json:"hosted"`
	HostID             string                       `json:"hostId"`
	ApprovalBroker     map[string]any               `json:"approvalBroker"`
	Approvals          map[string]string            `json:"approvals"`
	SubagentsEnabled   bool                         `json:"subagentsEnabled"`
	SkillExecEnabled   bool                         `json:"skillExecEnabled"`
	ExecAvailable      bool                         `json:"execAvailable"`
	ShellModeAvailable bool                         `json:"shellModeAvailable"`
	SandboxEnabled     bool                         `json:"sandboxEnabled"`
	SandboxRequired    bool                         `json:"sandboxRequired"`
	EnabledMCPServers  []string                     `json:"enabledMcpServers,omitempty"`
	NetworkPolicy      config.NetworkPolicyConfig   `json:"networkPolicy"`
	Channels           []capabilitiesIngressSummary `json:"channels,omitempty"`
	Triggers           []capabilitiesIngressSummary `json:"triggers,omitempty"`
	HeartbeatEnabled   bool                         `json:"heartbeatEnabled"`
	CronEnabled        bool                         `json:"cronEnabled"`
}

func runCapabilitiesCommand(cfg config.Config, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("capabilities", flag.ContinueOnError)
	fs.SetOutput(stderr)
	channelFilter := fs.String("channel", "", "filter to a specific channel")
	triggerFilter := fs.String("trigger", "", "filter to a specific trigger")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report := collectCapabilitiesReport(cfg, broker, strings.TrimSpace(*channelFilter), strings.TrimSpace(*triggerFilter))
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

func collectCapabilitiesReport(cfg config.Config, broker *approval.Broker, channelFilter, triggerFilter string) capabilitiesReport {
	spec := config.ProfileSpec(cfg.RuntimeProfile)
	report := capabilitiesReport{
		RuntimeProfile:     string(cfg.RuntimeProfile),
		Hosted:             spec.Hosted,
		HostID:             cfg.Security.Approvals.HostID,
		Approvals:          approvalModes(cfg),
		SubagentsEnabled:   cfg.Subagents.Enabled,
		SkillExecEnabled:   cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools && !spec.ForbidPrivilegedTools,
		ExecAvailable:      cfg.Hardening.GuardedTools && (!spec.RequireSandboxForExec || cfg.Hardening.Sandbox.Enabled),
		ShellModeAvailable: cfg.Hardening.GuardedTools && cfg.Hardening.PrivilegedTools && cfg.Hardening.EnableExecShell && !spec.ForbidExecShell && !spec.ForbidPrivilegedTools && (!spec.RequireSandboxForExec || cfg.Hardening.Sandbox.Enabled),
		SandboxEnabled:     cfg.Hardening.Sandbox.Enabled,
		SandboxRequired:    spec.RequireSandboxForExec,
		NetworkPolicy:      cfg.Security.Network,
		HeartbeatEnabled:   cfg.Heartbeat.Enabled,
		CronEnabled:        cfg.Cron.Enabled,
		ApprovalBroker: map[string]any{
			"enabled":       cfg.Security.Approvals.Enabled,
			"required":      approvalBrokerRequired(cfg),
			"available":     broker != nil,
			"canIssueToken": broker != nil && len(broker.SignKey) > 0,
		},
	}
	report.EnabledMCPServers = enabledMCPServers(cfg)
	report.Channels = collectChannelCapabilities(cfg, channelFilter)
	report.Triggers = collectTriggerCapabilities(cfg, triggerFilter)
	return report
}

func approvalModes(cfg config.Config) map[string]string {
	return map[string]string{
		"pairing":        string(cfg.Security.Approvals.Pairing.Mode),
		"exec":           string(cfg.Security.Approvals.Exec.Mode),
		"skillExecution": string(cfg.Security.Approvals.SkillExecution.Mode),
		"secretAccess":   string(cfg.Security.Approvals.SecretAccess.Mode),
		"messageSend":    string(cfg.Security.Approvals.MessageSend.Mode),
	}
}

func collectChannelCapabilities(cfg config.Config, filter string) []capabilitiesIngressSummary {
	items := []capabilitiesIngressSummary{
		{
			Name:          "telegram",
			Enabled:       cfg.Channels.Telegram.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["telegram"]),
		},
		{
			Name:          "slack",
			Enabled:       cfg.Channels.Slack.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["slack"]),
		},
		{
			Name:          "discord",
			Enabled:       cfg.Channels.Discord.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["discord"]),
		},
		{
			Name:          "whatsapp",
			Enabled:       cfg.Channels.WhatsApp.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["whatsapp"]),
		},
		{
			Name:          "email",
			Enabled:       cfg.Channels.Email.Enabled,
			InboundPolicy: config.EffectiveInboundPolicy(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, hasNonEmpty(cfg.Channels.Email.AllowedSenders)),
			Profile:       effectiveProfileSummary(cfg, cfg.Security.Profiles.Channels["email"]),
		},
	}
	return filterIngress(items, filter)
}

func collectTriggerCapabilities(cfg config.Config, filter string) []capabilitiesIngressSummary {
	items := []capabilitiesIngressSummary{
		{
			Name:    "webhook",
			Enabled: cfg.Triggers.Webhook.Enabled,
			Profile: effectiveProfileSummary(cfg, cfg.Security.Profiles.Triggers["webhook"]),
		},
		{
			Name:    "filewatch",
			Enabled: cfg.Triggers.FileWatch.Enabled,
			Profile: effectiveProfileSummary(cfg, firstNonEmptyCapabilityString(
				cfg.Security.Profiles.Triggers["file_change"],
				cfg.Security.Profiles.Triggers["file_watch"],
				cfg.Security.Profiles.Triggers["filewatch"],
			)),
		},
	}
	return filterIngress(items, filter)
}

func filterIngress(items []capabilitiesIngressSummary, filter string) []capabilitiesIngressSummary {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return items
	}
	out := make([]capabilitiesIngressSummary, 0, 1)
	for _, item := range items {
		if item.Name == filter {
			out = append(out, item)
		}
	}
	return out
}

func effectiveProfileSummary(cfg config.Config, name string) *capabilitiesProfileSummary {
	name = strings.TrimSpace(name)
	if !cfg.Security.Profiles.Enabled && name == "" {
		return nil
	}
	if name == "" {
		name = strings.TrimSpace(cfg.Security.Profiles.Default)
	}
	if name == "" {
		return nil
	}
	profile, ok := cfg.Security.Profiles.Profiles[name]
	if !ok {
		return &capabilitiesProfileSummary{Name: name}
	}
	allowedTools := append([]string{}, profile.AllowedTools...)
	sort.Strings(allowedTools)
	allowedHosts := append([]string{}, profile.AllowedHosts...)
	sort.Strings(allowedHosts)
	writablePaths := append([]string{}, profile.WritablePaths...)
	sort.Strings(writablePaths)
	return &capabilitiesProfileSummary{
		Name:           name,
		MaxCapability:  strings.TrimSpace(profile.MaxCapability),
		AllowedTools:   allowedTools,
		AllowedHosts:   allowedHosts,
		WritablePaths:  writablePaths,
		AllowSubagents: profile.AllowSubagents,
	}
}

func enabledMCPServers(cfg config.Config) []string {
	out := make([]string, 0, len(cfg.Tools.MCPServers))
	for name, server := range cfg.Tools.MCPServers {
		if server.Enabled {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func firstNonEmptyCapabilityString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func printCapabilitiesReport(w io.Writer, report capabilitiesReport) {
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
