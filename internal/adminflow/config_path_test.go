package adminflow

import (
	"testing"

	"or3-intern/internal/config"
)

func TestResolveConfigPathValue(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.Model = "gpt-4.1-mini"

	value, ok := resolveConfigPathValue(cfg, "provider.model")
	if !ok {
		t.Fatal("resolveConfigPathValue(provider.model) = false, want true")
	}
	if value != "gpt-4.1-mini" {
		t.Fatalf("value = %v, want gpt-4.1-mini", value)
	}

	_, ok = resolveConfigPathValue(cfg, "provider.missing")
	if ok {
		t.Fatal("resolveConfigPathValue(provider.missing) = true, want false")
	}
}
