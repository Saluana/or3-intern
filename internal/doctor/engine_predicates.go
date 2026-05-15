package doctor

import "or3-intern/internal/config"

func hasExternalIntegrations(cfg config.Config) bool {
	return cfg.Triggers.Webhook.Enabled || anyEnabledChannels(cfg) || anyEnabledMCPServers(cfg)
}

func anyEnabledMCPServers(cfg config.Config) bool {
	for _, server := range cfg.Tools.MCPServers {
		if server.Enabled {
			return true
		}
	}
	return false
}
