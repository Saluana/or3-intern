package configedit

import (
	"strings"

	"or3-intern/internal/configmeta"
)

// configureFieldKeysByPath maps JSON config paths to configure API field keys
// when the default section_key rule does not match configedit.ApplyFieldValue.
var configureFieldKeysByPath = map[string]string{
	"provider.model":                 "provider_model",
	"provider.apiBase":               "provider_api_base",
	"provider.apiKey":                "provider_api_key",
	"providers.profiles.openai.apiKey": "provider_openai_api_key",
	"providers.profiles.openrouter.apiKey": "provider_openrouter_api_key",
	"providers.profiles.custom.apiKey":   "provider_custom_api_key",
	"skills.load.globalDir":          "skills_global_dir",
	"skills.load.disableGlobalDir":   "skills_global_disabled",
	"tools.enableExec":               "tools_enable_exec",
	"tools.execAllowedPrograms":      "hardening_exec_allowed_programs",
	"tools.restrictToWorkspace":      "tools_restrict_to_workspace",
	"service.enabled":                "service_enabled",
	"service.listen":                 "service_listen",
	"service.secret":                 "service_secret",
	"agentCLI.enabled":               "agentCLI_enabled",
	"agentCLI.disabledRunners":       "agentCLI_disabled_runners",
}

// ConfigureFieldKeyForMetadata returns the configure API field key used by
// configedit.ApplyFieldValue for a metadata entry.
func ConfigureFieldKeyForMetadata(meta configmeta.ConfigFieldMetadata) string {
	if key, ok := configureFieldKeysByPath[strings.TrimSpace(meta.Path)]; ok {
		return key
	}
	section := strings.TrimSpace(meta.Section)
	key := strings.TrimSpace(meta.Key)
	if section == "" || key == "" {
		return key
	}
	switch section {
	case "skills_entry", "channels", "mcp", "service_action":
		return key
	}
	key = strings.ReplaceAll(key, ".", "_")
	return section + "_" + key
}
