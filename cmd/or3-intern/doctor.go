package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"or3-intern/internal/config"
	intdoctor "or3-intern/internal/doctor"
)

type doctorFinding struct {
	Level   string
	Area    string
	Message string
}

type doctorArgs struct {
	Strict      bool
	JSON        bool
	Fix         bool
	Interactive bool
	Probe       bool
	Severity    intdoctor.Severity
	Areas       []string
	FixableOnly bool
}

func parseDoctorArgs(args []string, stderr io.Writer) (doctorArgs, error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	strict := fs.Bool("strict", false, "exit non-zero when warnings are found")
	jsonOut := fs.Bool("json", false, "emit structured JSON output")
	fix := fs.Bool("fix", false, "apply safe fixes")
	interactive := fs.Bool("interactive", false, "use guided repair prompts for ambiguous fixes")
	probe := fs.Bool("probe", false, "run bounded local runtime probes")
	severity := fs.String("severity", "", "minimum severity filter: info, warn, error, block")
	fixableOnly := fs.Bool("fixable-only", false, "show only findings with available fixes")
	var areas stringSliceFlag
	fs.Var(&areas, "area", "repeatable area filter")
	if err := fs.Parse(args); err != nil {
		return doctorArgs{}, err
	}
	if fs.NArg() > 0 {
		return doctorArgs{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	minSeverity := intdoctor.NormalizeSeverity(*severity)
	if *severity != "" && minSeverity == "" {
		return doctorArgs{}, fmt.Errorf("invalid --severity %q", *severity)
	}
	return doctorArgs{
		Strict:      *strict,
		JSON:        *jsonOut,
		Fix:         *fix,
		Interactive: *interactive,
		Probe:       *probe,
		Severity:    minSeverity,
		Areas:       []string(areas),
		FixableOnly: *fixableOnly,
	}, nil
}

func runDoctorCommand(cfgPath string, cfg config.Config, validationError string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	parsed, err := parseDoctorArgs(args, stderr)
	if err != nil {
		return err
	}

	report := intdoctor.Evaluate(cfg, intdoctor.Options{
		Mode:            chooseDoctorMode(parsed.Strict),
		ConfigPath:      cfgPath,
		ValidationError: validationError,
		Probe:           parsed.Probe,
	})
	currentValidationError := validationError

	if parsed.Fix {
		applied, fixErr := intdoctor.ApplyAutomaticFixes(cfgPath, &cfg, report)
		if fixErr != nil {
			return fixErr
		}
		currentValidationError = refreshDoctorValidationError(cfgPath, currentValidationError)
		report = intdoctor.Evaluate(cfg, intdoctor.Options{
			Mode:            chooseDoctorMode(parsed.Strict),
			ConfigPath:      cfgPath,
			ValidationError: currentValidationError,
			Probe:           parsed.Probe,
		})
		if parsed.Interactive {
			appliedInteractive, interactiveErr := applyInteractiveDoctorFixes(stdin, stdout, cfgPath, &cfg, report)
			if interactiveErr != nil {
				return interactiveErr
			}
			applied = append(applied, appliedInteractive...)
		}
		currentValidationError = refreshDoctorValidationError(cfgPath, currentValidationError)
		report = intdoctor.Evaluate(cfg, intdoctor.Options{
			Mode:            chooseDoctorMode(parsed.Strict),
			ConfigPath:      cfgPath,
			ValidationError: currentValidationError,
			Probe:           parsed.Probe,
		})
		report.FixesApplied = applied
	}

	report = report.Filter(intdoctor.FilterOptions{
		Areas:       parsed.Areas,
		MinSeverity: parsed.Severity,
		FixableOnly: parsed.FixableOnly,
	})

	if parsed.JSON {
		payload, err := intdoctor.RenderJSON(report)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, string(payload))
	} else {
		_, _ = io.WriteString(stdout, intdoctor.RenderText(report))
	}

	if parsed.Strict && report.HasStrictFailures() {
		return fmt.Errorf("doctor found warnings")
	}
	return nil
}

func chooseDoctorMode(strict bool) intdoctor.Mode {
	if strict {
		return intdoctor.ModeStrict
	}
	return intdoctor.ModeAdvisory
}

func refreshDoctorValidationError(cfgPath, previous string) string {
	if strings.TrimSpace(cfgPath) == "" {
		return previous
	}
	if _, err := config.Load(cfgPath); err != nil {
		return err.Error()
	}
	return ""
}

func doctorFindings(cfg config.Config) []doctorFinding {
	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeAdvisory})
	items := make([]doctorFinding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		items = append(items, doctorFinding{
			Level:   string(finding.Severity),
			Area:    finding.Area,
			Message: finding.Summary,
		})
	}
	return items
}

func applyInteractiveDoctorFixes(in io.Reader, out io.Writer, cfgPath string, cfg *config.Config, report intdoctor.Report) ([]intdoctor.AppliedFix, error) {
	reader := bufio.NewReader(in)
	applied := []intdoctor.AppliedFix{}
	for _, finding := range report.Findings {
		if finding.FixMode != intdoctor.FixModeInteractive {
			continue
		}
		changed, summary, err := applySingleInteractiveDoctorFix(reader, out, cfg, finding)
		if err != nil {
			return applied, err
		}
		if changed {
			applied = append(applied, intdoctor.AppliedFix{ID: finding.ID, Summary: summary})
		}
	}
	if len(applied) > 0 {
		if err := config.Save(cfgPath, *cfg); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

func applySingleInteractiveDoctorFix(reader *bufio.Reader, out io.Writer, cfg *config.Config, finding intdoctor.Finding) (bool, string, error) {
	switch finding.ID {
	case "channels.invalid_ingress":
		channel := finding.Metadata["channel"]
		choice, err := promptMenuChoice(reader, out, fmt.Sprintf("Repair %s inbound access", channel), []string{
			"1) Pairing (secure default for interactive channels)",
			"2) Allowlist (specify allowed identities now)",
			"3) Open access",
			"4) Deny inbound (send-only)",
			"5) Disable channel",
			"6) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		var mode string
		var allowlist []string
		switch choice {
		case "1":
			mode = "pairing"
		case "2":
			mode = "allowlist"
			text, err := promptString(reader, out, fmt.Sprintf("%s allowlist (comma-separated)", channel), "")
			if err != nil {
				return false, "", err
			}
			allowlist = splitAndCompact(text)
		case "3":
			mode = "open"
		case "4":
			mode = "deny"
		case "5":
			mode = "disable"
		default:
			return false, "", nil
		}
		changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, mode, allowlist)
		if err != nil {
			return false, "", err
		}
		return changed, fmt.Sprintf("updated %s inbound access", channel), nil
	case "service.secret_missing", "service.secret_weak":
		choice, err := promptMenuChoice(reader, out, "Repair service secret", []string{
			"1) Generate a strong random secret",
			"2) Disable service mode",
			"3) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		switch choice {
		case "1":
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "generate", nil)
			return changed, "generated a service secret", err
		case "2":
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "disable", nil)
			return changed, "disabled service mode", err
		default:
			return false, "", nil
		}
	case "webhook.secret_missing":
		choice, err := promptMenuChoice(reader, out, "Repair webhook secret", []string{
			"1) Generate a strong random secret",
			"2) Disable webhook",
			"3) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		switch choice {
		case "1":
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "generate", nil)
			return changed, "generated a webhook secret", err
		case "2":
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "disable", nil)
			return changed, "disabled webhook", err
		default:
			return false, "", nil
		}
	case "service.public_bind":
		choice, err := promptMenuChoice(reader, out, "Repair service bind address", []string{
			"1) Bind to loopback",
			"2) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		if choice == "1" {
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "loopback", nil)
			return changed, "bound service to loopback", err
		}
	case "webhook.public_bind":
		choice, err := promptMenuChoice(reader, out, "Repair webhook bind address", []string{
			"1) Bind to loopback",
			"2) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		if choice == "1" {
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "loopback", nil)
			return changed, "bound webhook to loopback", err
		}
	case "security.secret_store_disabled_with_integrations":
		choice, err := promptMenuChoice(reader, out, "Repair secret store for external integrations", []string{
			"1) Enable secret store and generate a key file",
			"2) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		if choice == "1" {
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "enable", nil)
			return changed, "enabled secret store and generated a key file", err
		}
	case "privileged-exec.sandbox_disabled":
		choice, err := promptMenuChoice(reader, out, "Repair privileged tools without sandboxing", []string{
			"1) Disable privileged tools",
			"2) Enable Bubblewrap sandboxing",
			"3) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		switch choice {
		case "1":
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "disable_privileged", nil)
			return changed, "disabled privileged tools", err
		case "2":
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "enable_sandbox", nil)
			return changed, "enabled Bubblewrap sandboxing", err
		default:
			return false, "", nil
		}
	case "privileged-exec.bubblewrap_missing":
		choice, err := promptMenuChoice(reader, out, "Repair missing Bubblewrap binary", []string{
			"1) Disable privileged tools and sandboxing",
			"2) Set Bubblewrap path manually",
			"3) Skip",
		}, "1")
		if err != nil {
			return false, "", err
		}
		switch choice {
		case "1":
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "disable_privileged", nil)
			return changed, "disabled privileged tools and sandboxing", err
		case "2":
			path, err := promptString(reader, out, "Bubblewrap path", cfg.Hardening.Sandbox.BubblewrapPath)
			if err != nil {
				return false, "", err
			}
			changed, err := intdoctor.ApplyInteractiveChoice(cfg, finding, "set_path", []string{path})
			return changed, "updated Bubblewrap path", err
		default:
			return false, "", nil
		}
	}
	return false, "", nil
}
