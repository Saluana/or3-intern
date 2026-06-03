package agentcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerContextCacheExcludesSensitiveValues(t *testing.T) {
	c := NewRunnerContextCache(0)
	c.Put("secret", "approval_token=abc123")
	if _, ok := c.Get("secret"); ok {
		t.Fatal("expected sensitive cache value to be rejected")
	}
}

func TestRunnerContextCacheBootstrapHit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	if err := os.WriteFile(path, []byte("soul"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewRunnerContextCache(0)
	first, err := c.LoadBootstrapCached(path, nil)
	if err != nil || first != "soul" {
		t.Fatalf("first load: %q err=%v", first, err)
	}
	second, err := c.LoadBootstrapCached(path, nil)
	if err != nil || second != "soul" {
		t.Fatalf("cached load: %q err=%v", second, err)
	}
}

func TestRunnerContextCacheInvalidate(t *testing.T) {
	c := NewRunnerContextCache(0)
	c.Put("k", "v")
	c.Invalidate("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected cache miss after invalidation")
	}
}
