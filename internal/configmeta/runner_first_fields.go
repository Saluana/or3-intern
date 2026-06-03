package configmeta

import "or3-intern/internal/config"

// ConfigureFieldRunnerFirst holds runner-first UI metadata for a configure TUI field key.
type ConfigureFieldRunnerFirst struct {
	Status      FieldStatus
	Label       string
	Description string
}

// configureFieldRunnerFirst is the canonical registry for runner-first configure keys.
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
		Status:      FieldStatusCompatibility,
		Label:       "Agents routing provider (legacy)",
		Description: "Compatibility routing for legacy agent-style work. Background jobs should use agent_cli_run with an external runner.",
	},
	"routing_agents_model": {
		Status:      FieldStatusCompatibility,
		Label:       "Agents routing model (legacy)",
		Description: "Compatibility routing for legacy agent-style work. Background jobs should use agent_cli_run with an external runner instead.",
	},
	"routing_agents_fallbacks": {
		Status:      FieldStatusCompatibility,
		Label:       "Agents routing fallbacks (legacy)",
		Description: "Fallback list for legacy agent routing only.",
	},
	"routing_subagents_provider": {
		Status:      FieldStatusDeprecated,
		Label:       "Subagents provider (legacy)",
		Description: "Deprecated: internal subagent jobs are disabled in runner-first mode. Use external runners for background work.",
	},
	"routing_subagents_model": {
		Status:      FieldStatusDeprecated,
		Label:       "Subagents model (legacy)",
		Description: "Deprecated: internal subagent jobs are disabled in runner-first mode.",
	},
	"routing_subagents_fallbacks": {
		Status:      FieldStatusDeprecated,
		Label:       "Subagents fallbacks (legacy)",
		Description: "Deprecated: internal subagent jobs are disabled in runner-first mode.",
	},
	"runtime_subagents_enabled": {
		Status:      FieldStatusDeprecated,
		Label:       "Enable subagents (legacy)",
		Description: "Deprecated in runner-first mode. External runners handle parallel and scheduled work instead of the built-in subagent manager.",
	},
	"runtime_subagents_max_concurrent": {
		Status:      FieldStatusDeprecated,
		Label:       "Subagents max concurrent (legacy)",
		Description: "Deprecated in runner-first mode.",
	},
	"runtime_subagents_max_queued": {
		Status:      FieldStatusDeprecated,
		Label:       "Subagents max queued (legacy)",
		Description: "Deprecated in runner-first mode.",
	},
	"runtime_subagents_timeout": {
		Status:      FieldStatusDeprecated,
		Label:       "Subagents timeout (legacy)",
		Description: "Deprecated in runner-first mode.",
	},
	"runtime_max_tool_loops": {
		Status:      FieldStatusDeprecated,
		Label:       "Max tool loops (legacy)",
		Description: "Deprecated: applies only to the legacy built-in tool loop, not external runner chat.",
	},
	"runtime_max_tool_loops_exceeded_action": {
		Status:      FieldStatusDeprecated,
		Label:       "Tool loop limit action (legacy)",
		Description: "Deprecated with the legacy built-in tool loop.",
	},
	"tools_brave": {
		Status:      FieldStatusDeprecated,
		Label:       "Brave search API key (legacy)",
		Description: "Deprecated for model-callable tools in runner-first mode.",
	},
	"tools_enable_exec": {
		Status:      FieldStatusDeprecated,
		Label:       "Enable shell exec tool (legacy)",
		Description: "Deprecated for model-callable tools in runner-first mode. Use runner permissions instead.",
	},
	"tools_exec_timeout": {
		Status:      FieldStatusDeprecated,
		Label:       "Exec timeout (legacy)",
		Description: "Deprecated with legacy model-callable exec.",
	},
	"skills_enable_exec": {
		Status:      FieldStatusDeprecated,
		Label:       "Skills allow exec (legacy)",
		Description: "Deprecated for model-callable skill exec in runner-first mode.",
	},
	"context_dynamic_tools": {
		Status:      FieldStatusDeprecated,
		Label:       "Dynamic tool exposure (legacy)",
		Description: "Deprecated: dynamic tool schemas applied only to the legacy built-in loop.",
	},
	"context_task_card_enforce_plan": {
		Status:      FieldStatusDeprecated,
		Label:       "Enforce task card plan (legacy)",
		Description: "Deprecated: plan enforcement applied only to the legacy built-in tool loop.",
	},
	"context_section_tool_schemas": {
		Status:      FieldStatusDeprecated,
		Label:       "Tool schemas in context (legacy)",
		Description: "Deprecated for runner-first chat; external runners bring their own tool surfaces.",
	},
	"agentCLI_enabled":            {Status: FieldStatusActive},
	"agentCLI_default_runner":     {Status: FieldStatusActive},
	"agentCLI_max_concurrent":     {Status: FieldStatusActive},
	"agentCLI_max_queued":         {Status: FieldStatusActive},
	"agentCLI_default_timeout":    {Status: FieldStatusActive},
	"agentCLI_max_timeout":        {Status: FieldStatusActive},
	"agentCLI_allow_sandbox_auto": {Status: FieldStatusActive},
	"agentCLI_default_mode":       {Status: FieldStatusActive},
	"agentCLI_default_isolation":  {Status: FieldStatusActive},
	"agentCLI_disabled_runners":   {Status: FieldStatusActive},
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
