package doctor

import "or3-intern/internal/config"

func hasExternalIntegrations(cfg config.Config) bool {
	return cfg.Triggers.Webhook.Enabled || anyEnabledChannels(cfg)
}
