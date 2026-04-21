package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
)

func runPairingCommand(ctx context.Context, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	if broker == nil {
		return fmt.Errorf("approval broker is not configured")
	}
	cp := controlplane.New(config.Config{}, nil, broker, nil, nil)
	if len(args) == 0 {
		return fmt.Errorf("usage: pairing <list|request|approve|deny|exchange>")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		if err := requireArgRange(args[1:], 0, 1, "pairing list [status]"); err != nil {
			return err
		}
		status := ""
		if len(args) > 1 {
			status = strings.TrimSpace(args[1])
		}
		items, err := cp.ListPairingRequests(ctx, status, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Role, item.DeviceID, item.DisplayName, item.Origin)
		}
		return nil
	case "request":
		fs := flag.NewFlagSet("pairing request", flag.ContinueOnError)
		fs.SetOutput(stderr)
		role := fs.String("role", approval.RoleOperator, "device role")
		displayName := fs.String("name", "", "display name")
		origin := fs.String("origin", "", "origin description")
		deviceID := fs.String("device", "", "explicit device ID")
		channel := fs.String("channel", "", "channel name to bind")
		identity := fs.String("identity", "", "channel identity to bind")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "pairing request [--role role] [--name display] [--origin text] [--device id] [--channel name --identity value]"); err != nil {
			return err
		}
		metadata := map[string]any{}
		if strings.TrimSpace(*channel) != "" || strings.TrimSpace(*identity) != "" {
			if strings.TrimSpace(*channel) == "" || strings.TrimSpace(*identity) == "" {
				return fmt.Errorf("pairing request: --channel and --identity must be provided together")
			}
			metadata["channel"] = strings.ToLower(strings.TrimSpace(*channel))
			metadata["identity"] = strings.TrimSpace(*identity)
			if strings.TrimSpace(*deviceID) == "" {
				*deviceID = strings.ToLower(strings.TrimSpace(*channel)) + ":" + strings.TrimSpace(*identity)
			}
		}
		req, code, err := cp.CreatePairingRequest(ctx, approval.PairingRequestInput{
			Role:        strings.TrimSpace(*role),
			DisplayName: strings.TrimSpace(*displayName),
			Origin:      strings.TrimSpace(*origin),
			Metadata:    metadata,
			DeviceID:    strings.TrimSpace(*deviceID),
		})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "id: %d\nstatus: %s\ndevice_id: %s\nrole: %s\ncode: %s\n", req.ID, req.Status, req.DeviceID, req.Role, code)
		return nil
	case "approve":
		if err := requireExactArgs(args[1:], 1, "pairing approve <request-id>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid pairing request ID")
		}
		req, err := cp.ApprovePairingRequest(ctx, id, "cli")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "approved pairing request %d for %s\n", req.ID, req.DeviceID)
		return nil
	case "deny":
		if err := requireExactArgs(args[1:], 1, "pairing deny <request-id>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid pairing request ID")
		}
		if err := cp.DenyPairingRequest(ctx, id, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "denied pairing request %d\n", id)
		return nil
	case "exchange":
		if err := requireExactArgs(args[1:], 2, "pairing exchange <request-id> <code>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid pairing request ID")
		}
		device, token, err := cp.ExchangePairingCode(ctx, approval.PairingExchangeInput{
			RequestID: id,
			Code:      strings.TrimSpace(args[2]),
		})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "paired device %s\nrole: %s\ntoken: %s\n", device.DeviceID, device.Role, token)
		return nil
	default:
		return fmt.Errorf("unknown pairing subcommand: %s", args[0])
	}
}
