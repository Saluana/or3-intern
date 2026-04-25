package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/uxcopy"
	"or3-intern/internal/uxstate"
)

func parseStatusArgs(args []string, rootAdvanced bool) (bool, error) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	detailed := rootAdvanced
	fs.BoolVar(&detailed, "advanced", rootAdvanced, "include internal finding IDs")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	if fs.NArg() > 0 {
		return false, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return detailed, nil
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
	if len(view.Problems) == 0 {
		fmt.Fprintln(stdout, "\nEverything looks ready.")
		return nil
	}
	fmt.Fprintf(stdout, "\n%d thing(s) need attention:\n", len(view.Problems))
	for _, problem := range view.Problems {
		fmt.Fprintf(stdout, "\n- %s\n", problem.Title)
		fmt.Fprintf(stdout, "  Why it matters: %s\n", problem.WhyItMatters)
		fmt.Fprintf(stdout, "  Fix: %s\n", problem.RecommendedAction)
		if detailed {
			fmt.Fprintf(stdout, "  Advanced ID: %s\n", problem.ID)
		}
	}
	return nil
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
