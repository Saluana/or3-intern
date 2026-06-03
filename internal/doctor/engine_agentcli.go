package doctor

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
)

func agentCLIFindings(cfg config.Config, opts Options) []Finding {
	findings := make([]Finding, 0, 8)
	if !cfg.AgentCLI.Enabled {
		findings = append(findings, Finding{
			ID:       "agent_cli.disabled",
			Area:     "runners",
			Summary:  "External runners disabled",
			Detail:   "agentCLI.enabled is false; chat and channel turns require an enabled runner (OpenCode by default).",
			Severity: severityForConfigureOrStartup(opts.Mode, SeverityWarn),
			FixHint:  "Set agentCLI.enabled=true and install/authenticate OpenCode, or set OR3_AGENT_CLI_ENABLED=true.",
		})
		return findings
	}
	defaultRunner := agentcli.ResolveDefaultRunner(cfg)
	if err := agentcli.ValidateSelectableRunner(cfg, defaultRunner); err != nil {
		findings = append(findings, Finding{
			ID:       "agent_cli.default_runner_invalid",
			Area:     "runners",
			Summary:  "Default runner is not usable",
			Detail:   fmt.Sprintf("agentCLI.defaultRunner=%q: %v", cfg.AgentCLI.DefaultRunner, err),
			Severity: severityForConfigureOrStartup(opts.Mode, SeverityBlock),
			FixHint:  "Set agentCLI.defaultRunner to opencode, codex, claude, or gemini and remove it from agentCLI.disabledRunners.",
		})
	}
	if opts.Probe {
		findings = append(findings, agentCLIProbeFindings(cfg, defaultRunner)...)
	}
	return findings
}

func agentCLIProbeFindings(cfg config.Config, defaultRunner agentcli.RunnerID) []Finding {
	findings := make([]Finding, 0, 4)
	detectOpts := agentcli.DetectOptions{DisabledRunners: cfg.AgentCLI.DisabledRunners}
	for _, spec := range agentcli.SelectableRunners() {
		if spec.ID != defaultRunner {
			continue
		}
		info := agentcli.Detect(context.Background(), spec, detectOpts)
		switch info.Status {
		case agentcli.RunnerStatusAvailable:
			if info.AuthStatus != agentcli.AuthReady && info.AuthStatus != "" {
				findings = append(findings, Finding{
					ID:       "agent_cli.default_runner_auth",
					Area:     "runners",
					Summary:  fmt.Sprintf("%s needs authentication", spec.DisplayName),
					Detail:   fmt.Sprintf("Default runner %q is installed but auth is %s.", defaultRunner, info.AuthStatus),
					Severity: SeverityWarn,
					FixHint:  openCodeAuthFix(spec.ID),
				})
			}
		case agentcli.RunnerStatusMissing:
			findings = append(findings, Finding{
				ID:       "agent_cli.default_runner_missing",
				Area:     "runners",
				Summary:  fmt.Sprintf("%s is not installed", spec.DisplayName),
				Detail:   fmt.Sprintf("Default runner %q binary %q was not found on PATH.", defaultRunner, spec.Binary),
				Severity: SeverityBlock,
				FixHint:  fmt.Sprintf("Install %s and ensure %q is on PATH, or choose another runner in agentCLI.defaultRunner.", spec.DisplayName, spec.Binary),
			})
		case agentcli.RunnerStatusDisabledByConfig:
			findings = append(findings, Finding{
				ID:       "agent_cli.default_runner_disabled",
				Area:     "runners",
				Summary:  "Default runner disabled in config",
				Detail:   fmt.Sprintf("Default runner %q is listed in agentCLI.disabledRunners.", defaultRunner),
				Severity: SeverityBlock,
				FixHint:  "Remove the default runner from agentCLI.disabledRunners or choose another defaultRunner.",
			})
		default:
			if strings.TrimSpace(string(info.Status)) != "" && info.Status != agentcli.RunnerStatusAvailable {
				findings = append(findings, Finding{
					ID:       "agent_cli.default_runner_not_ready",
					Area:     "runners",
					Summary:  fmt.Sprintf("%s is not ready", spec.DisplayName),
					Detail:   fmt.Sprintf("Default runner %q status is %s.", defaultRunner, info.Status),
					Severity: SeverityWarn,
					FixHint:  fmt.Sprintf("Resolve %s installation/auth issues or select another agentCLI.defaultRunner.", spec.DisplayName),
				})
			}
		}
	}
	return findings
}

func openCodeAuthFix(id agentcli.RunnerID) string {
	switch id {
	case agentcli.RunnerOpenCode:
		return "Run `opencode auth login` (or `opencode auth list`) and retry."
	case agentcli.RunnerCodex:
		return "Run `codex login` and retry."
	case agentcli.RunnerClaude:
		return "Run `claude auth login` and retry."
	default:
		return "Authenticate the selected runner CLI, then retry."
	}
}
