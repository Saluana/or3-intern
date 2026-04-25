package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/uxcopy"
	"or3-intern/internal/uxstate"
)

func runDevicesCommand(ctx context.Context, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if broker == nil {
		return fmt.Errorf("approval broker is not configured")
	}
	appSvc := app.NewServiceApp(nil, nil, nil, newCLIControlplane(broker))
	if len(args) == 0 {
		return fmt.Errorf("usage: devices <list|requests|approve|deny|revoke|rotate>")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		if err := requireExactArgs(args[1:], 0, "devices list"); err != nil {
			return err
		}
		items, err := appSvc.ListDevices(ctx, 100)
		if err != nil {
			return err
		}
		views := uxstate.BuildDeviceViews(items)
		for _, item := range items {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", item.DeviceID, item.Status, item.Role, item.DisplayName)
		}
		for _, view := range views {
			fmt.Fprintf(stdout, "  %s · %s · %s\n", view.Name, view.RoleLabel, view.Status)
		}
		return nil
	case "requests":
		if err := requireArgRange(args[1:], 0, 1, "devices requests [status]"); err != nil {
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
		for _, item := range items {
			fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Role, item.DeviceID, item.DisplayName)
			fmt.Fprintf(stdout, "  %s requested %s access\n", firstNonEmptyString(item.DisplayName, item.DeviceID), uxcopy.DeviceRoleLabel(item.Role))
		}
		return nil
	case "approve":
		if err := requireExactArgs(args[1:], 1, "devices approve <pairing-request-id>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid pairing request ID")
		}
		req, err := appSvc.ApprovePairingRequest(ctx, id, "cli")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "approved pairing request %d for %s\n", req.ID, req.DeviceID)
		return nil
	case "deny":
		if err := requireExactArgs(args[1:], 1, "devices deny <pairing-request-id>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid pairing request ID")
		}
		if err := appSvc.DenyPairingRequest(ctx, id, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "denied pairing request %d\n", id)
		return nil
	case "revoke":
		if err := requireExactArgs(args[1:], 1, "devices revoke <device-id>"); err != nil {
			return err
		}
		deviceID := strings.TrimSpace(args[1])
		if deviceID == "" {
			return fmt.Errorf("device ID required")
		}
		if err := appSvc.RevokeDevice(ctx, deviceID, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "revoked device %s\n", deviceID)
		return nil
	case "rotate":
		if err := requireExactArgs(args[1:], 1, "devices rotate <device-id>"); err != nil {
			return err
		}
		deviceID := strings.TrimSpace(args[1])
		if deviceID == "" {
			return fmt.Errorf("device ID required")
		}
		rotated, token, err := appSvc.RotateDevice(ctx, deviceID)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "rotated device %s\ntoken: %s\n", rotated.DeviceID, token)
		return nil
	default:
		return fmt.Errorf("unknown devices subcommand: %s", args[0])
	}
}
