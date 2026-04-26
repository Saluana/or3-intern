package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/uxcopy"
	"or3-intern/internal/uxstate"
)

type statusArgs struct {
	Detailed bool
	FixID    string
}

func parseStatusArgs(args []string, rootAdvanced bool) (bool, error) {
	parsed, err := parseStatusCommandArgs(args, rootAdvanced)
	return parsed.Detailed, err
}

func parseStatusCommandArgs(args []string, rootAdvanced bool) (statusArgs, error) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	detailed := rootAdvanced
	fixID := ""
	fs.BoolVar(&detailed, "advanced", rootAdvanced, "include internal finding IDs")
	fs.StringVar(&fixID, "fix", "", "apply one safe automatic fix by finding ID")
	if err := fs.Parse(args); err != nil {
		return statusArgs{}, err
	}
	if fs.NArg() > 0 {
		return statusArgs{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return statusArgs{Detailed: detailed, FixID: strings.TrimSpace(fixID)}, nil
}

func runStatusCommandWithOptions(cfgPath string, cfg config.Config, validationError string, database *db.DB, stdout io.Writer, args statusArgs) error {
	if args.FixID != "" {
		report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeAdvisory, ValidationError: validationError, ConfigPath: cfgPath})
		selected, label, err := selectStatusFixes(report.Findings, args.FixID)
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			return fmt.Errorf("no safe automatic repair is available for %q", args.FixID)
		}
		applied, err := intdoctor.ApplyAutomaticFixes(cfgPath, &cfg, intdoctor.NewReport(intdoctor.ModeAdvisory, selected))
		if err != nil {
			return err
		}
		for _, fix := range applied {
			fmt.Fprintf(stdout, "Applied fix for %s: %s\n", fix.ID, fix.Summary)
		}
		if len(applied) == 0 {
			fmt.Fprintf(stdout, "No changes needed for %s.\n", label)
		}
		if loaded, err := config.Load(cfgPath); err == nil {
			cfg = loaded
		}
	}
	return runStatusCommand(cfg, validationError, database, stdout, args.Detailed)
}

func runStatusCommand(cfg config.Config, validationError string, database *db.DB, stdout io.Writer, detailed bool) error {
	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeAdvisory, ValidationError: validationError})
	deviceCount, pendingApprovals := 0, 0
	if database != nil {
		ctx := context.Background()
		if devices, err := database.ListPairedDevices(ctx, 100); err == nil {
			deviceCount = len(devices)
		}
		if approvals, err := database.ListApprovalRequests(ctx, "pending", 100); err == nil {
			pendingApprovals = len(approvals)
		}
	}
	view := uxstate.BuildStatusView(cfg, report, deviceCount, pendingApprovals)
	fmt.Fprintln(stdout, "OR3 status")
	fmt.Fprintf(stdout, "State: %s\n", view.Headline)
	fmt.Fprintf(stdout, "Safety: %s — %s\n", view.SafetyLabel, view.SafetySummary)
	fmt.Fprintf(stdout, "Files: %s\n", view.Workspace)
	fmt.Fprintf(stdout, "Commands: %s\n", view.Commands)
	fmt.Fprintf(stdout, "Internet: %s\n", view.Internet)
	fmt.Fprintf(stdout, "Devices: %s\n", view.Devices)
	fmt.Fprintf(stdout, "Activity log: %s\n", view.ActivityLog)
	if detailed {
		fmt.Fprintf(stdout, "Context: mode=%s maxInputTokens=%d outputReserve=%d dynamicTools=%v\n", cfg.Context.Mode, cfg.Context.MaxInputTokens, cfg.Context.OutputReserveTokens, cfg.Context.Tools.DynamicExpose)
	}
	fmt.Fprintln(stdout, "\nWhat OR3 can access")
	for _, section := range view.Access.Sections {
		fmt.Fprintf(stdout, "- %s: %s [%s]\n", section.Name, section.Status, section.Risk)
		fmt.Fprintf(stdout, "  Change: %s\n", section.Action)
		if detailed {
			fmt.Fprintf(stdout, "  Detail: %s\n", section.Detail)
		}
	}
	if len(view.Problems) == 0 {
		fmt.Fprintln(stdout, "\nEverything looks ready.")
		return nil
	}
	fmt.Fprintf(stdout, "\n%d thing(s) need attention:\n", len(view.Problems))
	for index, problem := range view.Problems {
		number := index + 1
		fmt.Fprintf(stdout, "\n%d. %s\n", number, problem.Title)
		fmt.Fprintf(stdout, "  Why it matters: %s\n", problem.WhyItMatters)
		fmt.Fprintf(stdout, "  Fix: %s\n", problem.RecommendedAction)
		if problem.FixMode == string(intdoctor.FixModeAutomatic) {
			fmt.Fprintf(stdout, "  Run: or3-intern status --fix %d\n", number)
		}
		fmt.Fprintln(stdout, "  Keep as-is: leave it unchanged if this is intentional, then review advanced details before exposing OR3 to other devices or channels.")
		if detailed {
			fmt.Fprintf(stdout, "  Advanced ID: %s\n", problem.ID)
			if problem.FixMode == string(intdoctor.FixModeAutomatic) {
				fmt.Fprintf(stdout, "  Fix now: or3-intern status --fix %s\n", problem.ID)
			}
		}
	}
	return nil
}

func selectStatusFixes(findings []intdoctor.Finding, raw string) ([]intdoctor.Finding, string, error) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "all") {
		selected := []intdoctor.Finding{}
		for _, finding := range findings {
			if finding.FixMode == intdoctor.FixModeAutomatic {
				selected = append(selected, finding)
			}
		}
		return selected, "all", nil
	}
	if index, err := strconv.Atoi(raw); err == nil {
		if index < 1 || index > len(findings) {
			return nil, raw, fmt.Errorf("unknown problem number %q", raw)
		}
		finding := findings[index-1]
		if finding.FixMode != intdoctor.FixModeAutomatic {
			return nil, raw, fmt.Errorf("problem %d does not support safe automatic repair; run `or3-intern doctor --fix --interactive` if guided repair is available", index)
		}
		return []intdoctor.Finding{finding}, raw, nil
	}
	for _, finding := range findings {
		if finding.ID == raw {
			if finding.FixMode != intdoctor.FixModeAutomatic {
				return nil, raw, fmt.Errorf("finding %q does not support safe automatic repair; run `or3-intern doctor --fix --interactive` if guided repair is available", raw)
			}
			return []intdoctor.Finding{finding}, raw, nil
		}
	}
	return nil, raw, fmt.Errorf("unknown finding ID %q", raw)
}

func translateAndPrintError(err error, out io.Writer) error {
	translated := uxcopy.TranslateError(err)
	if strings.TrimSpace(translated.Title) == "" {
		return err
	}
	fmt.Fprintf(out, "%s\n\nWhat happened:\n%s\n\nFix:\n%s\n", translated.Title, translated.WhatHappened, translated.Fix)
	if strings.TrimSpace(translated.Command) != "" {
		fmt.Fprintf(out, "\nTry:\n%s\n", translated.Command)
	}
	return nil
}
