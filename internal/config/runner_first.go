package config

// RunnerFirst reports whether active chat turns and background work are delegated
// to external runners instead of the legacy built-in OR3 tool loop.
func (c Config) RunnerFirst() bool {
	return c.AgentCLI.Enabled
}
