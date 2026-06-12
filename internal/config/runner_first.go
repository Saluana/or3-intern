package config

// RunnerFirst reports whether active chat turns and background work are delegated
// to external runners. Runner-only is the only supported mode; this always returns
// true. There is no longer a config switch that gates this — runner orchestration
// is the only execution path.
func (c Config) RunnerFirst() bool {
	return true
}
