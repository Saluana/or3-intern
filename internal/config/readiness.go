package config

import "strings"

// ReadinessState describes whether a config can safely run normal commands.
type ReadinessState string

const (
	ReadinessReady          ReadinessState = "ready"
	ReadinessNeedsRepair    ReadinessState = "needs-repair"
	ReadinessDraft          ReadinessState = "draft"
	ReadinessAdvancedCustom ReadinessState = "advanced-custom"
)

// ReadinessOptions scopes readiness to the command that is about to run.
type ReadinessOptions struct {
	Command         string
	ValidationError string
}

// ReadinessIssue explains one readiness blocker or advanced-custom reason.
type ReadinessIssue struct {
	Field  string `json:"field"`
	Title  string `json:"title"`
	Fix    string `json:"fix"`
	Draft  bool   `json:"draft,omitempty"`
	Repair bool   `json:"repair,omitempty"`
}

// ReadinessReport is the consumer-grade contract for config startup state.
type ReadinessReport struct {
	State   ReadinessState   `json:"state"`
	Command string           `json:"command,omitempty"`
	Issues  []ReadinessIssue `json:"issues,omitempty"`
}

// RequiredReadinessChecks names the high-level checks a command depends on.
func RequiredReadinessChecks(command string) []string {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "chat":
		return []string{"provider", "workspace", "database", "artifacts"}
	case "serve":
		return []string{"provider", "workspace", "database", "artifacts", "enabled-ingress"}
	case "service":
		return []string{"database", "artifacts", "service-auth", "service-bind"}
	default:
		return []string{"database", "artifacts"}
	}
}

// EvaluateReadiness classifies cfg without mutating it.
func EvaluateReadiness(cfg Config, opts ReadinessOptions) ReadinessReport {
	command := strings.ToLower(strings.TrimSpace(opts.Command))
	report := ReadinessReport{State: ReadinessReady, Command: command}
	checks := map[string]struct{}{}
	for _, check := range RequiredReadinessChecks(command) {
		checks[check] = struct{}{}
	}
	if strings.TrimSpace(opts.ValidationError) != "" {
		report.Issues = append(report.Issues, ReadinessIssue{
			Field:  "config",
			Title:  "Saved settings need repair",
			Fix:    "Run `or3-intern status` to review the repair guidance, then `or3-intern doctor --fix` for safe repairs.",
			Repair: true,
		})
	}
	if hasCheck(checks, "provider") {
		providerIssues := providerReadinessIssues(cfg)
		report.Issues = append(report.Issues, providerIssues...)
	}
	if hasCheck(checks, "workspace") && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		report.Issues = append(report.Issues, ReadinessIssue{
			Field: "workspaceDir",
			Title: "Workspace folder is not set",
			Fix:   "Run `or3-intern setup` and choose the folder OR3 may use.",
			Draft: true,
		})
	}
	if hasCheck(checks, "database") && strings.TrimSpace(cfg.DBPath) == "" {
		report.Issues = append(report.Issues, ReadinessIssue{
			Field:  "dbPath",
			Title:  "Local database path is missing",
			Fix:    "Run `or3-intern doctor --fix` to restore the default database path.",
			Repair: true,
		})
	}
	if hasCheck(checks, "artifacts") && strings.TrimSpace(cfg.ArtifactsDir) == "" {
		report.Issues = append(report.Issues, ReadinessIssue{
			Field:  "artifactsDir",
			Title:  "Artifacts folder is missing",
			Fix:    "Run `or3-intern doctor --fix` to restore the default artifacts folder.",
			Repair: true,
		})
	}
	if hasCheck(checks, "service-auth") {
		if strings.TrimSpace(cfg.Service.Secret) == "" {
			report.Issues = append(report.Issues, ReadinessIssue{
				Field:  "service.secret",
				Title:  "Connection password is missing",
				Fix:    "Run `or3-intern doctor --fix` to create a protected service secret.",
				Repair: true,
			})
		}
	}
	if hasCheck(checks, "service-bind") && strings.TrimSpace(cfg.Service.Listen) == "" {
		report.Issues = append(report.Issues, ReadinessIssue{
			Field:  "service.listen",
			Title:  "Service listen address is missing",
			Fix:    "Run `or3-intern doctor --fix` to restore the default local service address.",
			Repair: true,
		})
	}
	report.State = readinessStateFromIssues(report.Issues, cfg)
	return report
}

// LoadRepairable loads cfg for repair surfaces even when validation fails.
func LoadRepairable(path string, command string) (Config, ReadinessReport, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, EvaluateReadiness(cfg, ReadinessOptions{Command: command}), nil
	}
	raw, rawErr := readConfigFile(resolveConfigPath(path))
	if rawErr != nil {
		return raw, ReadinessReport{}, rawErr
	}
	ApplyEnvOverrides(&raw)
	return raw, EvaluateReadiness(raw, ReadinessOptions{Command: command, ValidationError: err.Error()}), nil
}

func providerReadinessIssues(cfg Config) []ReadinessIssue {
	issues := []ReadinessIssue{}
	if strings.TrimSpace(cfg.Provider.APIBase) == "" {
		issues = append(issues, ReadinessIssue{
			Field: "provider.apiBase",
			Title: "AI provider endpoint is missing",
			Fix:   "Run `or3-intern setup` and choose your AI provider.",
			Draft: true,
		})
	}
	if strings.TrimSpace(cfg.Provider.APIKey) == "" {
		issues = append(issues, ReadinessIssue{
			Field: "provider.apiKey",
			Title: "AI provider key is missing",
			Fix:   "Set the provider key in your environment or run `or3-intern setup` to save it locally.",
			Draft: true,
		})
	}
	if strings.TrimSpace(cfg.Provider.Model) == "" {
		issues = append(issues, ReadinessIssue{
			Field: "provider.model",
			Title: "Chat model is missing",
			Fix:   "Run `or3-intern setup` and choose a chat model.",
			Draft: true,
		})
	}
	return issues
}

func readinessStateFromIssues(issues []ReadinessIssue, cfg Config) ReadinessState {
	hasDraft := false
	hasRepair := false
	for _, issue := range issues {
		hasDraft = hasDraft || issue.Draft
		hasRepair = hasRepair || issue.Repair
	}
	switch {
	case hasRepair:
		return ReadinessNeedsRepair
	case hasDraft:
		return ReadinessDraft
	case isAdvancedCustomReadiness(cfg):
		return ReadinessAdvancedCustom
	default:
		return ReadinessReady
	}
}

func isAdvancedCustomReadiness(cfg Config) bool {
	if cfg.RuntimeProfile == ProfileHostedRemoteSandbox {
		return true
	}
	if cfg.Security.Profiles.Enabled {
		return true
	}
	if len(cfg.Tools.MCPServers) > 0 {
		return true
	}
	if len(cfg.ModelRouting.Chat.Fallbacks) > 0 || len(cfg.ModelRouting.Embeddings.Fallbacks) > 0 {
		return true
	}
	return false
}

func hasCheck(checks map[string]struct{}, check string) bool {
	_, ok := checks[check]
	return ok
}
