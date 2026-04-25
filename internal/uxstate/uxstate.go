package uxstate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/safetymode"
	"or3-intern/internal/uxcopy"
)

type StatusView struct {
	Headline      string
	SafetyLabel   string
	SafetySummary string
	Workspace     string
	Commands      string
	Internet      string
	Devices       string
	ActivityLog   string
	Access        AccessDashboardView
	Problems      []ProblemView
}

type ProblemView struct {
	ID                string
	Title             string
	WhyItMatters      string
	RecommendedAction string
	Severity          string
	FixMode           string
}

type ApprovalPromptView struct {
	RequestID       int64
	Title           string
	ActionSummary   string
	Why             string
	RiskLabel       string
	RiskExplanation string
	ChoiceHints     []string
	AdvancedDetails []string
}

type DeviceView struct {
	DeviceID         string
	Name             string
	RoleLabel        string
	Status           string
	LastUsed         string
	ChangeAccessHint string
	DisconnectHint   string
}

type SettingsHomeView struct {
	Sections []SettingsSectionView
	Commands []string
}

type SettingsSectionView struct {
	Key      string
	Title    string
	Summary  string
	Action   string
	Advanced bool
}

type AccessDashboardView struct {
	Sections []AccessSectionView
}

type AccessSectionView struct {
	Name   string
	Status string
	Risk   string
	Detail string
	Action string
}

func BuildStatusView(cfg config.Config, report intdoctor.Report, deviceCount, pendingApprovals int) StatusView {
	inference := safetymode.Infer(cfg)
	mode := inference.Mode
	if inference.IsCustom && inference.BaseMode != "" {
		mode = inference.BaseMode
	}
	return StatusView{
		Headline:      headline(report),
		SafetyLabel:   uxcopy.SafetyModeLabel(inference.Mode, inference.IsCustom, inference.BaseMode),
		SafetySummary: uxcopy.SafetyModeSummary(mode),
		Workspace:     workspaceSummary(cfg),
		Commands:      commandSummary(cfg),
		Internet:      internetSummary(cfg),
		Devices:       deviceSummary(cfg, deviceCount, pendingApprovals),
		ActivityLog:   activitySummary(cfg),
		Access:        BuildAccessDashboardView(cfg, report, deviceCount, pendingApprovals),
		Problems:      BuildProblemViews(report.Findings),
	}
}

func BuildSettingsHomeView(cfg config.Config) SettingsHomeView {
	inference := safetymode.Infer(cfg)
	return SettingsHomeView{Sections: []SettingsSectionView{
		settingsSection("provider", providerSummary(cfg), "or3-intern settings --section provider", false),
		settingsSection("workspace", workspaceSummary(cfg), "or3-intern settings --section workspace", false),
		settingsSection("devices", deviceSummary(cfg, 0, 0), "or3-intern connect-device", false),
		settingsSection("runtimeProfile", uxcopy.SafetyModeLabel(inference.Mode, inference.IsCustom, inference.BaseMode), "or3-intern settings --section safety", false),
		settingsSection("channels", channelsSummary(cfg), "or3-intern settings --section channels", false),
		settingsSection("tools", toolsSummary(cfg), "or3-intern settings --section tools", false),
		settingsSection("memory", memorySummary(cfg), "or3-intern settings --section memory", false),
		settingsSection("advanced", "Raw config sections and export", "or3-intern settings --advanced", true),
	}, Commands: []string{
		"or3-intern settings --section provider",
		"or3-intern settings --section workspace",
		"or3-intern settings --section safety",
		"or3-intern settings --export config.json",
	}}
}

func BuildAccessDashboardView(cfg config.Config, report intdoctor.Report, deviceCount, pendingApprovals int) AccessDashboardView {
	fileRisk := "green"
	if !cfg.Tools.RestrictToWorkspace {
		fileRisk = "red"
	}
	commandRisk := "green"
	execAvailable := cfg.Tools.EnableExec && len(cfg.Hardening.ExecAllowedPrograms) > 0 && !config.ProfileSpec(cfg.RuntimeProfile).ForbidPrivilegedTools
	if execAvailable && cfg.Security.Approvals.Exec.Mode == config.ApprovalModeDeny {
		commandRisk = "green"
	} else if execAvailable {
		commandRisk = "yellow"
		if !cfg.Hardening.GuardedTools && cfg.Security.Approvals.Exec.Mode == config.ApprovalModeTrusted {
			commandRisk = "red"
		}
	}
	internetRisk := "yellow"
	if cfg.Security.Network.Enabled && cfg.Security.Network.DefaultDeny {
		internetRisk = "green"
	}
	deviceRisk := "gray"
	if cfg.Service.Enabled {
		deviceRisk = "yellow"
		if strings.TrimSpace(cfg.Service.Secret) != "" && !cfg.Service.AllowUnauthenticatedPairing {
			deviceRisk = "green"
		}
	}
	logRisk := "red"
	if cfg.Security.Audit.Enabled {
		logRisk = "green"
	}
	if report.HasBlockingFindings() && logRisk == "green" {
		logRisk = "yellow"
	}
	return AccessDashboardView{Sections: []AccessSectionView{
		{Name: "Files", Status: workspaceSummary(cfg), Risk: fileRisk, Detail: "Answers whether OR3 can see your whole computer or only one folder.", Action: "or3-intern settings --section workspace"},
		{Name: "Commands", Status: commandSummary(cfg), Risk: commandRisk, Detail: "Shows whether local command execution is blocked, asks first, or follows tool defaults.", Action: "or3-intern settings --section safety"},
		{Name: "Internet", Status: internetSummary(cfg), Risk: internetRisk, Detail: "Covers web/proxy/network-policy posture for outbound access.", Action: "or3-intern settings --section tools"},
		{Name: "Connected Apps", Status: channelsSummary(cfg), Risk: channelsRisk(cfg), Detail: "External channel adapters stay optional and hidden until enabled.", Action: "or3-intern settings --section channels"},
		{Name: "Connected Devices", Status: deviceSummary(cfg, deviceCount, pendingApprovals), Risk: deviceRisk, Detail: "Shows whether phones, apps, or service clients can connect.", Action: "or3-intern connect-device list"},
		{Name: "Memory", Status: memorySummary(cfg), Risk: "yellow", Detail: "Summarizes standing memory and document indexing.", Action: "or3-intern settings --section memory"},
		{Name: "Activity Log", Status: activitySummary(cfg), Risk: logRisk, Detail: "Shows whether important actions are recorded for review.", Action: "or3-intern status --advanced"},
	}}
}

func BuildProblemViews(findings []intdoctor.Finding) []ProblemView {
	out := make([]ProblemView, 0, len(findings))
	for _, finding := range findings {
		copy := uxcopy.ProblemForFinding(finding.ID, finding.Summary)
		out = append(out, ProblemView{
			ID:                finding.ID,
			Title:             copy.Title,
			WhyItMatters:      copy.WhyItMatters,
			RecommendedAction: firstNonEmpty(strings.TrimSpace(finding.FixHint), copy.RecommendedAction),
			Severity:          string(finding.Severity),
			FixMode:           string(finding.FixMode),
		})
	}
	return out
}

func BuildApprovalPrompt(item db.ApprovalRequestRecord) ApprovalPromptView {
	view := ApprovalPromptView{
		RequestID: item.ID,
		Why:       "The assistant asked to do something that needs your permission.",
		ChoiceHints: []string{
			"Allow once",
			"Always allow this kind of action in this folder",
			"Deny",
		},
		AdvancedDetails: []string{
			fmt.Sprintf("Request ID: %d", item.ID),
			fmt.Sprintf("Type: %s", item.Type),
			fmt.Sprintf("Policy mode: %s", item.PolicyMode),
		},
	}
	switch item.Type {
	case string(approval.SubjectExec):
		var subject approval.ExecSubject
		_ = json.Unmarshal([]byte(item.SubjectJSON), &subject)
		command := strings.TrimSpace(strings.Join(subject.Argv, " "))
		if command == "" {
			command = strings.TrimSpace(subject.ExecutablePath)
		}
		view.Title = "OR3 wants to run a command"
		view.ActionSummary = command
		view.RiskLabel, view.RiskExplanation = classifyExecRisk(command)
	case string(approval.SubjectSkillExec):
		var subject approval.SkillExecutionSubject
		_ = json.Unmarshal([]byte(item.SubjectJSON), &subject)
		view.Title = "OR3 wants to run a skill"
		view.ActionSummary = firstNonEmpty(subject.SkillID, "skill execution")
		view.RiskLabel = "High"
		view.RiskExplanation = "Skills can run code or change files on your behalf."
	case string(approval.SubjectSecretAccess):
		view.Title = "OR3 wants to use a secret"
		view.ActionSummary = subjectSummary(item.SubjectJSON, "secret access")
		view.RiskLabel = "High"
		view.RiskExplanation = "Secrets can unlock private accounts or services."
	case string(approval.SubjectMessageSend):
		view.Title = "OR3 wants to send a message"
		view.ActionSummary = subjectSummary(item.SubjectJSON, "message send")
		view.RiskLabel = "Medium"
		view.RiskExplanation = "Messages can share information with another person or service."
	case string(approval.SubjectFileTransfer):
		view.Title = "OR3 wants to transfer a file"
		view.ActionSummary = subjectSummary(item.SubjectJSON, "file transfer")
		view.RiskLabel = "Medium"
		view.RiskExplanation = "File transfers can expose local files or import untrusted content."
	default:
		view.Title = "OR3 wants approval"
		view.ActionSummary = item.Type
		view.RiskLabel = "Medium"
		view.RiskExplanation = "This action can change OR3 behavior or share information."
	}
	return view
}

func BuildDeviceViews(records []db.PairedDeviceRecord) []DeviceView {
	out := make([]DeviceView, 0, len(records))
	for _, record := range records {
		out = append(out, DeviceView{
			DeviceID:         record.DeviceID,
			Name:             firstNonEmpty(record.DisplayName, record.DeviceID),
			RoleLabel:        uxcopy.DeviceRoleLabel(record.Role),
			Status:           humanStatus(record.Status),
			LastUsed:         lastUsed(record.LastSeenAt),
			ChangeAccessHint: "Change access: or3-intern connect-device role " + record.DeviceID,
			DisconnectHint:   "Disconnect: or3-intern connect-device disconnect " + record.DeviceID,
		})
	}
	return out
}

func settingsSection(key, summary, action string, advanced bool) SettingsSectionView {
	copy := uxcopy.LabelForSetting(key)
	return SettingsSectionView{Key: key, Title: copy.Label, Summary: firstNonEmpty(summary, copy.Hint), Action: action, Advanced: advanced}
}

func providerSummary(cfg config.Config) string {
	if strings.TrimSpace(cfg.Provider.APIBase) == "" {
		return "Provider is not configured yet"
	}
	return fmt.Sprintf("%s using %s", cfg.Provider.APIBase, firstNonEmpty(cfg.Provider.Model, "default model"))
}

func channelsSummary(cfg config.Config) string {
	enabled := []string{}
	if cfg.Channels.Telegram.Enabled {
		enabled = append(enabled, "Telegram")
	}
	if cfg.Channels.Slack.Enabled {
		enabled = append(enabled, "Slack")
	}
	if cfg.Channels.Discord.Enabled {
		enabled = append(enabled, "Discord")
	}
	if cfg.Channels.WhatsApp.Enabled {
		enabled = append(enabled, "WhatsApp")
	}
	if cfg.Channels.Email.Enabled {
		enabled = append(enabled, "Email")
	}
	if len(enabled) == 0 {
		return "No external channels enabled"
	}
	return strings.Join(enabled, ", ") + " enabled"
}

func channelsRisk(cfg config.Config) string {
	if channelsSummary(cfg) == "No external channels enabled" {
		return "gray"
	}
	return "yellow"
}

func toolsSummary(cfg config.Config) string {
	parts := []string{}
	if cfg.Tools.EnableExec {
		parts = append(parts, "commands enabled")
	} else {
		parts = append(parts, "commands off")
	}
	if len(cfg.Tools.MCPServers) > 0 {
		parts = append(parts, fmt.Sprintf("%d MCP server(s)", len(cfg.Tools.MCPServers)))
	}
	if strings.TrimSpace(cfg.Tools.BraveAPIKey) != "" {
		parts = append(parts, "web search configured")
	}
	return strings.Join(parts, ", ")
}

func memorySummary(cfg config.Config) string {
	if cfg.DocIndex.Enabled {
		return fmt.Sprintf("Standing memory on; document indexing on for %d root(s)", len(cfg.DocIndex.Roots))
	}
	return "Standing memory on; document indexing off"
}

func headline(report intdoctor.Report) string {
	if len(report.Findings) == 0 {
		return "Ready"
	}
	if report.HasBlockingFindings() {
		return "Needs attention"
	}
	return "Review recommended"
}

func workspaceSummary(cfg config.Config) string {
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) != "" {
		return "Only this folder: " + cfg.WorkspaceDir
	}
	if cfg.Tools.RestrictToWorkspace {
		return "Restricted to your workspace folder"
	}
	return "Not restricted to one folder"
}

func commandSummary(cfg config.Config) string {
	switch {
	case cfg.Security.Approvals.Exec.Mode == config.ApprovalModeDeny:
		return "Blocks direct commands by default"
	case cfg.Security.Approvals.Exec.Mode == config.ApprovalModeAsk || cfg.Hardening.GuardedTools:
		return "Ask before risky commands"
	default:
		return "Commands use the current local tool settings"
	}
}

func internetSummary(cfg config.Config) string {
	if cfg.Security.Network.Enabled && cfg.Security.Network.DefaultDeny {
		return "Internet access is restricted"
	}
	if strings.TrimSpace(cfg.Tools.WebProxy) != "" {
		return "Internet access uses a proxy"
	}
	return "Internet access follows the default tool settings"
}

func deviceSummary(cfg config.Config, deviceCount, pendingApprovals int) string {
	if !cfg.Service.Enabled {
		return "Other devices and apps cannot connect"
	}
	if deviceCount == 0 {
		if pendingApprovals > 0 {
			return fmt.Sprintf("No devices connected yet · %d approval(s) waiting", pendingApprovals)
		}
		return "Connections are enabled, but no devices are paired yet"
	}
	return fmt.Sprintf("%d connected device(s)", deviceCount)
}

func activitySummary(cfg config.Config) string {
	if cfg.Security.Audit.Enabled {
		if cfg.Security.Audit.Strict {
			return "Safety log is on and strict"
		}
		return "Safety log is on"
	}
	return "Safety log is off"
}

func classifyExecRisk(command string) (string, string) {
	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, " rm ") || strings.HasPrefix(lower, "rm ") || strings.Contains(lower, "sudo") || strings.Contains(lower, "bash") || strings.Contains(lower, "sh -c"):
		return "High", "This command can change or remove files on your computer."
	case strings.Contains(lower, "npm install") || strings.Contains(lower, "npx ") || strings.Contains(lower, "curl") || strings.Contains(lower, "wget"):
		return "Medium", "This command can download code or data from the internet."
	default:
		return "Low", "This looks like a bounded local command, but it can still affect files in your workspace."
	}
}

func humanStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case approval.StatusActive:
		return "Connected"
	case approval.StatusRevoked:
		return "Disconnected"
	case approval.StatusPending:
		return "Waiting for approval"
	default:
		return strings.Title(strings.TrimSpace(status))
	}
}

func lastUsed(epoch int64) string {
	if epoch <= 0 {
		return "Never used"
	}
	return time.Unix(epoch, 0).UTC().Format("2006-01-02 15:04 UTC")
}

func subjectSummary(subjectJSON, fallback string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(subjectJSON), &raw); err != nil {
		return fallback
	}
	for _, key := range []string{"name", "secret", "secret_name", "channel", "target", "path", "file", "destination", "type"} {
		if value, ok := raw[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				return text
			}
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "Unnamed item"
}
