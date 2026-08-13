package security

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func TestLoadOrCreateKeyConcurrentFirstStartUsesOneKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.key")
	const callers = 32
	keys := make([][]byte, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			keys[index], errs[index] = LoadOrCreateKey(path)
		}(i)
	}
	wg.Wait()
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if !bytes.Equal(keys[0], keys[i]) {
			t.Fatalf("caller %d received a different key", i)
		}
	}
}

func openSecurityTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestSecretManager_RoundTripAndResolveConfigSecrets(t *testing.T) {
	d := openSecurityTestDB(t)
	ctx := context.Background()
	mgr := &SecretManager{DB: d, Key: []byte("01234567890123456789012345678901")}
	if err := mgr.Put(ctx, "provider.apiKey", "super-secret"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	value, ok, err := mgr.Get(ctx, "provider.apiKey")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || value != "super-secret" {
		t.Fatalf("unexpected secret round trip: ok=%v value=%q", ok, value)
	}
	cfg := config.Default()
	cfg.Provider.APIKey = "secret:provider.apiKey"
	resolved, err := ResolveConfigSecrets(ctx, cfg, mgr)
	if err != nil {
		t.Fatalf("ResolveConfigSecrets: %v", err)
	}
	if resolved.Provider.APIKey != "super-secret" {
		t.Fatalf("expected resolved secret, got %q", resolved.Provider.APIKey)
	}
}

func TestValidateNoSecretRefs_DetectsRemainingSecretRefs(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.APIKey = "secret:provider.apiKey"
	err := ValidateNoSecretRefs(cfg)
	if err == nil || !strings.Contains(err.Error(), "unresolved secret ref") {
		t.Fatalf("expected unresolved secret ref error, got %v", err)
	}
}
