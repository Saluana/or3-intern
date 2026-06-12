package configmeta

import "or3-intern/internal/config"

// ConfigureFieldRunnerFirst holds runner-first UI metadata for a configure TUI field key.
type ConfigureFieldRunnerFirst struct {
	Status      FieldStatus
	Label       string
	Description string
}

// configureFieldRunnerFirst is the canonical registry for runner-first configure keys.
//
// `FieldStatusHidden` fields are removed from every UI surface (TUI,
// or3-app, doctor plan) when runner-first mode is active.
//
// `FieldStatusCompatibility` fields still drive provider-side service work
// such as summarization, embeddings, and doctor repair, but do not select the
// runner chat model.
var configureFieldRunnerFirst = map[string]ConfigureFieldRunnerFirst{
	"provider_model": {
		Status:      FieldStatusHidden,
		Label:       "Provider model",
		Description: "Hidden in runner-first mode. External runners own chat models; summarization and embeddings use role-specific settings.",
	},
	"provider_preset": {Status: FieldStatusCompatibility},
	"provider_temperature": {
		Status:      FieldStatusCompatibility,
		Description: "Sampling temperature for OR3 provider calls such as summarization helpers. External runners use their own model settings.",
	},
	"provider_timeout": {
		Status:      FieldStatusCompatibility,
		Description: "Timeout for OR3 provider calls such as summarization helpers. External runners use runner-specific timeouts.",
	},
	"provider_vision": {
		Status:      FieldStatusHidden,
		Label:       "Image understanding (legacy)",
		Description: "Hidden in runner-first mode. External runner attachment handling is not gated by this toggle.",
	},
	"routing_chat_provider": {
		Status:      FieldStatusHidden,
		Label:       "Chat routing provider (legacy)",
		Description: "Hidden in runner-first mode. Runner chat uses external runner configuration.",
	},
	"routing_chat_model": {
		Status:      FieldStatusHidden,
		Label:       "Chat routing model (legacy)",
		Description: "Hidden in runner-first mode. Runner chat uses external runner configuration.",
	},
	"routing_chat_fallbacks": {
		Status:      FieldStatusHidden,
		Label:       "Chat routing fallbacks (legacy)",
		Description: "Hidden in runner-first mode. Runner chat uses external runner configuration.",
	},
	"routing_agents_provider": {
		Status:      FieldStatusHidden,
		Label:       "Agents routing provider (legacy)",
		Description: "Hidden in runner-first mode. External runner selection replaces the built-in agent model role.",
	},
	"routing_agents_model": {
		Status:      FieldStatusHidden,
		Label:       "Agents routing model (legacy)",
		Description: "Hidden in runner-first mode. External runners choose their own model.",
	},
	"routing_agents_fallbacks": {
		Status:      FieldStatusHidden,
		Label:       "Agents routing fallbacks (legacy)",
		Description: "Hidden in runner-first mode. External runners choose their own model.",
	},
	"tools_brave": {
		Status:      FieldStatusHidden,
		Label:       "Brave search API key (legacy)",
		Description: "Hidden in runner-first mode. Model-callable web-search tools are gone.",
	},
	"tools_enable_exec": {
		Status:      FieldStatusHidden,
		Label:       "Enable shell exec tool (legacy)",
		Description: "Hidden in runner-first mode. The or3-intern built-in agent loop is gone; runners own their own exec permissions.",
	},
	"tools_exec_timeout": {
		Status:      FieldStatusHidden,
		Label:       "Exec timeout (legacy)",
		Description: "Hidden in runner-first mode. Replaced by per-runner timeouts.",
	},
	"tools_exec_allowed_programs": {
		Status:      FieldStatusHidden,
		Label:       "Exec allowed programs (legacy)",
		Description: "Hidden in runner-first mode. Runners control their own program allowlist.",
	},
	"tools_path_append": {
		Status:      FieldStatusHidden,
		Label:       "Extra command PATH (legacy)",
		Description: "Hidden in runner-first mode. Runners inherit the host PATH.",
	},
	"tools_restrict_to_workspace": {Status: FieldStatusActive},
	"skills_enable_exec": {
		Status:      FieldStatusHidden,
		Label:       "Skills allow exec (legacy)",
		Description: "Hidden in runner-first mode. Skill execution moved to runner-owned scripts.",
	},
	"context_max_input_tokens": {
		Status:      FieldStatusHidden,
		Label:       "Max input tokens (legacy)",
		Description: "Hidden in runner-first mode. External runner context is assembled by runner-specific bootstrap paths.",
	},
	"security_approval_skill_mode": {
		Status:      FieldStatusHidden,
		Label:       "Skill execution mode (legacy)",
		Description: "Hidden in runner-first mode. Skill scripts are runner-managed; the or3-intern skill approval broker is gone.",
	},
	"security_approval_exec_mode": {
		Status:      FieldStatusHidden,
		Label:       "Command approval mode (legacy)",
		Description: "Hidden in runner-first mode. The or3-intern exec tool is gone; runner commands have their own approval path.",
	},
	"hardening_guarded_tools": {
		Status:      FieldStatusHidden,
		Label:       "Guarded tools (legacy)",
		Description: "Hidden in runner-first mode. The legacy guarded-tool cap is gone; runner permissions replace it.",
	},
	"hardening_privileged_tools": {
		Status:      FieldStatusHidden,
		Label:       "Privileged tools (legacy)",
		Description: "Hidden in runner-first mode. Runners declare their own privilege model.",
	},
	"hardening_exec_shell": {
		Status:      FieldStatusHidden,
		Label:       "Allow terminal commands (legacy)",
		Description: "Hidden in runner-first mode. Runners own shell access.",
	},
	"hardening_exec_allowed_programs": {
		Status:      FieldStatusHidden,
		Label:       "Allowed command programs (legacy)",
		Description: "Hidden in runner-first mode. Replaced by per-runner allowlists.",
	},
	"service_max_capability": {
		Status:      FieldStatusHidden,
		Label:       "Service tool power (legacy)",
		Description: "Hidden in runner-first mode. Tool capability is set per-runner.",
	},
	"runners_default":            {Status: FieldStatusHidden, Label: "Default runner (legacy)", Description: "Hidden in runner-first mode. The service picks the default runner itself."},
	"runners_max_concurrent":     {Status: FieldStatusHidden, Label: "Runner concurrency (legacy)", Description: "Hidden in runner-first mode. Set by the runner host."},
	"runners_max_queued":         {Status: FieldStatusHidden, Label: "Runner queue size (legacy)", Description: "Hidden in runner-first mode. Set by the runner host."},
	"runners_default_timeout":    {Status: FieldStatusHidden, Label: "Default runner timeout (legacy)", Description: "Hidden in runner-first mode. Per-runner."},
	"runners_max_timeout":        {Status: FieldStatusHidden, Label: "Max runner timeout (legacy)", Description: "Hidden in runner-first mode. Per-runner."},
	"runners_allow_sandbox_auto": {Status: FieldStatusHidden, Label: "Full autonomy in sandbox (legacy)", Description: "Hidden in runner-first mode. Sandbox autonomy is a runner setting."},
	"runners_default_mode":       {Status: FieldStatusHidden, Label: "Default mode (legacy)"},
	"runners_default_isolation":  {Status: FieldStatusHidden, Label: "Default isolation (legacy)"},
	"runners_disabled_runners":   {Status: FieldStatusHidden, Label: "Disabled runners (legacy)", Description: "Hidden in runner-first mode. The runner host decides what's available."},
}

// ApplyConfigureFieldCopy returns runner-first label and description overrides for configure TUI fields.
func ApplyConfigureFieldCopy(cfg config.Config, key, label, description string) (string, string) {
	if !cfg.RunnerFirst() {
		return label, description
	}
	if meta, ok := configureFieldRunnerFirst[key]; ok {
		if meta.Label != "" {
			label = meta.Label
		}
		if meta.Description != "" {
			description = meta.Description
		}
		return label, description
	}
	status := StatusForConfigureKey(cfg, key)
	switch status {
	case FieldStatusCompatibility:
		if description != "" {
			description = "Compatibility only in runner-first mode. " + description
		}
	}
	return label, description
}
