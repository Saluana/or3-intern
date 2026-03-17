package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"or3-intern/internal/approval"
)

func runDevicesCommand(ctx context.Context, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	_ = stderr
	if broker == nil {
		return fmt.Errorf("approval broker is not configured")
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: devices <list|requests|approve|deny|revoke|rotate>")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		items, err := broker.ListDevices(ctx, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", item.DeviceID, item.Status, item.Role, item.DisplayName)
		}
		return nil
	case "requests":
		status := ""
		if len(args) > 1 {
			status = strings.TrimSpace(args[1])
		}
		items, err := broker.ListPairingRequests(ctx, status, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Role, item.DeviceID, item.DisplayName)
		}
		return nil
	case "approve":
		if len(args) < 2 {
			return fmt.Errorf("usage: devices approve <pairing-request-id>")
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid pairing request ID")
		}
		req, err := broker.ApprovePairingRequest(ctx, id, "cli")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "approved pairing request %d for %s\n", req.ID, req.DeviceID)
		return nil
	case "deny":
		if len(args) < 2 {
			return fmt.Errorf("usage: devices deny <pairing-request-id>")
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid pairing request ID")
		}
		if err := broker.DenyPairingRequest(ctx, id, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "denied pairing request %d\n", id)
		return nil
	case "revoke":
		if len(args) < 2 {
			return fmt.Errorf("usage: devices revoke <device-id>")
		}
		deviceID := strings.TrimSpace(args[1])
		if deviceID == "" {
			return fmt.Errorf("device ID required")
		}
		if err := broker.RevokeDevice(ctx, deviceID, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "revoked device %s\n", deviceID)
		return nil
	case "rotate":
		if len(args) < 2 {
			return fmt.Errorf("usage: devices rotate <device-id>")
		}
		deviceID := strings.TrimSpace(args[1])
		if deviceID == "" {
			return fmt.Errorf("device ID required")
		}
		device, err := broker.DB.GetPairedDevice(ctx, deviceID)
		if err != nil {
			return err
		}
		rotated, token, err := broker.RotateDeviceToken(ctx, device.DeviceID, device.Role, device.DisplayName, device.Metadata)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "rotated device %s\ntoken: %s\n", rotated.DeviceID, token)
		return nil
	default:
		return fmt.Errorf("unknown devices subcommand: %s", args[0])
	}
}
