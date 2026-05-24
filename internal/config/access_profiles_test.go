package config

import (
	"path/filepath"
	"testing"
)

func TestSetChannelAccessLevelEnsuresBuiltins(t *testing.T) {
	var profiles AccessProfilesConfig
	if !SetChannelAccessLevel(&profiles, "Telegram", "operator") {
		t.Fatal("expected channel access level to be set")
	}
	if !profiles.Enabled {
		t.Fatal("expected profiles to be enabled")
	}
	if profiles.Channels["telegram"] != AccessLevelOperator {
		t.Fatalf("expected telegram operator mapping, got %q", profiles.Channels["telegram"])
	}
	if profiles.Profiles[AccessLevelOperator].MaxCapability != "guarded" {
		t.Fatalf("expected operator builtin profile, got %#v", profiles.Profiles[AccessLevelOperator])
	}
}

func TestExpandAccessProfileWorkspaceDir(t *testing.T) {
	workspace := t.TempDir()
	profile := AccessProfileConfig{WritablePaths: []string{AccessProfileWorkspaceDir, "${workspaceDir}/nested", ""}}
	expanded := ExpandAccessProfile(profile, workspace)
	if len(expanded.WritablePaths) != 2 {
		t.Fatalf("expected two expanded paths, got %#v", expanded.WritablePaths)
	}
	if expanded.WritablePaths[0] != workspace {
		t.Fatalf("expected workspace path, got %q", expanded.WritablePaths[0])
	}
	wantNested := filepath.Join(workspace, "nested")
	if expanded.WritablePaths[1] != wantNested {
		t.Fatalf("expected nested workspace path %q, got %q", wantNested, expanded.WritablePaths[1])
	}
}
