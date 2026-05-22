package configmeta

import "sync"

var firstSliceOnce sync.Once

func EnsureFirstSliceFieldsRegistered() {
	firstSliceOnce.Do(RegisterFirstSliceFields)
}

// RegisterFirstSliceFields registers metadata for the first vertical slice
// of config fields, covering generic installable skill settings, provider keys,
// runner/admin-brain availability, tool exec policy, allowed programs, service
// restart, and credential/config paths.
func RegisterFirstSliceFields() {
	// Provider and model routing fields
	Register(ConfigFieldMetadata{
		Section:          "provider",
		Key:              "api_base",
		Path:             "provider.apiBase",
		Label:            "API Base URL",
		Description:      "The base URL for the LLM provider API endpoint",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Validation: []ValidationRule{
			{Kind: "required"},
			{Kind: "url"},
		},
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"change provider", "update api endpoint"},
	})

	Register(ConfigFieldMetadata{
		Section:          "provider",
		Key:              "api_key",
		Path:             "provider.apiKey",
		Label:            "API Key",
		Description:      "Authentication key for the LLM provider",
		Secret:           true,
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Validation: []ValidationRule{
			{Kind: "required"},
		},
		Rollback: RollbackBehavior{
			Safe:         true,
			Instructions: "Restore previous API key from backup or reconfigure",
		},
		UserIntents: []string{"update api key", "change credentials"},
	})

	Register(ConfigFieldMetadata{
		Section:          "provider",
		Key:              "openai_api_key",
		Path:             "providers.profiles.openai.apiKey",
		Label:            "OpenAI API Key",
		Description:      "Credential override for the OpenAI provider profile",
		Secret:           true,
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback:         RollbackBehavior{Safe: true, Instructions: "Restore the previous provider key if downstream auth fails."},
		UserIntents:      []string{"set openai key", "repair provider credentials"},
	})

	Register(ConfigFieldMetadata{
		Section:          "provider",
		Key:              "openrouter_api_key",
		Path:             "providers.profiles.openrouter.apiKey",
		Label:            "OpenRouter API Key",
		Description:      "Credential override for the OpenRouter provider profile",
		Secret:           true,
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback:         RollbackBehavior{Safe: true, Instructions: "Restore the previous provider key if downstream auth fails."},
		UserIntents:      []string{"set openrouter key", "repair provider credentials"},
	})

	Register(ConfigFieldMetadata{
		Section:          "provider",
		Key:              "custom_api_key",
		Path:             "providers.profiles.custom.apiKey",
		Label:            "Custom Provider API Key",
		Description:      "Credential override for a custom provider profile",
		Secret:           true,
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback:         RollbackBehavior{Safe: true, Instructions: "Restore the previous provider key if downstream auth fails."},
		UserIntents:      []string{"set custom provider key", "repair provider credentials"},
	})

	Register(ConfigFieldMetadata{
		Section:          "provider",
		Key:              "model",
		Path:             "provider.model",
		Label:            "Default Model",
		Description:      "The default model to use for chat and agents",
		Risk:             RiskSafe,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"change model", "switch to different ai model"},
	})

	// Tools and execution policy
	Register(ConfigFieldMetadata{
		Section:          "tools",
		Key:              "enable_exec",
		Path:             "tools.enableExec",
		Label:            "Enable Shell Execution",
		Description:      "Allow tools to execute shell commands",
		Risk:             RiskDanger,
		RestartRequired:  true,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
		},
		UserIntents: []string{"enable command execution", "allow shell commands"},
	})

	Register(ConfigFieldMetadata{
		Section:          "tools",
		Key:              "exec_allowed_programs",
		Path:             "tools.execAllowedPrograms",
		Label:            "Allowed Programs",
		Description:      "List of programs that can be executed by tools",
		Risk:             RiskWarning,
		RestartRequired:  true,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
		},
		UserIntents: []string{"add allowed program", "whitelist executable"},
	})

	Register(ConfigFieldMetadata{
		Section:          "tools",
		Key:              "restrict_to_workspace",
		Path:             "tools.restrictToWorkspace",
		Label:            "Restrict to Workspace",
		Description:      "Restrict file operations to workspace directory only",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"restrict file access", "limit to workspace"},
	})

	// Hardening and security
	Register(ConfigFieldMetadata{
		Section:          "hardening",
		Key:              "guarded_tools",
		Path:             "hardening.guardedTools",
		Label:            "Guarded Tools",
		Description:      "Require approval for sensitive tool operations",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"enable tool guards", "require approval for tools"},
	})

	Register(ConfigFieldMetadata{
		Section:          "hardening",
		Key:              "privileged_tools",
		Path:             "hardening.privilegedTools",
		Label:            "Privileged Tools",
		Description:      "Enable privileged tool mode with elevated permissions",
		Risk:             RiskDanger,
		RestartRequired:  true,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
		},
		UserIntents: []string{"enable privileged mode", "allow elevated tools"},
	})

	// Skills configuration
	Register(ConfigFieldMetadata{
		Section:          "skills",
		Key:              "load.global_dir",
		Path:             "skills.load.globalDir",
		Label:            "Global Skills Directory",
		Description:      "Directory path for globally installed skills",
		Risk:             RiskWarning,
		RestartRequired:  true,
		RequiresApproval: true,
		RequiresStepUp:   false,
		Validation: []ValidationRule{
			{Kind: "required"},
		},
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
			Instructions:    "Verify skill directory exists and is accessible",
		},
		UserIntents: []string{"change skills directory", "update skill path"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills",
		Key:              "load.disable_global_dir",
		Path:             "skills.load.disableGlobalDir",
		Label:            "Disable Global Skills",
		Description:      "Disable loading skills from the global directory",
		Risk:             RiskNotice,
		RestartRequired:  true,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
		},
		UserIntents: []string{"disable global skills", "turn off global directory"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills",
		Key:              "trust_policy",
		Path:             "skills.trustPolicy",
		Label:            "Skill Trust Policy",
		Description:      "Trust policy for skill execution (strict, moderate, permissive)",
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		AllowedValues:    []string{"strict", "moderate", "permissive"},
		Validation: []ValidationRule{
			{Kind: "enum", Value: []string{"strict", "moderate", "permissive"}},
		},
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"change trust policy", "update skill security"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills_entry",
		Key:              "enabled",
		Path:             "skills.entries.*.enabled",
		Label:            "Skill Enabled",
		Description:      "Enable or disable an installed skill entry",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback:         RollbackBehavior{Safe: true},
		UserIntents:      []string{"enable skill", "disable skill"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills_entry",
		Key:              "api_key",
		Path:             "skills.entries.*.apiKey",
		Label:            "Skill API Key",
		Description:      "Stored credential for an installed skill entry",
		Secret:           true,
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback:         RollbackBehavior{Safe: false, ManualOnly: true, Instructions: "Restore the previous skill credential manually if needed."},
		UserIntents:      []string{"rotate skill key", "repair skill credentials"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills_entry",
		Key:              "env.*",
		Path:             "skills.entries.*.env.*",
		Label:            "Skill Environment Override",
		Description:      "Environment variable override passed to an installed skill",
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   false,
		Rollback:         RollbackBehavior{Safe: false, ManualOnly: true, Instructions: "Review the previous environment override before restoring it."},
		UserIntents:      []string{"set skill environment", "repair skill override"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills_entry",
		Key:              "config.*",
		Path:             "skills.entries.*.config.*",
		Label:            "Skill Configuration Override",
		Description:      "Configuration override stored for an installed skill",
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   false,
		Rollback:         RollbackBehavior{Safe: true},
		UserIntents:      []string{"set skill config", "repair skill configuration"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills_entry",
		Key:              "config.credential_path",
		Path:             "skills.entries.*.config.credential_path",
		Label:            "Skill Credential Path",
		Description:      "Path to an external credential file used by an installed skill",
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback:         RollbackBehavior{Safe: true},
		UserIntents:      []string{"fix credential path", "repair skill auth source"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills_entry",
		Key:              "config.config_path",
		Path:             "skills.entries.*.config.config_path",
		Label:            "Skill Config Path",
		Description:      "Path to an external config file used by an installed skill",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   false,
		Rollback:         RollbackBehavior{Safe: true},
		UserIntents:      []string{"fix config path", "repair skill config source"},
	})

	Register(ConfigFieldMetadata{
		Section:          "skills_entry",
		Key:              "config.managed_reference",
		Path:             "skills.entries.*.config.managed_reference",
		Label:            "Skill Managed Reference",
		Description:      "OR3-managed credential or config reference used by an installed skill",
		Risk:             RiskWarning,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   false,
		Rollback:         RollbackBehavior{Safe: true},
		UserIntents:      []string{"clear stale reference", "repair managed reference"},
	})

	// Service configuration
	Register(ConfigFieldMetadata{
		Section:          "service",
		Key:              "enabled",
		Path:             "service.enabled",
		Label:            "Enable Service",
		Description:      "Enable the authenticated HTTP service listener",
		Risk:             RiskNotice,
		RestartRequired:  true,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
		},
		UserIntents: []string{"enable service", "start http server"},
	})

	Register(ConfigFieldMetadata{
		Section:          "service",
		Key:              "listen",
		Path:             "service.listen",
		Label:            "Listen Address",
		Description:      "Network address and port for the service listener",
		Risk:             RiskWarning,
		RestartRequired:  true,
		RequiresApproval: true,
		RequiresStepUp:   false,
		Validation: []ValidationRule{
			{Kind: "required"},
		},
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
		},
		UserIntents: []string{"change listen address", "update service port"},
	})

	Register(ConfigFieldMetadata{
		Section:          "service",
		Key:              "secret",
		Path:             "service.secret",
		Label:            "Service Secret",
		Description:      "Authentication secret for service API access",
		Secret:           true,
		Risk:             RiskDanger,
		RestartRequired:  true,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Validation: []ValidationRule{
			{Kind: "required"},
		},
		Rollback: RollbackBehavior{
			Safe:            false,
			ManualOnly:      true,
			RestartRequired: true,
			Instructions:    "Service secret changes require manual reconfiguration and restart",
		},
		UserIntents: []string{"change service secret", "update auth token"},
	})

	// Auth configuration
	Register(ConfigFieldMetadata{
		Section:          "auth",
		Key:              "passkeys_enabled",
		Path:             "auth.passkeysEnabled",
		Label:            "Enable Passkeys",
		Description:      "Enable WebAuthn passkey authentication",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"enable passkeys", "turn on webauthn"},
	})

	Register(ConfigFieldMetadata{
		Section:          "auth",
		Key:              "step_up_required",
		Path:             "auth.stepUpRequired",
		Label:            "Require Step-Up Auth",
		Description:      "Require step-up authentication for sensitive operations",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"require step-up", "enable extra verification"},
	})

	// Agent CLI configuration
	Register(ConfigFieldMetadata{
		Section:          "agentCLI",
		Key:              "enabled",
		Path:             "agentCLI.enabled",
		Label:            "Enable External CLI Agents",
		Description:      "Enable external agent CLI delegation",
		Risk:             RiskWarning,
		RestartRequired:  true,
		RequiresApproval: true,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe:            true,
			RestartRequired: true,
		},
		UserIntents: []string{"enable cli agents", "allow external agents"},
	})

	Register(ConfigFieldMetadata{
		Section:          "agentCLI",
		Key:              "disabled_runners",
		Path:             "agentCLI.disabledRunners",
		Label:            "Disabled Runners",
		Description:      "List of disabled runner IDs",
		Risk:             RiskNotice,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback: RollbackBehavior{
			Safe: true,
		},
		UserIntents: []string{"disable runner", "block specific agent"},
	})

	Register(ConfigFieldMetadata{
		Section:          "admin_brain",
		Key:              "provider_status",
		Path:             "doctor.adminBrain.kind",
		Label:            "Admin Brain Availability",
		Description:      "Derived availability of the Admin Brain provider or runner for Doctor conversations",
		Risk:             RiskSafe,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		AllowedValues:    []string{"runner", "apiKeyProvider", "unavailable"},
		Rollback:         RollbackBehavior{Safe: true},
		AdvancedOnly:     true,
	})

	Register(ConfigFieldMetadata{
		Section:          "admin_brain",
		Key:              "runner_id",
		Path:             "doctor.adminBrain.runnerId",
		Label:            "Admin Brain Runner ID",
		Description:      "Advanced identifier for the runner backing the Admin Brain",
		Risk:             RiskSafe,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
		Rollback:         RollbackBehavior{Safe: true},
		AdvancedOnly:     true,
	})

	Register(ConfigFieldMetadata{
		Section:          "service_action",
		Key:              "restart",
		Path:             "actions.restartService",
		Label:            "Restart Service",
		Description:      "Restart the OR3 service so restart-required settings changes can take effect",
		Risk:             RiskDanger,
		RestartRequired:  false,
		RequiresApproval: true,
		RequiresStepUp:   true,
		Rollback:         RollbackBehavior{Safe: false, ManualOnly: true, Instructions: "Restart actions are not rolled back automatically."},
		AdvancedOnly:     true,
		UserIntents:      []string{"restart service", "apply restart-required changes"},
	})
}
