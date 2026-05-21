package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"or3-intern/internal/config"
	intdoctor "or3-intern/internal/doctor"
)

type healthArgs struct {
	Check       bool
	Fix         bool
	JSON        bool
	Interactive bool
	Probe       bool
	Severity    intdoctor.Severity
	Areas       []string
	FixableOnly bool
	Advanced    bool
}

func parseHealthArgs(args []string, stderr io.Writer) (healthArgs, error) {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "run the default readiness check (explicit)")
	fix := fs.Bool("fix", false, "apply safe fixes")
	jsonOut := fs.Bool("json", false, "emit structured JSON output")
	interactive := fs.Bool("interactive", false, "use guided repair prompts for ambiguous fixes")
	probe := fs.Bool("probe", false, "run bounded local runtime probes")
	severity := fs.String("severity", "", "minimum severity filter: info, warn, error, block")
	fixableOnly := fs.Bool("fixable-only", false, "show only findings with available fixes")
	advanced := fs.Bool("advanced", false, "show advanced filters and options")
	var areas stringSliceFlag
	fs.Var(&areas, "area", "repeatable area filter")
	if err := fs.Parse(args); err != nil {
		return healthArgs{}, err
	}
	if fs.NArg() > 0 {
		return healthArgs{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	minSeverity := intdoctor.NormalizeSeverity(*severity)
	if *severity != "" && minSeverity == "" {
		return healthArgs{}, fmt.Errorf("invalid --severity %q", *severity)
	}
	return healthArgs{
		Check:       *check,
		Fix:         *fix,
		JSON:        *jsonOut,
		Interactive: *interactive,
		Probe:       *probe,
		Severity:    minSeverity,
		Areas:       []string(areas),
		FixableOnly: *fixableOnly,
		Advanced:    *advanced,
	}, nil
}

func runHealthCommand(cfgPath string, cfg config.Config, validationError string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	parsed, err := parseHealthArgs(args, stderr)
	if err != nil {
		return err
	}

	// Health defaults to the check/readiness report (same as doctor without flags)
	report := intdoctor.Evaluate(cfg, intdoctor.Options{
		Mode:            chooseHealthMode(parsed.Advanced),
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
			Mode:            chooseHealthMode(parsed.Advanced),
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
			Mode:            chooseHealthMode(parsed.Advanced),
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

	if parsed.Advanced && report.HasStrictFailures() {
		return fmt.Errorf("health check found issues")
	}
	return nil
}

func chooseHealthMode(advanced bool) intdoctor.Mode {
	if advanced {
		return intdoctor.ModeStrict
	}
	return intdoctor.ModeAdvisory
}
