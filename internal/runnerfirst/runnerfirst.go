// Package runnerfirst holds process-wide runner-first mode set during startup.
package runnerfirst

import "sync/atomic"

var enabled atomic.Bool

// SetEnabled records whether runner-first mode is active for this process.
func SetEnabled(active bool) {
	enabled.Store(active)
}

// Enabled reports whether new legacy subagent jobs and skill run plans should be rejected.
func Enabled() bool {
	return enabled.Load()
}
