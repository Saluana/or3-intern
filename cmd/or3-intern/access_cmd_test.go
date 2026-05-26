package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
)

func TestRunAccessCommandSetsChannelProfile(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runAccessCommand(context.Background(), cfgPath, cfg, []string{"channel", "telegram", "operator"}, &out, nil); err != nil {
		t.Fatalf("runAccessCommand: %v", err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Security.Profiles.Channels["telegram"] != config.AccessLevelOperator {
		t.Fatalf("expected telegram operator, got %#v", loaded.Security.Profiles.Channels)
	}
	if !loaded.Hardening.GuardedTools || !loaded.Tools.EnableExec {
		t.Fatalf("expected operator runtime requirements, got hardening=%#v tools=%#v", loaded.Hardening, loaded.Tools)
	}
	if loaded.Service.MaxCapability != "guarded" {
		t.Fatalf("expected operator service maxCapability guarded, got %q", loaded.Service.MaxCapability)
	}
}

func TestRunAccessCommandSetsAdminServiceCeiling(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runAccessCommand(context.Background(), cfgPath, cfg, []string{"channel", "service", "admin"}, &out, nil); err != nil {
		t.Fatalf("runAccessCommand: %v", err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Security.Profiles.Channels["service"] != config.AccessLevelAdmin {
		t.Fatalf("expected service admin, got %#v", loaded.Security.Profiles.Channels)
	}
	if loaded.Service.MaxCapability != "privileged" {
		t.Fatalf("expected admin service maxCapability privileged, got %q", loaded.Service.MaxCapability)
	}
	if !loaded.Hardening.GuardedTools || !loaded.Hardening.PrivilegedTools || !loaded.Tools.EnableExec {
		t.Fatalf("expected admin runtime requirements, got hardening=%#v tools=%#v", loaded.Hardening, loaded.Tools)
	}
}
