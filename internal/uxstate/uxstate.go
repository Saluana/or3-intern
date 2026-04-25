package uxstate

import (
	"encoding/json"
	"fmt"
	"strings"

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
	Problems      []ProblemView
}

type ProblemView struct {
	ID                string
	Title             string
	WhyItMatters      string
	RecommendedAction string
	Severity          string
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
	DeviceID  string
	Name      string
	RoleLabel string
	Status    string
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
		Problems:      BuildProblemViews(report.Findings),
	}
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
			DeviceID:  record.DeviceID,
			Name:      firstNonEmpty(record.DisplayName, record.DeviceID),
			RoleLabel: uxcopy.DeviceRoleLabel(record.Role),
			Status:    humanStatus(record.Status),
		})
	}
	return out
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "Unnamed item"
}
