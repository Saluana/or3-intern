package uxcopy

import (
	"strings"

	"or3-intern/internal/safetymode"
)

type SettingCopy struct {
	Label string
	Hint  string
}

type ProblemCopy struct {
	Title             string
	WhyItMatters      string
	RecommendedAction string
}

type UserError struct {
	Title        string
	WhatHappened string
	Fix          string
	Command      string
	Advanced     string
}

var settingLabels = map[string]SettingCopy{
	"tools.restrictToWorkspace": {Label: "Only let OR3 access this folder", Hint: "Prevents OR3 from reading or writing outside your chosen workspace."},
	"hardening.guardedTools":    {Label: "Ask before risky actions", Hint: "OR3 pauses before actions that can change files, run code, or contact services."},
	"security.audit.enabled":    {Label: "Keep a safety log", Hint: "Saves a tamper-evident record of important actions."},
	"security.approvals.mode":   {Label: "When should OR3 ask?", Hint: "Controls prompts for commands, skills, secrets, messages, and pairing."},
	"service.enabled":           {Label: "Allow other devices and apps to connect", Hint: "Lets phones or companion apps connect when protected by a secret."},
	"runtimeProfile":            {Label: "Safety posture", Hint: "Advanced implementation detail behind Safety Mode."},
}

var findingCopy = map[string]ProblemCopy{
	"security.audit_disabled":                   {Title: "Safety log is off", WhyItMatters: "Without a safety log, it is harder to review what OR3 did.", RecommendedAction: "Turn on the safety log."},
	"security.audit_not_strict":                 {Title: "Safety log can fail open", WhyItMatters: "Important actions may continue even if the log cannot be written.", RecommendedAction: "Use stricter safety log settings."},
	"filesystem.workspace_restriction_disabled": {Title: "Folder limits are too broad", WhyItMatters: "OR3 is not restricted to a single workspace folder.", RecommendedAction: "Restrict OR3 to your chosen workspace folder."},
	"filesystem.workspace_dir_missing":          {Title: "Workspace folder is missing", WhyItMatters: "OR3 cannot stay inside a folder that no longer exists.", RecommendedAction: "Choose an existing workspace folder."},
	"privileged-exec.sandbox_disabled":          {Title: "Risky tools are not isolated", WhyItMatters: "Commands could affect your computer directly.", RecommendedAction: "Use safer command settings or enable sandboxing."},
	"service.secret_missing":                    {Title: "Connections are not protected yet", WhyItMatters: "Other devices or apps should not connect without a secret.", RecommendedAction: "Create a connection password."},
	"service.secret_weak":                       {Title: "Connection password is too weak", WhyItMatters: "A weak shared secret is easier to guess.", RecommendedAction: "Generate a stronger connection password."},
	"approvals.key_missing":                     {Title: "Approvals are not ready yet", WhyItMatters: "OR3 cannot safely issue approval tokens without its local signing key.", RecommendedAction: "Create the local approval key."},
	"approvals.key_path_missing":                {Title: "Approvals are missing a key path", WhyItMatters: "OR3 needs a place to store its approval signing key.", RecommendedAction: "Choose a local approval key path."},
	"config.validation.load":                    {Title: "Saved settings need repair", WhyItMatters: "The current config cannot be loaded normally.", RecommendedAction: "Review and repair the saved settings."},
	"config.validation.snapshot":                {Title: "Current settings are not valid", WhyItMatters: "OR3 cannot start safely with the current configuration.", RecommendedAction: "Repair the invalid settings before starting."},
	"runtime-profile.validation":                {Title: "Safety posture is inconsistent", WhyItMatters: "Some settings contradict the selected safety posture.", RecommendedAction: "Use a matching safety mode or repair the conflicting settings."},
}

func LabelForSetting(key string) SettingCopy {
	if copy, ok := settingLabels[strings.TrimSpace(key)]; ok {
		return copy
	}
	return SettingCopy{Label: strings.TrimSpace(key), Hint: "Advanced setting."}
}

func ProblemForFinding(id, summary string) ProblemCopy {
	if copy, ok := findingCopy[strings.TrimSpace(id)]; ok {
		return copy
	}
	return ProblemCopy{
		Title:             fallbackTitle(summary),
		WhyItMatters:      summary,
		RecommendedAction: "Review this setting and apply the recommended fix.",
	}
}

func TranslateError(err error) UserError {
	if err == nil {
		return UserError{}
	}
	raw := strings.TrimSpace(err.Error())
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "approval broker unavailable") || strings.Contains(lower, "approval broker is not configured"):
		return UserError{Title: "Approvals are not set up yet", WhatHappened: "OR3 cannot safely approve actions because the local approval system is missing or incomplete.", Fix: "Create the local approval key and turn on approvals.", Command: "or3-intern status", Advanced: raw}
	case strings.Contains(lower, "runtime unavailable"):
		return UserError{Title: "The assistant engine did not start", WhatHappened: "OR3 could not start its runtime safely.", Fix: "Check your provider settings and local runtime setup.", Command: "or3-intern status", Advanced: raw}
	case strings.Contains(lower, "service auth missing") || strings.Contains(lower, "service secret"):
		return UserError{Title: "Connections are not protected yet", WhatHappened: "A device or service connection needs a shared secret before it can be used safely.", Fix: "Create a connection password.", Command: "or3-intern connect-device", Advanced: raw}
	case strings.Contains(lower, "unknown tool in tool_policy"):
		return UserError{Title: "A saved tool rule is out of date", WhatHappened: "One of your saved settings refers to a tool that no longer exists.", Fix: "Review and update the saved tool settings.", Command: "or3-intern settings", Advanced: raw}
	case strings.Contains(lower, "audit logger unavailable"):
		return UserError{Title: "Safety log is not ready", WhatHappened: "OR3 is trying to use the safety log, but the logger could not start.", Fix: "Repair the safety log settings or generate its key.", Command: "or3-intern status", Advanced: raw}
	case strings.Contains(lower, "config error") || strings.Contains(lower, "validation"):
		return UserError{Title: "Saved settings need repair", WhatHappened: "OR3 could not use the current settings as-is.", Fix: "Run setup or settings to repair them.", Command: "or3-intern setup", Advanced: raw}
	default:
		return UserError{Title: fallbackTitle(raw), WhatHappened: raw, Fix: "Review the details and retry.", Command: "or3-intern status", Advanced: raw}
	}
}

func DeviceRoleLabel(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "viewer":
		return "Chat only"
	case "operator":
		return "Chat and workspace files"
	case "admin":
		return "Admin device"
	default:
		return "Connected device"
	}
}

func SafetyModeLabel(mode safetymode.Mode, custom bool, base safetymode.Mode) string {
	if custom {
		if base != "" && base != safetymode.ModeCustom {
			return "Custom based on " + strings.Title(strings.ReplaceAll(string(base), "-", " "))
		}
		return "Custom"
	}
	switch mode {
	case safetymode.ModeRelaxed:
		return "Relaxed"
	case safetymode.ModeLockedDown:
		return "Locked Down"
	default:
		return "Balanced"
	}
}

func SafetyModeSummary(mode safetymode.Mode) string {
	switch mode {
	case safetymode.ModeRelaxed:
		return "Good for local testing. Fewer prompts."
	case safetymode.ModeLockedDown:
		return "Best for servers or shared devices. Blocks dangerous actions by default."
	default:
		return "Recommended. Ask before risky actions."
	}
}

func fallbackTitle(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Something needs attention"
	}
	text = strings.TrimLeft(text, "- ")
	parts := strings.SplitN(text, ":", 2)
	text = strings.TrimSpace(parts[0])
	if text == "" {
		return "Something needs attention"
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
