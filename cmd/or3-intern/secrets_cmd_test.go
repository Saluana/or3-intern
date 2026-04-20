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

func TestRunSecretsCommand_RejectsExtraArgs(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	var out bytes.Buffer
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"list", "extra"}, &out, &out); err == nil {
		t.Fatal("expected list with extra args to fail")
	}
	if err := runSecretsCommand(context.Background(), mgr, nil, []string{"set", "name", "value", "extra"}, &out, &out); err == nil {
		t.Fatal("expected set with extra args to fail")
	}
}

func TestRunSecretsCommand_StrictAuditFailureBlocksMutation(t *testing.T) {
	mgr := newSecretManagerForTest(t)
	audit := &security.AuditLogger{Strict: true}
	var out bytes.Buffer

	if err := runSecretsCommand(context.Background(), mgr, audit, []string{"set", "provider.openai", "secret-value"}, &out, &out); err == nil {
		t.Fatal("expected strict audit failure during set")
	}
	if _, ok, err := mgr.Get(context.Background(), "provider.openai"); err != nil {
		t.Fatalf("Get after failed set: %v", err)
	} else if ok {
		t.Fatal("expected set mutation to be blocked before persistence")
	}

	if err := mgr.Put(context.Background(), "provider.openai", "seed"); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	if err := runSecretsCommand(context.Background(), mgr, audit, []string{"delete", "provider.openai"}, &out, &out); err == nil {
		t.Fatal("expected strict audit failure during delete")
	}
	value, ok, err := mgr.Get(context.Background(), "provider.openai")
	if err != nil {
		t.Fatalf("Get after failed delete: %v", err)
	}
	if !ok || value != "seed" {
		t.Fatalf("expected delete mutation to be blocked, got ok=%v value=%q", ok, value)
	}
}
