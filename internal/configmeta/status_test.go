package configmeta

import (
	"testing"

	"or3-intern/internal/config"
)

func TestStatusForConfigureKey_AlwaysActive(t *testing.T) {
	// Runner-only mode is the only supported mode; every field is active.
	cfg := config.Default()
	if got := StatusForConfigureKey(cfg, "runners_default"); got != FieldStatusActive {
		t.Fatalf("status = %q, want active", got)
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
