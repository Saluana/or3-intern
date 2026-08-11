package config

import "encoding/json"

// Clone returns an independent config snapshot, including nested maps and
// slices. It is intended for live configuration publication, where callers
// must never mutate data reachable from an already-published snapshot.
func Clone(cfg Config) Config {
	data, err := json.Marshal(cfg)
	if err != nil {
		return cfg
	}
	var clone Config
	if err := json.Unmarshal(data, &clone); err != nil {
		return cfg
	}
	clone.ContextConfigured = cfg.ContextConfigured
	clone.CompatEnvWarnings = append([]string(nil), cfg.CompatEnvWarnings...)
	clone.IntegrationWarnings = append([]IntegrationQuarantine(nil), cfg.IntegrationWarnings...)
	return clone
}
