package configmeta

import (
	"testing"

	"or3-intern/internal/config"
)

func TestStatusForConfigureKey_RunnerFirstHidesRunnerFieldsFromUI(t *testing.T) {
	// The or3-intern built-in agent loop is gone; runner-only mode hides
	// every `runners.*` field from the settings UI. The values are still
	// read at runtime by the runner host, but no UI control writes them.
	cfg := config.Default()
	if got := StatusForConfigureKey(cfg, "runners_default"); got != FieldStatusHidden {
		t.Fatalf("status = %q, want hidden", got)
	}
}

func TestListForConfig_Default_DoesNotIncludeRunnersEnabled(t *testing.T) {
	// runners.enabled was removed from RunnersConfig; ensure no stale
	// metadata row leaks it back into the configured-fields payload.
	Clear()
	RegisterFirstSliceFields()

	paths := ListForConfig(config.Default())
	for _, p := range paths {
		if p.Path == "runners.enabled" {
			t.Errorf("ListForConfig(Default()) contains dead runners.enabled path: %+v", p)
		}
	}
}
