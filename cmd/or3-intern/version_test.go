package main

import (
	"strings"
	"testing"
)

func TestBuildVersionStringIncludesBuildProvenance(t *testing.T) {
	originalVersion, originalCommit, originalDirty, originalTime := buildVersion, buildCommit, buildDirty, buildTime
	t.Cleanup(func() {
		buildVersion, buildCommit, buildDirty, buildTime = originalVersion, originalCommit, originalDirty, originalTime
	})
	buildVersion = "v9.8.7"
	buildCommit = "abc123"
	buildDirty = "true"
	buildTime = "2026-08-11T12:00:00Z"
	got := buildVersionString()
	for _, want := range []string{"or3-intern v9.8.7", "commit: abc123", "dirty: true", "built: 2026-08-11T12:00:00Z", "go:", "platform:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output missing %q: %s", want, got)
		}
	}
}
