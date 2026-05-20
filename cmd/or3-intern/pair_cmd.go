package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/uxcopy"
)

type pairAutoOptions struct {
	Auto       bool
	DeviceName string
	Role       string
	Manual     bool
	JSON       bool
	NoFix      bool
}

type pairAutoResult struct {
	Status         string   `json:"status"`
	DeviceName     string   `json:"device_name,omitempty"`
	Role           string   `json:"role,omitempty"`
	Code           string   `json:"code,omitempty"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
	AppInstruction string   `json:"app_instruction,omitempty"`
	AppliedFixes   []string `json:"applied_fixes,omitempty"`
	NextAction     string   `json:"next_action,omitempty"`
}

func parsePairArgs(args []string) (pairAutoOptions, error) {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	auto := fs.Bool("auto", false, "run automatic pairing with readiness checks")
	name := fs.String("name", "", "device display name")
	role := fs.String("role", "", "device role: viewer, operator, admin")
	manual := fs.Bool("manual", false, "use manual pairing flow")
	jsonOut := fs.Bool("json", false, "emit JSON output")
	noFix := fs.Bool("no-fix", false, "skip automatic fixes")
	if err := fs.Parse(args); err != nil {
		return pairAutoOptions{}, err
	}
	if fs.NArg() > 0 {
		return pairAutoOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if !*auto && !*manual && fs.NArg() == 0 {
		return pairAutoOptions{}, fmt.Errorf("use `pair --auto` for automatic pairing or `pair --manual` for manual pairing")
	}
	if *auto && *manual {
		return pairAutoOptions{}, fmt.Errorf("choose either `pair --auto` or `pair --manual`, not both")
	}
	roleValue := strings.ToLower(strings.TrimSpace(*role))
	if roleValue != "" && roleValue != approval.RoleViewer && roleValue != approval.RoleOperator && roleValue != approval.RoleAdmin {
		return pairAutoOptions{}, fmt.Errorf("invalid --role %q; use viewer, operator, or admin", *role)
	}
	return pairAutoOptions{
		Auto:       *auto,
		DeviceName: *name,
		Role:       roleValue,
		Manual:     *manual,
		JSON:       *jsonOut,
		NoFix:      *noFix,
	}, nil
}

func runPairCommand(ctx context.Context, cfgPath string, cfg *config.Config, database *db.DB, broker *approval.Broker, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list":
			return runConnectDeviceList(ctx, database, stdout)
		default:
			return fmt.Errorf("usage: pair --auto [--name device-name] [--role viewer|operator|admin]")
		}
	}
	opts, err := parsePairArgs(args)
	if err != nil {
		return err
	}
	if opts.Manual {
		return runConnectDeviceCommand(ctx, cfgPath, cfg, database, broker, nil, stdout, stderr)
	}
	return runPairAuto(ctx, cfgPath, cfg, database, broker, opts, stdin, stdout, stderr)
}

func runPairAuto(ctx context.Context, cfgPath string, cfg *config.Config, database *db.DB, broker *approval.Broker, opts pairAutoOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	if cfg == nil {
		return fmt.Errorf("config required")
	}
	if database == nil {
		return fmt.Errorf("device storage is not available")
	}
	if stdin == nil {
		stdin = os.Stdin
	}

	if !opts.JSON {
		fmt.Fprintln(stdout, "Pair a device")
		fmt.Fprintln(stdout)
	}

	// Step 1: Run health/readiness checks
	if !opts.JSON {
		fmt.Fprintln(stdout, "Checking readiness...")
	}
	report := intdoctor.Evaluate(*cfg, intdoctor.Options{
		Mode:       intdoctor.ModeAdvisory,
		ConfigPath: cfgPath,
	})

	appliedFixes := []string{}
	if !opts.NoFix {
		// Apply safe automatic fixes
		applied, fixErr := intdoctor.ApplyAutomaticFixes(cfgPath, cfg, report)
		if fixErr != nil {
			return fmt.Errorf("auto-fix error: %v", fixErr)
		}
		for _, fix := range applied {
			appliedFixes = append(appliedFixes, fix.Summary)
		}
		if len(applied) > 0 {
			if !opts.JSON {
				fmt.Fprintf(stdout, "Applied %d safe repair(s).\n", len(applied))
			}
			report = intdoctor.Evaluate(*cfg, intdoctor.Options{
				Mode:       intdoctor.ModeAdvisory,
				ConfigPath: cfgPath,
			})
		}
	}

	// Step 2: Check for blockers
	blockers := report.BlockingFindings()
	if len(blockers) > 0 {
		nextAction := "Run `or3-intern health --fix`, then try `or3-intern pair --auto` again."
		if opts.JSON {
			return writePairJSON(stdout, pairAutoResult{
				Status:       "blocked",
				AppliedFixes: appliedFixes,
				NextAction:   nextAction,
			})
		}
		fmt.Fprintln(stdout, "\nCannot pair yet. These issues need attention:")
		for _, finding := range intdoctor.TopFindings(blockers, 3) {
			fmt.Fprintf(stdout, "  - %s: %s\n", finding.Area, finding.Summary)
		}
		fmt.Fprintln(stdout, "\nNext: run `or3-intern health --fix` to repair safe issues, or `or3-intern health` for details.")
		return nil
	}

	// Step 3: Ensure prerequisites (service enabled, approvals, etc.)
	updatedBroker, err := ensureConnectDevicePrereqs(cfgPath, cfg, database, broker)
	if err != nil {
		return fmt.Errorf("prerequisite error: %v", err)
	}

	// Step 4: Get device name and role
	reader := bufio.NewReader(stdin)
	deviceName := opts.DeviceName
	if deviceName == "" && opts.JSON {
		deviceName = "My device"
	}
	if deviceName == "" {
		deviceName, err = promptString(reader, stdout, "Device name", "My device")
		if err != nil {
			return err
		}
	}

	role := opts.Role
	if role == "" && opts.JSON {
		role = approval.RoleViewer
	}
	if role == "" {
		choice, err := promptMenuChoice(reader, stdout, "Choose what this device can do", []string{
			"1) Chat only",
			"2) Chat and manage files in workspace",
			"3) Admin device",
		}, "1")
		if err != nil {
			return err
		}
		switch choice {
		case "2":
			role = "operator"
		case "3":
			role = "admin"
		default:
			role = "viewer"
		}
	}

	approvalRole := approval.RoleViewer
	switch strings.ToLower(role) {
	case "operator":
		approvalRole = approval.RoleOperator
	case "admin":
		approvalRole = approval.RoleAdmin
	}

	// Step 5: Run local pairing flow
	pairing, err := runLocalPairingFlow(ctx, updatedBroker, localPairingFlowInput{
		Role:        approvalRole,
		DisplayName: deviceName,
		Origin:      "pair --auto",
	})
	if err != nil {
		return fmt.Errorf("pairing error: %v", err)
	}

	// Step 6: Print success
	MarkMilestone(cfg, MilestonePairingComplete)
	if err := config.Save(cfgPath, *cfg); err != nil {
		return fmt.Errorf("config save error: %v", err)
	}

	formattedCode := formatPairingCode(pairing.Code)
	roleLabel := uxcopy.DeviceRoleLabel(approvalRole)
	if opts.JSON {
		return writePairJSON(stdout, pairAutoResult{
			Status:         "ready",
			DeviceName:     deviceName,
			Role:           roleLabel,
			Code:           formattedCode,
			ExpiresAt:      time.Unix(pairing.Request.ExpiresAt, 0).Format(time.RFC3339),
			AppInstruction: "Open the OR3 app and enter the code.",
			AppliedFixes:   appliedFixes,
			NextAction:     "Open the OR3 app and enter the code.",
		})
	}

	PrintPairingSuccess(stdout, deviceName, uxcopy.DeviceRoleLabel(approvalRole))

	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  Code: %s\n", formattedCode)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Next steps:")
	fmt.Fprintln(stdout, "  1. Open the OR3 app on your phone or other device")
	fmt.Fprintln(stdout, "  2. Enter the code shown above")
	fmt.Fprintln(stdout, "  3. Start chatting!")

	if len(appliedFixes) > 0 {
		fmt.Fprintln(stdout, "\nApplied repairs:")
		for _, fix := range appliedFixes {
			fmt.Fprintf(stdout, "  - %s\n", fix)
		}
	}

	return nil
}

func writePairJSON(stdout io.Writer, result pairAutoResult) error {
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, string(payload))
	return nil
}
