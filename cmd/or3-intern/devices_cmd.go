package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func runDevicesCommand(ctx context.Context, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	if broker == nil || broker.DB == nil {
		return fmt.Errorf("approval broker is not configured")
	}
	appSvc := app.NewServiceApp(config.Config{}, nil, newCLIControlplane(broker))
	if len(args) == 0 {
		return fmt.Errorf("usage: devices <create|list|rotate|revoke|requests|approve|deny>")
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "create":
		fs := flag.NewFlagSet("devices create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		deviceID := fs.String("id", "or3-chat-local", "unique device ID")
		displayName := fs.String("name", "OR3 Chat", "display name")
		role := fs.String("role", approval.RoleOperator, "device role")
		channel := fs.String("channel", "", "optional channel binding")
		identity := fs.String("identity", "", "optional channel identity")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "devices create [--id id] [--name name] [--role role]"); err != nil {
			return err
		}
		id := strings.TrimSpace(*deviceID)
		if id == "" {
			return fmt.Errorf("device ID is required")
		}
		normalizedRole, err := validateDeviceRole(*role)
		if err != nil {
			return err
		}
		if (strings.TrimSpace(*channel) == "") != (strings.TrimSpace(*identity) == "") {
			return fmt.Errorf("--channel and --identity must be provided together")
		}
		if _, err := broker.DB.GetPairedDevice(ctx, id); err == nil {
			return fmt.Errorf("device %q already exists; use `or3-intern devices rotate %s` to issue a new token", id, id)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		metadata := pairingMetadata(*channel, *identity)
		device, token, err := broker.RotateDeviceToken(ctx, id, normalizedRole, strings.TrimSpace(*displayName), metadata)
		if err != nil {
			return err
		}
		printIssuedDeviceToken(stdout, "created", device, token)
		return nil

	case "list":
		if err := requireExactArgs(args[1:], 0, "devices list"); err != nil {
			return err
		}
		items, err := appSvc.ListDevices(ctx, 100)
		if err != nil {
			return err
		}
		printDevices(stdout, items)
		return nil

	case "rotate":
		rest, force, err := splitForceFlag(args[1:])
		if err != nil {
			return err
		}
		if err := requireExactArgs(rest, 1, "devices rotate <device-id> [--force]"); err != nil {
			return err
		}
		deviceID := strings.TrimSpace(rest[0])
		ok, err := confirmDeviceCredentialChange(stdout, force, "Rotate device token", deviceID, "The current token and active sessions for this device will stop working.")
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(stdout, "Canceled.")
			return nil
		}
		device, token, err := appSvc.RotateDevice(ctx, deviceID)
		if err != nil {
			return err
		}
		printIssuedDeviceToken(stdout, "rotated", device, token)
		return nil

	case "revoke":
		rest, force, err := splitForceFlag(args[1:])
		if err != nil {
			return err
		}
		if err := requireExactArgs(rest, 1, "devices revoke <device-id> [--force]"); err != nil {
			return err
		}
		deviceID := strings.TrimSpace(rest[0])
		ok, err := confirmDeviceCredentialChange(stdout, force, "Revoke device", deviceID, "This device token and its active sessions will stop working.")
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(stdout, "Canceled.")
			return nil
		}
		if err := appSvc.RevokeDevice(ctx, deviceID, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "revoked %s\n", deviceID)
		return nil

	case "requests":
		return runPairingCommand(ctx, broker, append([]string{"list"}, args[1:]...), stdout, stderr)
	case "approve":
		return runPairingCommand(ctx, broker, append([]string{"approve"}, args[1:]...), stdout, stderr)
	case "deny":
		return runPairingCommand(ctx, broker, append([]string{"deny"}, args[1:]...), stdout, stderr)
	default:
		return fmt.Errorf("unknown devices subcommand: %s", args[0])
	}
}

func runPairingCommand(ctx context.Context, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	if broker == nil || broker.DB == nil {
		return fmt.Errorf("approval broker is not configured")
	}
	appSvc := app.NewServiceApp(config.Config{}, nil, newCLIControlplane(broker))
	if len(args) == 0 {
		return fmt.Errorf("usage: pairing <request|list|approve-code|approve|deny|exchange>")
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "request":
		fs := flag.NewFlagSet("pairing request", flag.ContinueOnError)
		fs.SetOutput(stderr)
		deviceID := fs.String("id", "", "device ID; generated when omitted")
		displayName := fs.String("name", "", "device display name")
		role := fs.String("role", approval.RoleOperator, "requested device role")
		origin := fs.String("origin", "cli", "pairing request origin")
		channel := fs.String("channel", "", "optional channel binding")
		identity := fs.String("identity", "", "optional channel identity")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "pairing request [--id id] [--name name] [--role role] [--channel name --identity id]"); err != nil {
			return err
		}
		normalizedRole, err := validateDeviceRole(*role)
		if err != nil {
			return err
		}
		if (strings.TrimSpace(*channel) == "") != (strings.TrimSpace(*identity) == "") {
			return fmt.Errorf("--channel and --identity must be provided together")
		}
		req, code, err := appSvc.CreatePairingRequest(ctx, approval.PairingRequestInput{
			DeviceID:    strings.TrimSpace(*deviceID),
			DisplayName: strings.TrimSpace(*displayName),
			Role:        normalizedRole,
			Origin:      strings.TrimSpace(*origin),
			Metadata:    pairingMetadata(*channel, *identity),
		})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "request_id: %d\ndevice_id: %s\nstatus: %s\npairing_code: %s\nexpires: %s\n",
			req.ID, req.DeviceID, req.Status, code, formatCLIUnixMillis(req.ExpiresAt))
		if req.Status == approval.StatusPending {
			_, _ = fmt.Fprintf(stdout, "\nApprove it with: or3-intern pairing approve-code %s\n", code)
		}
		return nil

	case "list":
		if err := requireArgRange(args[1:], 0, 1, "pairing list [status]"); err != nil {
			return err
		}
		status := ""
		if len(args) > 1 {
			status = strings.TrimSpace(args[1])
		}
		items, err := appSvc.ListPairingRequests(ctx, status, 100)
		if err != nil {
			return err
		}
		printPairingRequests(stdout, items)
		return nil

	case "approve-code":
		if err := requireExactArgs(args[1:], 1, "pairing approve-code <code>"); err != nil {
			return err
		}
		req, err := appSvc.ApprovePairingRequestByCode(ctx, args[1], "cli")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "approved %d (%s)\n", req.ID, req.DeviceID)
		return nil

	case "approve":
		if err := requireExactArgs(args[1:], 1, "pairing approve <request-id>"); err != nil {
			return err
		}
		id, err := parsePairingRequestID(args[1])
		if err != nil {
			return err
		}
		req, err := appSvc.ApprovePairingRequest(ctx, id, "cli")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "approved %d (%s)\n", req.ID, req.DeviceID)
		return nil

	case "deny":
		if err := requireExactArgs(args[1:], 1, "pairing deny <request-id>"); err != nil {
			return err
		}
		id, err := parsePairingRequestID(args[1])
		if err != nil {
			return err
		}
		if err := appSvc.DenyPairingRequest(ctx, id, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "denied %d\n", id)
		return nil

	case "exchange":
		if err := requireExactArgs(args[1:], 2, "pairing exchange <request-id> <code>"); err != nil {
			return err
		}
		id, err := parsePairingRequestID(args[1])
		if err != nil {
			return err
		}
		device, token, err := appSvc.ExchangePairingCode(ctx, approval.PairingExchangeInput{RequestID: id, Code: args[2]})
		if err != nil {
			return err
		}
		printIssuedDeviceToken(stdout, "paired", device, token)
		return nil

	default:
		return fmt.Errorf("unknown pairing subcommand: %s", args[0])
	}
}

func validateDeviceRole(role string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case approval.RoleViewer, approval.RoleOperator, approval.RoleServiceClient, approval.RoleWebUI, approval.RoleNode, approval.RoleAdmin:
		return role, nil
	default:
		return "", fmt.Errorf("invalid device role %q; use viewer, operator, service-client, web-ui, node, or admin", role)
	}
}

func pairingMetadata(channel, identity string) map[string]any {
	channel = strings.ToLower(strings.TrimSpace(channel))
	identity = strings.TrimSpace(identity)
	if channel == "" && identity == "" {
		return nil
	}
	return map[string]any{"channel": channel, "identity": identity}
}

func parsePairingRequestID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid pairing request ID")
	}
	return id, nil
}

func printDevices(w io.Writer, items []db.PairedDeviceRecord) {
	if len(items) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DEVICE ID\tSTATUS\tROLE\tNAME\tLAST SEEN")
	for _, item := range items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", item.DeviceID, item.Status, item.Role, item.DisplayName, formatCLIUnixMillis(item.LastSeenAt))
	}
	_ = tw.Flush()
}

func printPairingRequests(w io.Writer, items []db.PairingRequestRecord) {
	if len(items) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tSTATUS\tDEVICE ID\tROLE\tNAME\tEXPIRES")
	for _, item := range items {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.DeviceID, item.Role, item.DisplayName, formatCLIUnixMillis(item.ExpiresAt))
	}
	_ = tw.Flush()
}

func printIssuedDeviceToken(w io.Writer, action string, device db.PairedDeviceRecord, token string) {
	_, _ = fmt.Fprintf(w, "%s %s (%s)\n\nToken (shown once):\n%s\n\nPaste this token into OR3 Chat when adding the host.\n", action, device.DeviceID, device.Role, token)
}

func formatCLIUnixMillis(value int64) string {
	if value <= 0 {
		return "-"
	}
	return time.UnixMilli(value).UTC().Format(time.RFC3339)
}

func confirmDeviceCredentialChange(stdout io.Writer, force bool, action, deviceID, consequence string) (bool, error) {
	stdinTTY, stdoutTTY := stdioIsTerminal(os.Stdin, stdout)
	return confirmDestructiveAction(destructiveConfirmation{
		Action:      action,
		ItemName:    deviceID,
		Consequence: consequence,
		Undo:        "There is no undo. Create or pair the device again to issue another token.",
		Force:       force,
		Stdin:       os.Stdin,
		Stdout:      stdout,
		StdinTTY:    stdinTTY,
		StdoutTTY:   stdoutTTY,
	})
}
