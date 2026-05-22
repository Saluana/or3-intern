package configedit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"or3-intern/internal/config"
)

const secretClearKeyword = "clear"

type providerPreset struct {
	apiBase    string
	model      string
	embedModel string
}

var providerPresets = map[string]providerPreset{
	"1": {
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
	"2": {
		apiBase:    "https://openrouter.ai/api/v1",
		model:      "openai/gpt-4o-mini",
		embedModel: "text-embedding-3-small",
	},
	"3": {
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
}

func ApplyProviderPreset(cfg *config.Config, choice string) {
	preset, ok := providerPresets[choice]
	if !ok || cfg == nil {
		return
	}
	cfg.Provider.APIBase = preset.apiBase
	cfg.Provider.Model = preset.model
	cfg.Provider.EmbedModel = preset.embedModel
	providerKey := ConfigureProviderKeyFromBase(preset.apiBase)
	if cfg.ModelRouting.Chat.Primary.Provider == "" || choice != "3" {
		cfg.ModelRouting.Chat.Primary = config.ModelRef{Provider: providerKey, Model: preset.model}
		cfg.ModelRouting.Agents.Primary = cfg.ModelRouting.Chat.Primary
		cfg.ModelRouting.Subagents.Primary = cfg.ModelRouting.Chat.Primary
		cfg.ModelRouting.Summarization.Primary = cfg.ModelRouting.Chat.Primary
		cfg.ModelRouting.ContextManager.Primary = cfg.ModelRouting.Chat.Primary
		cfg.ModelRouting.Embeddings.Primary = config.ModelRef{Provider: providerKey, Model: preset.embedModel}
	}
	SetProviderProfileAPIBase(cfg, providerKey, preset.apiBase)
}

func ToggleFieldValue(cfg *config.Config, section, channel, fieldKey string) bool {
	if cfg == nil {
		return false
	}
	current := false
	if section == "channels" {
		switch channel {
		case "telegram":
			current = cfg.Channels.Telegram.Enabled
		case "slack":
			if fieldKey == "require_mention" {
				current = cfg.Channels.Slack.RequireMention
			} else {
				current = cfg.Channels.Slack.Enabled
			}
		case "discord":
			if fieldKey == "require_mention" {
				current = cfg.Channels.Discord.RequireMention
			} else {
				current = cfg.Channels.Discord.Enabled
			}
		case "whatsapp":
			current = cfg.Channels.WhatsApp.Enabled
		case "email":
			if fieldKey == "consent" {
				current = cfg.Channels.Email.ConsentGranted
			} else {
				current = cfg.Channels.Email.Enabled
			}
		}
	}
	return SetToggleFieldValue(cfg, section, channel, fieldKey, !current)
}

func SetToggleFieldValue(cfg *config.Config, section, channel, fieldKey string, value bool) bool {
	if cfg == nil {
		return false
	}
	if section == "channels" {
		switch channel {
		case "telegram":
			if fieldKey == "enabled" {
				cfg.Channels.Telegram.Enabled = value
				return true
			}
		case "slack":
			if fieldKey == "enabled" {
				cfg.Channels.Slack.Enabled = value
				return true
			}
			if fieldKey == "require_mention" {
				cfg.Channels.Slack.RequireMention = value
				return true
			}
		case "discord":
			if fieldKey == "enabled" {
				cfg.Channels.Discord.Enabled = value
				config.NormalizeManagedChannelInboundDefaults(cfg)
				return true
			}
			if fieldKey == "require_mention" {
				cfg.Channels.Discord.RequireMention = value
				return true
			}
		case "whatsapp":
			if fieldKey == "enabled" {
				cfg.Channels.WhatsApp.Enabled = value
				return true
			}
		case "email":
			if fieldKey == "enabled" {
				cfg.Channels.Email.Enabled = value
				return true
			}
			if fieldKey == "consent" {
				cfg.Channels.Email.ConsentGranted = value
				return true
			}
		}
		return false
	}
	if section == "mcp" && fieldKey == "mcp_enabled" {
		server, ok := cfg.Tools.MCPServers[channel]
		if !ok {
			return false
		}
		server.Enabled = value
		cfg.Tools.MCPServers[channel] = server
		return true
	}
	if section == "mcp" && fieldKey == "mcp_allow_insecure_http" {
		server, ok := cfg.Tools.MCPServers[channel]
		if !ok {
			return false
		}
		server.AllowInsecureHTTP = value
		cfg.Tools.MCPServers[channel] = server
		return true
	}
	if section == "skills_entry" {
		if strings.TrimSpace(channel) == "" {
			return false
		}
		if cfg.Skills.Entries == nil {
			cfg.Skills.Entries = map[string]config.SkillEntryConfig{}
		}
		entry := cfg.Skills.Entries[channel]
		if fieldKey == "enabled" {
			entry.Enabled = &value
			cfg.Skills.Entries[channel] = entry
			return true
		}
		return false
	}
	switch fieldKey {
	case "provider_vision":
		cfg.Provider.EnableVision = value
	case "runtime_consolidation_enabled":
		cfg.ConsolidationEnabled = value
	case "runtime_subagents_enabled":
		cfg.Subagents.Enabled = value
	case "context_dynamic_tools":
		cfg.Context.Tools.DynamicExpose = value
	case "context_task_card_enabled":
		cfg.Context.TaskCard.Enabled = value
	case "context_manager_enabled":
		cfg.ContextManager.Enabled = value
	case "context_manager_allow_task_updates":
		cfg.ContextManager.AllowTaskUpdates = value
	case "context_manager_allow_stale_propose":
		cfg.ContextManager.AllowStalePropose = value
	case "workspace_restrict":
		cfg.Tools.RestrictToWorkspace = value
	case "workspace_allow_full_read":
		cfg.Tools.AllowFullFileRead = value
	case "tools_enable_exec":
		cfg.Tools.EnableExec = value
	case "docindex_enabled":
		cfg.DocIndex.Enabled = value
	case "skills_enable_exec":
		cfg.Skills.EnableExec = value
	case "skills_quarantine":
		cfg.Skills.Policy.QuarantineByDefault = value
	case "skills_global_disabled":
		cfg.Skills.Load.DisableGlobalDir = value
	case "skills_watch":
		cfg.Skills.Load.Watch = value
	case "auth_enabled":
		cfg.Auth.Enabled = value
	case "auth_allow_paired_token_fallback":
		cfg.Auth.AllowPairedTokenFallback = value
	case "auth_require_passkey_for_sensitive":
		cfg.Auth.RequirePasskeyForSensitive = value
	case "security_secret_store_enabled":
		cfg.Security.SecretStore.Enabled = value
	case "security_secret_store_required":
		cfg.Security.SecretStore.Required = value
	case "security_audit_enabled":
		cfg.Security.Audit.Enabled = value
	case "security_audit_strict":
		cfg.Security.Audit.Strict = value
	case "security_audit_verify_on_start":
		cfg.Security.Audit.VerifyOnStart = value
	case "security_approvals_enabled":
		cfg.Security.Approvals.Enabled = value
	case "security_profiles_enabled":
		cfg.Security.Profiles.Enabled = value
	case "security_network_enabled":
		cfg.Security.Network.Enabled = value
	case "security_network_default_deny":
		cfg.Security.Network.DefaultDeny = value
	case "security_network_allow_loopback":
		cfg.Security.Network.AllowLoopback = value
	case "security_network_allow_private":
		cfg.Security.Network.AllowPrivate = value
	case "hardening_guarded_tools":
		cfg.Hardening.GuardedTools = value
	case "hardening_privileged_tools":
		cfg.Hardening.PrivilegedTools = value
	case "hardening_exec_shell":
		cfg.Hardening.EnableExecShell = value
	case "hardening_isolate_channel_peers":
		cfg.Hardening.IsolateChannelPeers = value
	case "hardening_sandbox_enabled":
		cfg.Hardening.Sandbox.Enabled = value
	case "hardening_sandbox_allow_network":
		cfg.Hardening.Sandbox.AllowNetwork = value
	case "hardening_quotas_enabled":
		cfg.Hardening.Quotas.Enabled = value
	case "session_direct_messages_share_default":
		cfg.Session.DirectMessagesShareDefault = value
	case "automation_cron_enabled":
		cfg.Cron.Enabled = value
	case "automation_heartbeat_enabled":
		cfg.Heartbeat.Enabled = value
	case "automation_webhook_enabled":
		cfg.Triggers.Webhook.Enabled = value
	case "automation_filewatch_enabled":
		cfg.Triggers.FileWatch.Enabled = value
	case "service_enabled":
		cfg.Service.Enabled = value
	case "service_allow_unauthenticated_pairing":
		cfg.Service.AllowUnauthenticatedPairing = value
	case "agentCLI_enabled":
		cfg.AgentCLI.Enabled = value
	case "agentCLI_allow_sandbox_auto":
		cfg.AgentCLI.AllowSandboxAuto = value
	default:
		return false
	}
	return true
}

func ApplyChoiceSelection(cfg *config.Config, section, channel, fieldKey, choice string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	if section == "channels" && fieldKey == "access" {
		applyAccessChoice(cfg, channel, choice)
		return true, nil
	}
	if section == "mcp" && fieldKey == "mcp_transport" {
		server, ok := cfg.Tools.MCPServers[channel]
		if !ok {
			return false, nil
		}
		server.Transport = choice
		cfg.Tools.MCPServers[channel] = server
		return true, nil
	}
	switch fieldKey {
	case "provider_preset":
		switch choice {
		case "OpenAI":
			ApplyProviderPreset(cfg, "1")
		case "OpenRouter":
			ApplyProviderPreset(cfg, "2")
		default:
			ApplyProviderPreset(cfg, "3")
		}
		return true, nil
	case "runtime_profile":
		if choice == "default" {
			cfg.RuntimeProfile = ""
		} else {
			cfg.RuntimeProfile = config.RuntimeProfile(choice)
		}
		return true, nil
	case "context_mode":
		cfg.Context.Mode = choice
		return true, nil
	case "auth_fallback_policy":
		cfg.Auth.FallbackPolicy = choice
		return true, nil
	case "auth_enforcement_mode":
		cfg.Auth.EnforcementMode = config.AuthEnforcementMode(choice)
		return true, nil
	case "service_max_capability":
		normalized := normalizeConfigureCapability(choice)
		if normalized == "" {
			return false, fmt.Errorf("service_max_capability must be safe, guarded, or privileged")
		}
		cfg.Service.MaxCapability = normalized
		return true, nil
	case "agentCLI_default_mode":
		switch choice {
		case "review", "safe_edit", "sandbox_auto":
			cfg.AgentCLI.DefaultMode = choice
			return true, nil
		default:
			return false, fmt.Errorf("agentCLI.defaultMode must be review, safe_edit, or sandbox_auto")
		}
	case "agentCLI_default_isolation":
		switch choice {
		case "host_readonly", "host_workspace_write", "sandbox_workspace_write", "sandbox_dangerous":
			cfg.AgentCLI.DefaultIsolation = choice
			return true, nil
		default:
			return false, fmt.Errorf("agentCLI.defaultIsolation must be host_readonly, host_workspace_write, sandbox_workspace_write, or sandbox_dangerous")
		}
	case "security_approval_pairing_mode":
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_exec_mode":
		cfg.Security.Approvals.Exec.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_skill_mode":
		cfg.Security.Approvals.SkillExecution.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_secret_mode":
		cfg.Security.Approvals.SecretAccess.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_message_mode":
		cfg.Security.Approvals.MessageSend.Mode = config.ApprovalMode(choice)
		return true, nil
	default:
		return false, nil
	}
}

func ApplyFieldValue(cfg *config.Config, section, channel, fieldKey, value string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	clearRequested := strings.EqualFold(strings.TrimSpace(value), secretClearKeyword)
	if section == "channels" {
		switch channel {
		case "telegram":
			switch fieldKey {
			case "token":
				if clearRequested {
					cfg.Channels.Telegram.Token = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Telegram.Token = value
				}
				return true, nil
			case "default_id":
				cfg.Channels.Telegram.DefaultChatID = value
				return true, nil
			case "allowlist":
				cfg.Channels.Telegram.AllowedChatIDs = splitAndCompact(value)
				return true, nil
			}
		case "slack":
			switch fieldKey {
			case "app_token":
				if clearRequested {
					cfg.Channels.Slack.AppToken = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Slack.AppToken = value
				}
				return true, nil
			case "bot_token":
				if clearRequested {
					cfg.Channels.Slack.BotToken = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Slack.BotToken = value
				}
				return true, nil
			case "default_id":
				cfg.Channels.Slack.DefaultChannelID = value
				return true, nil
			case "allowlist":
				cfg.Channels.Slack.AllowedUserIDs = splitAndCompact(value)
				return true, nil
			}
		case "discord":
			switch fieldKey {
			case "token":
				if clearRequested {
					cfg.Channels.Discord.Token = ""
					config.NormalizeManagedChannelInboundDefaults(cfg)
					return true, nil
				}
				if value != "" {
					cfg.Channels.Discord.Token = value
				}
				config.NormalizeManagedChannelInboundDefaults(cfg)
				return true, nil
			case "default_id":
				cfg.Channels.Discord.DefaultChannelID = value
				config.NormalizeManagedChannelInboundDefaults(cfg)
				return true, nil
			case "allowlist":
				cfg.Channels.Discord.AllowedUserIDs = splitAndCompact(value)
				config.NormalizeManagedChannelInboundDefaults(cfg)
				return true, nil
			}
		case "whatsapp":
			switch fieldKey {
			case "bridge_url":
				cfg.Channels.WhatsApp.BridgeURL = value
				return true, nil
			case "bridge_token":
				if clearRequested {
					cfg.Channels.WhatsApp.BridgeToken = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.WhatsApp.BridgeToken = value
				}
				return true, nil
			case "default_to":
				cfg.Channels.WhatsApp.DefaultTo = value
				return true, nil
			case "allowlist":
				cfg.Channels.WhatsApp.AllowedFrom = splitAndCompact(value)
				return true, nil
			}
		case "email":
			switch fieldKey {
			case "imap_host":
				cfg.Channels.Email.IMAPHost = value
				return true, nil
			case "imap_user":
				cfg.Channels.Email.IMAPUsername = value
				return true, nil
			case "imap_password":
				if clearRequested {
					cfg.Channels.Email.IMAPPassword = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Email.IMAPPassword = value
				}
				return true, nil
			case "smtp_host":
				cfg.Channels.Email.SMTPHost = value
				return true, nil
			case "smtp_user":
				cfg.Channels.Email.SMTPUsername = value
				return true, nil
			case "smtp_password":
				if clearRequested {
					cfg.Channels.Email.SMTPPassword = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Email.SMTPPassword = value
				}
				return true, nil
			case "from_address":
				cfg.Channels.Email.FromAddress = value
				return true, nil
			case "default_to":
				cfg.Channels.Email.DefaultTo = value
				return true, nil
			case "allowlist":
				cfg.Channels.Email.AllowedSenders = splitAndCompact(value)
				return true, nil
			}
		}
		return false, nil
	}
	if section == "mcp" {
		if cfg.Tools.MCPServers == nil {
			cfg.Tools.MCPServers = map[string]config.MCPServerConfig{}
		}
		server, ok := cfg.Tools.MCPServers[channel]
		if !ok {
			return false, nil
		}
		switch fieldKey {
		case "mcp_command":
			server.Command = value
		case "mcp_args":
			server.Args = splitAndCompact(value)
		case "mcp_child_env_allowlist":
			server.ChildEnvAllowlist = splitAndCompact(value)
		case "mcp_url":
			server.URL = value
		case "mcp_headers":
			headers, err := parseStringMap(value)
			if err != nil {
				return false, err
			}
			server.Headers = headers
		case "mcp_env":
			env, err := parseStringMap(value)
			if err != nil {
				return false, err
			}
			server.Env = env
		case "mcp_connect_timeout":
			changed, err := setIntValue(&server.ConnectTimeoutSeconds, value, fieldKey)
			cfg.Tools.MCPServers[channel] = server
			return changed, err
		case "mcp_tool_timeout":
			changed, err := setIntValue(&server.ToolTimeoutSeconds, value, fieldKey)
			cfg.Tools.MCPServers[channel] = server
			return changed, err
		default:
			return false, nil
		}
		cfg.Tools.MCPServers[channel] = server
		return true, nil
	}
	if section == "skills_entry" {
		if strings.TrimSpace(channel) == "" {
			return false, nil
		}
		if cfg.Skills.Entries == nil {
			cfg.Skills.Entries = map[string]config.SkillEntryConfig{}
		}
		entry := cfg.Skills.Entries[channel]
		switch {
		case fieldKey == "api_key":
			if clearRequested {
				entry.APIKey = ""
			} else {
				entry.APIKey = value
			}
		case strings.HasPrefix(fieldKey, "env."):
			key := strings.TrimSpace(strings.TrimPrefix(fieldKey, "env."))
			if key == "" {
				return false, nil
			}
			if entry.Env == nil {
				entry.Env = map[string]string{}
			}
			if clearRequested {
				delete(entry.Env, key)
			} else {
				entry.Env[key] = value
			}
		case strings.HasPrefix(fieldKey, "config."):
			key := strings.TrimSpace(strings.TrimPrefix(fieldKey, "config."))
			if key == "" {
				return false, nil
			}
			if entry.Config == nil {
				entry.Config = map[string]any{}
			}
			if clearRequested {
				delete(entry.Config, key)
			} else {
				entry.Config[key] = parseSkillEntryConfigValue(value)
			}
		default:
			return false, nil
		}
		cfg.Skills.Entries[channel] = entry
		return true, nil
	}
	switch fieldKey {
	case "provider_api_base":
		cfg.Provider.APIBase = value
		cfg.ModelRouting.Chat.Primary.Provider = ConfigureProviderKeyFromBase(value)
		SetProviderProfileAPIBase(cfg, cfg.ModelRouting.Chat.Primary.Provider, value)
		return true, nil
	case "provider_model":
		cfg.Provider.Model = value
		cfg.ModelRouting.Chat.Primary.Model = value
		cfg.ModelRouting.Agents.Primary.Model = value
		cfg.ModelRouting.Subagents.Primary.Model = value
		return true, nil
	case "provider_embed":
		cfg.Provider.EmbedModel = value
		cfg.ModelRouting.Embeddings.Primary.Model = value
		return true, nil
	case "provider_embed_dimensions":
		changed, err := setIntValue(&cfg.Provider.EmbedDimensions, value, fieldKey)
		cfg.ModelRouting.Embeddings.EmbedDimensions = cfg.Provider.EmbedDimensions
		return changed, err
	case "provider_temperature":
		return setFloatValue(&cfg.Provider.Temperature, value, fieldKey)
	case "provider_timeout":
		return setIntValue(&cfg.Provider.TimeoutSeconds, value, fieldKey)
	case "provider_api_key":
		if clearRequested {
			cfg.Provider.APIKey = ""
			SetProviderProfileAPIKey(cfg, "openai", "")
			return true, nil
		}
		if value != "" {
			cfg.Provider.APIKey = value
			SetProviderProfileAPIKey(cfg, ConfigureProviderKeyFromBase(cfg.Provider.APIBase), value)
		}
		return true, nil
	case "provider_openai_api_key":
		if clearRequested {
			SetProviderProfileAPIKey(cfg, "openai", "")
			return true, nil
		}
		if value != "" {
			SetProviderProfileAPIKey(cfg, "openai", value)
		}
		return true, nil
	case "provider_openrouter_api_key":
		if clearRequested {
			SetProviderProfileAPIKey(cfg, "openrouter", "")
			return true, nil
		}
		if value != "" {
			SetProviderProfileAPIKey(cfg, "openrouter", value)
		}
		return true, nil
	case "provider_custom_api_base":
		SetProviderProfileAPIBase(cfg, "custom", value)
		return true, nil
	case "provider_custom_api_key":
		if clearRequested {
			SetProviderProfileAPIKey(cfg, "custom", "")
			return true, nil
		}
		if value != "" {
			SetProviderProfileAPIKey(cfg, "custom", value)
		}
		return true, nil
	case "routing_chat_provider":
		cfg.ModelRouting.Chat.Primary.Provider = value
		return true, nil
	case "routing_chat_model":
		cfg.ModelRouting.Chat.Primary.Model = value
		return true, nil
	case "routing_chat_fallbacks":
		cfg.ModelRouting.Chat.Fallbacks = parseModelRefs(value)
		return true, nil
	case "routing_agents_provider":
		cfg.ModelRouting.Agents.Primary.Provider = value
		return true, nil
	case "routing_agents_model":
		cfg.ModelRouting.Agents.Primary.Model = value
		return true, nil
	case "routing_agents_fallbacks":
		cfg.ModelRouting.Agents.Fallbacks = parseModelRefs(value)
		return true, nil
	case "routing_subagents_provider":
		cfg.ModelRouting.Subagents.Primary.Provider = value
		return true, nil
	case "routing_subagents_model":
		cfg.ModelRouting.Subagents.Primary.Model = value
		return true, nil
	case "routing_subagents_fallbacks":
		cfg.ModelRouting.Subagents.Fallbacks = parseModelRefs(value)
		return true, nil
	case "routing_summarization_provider":
		cfg.ModelRouting.Summarization.Primary.Provider = value
		return true, nil
	case "routing_summarization_model":
		cfg.ModelRouting.Summarization.Primary.Model = value
		cfg.ConsolidationModel = value
		return true, nil
	case "routing_summarization_fallbacks":
		cfg.ModelRouting.Summarization.Fallbacks = parseModelRefs(value)
		return true, nil
	case "routing_context_provider":
		cfg.ModelRouting.ContextManager.Primary.Provider = value
		return true, nil
	case "routing_context_model":
		cfg.ModelRouting.ContextManager.Primary.Model = value
		cfg.ContextManager.Model = value
		return true, nil
	case "routing_context_fallbacks":
		cfg.ModelRouting.ContextManager.Fallbacks = parseModelRefs(value)
		return true, nil
	case "routing_embeddings_provider":
		cfg.ModelRouting.Embeddings.Primary.Provider = value
		return true, nil
	case "routing_embeddings_model":
		cfg.ModelRouting.Embeddings.Primary.Model = value
		cfg.Provider.EmbedModel = value
		return true, nil
	case "routing_embeddings_fallbacks":
		cfg.ModelRouting.Embeddings.Fallbacks = parseModelRefs(value)
		return true, nil
	case "routing_embeddings_dimensions":
		changed, err := setIntValue(&cfg.ModelRouting.Embeddings.EmbedDimensions, value, fieldKey)
		cfg.Provider.EmbedDimensions = cfg.ModelRouting.Embeddings.EmbedDimensions
		return changed, err
	case "favorites_openai":
		if cfg.FavoriteModels == nil {
			cfg.FavoriteModels = config.FavoriteModelsConfig{}
		}
		cfg.FavoriteModels["openai"] = parseFavoriteModels(value)
		return true, nil
	case "favorites_openrouter":
		if cfg.FavoriteModels == nil {
			cfg.FavoriteModels = config.FavoriteModelsConfig{}
		}
		cfg.FavoriteModels["openrouter"] = parseFavoriteModels(value)
		return true, nil
	case "storage_db":
		cfg.DBPath = value
		return true, nil
	case "storage_artifacts":
		cfg.ArtifactsDir = value
		return true, nil
	case "storage_soul":
		cfg.SoulFile = value
		return true, nil
	case "storage_agents":
		cfg.AgentsFile = value
		return true, nil
	case "storage_tools":
		cfg.ToolsFile = value
		return true, nil
	case "storage_identity":
		cfg.IdentityFile = value
		return true, nil
	case "storage_memory":
		cfg.MemoryFile = value
		return true, nil
	case "runtime_default_session":
		cfg.DefaultSessionKey = value
		return true, nil
	case "runtime_bootstrap_max_chars":
		return setIntValue(&cfg.BootstrapMaxChars, value, fieldKey)
	case "runtime_bootstrap_total_chars":
		return setIntValue(&cfg.BootstrapTotalMaxChars, value, fieldKey)
	case "runtime_session_cache":
		return setIntValue(&cfg.SessionCache, value, fieldKey)
	case "runtime_history_max":
		return setIntValue(&cfg.HistoryMax, value, fieldKey)
	case "runtime_max_tool_bytes":
		return setIntValue(&cfg.MaxToolBytes, value, fieldKey)
	case "runtime_max_media_bytes":
		return setIntValue(&cfg.MaxMediaBytes, value, fieldKey)
	case "runtime_max_tool_loops":
		return setIntValue(&cfg.MaxToolLoops, value, fieldKey)
	case "runtime_max_tool_loops_exceeded_action":
		action := config.QuotaExceededAction(strings.ToLower(strings.TrimSpace(value)))
		if action != config.QuotaExceededActionAsk && action != config.QuotaExceededActionFail {
			return false, fmt.Errorf("%s must be ask or fail", fieldKey)
		}
		cfg.MaxToolLoopsExceededAction = action
		return true, nil
	case "runtime_memory_retrieve":
		return setIntValue(&cfg.MemoryRetrieve, value, fieldKey)
	case "runtime_vector_k":
		return setIntValue(&cfg.VectorK, value, fieldKey)
	case "runtime_fts_k":
		return setIntValue(&cfg.FTSK, value, fieldKey)
	case "runtime_vector_scan_limit":
		return setIntValue(&cfg.VectorScanLimit, value, fieldKey)
	case "runtime_worker_count":
		return setIntValue(&cfg.WorkerCount, value, fieldKey)
	case "runtime_consolidation_model":
		cfg.ConsolidationModel = value
		return true, nil
	case "runtime_consolidation_window":
		return setIntValue(&cfg.ConsolidationWindowSize, value, fieldKey)
	case "runtime_consolidation_max_messages":
		return setIntValue(&cfg.ConsolidationMaxMessages, value, fieldKey)
	case "runtime_consolidation_max_input_chars":
		return setIntValue(&cfg.ConsolidationMaxInputChars, value, fieldKey)
	case "runtime_consolidation_async_timeout":
		return setIntValue(&cfg.ConsolidationAsyncTimeoutSeconds, value, fieldKey)
	case "runtime_subagents_max_concurrent":
		return setIntValue(&cfg.Subagents.MaxConcurrent, value, fieldKey)
	case "runtime_subagents_max_queued":
		return setIntValue(&cfg.Subagents.MaxQueued, value, fieldKey)
	case "runtime_subagents_timeout":
		return setIntValue(&cfg.Subagents.TaskTimeoutSeconds, value, fieldKey)
	case "context_max_input_tokens":
		return setIntValue(&cfg.Context.MaxInputTokens, value, fieldKey)
	case "context_output_reserve":
		return setIntValue(&cfg.Context.OutputReserveTokens, value, fieldKey)
	case "context_safety_margin":
		return setIntValue(&cfg.Context.SafetyMarginTokens, value, fieldKey)
	case "context_retrieval_multiplier":
		return setIntValue(&cfg.Context.Retrieval.CandidateMultiplier, value, fieldKey)
	case "context_retrieval_min_score":
		return setFloatValue(&cfg.Context.Retrieval.MinScore, value, fieldKey)
	case "context_pressure_warning":
		return setIntValue(&cfg.Context.Pressure.WarningPercent, value, fieldKey)
	case "context_pressure_high":
		return setIntValue(&cfg.Context.Pressure.HighPercent, value, fieldKey)
	case "context_pressure_emergency":
		return setIntValue(&cfg.Context.Pressure.EmergencyPercent, value, fieldKey)
	case "context_section_system_core":
		return setIntValue(&cfg.Context.Sections.SystemCore, value, fieldKey)
	case "context_section_soul_identity":
		return setIntValue(&cfg.Context.Sections.SoulIdentity, value, fieldKey)
	case "context_section_tool_policy":
		return setIntValue(&cfg.Context.Sections.ToolPolicy, value, fieldKey)
	case "context_section_active_task_card":
		return setIntValue(&cfg.Context.Sections.ActiveTaskCard, value, fieldKey)
	case "context_section_pinned_memory":
		return setIntValue(&cfg.Context.Sections.PinnedMemory, value, fieldKey)
	case "context_section_recent_history":
		return setIntValue(&cfg.Context.Sections.RecentHistory, value, fieldKey)
	case "context_section_retrieved_memory":
		return setIntValue(&cfg.Context.Sections.RetrievedMemory, value, fieldKey)
	case "context_section_memory_digest":
		return setIntValue(&cfg.Context.Sections.MemoryDigest, value, fieldKey)
	case "context_section_workspace":
		return setIntValue(&cfg.Context.Sections.WorkspaceContext, value, fieldKey)
	case "context_section_tool_schemas":
		return setIntValue(&cfg.Context.Sections.ToolSchemas, value, fieldKey)
	case "context_task_card_max_refs":
		return setIntValue(&cfg.Context.TaskCard.MaxRefs, value, fieldKey)
	case "context_task_card_max_plan":
		return setIntValue(&cfg.Context.TaskCard.MaxPlanItems, value, fieldKey)
	case "context_artifact_summary_chars":
		return setIntValue(&cfg.Context.Artifacts.SummaryMaxChars, value, fieldKey)
	case "context_manager_provider":
		cfg.ContextManager.Provider = value
		return true, nil
	case "context_manager_model":
		cfg.ContextManager.Model = value
		return true, nil
	case "context_manager_timeout":
		return setIntValue(&cfg.ContextManager.TimeoutSeconds, value, fieldKey)
	case "context_manager_idle_prune":
		return setIntValue(&cfg.ContextManager.IdlePruneSeconds, value, fieldKey)
	case "context_manager_max_input":
		return setIntValue(&cfg.ContextManager.MaxInputTokens, value, fieldKey)
	case "context_manager_max_output":
		return setIntValue(&cfg.ContextManager.MaxOutputTokens, value, fieldKey)
	case "workspace_dir":
		cfg.WorkspaceDir = value
		return true, nil
	case "workspace_allowed_dir":
		cfg.AllowedDir = value
		return true, nil
	case "tools_brave":
		if clearRequested {
			cfg.Tools.BraveAPIKey = ""
			return true, nil
		}
		if value != "" {
			cfg.Tools.BraveAPIKey = value
		}
		return true, nil
	case "tools_web_proxy":
		cfg.Tools.WebProxy = value
		return true, nil
	case "tools_enable_exec":
		enabled, err := parseBoolValue(value, fieldKey)
		if err != nil {
			return false, err
		}
		cfg.Tools.EnableExec = enabled
		return true, nil
	case "tools_exec_timeout":
		return setIntValue(&cfg.Tools.ExecTimeoutSeconds, value, fieldKey)
	case "tools_path_append":
		cfg.Tools.PathAppend = value
		return true, nil
	case "docindex_roots":
		cfg.DocIndex.Roots = splitAndCompact(value)
		return true, nil
	case "docindex_max_files":
		return setIntValue(&cfg.DocIndex.MaxFiles, value, fieldKey)
	case "docindex_max_file_bytes":
		return setIntValue(&cfg.DocIndex.MaxFileBytes, value, fieldKey)
	case "docindex_max_chunks":
		return setIntValue(&cfg.DocIndex.MaxChunks, value, fieldKey)
	case "docindex_embed_max_bytes":
		return setIntValue(&cfg.DocIndex.EmbedMaxBytes, value, fieldKey)
	case "docindex_refresh_seconds":
		return setIntValue(&cfg.DocIndex.RefreshSeconds, value, fieldKey)
	case "docindex_retrieve_limit":
		return setIntValue(&cfg.DocIndex.RetrieveLimit, value, fieldKey)
	case "skills_max_run_seconds":
		return setIntValue(&cfg.Skills.MaxRunSeconds, value, fieldKey)
	case "skills_managed_dir":
		cfg.Skills.ManagedDir = value
		return true, nil
	case "skills_approved":
		cfg.Skills.Policy.Approved = splitAndCompact(value)
		return true, nil
	case "skills_trusted_owners":
		cfg.Skills.Policy.TrustedOwners = splitAndCompact(value)
		return true, nil
	case "skills_blocked_owners":
		cfg.Skills.Policy.BlockedOwners = splitAndCompact(value)
		return true, nil
	case "skills_trusted_registries":
		cfg.Skills.Policy.TrustedRegistries = splitAndCompact(value)
		return true, nil
	case "skills_global_dir":
		cfg.Skills.Load.GlobalDir = value
		return true, nil
	case "skills_extra_dirs":
		cfg.Skills.Load.ExtraDirs = splitAndCompact(value)
		return true, nil
	case "skills_watch_debounce":
		return setIntValue(&cfg.Skills.Load.WatchDebounceMS, value, fieldKey)
	case "skills_clawhub_site":
		cfg.Skills.ClawHub.SiteURL = value
		return true, nil
	case "skills_clawhub_registry":
		cfg.Skills.ClawHub.RegistryURL = value
		return true, nil
	case "skills_clawhub_install":
		cfg.Skills.ClawHub.InstallDir = value
		return true, nil
	case "auth_rp_id":
		cfg.Auth.RPID = value
		return true, nil
	case "auth_rp_display_name":
		cfg.Auth.RPDisplayName = value
		return true, nil
	case "auth_allowed_origins":
		cfg.Auth.AllowedOrigins = splitAndCompact(value)
		return true, nil
	case "auth_related_origins":
		cfg.Auth.RelatedOrigins = splitAndCompact(value)
		return true, nil
	case "auth_session_idle_ttl":
		return setIntValue(&cfg.Auth.SessionIdleTTLSeconds, value, fieldKey)
	case "auth_session_absolute_ttl":
		return setIntValue(&cfg.Auth.SessionAbsoluteTTLSeconds, value, fieldKey)
	case "auth_step_up_ttl":
		return setIntValue(&cfg.Auth.StepUpTTLSeconds, value, fieldKey)
	case "security_secret_store_key_file":
		cfg.Security.SecretStore.KeyFile = value
		return true, nil
	case "security_audit_key_file":
		cfg.Security.Audit.KeyFile = value
		return true, nil
	case "security_approvals_host_id":
		cfg.Security.Approvals.HostID = value
		return true, nil
	case "security_approvals_key_file":
		cfg.Security.Approvals.KeyFile = value
		return true, nil
	case "security_approvals_pairing_ttl":
		return setIntValue(&cfg.Security.Approvals.PairingCodeTTLSeconds, value, fieldKey)
	case "security_approvals_pending_ttl":
		return setIntValue(&cfg.Security.Approvals.PendingTTLSeconds, value, fieldKey)
	case "security_approvals_token_ttl":
		return setIntValue(&cfg.Security.Approvals.ApprovalTokenTTLSeconds, value, fieldKey)
	case "security_profiles_default":
		cfg.Security.Profiles.Default = value
		return true, nil
	case "security_profiles_channels":
		mapping, err := parseStringMap(value)
		if err != nil {
			return false, err
		}
		cfg.Security.Profiles.Channels = mapping
		return true, nil
	case "security_profiles_triggers":
		mapping, err := parseStringMap(value)
		if err != nil {
			return false, err
		}
		cfg.Security.Profiles.Triggers = mapping
		return true, nil
	case "security_network_allowed_hosts":
		cfg.Security.Network.AllowedHosts = splitAndCompact(value)
		return true, nil
	case "hardening_exec_allowed_programs":
		cfg.Hardening.ExecAllowedPrograms = splitAndCompact(value)
		return true, nil
	case "hardening_child_env_allowlist":
		cfg.Hardening.ChildEnvAllowlist = splitAndCompact(value)
		return true, nil
	case "hardening_sandbox_bwrap":
		cfg.Hardening.Sandbox.BubblewrapPath = value
		return true, nil
	case "hardening_sandbox_writable_paths":
		cfg.Hardening.Sandbox.WritablePaths = splitAndCompact(value)
		return true, nil
	case "hardening_quota_exceeded_action":
		action := config.QuotaExceededAction(strings.ToLower(strings.TrimSpace(value)))
		if action != config.QuotaExceededActionAsk && action != config.QuotaExceededActionFail {
			return false, fmt.Errorf("%s must be ask or fail", fieldKey)
		}
		cfg.Hardening.Quotas.ExceededAction = action
		return true, nil
	case "hardening_max_tool_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxToolCalls, value, fieldKey)
	case "hardening_max_exec_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxExecCalls, value, fieldKey)
	case "hardening_max_web_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxWebCalls, value, fieldKey)
	case "hardening_max_subagent_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxSubagentCalls, value, fieldKey)
	case "hardening_max_session_tool_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxSessionToolCalls, value, fieldKey)
	case "hardening_max_session_exec_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxSessionExecCalls, value, fieldKey)
	case "hardening_max_session_web_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxSessionWebCalls, value, fieldKey)
	case "hardening_max_session_subagent_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxSessionSubagentCalls, value, fieldKey)
	case "session_identity_links":
		links, err := parseIdentityLinks(value)
		if err != nil {
			return false, err
		}
		cfg.Session.IdentityLinks = links
		return true, nil
	case "automation_cron_store_path":
		cfg.Cron.StorePath = value
		return true, nil
	case "automation_heartbeat_interval":
		return setIntValue(&cfg.Heartbeat.IntervalMinutes, value, fieldKey)
	case "automation_heartbeat_tasks_file":
		cfg.Heartbeat.TasksFile = value
		return true, nil
	case "automation_heartbeat_session":
		cfg.Heartbeat.SessionKey = value
		return true, nil
	case "automation_webhook_addr":
		cfg.Triggers.Webhook.Addr = value
		return true, nil
	case "automation_webhook_secret":
		if clearRequested {
			cfg.Triggers.Webhook.Secret = ""
			return true, nil
		}
		if value != "" {
			cfg.Triggers.Webhook.Secret = value
		}
		return true, nil
	case "automation_webhook_max_body_kb":
		return setIntValue(&cfg.Triggers.Webhook.MaxBodyKB, value, fieldKey)
	case "automation_filewatch_paths":
		cfg.Triggers.FileWatch.Paths = splitAndCompact(value)
		return true, nil
	case "automation_filewatch_poll_seconds":
		return setIntValue(&cfg.Triggers.FileWatch.PollSeconds, value, fieldKey)
	case "automation_filewatch_debounce":
		return setIntValue(&cfg.Triggers.FileWatch.DebounceSeconds, value, fieldKey)
	case "service_listen":
		cfg.Service.Listen = value
		return true, nil
	case "service_secret":
		if clearRequested {
			cfg.Service.Secret = ""
			return true, nil
		}
		if value != "" {
			cfg.Service.Secret = value
		}
		return true, nil
	case "service_max_capability":
		normalized := normalizeConfigureCapability(value)
		if normalized == "" {
			return false, fmt.Errorf("%s must be safe, guarded, or privileged", fieldKey)
		}
		cfg.Service.MaxCapability = normalized
		return true, nil
	case "service_trusted_browser_origins":
		cfg.Service.TrustedBrowserOrigins = splitAndCompact(value)
		return true, nil
	case "service_trusted_browser_cidrs":
		cfg.Service.TrustedBrowserCIDRs = splitAndCompact(value)
		return true, nil
	case "agentCLI_max_concurrent":
		return setIntValue(&cfg.AgentCLI.MaxConcurrent, value, fieldKey)
	case "agentCLI_max_queued":
		return setIntValue(&cfg.AgentCLI.MaxQueued, value, fieldKey)
	case "agentCLI_default_timeout":
		return setIntValue(&cfg.AgentCLI.DefaultTimeoutSeconds, value, fieldKey)
	case "agentCLI_max_timeout":
		return setIntValue(&cfg.AgentCLI.MaxTimeoutSeconds, value, fieldKey)
	case "agentCLI_disabled_runners":
		cfg.AgentCLI.DisabledRunners = splitAndCompact(value)
		return true, nil
	case "agentCLI_enabled":
		cfg.AgentCLI.Enabled = value == "true" || value == "on" || value == "1"
		return true, nil
	case "agentCLI_allow_sandbox_auto":
		cfg.AgentCLI.AllowSandboxAuto = value == "true" || value == "on" || value == "1"
		return true, nil
	default:
		return false, nil
	}
}

func parseSkillEntryConfigValue(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, "true") {
		return true
	}
	if strings.EqualFold(trimmed, "false") {
		return false
	}
	if n, err := strconv.Atoi(trimmed); err == nil {
		return n
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`) {
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
	}
	return value
}

func setIntValue(target *int, value string, field string) (bool, error) {
	if target == nil {
		return false, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("invalid integer for %s: %q", field, value)
	}
	*target = parsed
	return true, nil
}

func setFloatValue(target *float64, value string, field string) (bool, error) {
	if target == nil {
		return false, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return false, fmt.Errorf("invalid number for %s: %q", field, value)
	}
	*target = parsed
	return true, nil
}

func parseBoolValue(value string, field string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on", "enabled":
		return true, nil
	case "0", "false", "f", "no", "n", "off", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean for %s: %q", field, value)
	}
}

func normalizeConfigureCapability(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "safe":
		return "safe"
	case "guarded":
		return "guarded"
	case "privileged":
		return "privileged"
	default:
		return ""
	}
}

func parseStringMap(value string) (map[string]string, error) {
	items := splitAndCompact(value)
	result := map[string]string{}
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid mapping %q (expected key=value)", item)
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func parseIdentityLinks(value string) ([]config.SessionIdentityLink, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []config.SessionIdentityLink{}, nil
	}
	entries := strings.Split(value, ";")
	links := make([]config.SessionIdentityLink, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid identity link %q (expected canonical=peer1|peer2)", entry)
		}
		canonical := strings.TrimSpace(parts[0])
		if canonical == "" {
			return nil, fmt.Errorf("invalid identity link %q (missing canonical identity)", entry)
		}
		peers := compactStrings(strings.Split(parts[1], "|"))
		if len(peers) == 0 {
			return nil, fmt.Errorf("invalid identity link %q (missing peers)", entry)
		}
		links = append(links, config.SessionIdentityLink{Canonical: canonical, Peers: peers})
	}
	return links, nil
}

func applyAccessChoice(cfg *config.Config, channel, choice string) {
	switch channel {
	case "telegram":
		setInboundChoice(choice, &cfg.Channels.Telegram.InboundPolicy, &cfg.Channels.Telegram.OpenAccess)
	case "slack":
		setInboundChoice(choice, &cfg.Channels.Slack.InboundPolicy, &cfg.Channels.Slack.OpenAccess)
	case "discord":
		setInboundChoice(choice, &cfg.Channels.Discord.InboundPolicy, &cfg.Channels.Discord.OpenAccess)
	case "whatsapp":
		setInboundChoice(choice, &cfg.Channels.WhatsApp.InboundPolicy, &cfg.Channels.WhatsApp.OpenAccess)
	case "email":
		setInboundChoice(choice, &cfg.Channels.Email.InboundPolicy, &cfg.Channels.Email.OpenAccess)
	}
}

func setInboundChoice(choice string, policy *config.InboundPolicy, openAccess *bool) {
	if policy == nil || openAccess == nil {
		return
	}
	switch strings.TrimSpace(choice) {
	case "pairing":
		*policy = config.InboundPolicyPairing
		*openAccess = false
	case "allowlist":
		*policy = config.InboundPolicyAllowlist
		*openAccess = false
	case "open":
		*policy = ""
		*openAccess = true
	case "deny":
		*policy = config.InboundPolicyDeny
		*openAccess = false
	}
}

func parseModelRefs(value string) []config.ModelRef {
	raw := splitAndCompact(value)
	items := make([]config.ModelRef, 0, len(raw))
	for _, item := range raw {
		parts := strings.SplitN(item, "/", 2)
		if len(parts) != 2 {
			continue
		}
		provider := strings.TrimSpace(parts[0])
		model := strings.TrimSpace(parts[1])
		if provider == "" || model == "" {
			continue
		}
		items = append(items, config.ModelRef{Provider: provider, Model: model})
	}
	return items
}

func parseFavoriteModels(value string) []config.FavoriteModelConfig {
	models := splitAndCompact(value)
	items := make([]config.FavoriteModelConfig, 0, len(models))
	for _, model := range models {
		items = append(items, config.FavoriteModelConfig{Model: model})
	}
	return items
}

func SetProviderProfileAPIKey(cfg *config.Config, key, value string) {
	if cfg == nil {
		return
	}
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return
	}
	if cfg.Providers == nil {
		cfg.Providers = config.ProviderProfiles{}
	}
	profile := cfg.Providers[key]
	profile.APIKey = strings.TrimSpace(value)
	cfg.Providers[key] = profile
}

func SetProviderProfileAPIBase(cfg *config.Config, key, value string) {
	if cfg == nil {
		return
	}
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return
	}
	if cfg.Providers == nil {
		cfg.Providers = config.ProviderProfiles{}
	}
	profile := cfg.Providers[key]
	profile.APIBase = strings.TrimSpace(value)
	if profile.TimeoutSeconds <= 0 {
		profile.TimeoutSeconds = 60
	}
	cfg.Providers[key] = profile
}

func ConfigureProviderKeyFromBase(apiBase string) string {
	normalized := strings.ToLower(strings.TrimSpace(apiBase))
	switch {
	case strings.Contains(normalized, "openrouter.ai"):
		return "openrouter"
	case normalized != "" && !strings.Contains(normalized, "api.openai.com"):
		return "custom"
	default:
		return "openai"
	}
}

func splitAndCompact(value string) []string {
	raw := strings.Split(value, ",")
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func compactStrings(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}
