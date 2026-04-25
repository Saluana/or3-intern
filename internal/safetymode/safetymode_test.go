package safetymode

import (
	"testing"

	"or3-intern/internal/config"
)

func TestApplyBalanced(t *testing.T) {
	cfg := config.Default()
	Apply(&cfg, ModeBalanced)
	if !cfg.Tools.RestrictToWorkspace {
		t.Fatal("expected workspace restriction enabled")
	}
	if !cfg.Security.Audit.Enabled {
		t.Fatal("expected audit enabled")
	}
	if !cfg.Security.Approvals.Enabled {
		t.Fatal("expected approvals enabled")
	}
	if cfg.Security.Approvals.Exec.Mode != config.ApprovalModeAsk {
		t.Fatalf("expected exec ask, got %q", cfg.Security.Approvals.Exec.Mode)
	}
	if cfg.RuntimeProfile != config.ProfileSingleUserHardened {
		t.Fatalf("expected single-user-hardened, got %q", cfg.RuntimeProfile)
	}
}

func TestApplyLockedDown(t *testing.T) {
	cfg := config.Default()
	Apply(&cfg, ModeLockedDown)
	if cfg.Security.Approvals.Exec.Mode != config.ApprovalModeDeny {
		t.Fatalf("expected exec deny, got %q", cfg.Security.Approvals.Exec.Mode)
	}
	if !cfg.Security.Audit.Strict || !cfg.Security.Audit.VerifyOnStart {
		t.Fatal("expected strict verified audit")
	}
	if !cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny {
		t.Fatal("expected default-deny network")
	}
	if !cfg.Hardening.Sandbox.Enabled {
		t.Fatal("expected sandbox enabled")
	}
}

func TestApplyScenarioPhoneCompanion(t *testing.T) {
	cfg := config.Default()
	ApplyScenario(&cfg, ScenarioPhoneCompanion)
	if !cfg.Service.Enabled {
		t.Fatal("expected service enabled for phone companion")
	}
	if cfg.Security.Approvals.Pairing.Mode != config.ApprovalModeAsk {
		t.Fatalf("expected pairing ask, got %q", cfg.Security.Approvals.Pairing.Mode)
	}
}

func TestInferCustomFromDrift(t *testing.T) {
	cfg := config.Default()
	Apply(&cfg, ModeBalanced)
	cfg.Security.Audit.Enabled = false
	inference := Infer(cfg)
	if !inference.IsCustom {
		t.Fatal("expected custom inference")
	}
	if inference.BaseMode != ModeBalanced {
		t.Fatalf("expected balanced base mode, got %q", inference.BaseMode)
	}
	if len(inference.Drift) == 0 {
		t.Fatal("expected drift details")
	}
}

func TestInferExact(t *testing.T) {
	cfg := config.Default()
	Apply(&cfg, ModeRelaxed)
	inference := Infer(cfg)
	if inference.Mode != ModeRelaxed || inference.IsCustom {
		t.Fatalf("unexpected inference: %#v", inference)
	}
}
