package doctor

import "or3-intern/internal/config"

type Options struct {
	Mode            Mode
	ConfigPath      string
	ValidationError string
	Probe           bool
}

func Evaluate(cfg config.Config, opts Options) Report {
	if opts.Mode == "" {
		opts.Mode = ModeAdvisory
	}
	findings := make([]Finding, 0, 48)
	findings = append(findings, configValidationFindings(cfg, opts)...)
	findings = append(findings, providerFindings(cfg, opts)...)
	findings = append(findings, filesystemFindings(cfg, opts)...)
	findings = append(findings, hardeningFindings(cfg, opts)...)
	findings = append(findings, securityFindings(cfg, opts)...)
	findings = append(findings, approvalFindings(cfg, opts)...)
	findings = append(findings, webhookFindings(cfg, opts)...)
	findings = append(findings, serviceFindings(cfg, opts)...)
	findings = append(findings, mcpFindings(cfg, opts)...)
	findings = append(findings, networkFindings(cfg, opts)...)
	findings = append(findings, profileFindings(cfg, opts)...)
	findings = append(findings, execFindings(cfg, opts)...)
	findings = append(findings, skillFindings(cfg, opts)...)
	findings = append(findings, channelExposureFindings(cfg, opts)...)
	findings = append(findings, channelIngressFindings(cfg, opts)...)
	findings = append(findings, runtimeProfileFindings(cfg, opts)...)
	if opts.Probe {
		findings = append(findings, probeFindings(cfg, opts)...)
	}
	return NewReport(opts.Mode, findings)
}

func severityFor(mode Mode, advisory Severity, blockOnStartup bool) Severity {
	if blockOnStartup && (mode == ModeStartupChat || mode == ModeStartupServe || mode == ModeStartupService) {
		return SeverityBlock
	}
	return advisory
}

func severityForConfigureOrStartup(mode Mode, advisory Severity) Severity {
	if mode == ModeConfigurePostSave || mode == ModeStartupChat || mode == ModeStartupServe || mode == ModeStartupService {
		return SeverityBlock
	}
	return advisory
}
