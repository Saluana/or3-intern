package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

func newSecretManagerForTest(t *testing.T) *security.SecretManager {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "secrets.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &security.SecretManager{DB: d, Key: []byte("01234567890123456789012345678901")}
}

func TestRunSecretsCommand_SetAndList(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	var out bytes.Buffer
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"set", "provider.openai", "secret-value"}, &out, &out); err != nil {
		t.Fatalf("set: %v", err)
	}
	out.Reset()
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"list"}, &out, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if out.String() == "" {
		t.Fatal("expected secret name in list output")
	}
}
