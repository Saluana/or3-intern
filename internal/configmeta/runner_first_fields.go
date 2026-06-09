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
	"context_max_input_tokens": {
		Status:      FieldStatusDeprecated,
		Label:       "Max input tokens (legacy)",
		Description: "Deprecated in runner-first mode. External runner context is assembled by runner-specific bootstrap paths.",
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
		Status:      FieldStatusDeprecated,
		Label:       "Skill execution mode (legacy)",
		Description: "Deprecated with legacy model-callable skill execution.",
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
