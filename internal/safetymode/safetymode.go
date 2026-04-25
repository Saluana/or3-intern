package safetymode

import (
	"fmt"
	"strings"

	"or3-intern/internal/config"
)

type Mode string

type Scenario string

type Inference struct {
	Mode       Mode
	BaseMode   Mode
	IsCustom   bool
	Drift      []string
	Scenario   Scenario
	Confidence string
}

const (
	ModeRelaxed    Mode = "relaxed"
	ModeBalanced   Mode = "balanced"
	ModeLockedDown Mode = "locked-down"
	ModeCustom     Mode = "custom"
)

const (
	ScenarioSoloComputer   Scenario = "solo-computer"
	ScenarioPhoneCompanion Scenario = "phone-companion"
	ScenarioPrivateServer  Scenario = "private-server"
	ScenarioHostedService  Scenario = "hosted-service"
	ScenarioAdvanced       Scenario = "advanced"
)

type ScenarioOption struct {
	Scenario    Scenario
	Label       string
	Description string
}

var scenarioOptions = []ScenarioOption{
	{Scenario: ScenarioSoloComputer, Label: "Just me, on this computer", Description: "Local use on one computer with a single trusted user."},
	{Scenario: ScenarioPhoneCompanion, Label: "Me and my phone", Description: "Personal use across this computer and a paired device."},
	{Scenario: ScenarioPrivateServer, Label: "A small private server", Description: "A protected self-hosted service for a small trusted group."},
	{Scenario: ScenarioHostedService, Label: "Public/hosted service", Description: "Internet-facing or shared hosted deployment."},
	{Scenario: ScenarioAdvanced, Label: "Advanced/manual setup", Description: "Keep the low-level knobs visible and configure manually."},
}

func ScenarioOptions() []ScenarioOption {
	out := make([]ScenarioOption, len(scenarioOptions))
	copy(out, scenarioOptions)
	return out
}

func NormalizeMode(raw string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case ModeRelaxed, ModeBalanced, ModeLockedDown:
		return Mode(strings.ToLower(strings.TrimSpace(raw)))
	default:
		return ModeCustom
	}
}

func NormalizeScenario(raw string) Scenario {
	switch Scenario(strings.ToLower(strings.TrimSpace(raw))) {
	case ScenarioSoloComputer, ScenarioPhoneCompanion, ScenarioPrivateServer, ScenarioHostedService, ScenarioAdvanced:
		return Scenario(strings.ToLower(strings.TrimSpace(raw)))
	default:
		return ScenarioAdvanced
	}
}

func RecommendMode(scenario Scenario) Mode {
	switch NormalizeScenario(string(scenario)) {
	case ScenarioPrivateServer, ScenarioHostedService:
		return ModeLockedDown
	default:
		return ModeBalanced
	}
}

func ApplyScenario(cfg *config.Config, scenario Scenario) {
	if cfg == nil {
		return
	}
	switch NormalizeScenario(string(scenario)) {
	case ScenarioSoloComputer:
		cfg.RuntimeProfile = config.ProfileSingleUserHardened
		cfg.Service.Enabled = false
		cfg.Service.AllowUnauthenticatedPairing = false
		cfg.Security.Approvals.Enabled = true
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAsk
	case ScenarioPhoneCompanion:
		cfg.RuntimeProfile = config.ProfileSingleUserHardened
		cfg.Service.Enabled = true
		cfg.Service.AllowUnauthenticatedPairing = false
		cfg.Security.Approvals.Enabled = true
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAsk
	case ScenarioPrivateServer:
		cfg.RuntimeProfile = config.ProfileHostedService
		cfg.Service.Enabled = true
		cfg.Service.AllowUnauthenticatedPairing = false
		cfg.Security.Approvals.Enabled = true
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAsk
		cfg.Security.SecretStore.Enabled = true
		cfg.Security.SecretStore.Required = true
		cfg.Security.Audit.Enabled = true
		cfg.Security.Audit.Strict = true
		cfg.Security.Audit.VerifyOnStart = true
		cfg.Security.Network.Enabled = true
		cfg.Security.Network.DefaultDeny = true
		cfg.Security.Network.AllowLoopback = true
	case ScenarioHostedService:
		cfg.RuntimeProfile = config.ProfileHostedNoExec
		cfg.Service.Enabled = true
		cfg.Service.AllowUnauthenticatedPairing = false
		cfg.Security.Approvals.Enabled = true
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAsk
		cfg.Security.SecretStore.Enabled = true
		cfg.Security.SecretStore.Required = true
		cfg.Security.Audit.Enabled = true
		cfg.Security.Audit.Strict = true
		cfg.Security.Audit.VerifyOnStart = true
		cfg.Security.Network.Enabled = true
		cfg.Security.Network.DefaultDeny = true
		cfg.Security.Network.AllowLoopback = false
		cfg.Security.Network.AllowPrivate = false
		cfg.Hardening.EnableExecShell = false
		cfg.Hardening.PrivilegedTools = false
	case ScenarioAdvanced:
	}
}

func Apply(cfg *config.Config, mode Mode) {
	if cfg == nil {
		return
	}
	defaults := config.Default()
	cfg.Tools.RestrictToWorkspace = true
	cfg.Hardening.Quotas = defaults.Hardening.Quotas
	switch NormalizeMode(string(mode)) {
	case ModeRelaxed:
		cfg.Hardening.GuardedTools = false
		cfg.Security.Audit.Enabled = false
		cfg.Security.Audit.Strict = false
		cfg.Security.Audit.VerifyOnStart = false
		cfg.Security.Approvals.Enabled = false
		cfg.Security.Approvals.Exec.Mode = config.ApprovalModeTrusted
		cfg.Security.Approvals.SkillExecution.Mode = config.ApprovalModeTrusted
		cfg.Security.Approvals.SecretAccess.Mode = config.ApprovalModeTrusted
		cfg.Security.Approvals.MessageSend.Mode = config.ApprovalModeTrusted
		cfg.Security.Network.Enabled = false
		cfg.Security.Network.DefaultDeny = false
		cfg.Hardening.Sandbox.Enabled = false
		if cfg.RuntimeProfile == "" {
			cfg.RuntimeProfile = config.ProfileLocalDev
		}
	case ModeLockedDown:
		cfg.Hardening.GuardedTools = true
		cfg.Hardening.PrivilegedTools = false
		cfg.Hardening.EnableExecShell = false
		cfg.Hardening.Sandbox.Enabled = true
		if strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
			cfg.Hardening.Sandbox.BubblewrapPath = defaults.Hardening.Sandbox.BubblewrapPath
		}
		cfg.Security.SecretStore.Enabled = true
		cfg.Security.SecretStore.Required = true
		cfg.Security.Audit.Enabled = true
		cfg.Security.Audit.Strict = true
		cfg.Security.Audit.VerifyOnStart = true
		cfg.Security.Approvals.Enabled = true
		cfg.Security.Approvals.Exec.Mode = config.ApprovalModeDeny
		cfg.Security.Approvals.SkillExecution.Mode = config.ApprovalModeAsk
		cfg.Security.Approvals.SecretAccess.Mode = config.ApprovalModeAsk
		cfg.Security.Approvals.MessageSend.Mode = config.ApprovalModeAsk
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAsk
		cfg.Security.Network.Enabled = true
		cfg.Security.Network.DefaultDeny = true
		if cfg.RuntimeProfile == "" || cfg.RuntimeProfile == config.ProfileLocalDev || cfg.RuntimeProfile == config.ProfileSingleUserHardened {
			cfg.RuntimeProfile = config.ProfileHostedNoExec
		}
	default:
		cfg.Hardening.GuardedTools = true
		cfg.Security.Audit.Enabled = true
		cfg.Security.Audit.Strict = false
		cfg.Security.Audit.VerifyOnStart = false
		cfg.Security.Approvals.Enabled = true
		cfg.Security.Approvals.Exec.Mode = config.ApprovalModeAsk
		cfg.Security.Approvals.SkillExecution.Mode = config.ApprovalModeAsk
		cfg.Security.Approvals.SecretAccess.Mode = config.ApprovalModeAsk
		cfg.Security.Approvals.MessageSend.Mode = config.ApprovalModeAsk
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAsk
		cfg.Security.Network.Enabled = false
		cfg.Security.Network.DefaultDeny = false
		cfg.Hardening.Sandbox.Enabled = false
		if cfg.RuntimeProfile == "" {
			cfg.RuntimeProfile = config.ProfileSingleUserHardened
		}
	}
}

func Infer(cfg config.Config) Inference {
	candidates := []Mode{ModeRelaxed, ModeBalanced, ModeLockedDown}
	best := ModeCustom
	bestDrift := []string{"safety posture does not match a standard mode yet"}
	for _, candidate := range candidates {
		drift := Drift(cfg, candidate)
		if len(drift) == 0 {
			return Inference{Mode: candidate, BaseMode: candidate, Scenario: InferScenario(cfg), Confidence: "exact"}
		}
		if best == ModeCustom || len(drift) < len(bestDrift) {
			best = candidate
			bestDrift = drift
		}
	}
	return Inference{Mode: ModeCustom, BaseMode: best, IsCustom: true, Drift: bestDrift, Scenario: InferScenario(cfg), Confidence: "closest-match"}
}

func Drift(cfg config.Config, mode Mode) []string {
	baseline := config.Default()
	Apply(&baseline, mode)
	drift := []string{}
	if cfg.Tools.RestrictToWorkspace != baseline.Tools.RestrictToWorkspace {
		drift = append(drift, fmt.Sprintf("workspace boundary expected %t", baseline.Tools.RestrictToWorkspace))
	}
	if cfg.Hardening.GuardedTools != baseline.Hardening.GuardedTools {
		drift = append(drift, fmt.Sprintf("ask-before-risky-actions expected %t", baseline.Hardening.GuardedTools))
	}
	if cfg.Security.Audit.Enabled != baseline.Security.Audit.Enabled {
		drift = append(drift, fmt.Sprintf("safety log enabled expected %t", baseline.Security.Audit.Enabled))
	}
	if cfg.Security.Audit.Strict != baseline.Security.Audit.Strict {
		drift = append(drift, fmt.Sprintf("strict safety log expected %t", baseline.Security.Audit.Strict))
	}
	if cfg.Security.Approvals.Enabled != baseline.Security.Approvals.Enabled {
		drift = append(drift, fmt.Sprintf("approvals enabled expected %t", baseline.Security.Approvals.Enabled))
	}
	if cfg.Security.Approvals.Exec.Mode != baseline.Security.Approvals.Exec.Mode {
		drift = append(drift, fmt.Sprintf("command approval mode expected %s", baseline.Security.Approvals.Exec.Mode))
	}
	if cfg.Security.Network.Enabled != baseline.Security.Network.Enabled {
		drift = append(drift, fmt.Sprintf("network policy enabled expected %t", baseline.Security.Network.Enabled))
	}
	if cfg.Security.Network.DefaultDeny != baseline.Security.Network.DefaultDeny {
		drift = append(drift, fmt.Sprintf("network default deny expected %t", baseline.Security.Network.DefaultDeny))
	}
	if cfg.Hardening.Sandbox.Enabled != baseline.Hardening.Sandbox.Enabled {
		drift = append(drift, fmt.Sprintf("sandbox enabled expected %t", baseline.Hardening.Sandbox.Enabled))
	}
	if cfg.Hardening.EnableExecShell != baseline.Hardening.EnableExecShell {
		drift = append(drift, fmt.Sprintf("shell execution expected %t", baseline.Hardening.EnableExecShell))
	}
	if cfg.Hardening.PrivilegedTools != baseline.Hardening.PrivilegedTools {
		drift = append(drift, fmt.Sprintf("privileged tools expected %t", baseline.Hardening.PrivilegedTools))
	}
	return drift
}

func InferScenario(cfg config.Config) Scenario {
	switch cfg.RuntimeProfile {
	case config.ProfileHostedService:
		return ScenarioPrivateServer
	case config.ProfileHostedNoExec, config.ProfileHostedRemoteSandbox:
		return ScenarioHostedService
	case config.ProfileSingleUserHardened:
		if cfg.Service.Enabled {
			return ScenarioPhoneCompanion
		}
		return ScenarioSoloComputer
	default:
		if cfg.Service.Enabled {
			return ScenarioPhoneCompanion
		}
		return ScenarioSoloComputer
	}
}
