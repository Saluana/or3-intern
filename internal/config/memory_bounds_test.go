package config

import "testing"

func TestClampMemoryAndDocConfig(t *testing.T) {
	cfg := Default()
	cfg.MemoryRetrieve = 9999
	cfg.VectorK = 0
	cfg.DocIndex.MaxFiles = 100000
	cfg = func() Config {
		c := cfg
		clampMemoryAndDocConfig(&c)
		return c
	}()
	if cfg.MemoryRetrieve != MaxMemoryRetrieveLimit {
		t.Fatalf("expected memory retrieve clamped to %d, got %d", MaxMemoryRetrieveLimit, cfg.MemoryRetrieve)
	}
	if cfg.VectorK != defaultVectorSearchK {
		t.Fatalf("expected default vector k, got %d", cfg.VectorK)
	}
	if cfg.DocIndex.MaxFiles != MaxDocIndexMaxFiles {
		t.Fatalf("expected doc max files clamped, got %d", cfg.DocIndex.MaxFiles)
	}
}

func TestValidateMemoryIntField(t *testing.T) {
	if err := ValidateMemoryIntField("runtime_memory_retrieve", 0); err == nil {
		t.Fatal("expected error for out-of-range value")
	}
	if err := ValidateMemoryIntField("runtime_memory_retrieve", 8); err != nil {
		t.Fatalf("expected valid value: %v", err)
	}
}
