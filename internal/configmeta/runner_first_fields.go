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
// or3-app, doctor plan) when runner-first mode is active. They still
// exist on disk for backwards compatibility with users upgrading from
// the legacy agent, but no settings UI writes to them anymore.
//
// `FieldStatusDeprecated` fields are surfaced with a "(legacy)" label
// and a banner telling the user the setting no longer affects chat
// behavior — useful for the few users who still want to see what's
// being phased out.
//
// `FieldStatusCompatibility` fields still drive the legacy compat
// paths (summarization, embeddings, doctor repair) but no longer
// select the chat model.
var configureFieldRunnerFirst = map[string]ConfigureFieldRunnerFirst{
	"provider_model": {
		Status:      FieldStatusCompatibility,
		Label:       "Legacy provider default",
		Description: "Compatibility default for embeddings, summarization, and doctor flows. Active chat and channels use external runners, not this model ID.",
	},
	"provider_preset": {Status: FieldStatusCompatibility},
	"provider_temperature": {
		Status:      FieldStatusCompatibility,
		Description: "Sampling temperature for legacy provider calls (summarization, embeddings helpers). External runners use their own model settings for chat.",
	},
	"provider_timeout": {
		Status:      FieldStatusCompatibility,
		Description: "Timeout for legacy provider calls. External runners use runner-specific timeouts.",
	},
	"provider_vision": {
		Status:      FieldStatusDeprecated,
		Label:       "Image understanding (legacy)",
		Description: "Deprecated in runner-first mode. External runner attachment handling is not gated by this toggle.",
	},
	"routing_chat_provider": {
		Status:      FieldStatusCompatibility,
		Label:       "Chat routing provider (legacy)",
		Description: "Compatibility routing for legacy built-in chat turns. Runner-first chat uses external runners discovered via agent CLI.",
	},
	"routing_chat_model": {
		Status:      FieldStatusCompatibility,
		Label:       "Chat routing model (legacy)",
		Description: "Compatibility routing for legacy built-in chat turns. Does not select the model used by OpenCode or other external runners.",
	},
	"routing_chat_fallbacks": {
		Status:      FieldStatusCompatibility,
		Label:       "Chat routing fallbacks (legacy)",
		Description: "Fallback list for legacy built-in chat turns only.",
	},
	"routing_agents_provider": {
		Status:      FieldStatusDeprecated,
		Label:       "Agents routing provider (legacy)",
		Description: "Deprecated in runner-first mode. External runner selection replaces the built-in agent model role.",
	},
	"routing_agents_model": {
		Status:      FieldStatusDeprecated,
		Label:       "Agents routing model (legacy)",
		Description: "Deprecated in runner-first mode. External runners choose their own model.",
	},
	"routing_agents_fallbacks": {
		Status:      FieldStatusDeprecated,
		Label:       "Agents routing fallbacks (legacy)",
		Description: "Deprecated with the legacy built-in agent model role.",
	},
	"routing_context_provider": {
		Status:      FieldStatusDeprecated,
		Label:       "Context manager provider (legacy)",
		Description: "Deprecated in runner-first mode. Runner-first chat does not call the context-manager provider client.",
	},
	"routing_context_model": {
		Status:      FieldStatusDeprecated,
		Label:       "Context manager model (legacy)",
		Description: "Deprecated in runner-first mode. Memory consolidation uses the summarization role instead.",
	},
	"routing_context_fallbacks": {
		Status:      FieldStatusDeprecated,
		Label:       "Context manager fallbacks (legacy)",
		Description: "Deprecated with the legacy context-manager provider role.",
	},
	"tools_brave": {
		Status:      FieldStatusDeprecated,
		Label:       "Brave search API key (legacy)",
		Description: "Deprecated for model-callable tools in runner-first mode.",
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
	"context_dynamic_tools": {
		Status:      FieldStatusDeprecated,
		Label:       "Dynamic tool exposure (legacy)",
		Description: "Deprecated: dynamic tool schemas applied only to the legacy built-in loop.",
	},
	"context_max_input_tokens": {
		Status:      FieldStatusDeprecated,
		Label:       "Max input tokens (legacy)",
		Description: "Deprecated in runner-first mode. External runner context is assembled by runner-specific bootstrap paths.",
	},
	"context_task_card_enforce_plan": {
		Status:      FieldStatusHidden,
		Label:       "Enforce task card plan (legacy)",
		Description: "Hidden in runner-first mode. Runners plan their own work; the or3-intern task card is gone.",
	},
	"context_section_tool_schemas": {
		Status:      FieldStatusDeprecated,
		Label:       "Tool schemas in context (legacy)",
		Description: "Deprecated for runner-first chat; external runners bring their own tool surfaces.",
	},
	"context_manager_enabled": {
		Status:      FieldStatusDeprecated,
		Label:       "Context manager (legacy)",
		Description: "Deprecated in runner-first mode. Background memory consolidation uses the summarization path.",
	},
	"context_manager_provider":   {Status: FieldStatusDeprecated, Label: "Context manager provider (legacy)"},
	"context_manager_model":      {Status: FieldStatusDeprecated, Label: "Context manager model (legacy)"},
	"context_manager_timeout":    {Status: FieldStatusDeprecated, Label: "Context manager timeout (legacy)"},
	"context_manager_idle_prune": {Status: FieldStatusDeprecated, Label: "Idle prune seconds (legacy)"},
	"context_manager_max_input":  {Status: FieldStatusDeprecated, Label: "Context manager max input (legacy)"},
	"context_manager_max_output": {Status: FieldStatusDeprecated, Label: "Context manager max output (legacy)"},
	"context_manager_allow_task_updates": {
		Status: FieldStatusDeprecated,
		Label:  "Allow task updates (legacy)",
	},
	"context_manager_allow_stale_propose": {
		Status: FieldStatusDeprecated,
		Label:  "Allow stale proposals (legacy)",
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
	"docindex_enabled":   {Status: FieldStatusHidden, Label: "Search workspace files (legacy)", Description: "Hidden in runner-first mode. Runners bring their own file search."},
	"docindex_roots":     {Status: FieldStatusHidden, Label: "Doc index roots (legacy)"},
	"docindex_max_files": {Status: FieldStatusHidden, Label: "Doc index max files (legacy)"},
	"docindex_max_file_bytes": {Status: FieldStatusHidden, Label: "Doc index max file bytes (legacy)"},
	"docindex_max_chunks":      {Status: FieldStatusHidden, Label: "Doc index max chunks (legacy)"},
	"docindex_embed_max_bytes": {Status: FieldStatusHidden, Label: "Doc index embed max bytes (legacy)"},
	"docindex_refresh_seconds": {Status: FieldStatusHidden, Label: "Doc index refresh seconds (legacy)"},
	"docindex_retrieve_limit":  {Status: FieldStatusHidden, Label: "Doc index retrieve limit (legacy)"},
	"agentCLI_enabled": {
		Status:      FieldStatusHidden,
		Label:       "Runners toggle (legacy)",
		Description: "Hidden in runner-first mode. Runners are always on; there is no legacy agent to compare against.",
	},
	"agentCLI_default_runner":     {Status: FieldStatusHidden, Label: "Default runner (legacy)", Description: "Hidden in runner-first mode. The service picks the default runner itself."},
	"agentCLI_max_concurrent":     {Status: FieldStatusHidden, Label: "Runner concurrency (legacy)", Description: "Hidden in runner-first mode. Set by the runner host."},
	"agentCLI_max_queued":         {Status: FieldStatusHidden, Label: "Runner queue size (legacy)", Description: "Hidden in runner-first mode. Set by the runner host."},
	"agentCLI_default_timeout":    {Status: FieldStatusHidden, Label: "Default runner timeout (legacy)", Description: "Hidden in runner-first mode. Per-runner."},
	"agentCLI_max_timeout":        {Status: FieldStatusHidden, Label: "Max runner timeout (legacy)", Description: "Hidden in runner-first mode. Per-runner."},
	"agentCLI_allow_sandbox_auto": {Status: FieldStatusHidden, Label: "Full autonomy in sandbox (legacy)", Description: "Hidden in runner-first mode. Sandbox autonomy is a runner setting."},
	"agentCLI_default_mode":       {Status: FieldStatusHidden, Label: "Default mode (legacy)"},
	"agentCLI_default_isolation":  {Status: FieldStatusHidden, Label: "Default isolation (legacy)"},
	"agentCLI_disabled_runners":   {Status: FieldStatusHidden, Label: "Disabled runners (legacy)", Description: "Hidden in runner-first mode. The runner host decides what's available."},
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
	case FieldStatusDeprecated:
		if label != "" {
			label += " (legacy)"
		}
		if description != "" {
			description += " Deprecated in runner-first mode."
		}
	case FieldStatusCompatibility:
		if description != "" {
			description = "Compatibility only in runner-first mode. " + description
		}
	}
	return label, description
}
