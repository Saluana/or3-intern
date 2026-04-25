package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/security"
	"or3-intern/internal/uxcopy"
	"or3-intern/internal/uxstate"
)

func runConnectDeviceCommand(ctx context.Context, cfgPath string, cfg *config.Config, database *db.DB, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list":
			return runConnectDeviceList(ctx, database, stdout)
		case "disconnect":
			if len(args) != 2 {
				return fmt.Errorf("usage: connect-device disconnect <device-id>")
			}
			if broker == nil {
				return fmt.Errorf("approval broker unavailable")
			}
			if err := broker.RevokeDevice(ctx, strings.TrimSpace(args[1]), "cli"); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Disconnected %s\n", strings.TrimSpace(args[1]))
			return nil
		default:
			return fmt.Errorf("usage: connect-device [list|disconnect <device-id>]")
		}
	}
	if cfg == nil {
		return fmt.Errorf("config required")
	}
	if database == nil {
		return fmt.Errorf("device storage is not available")
	}
	updatedBroker, err := ensureConnectDevicePrereqs(cfgPath, cfg, database, broker)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	choice, err := promptMenuChoice(reader, stdout, "Choose what this device can do", []string{
		"1) Chat only",
		"2) Chat and manage files in workspace",
		"3) Admin device",
	}, "1")
	if err != nil {
		return err
	}
	name, err := promptString(reader, stdout, "Device name", "My device")
	if err != nil {
		return err
	}
	role := approval.RoleViewer
	if choice == "2" {
		role = approval.RoleOperator
	}
	if choice == "3" {
		role = approval.RoleAdmin
	}
	req, code, err := updatedBroker.CreatePairingRequest(ctx, approval.PairingRequestInput{Role: role, DisplayName: name, Origin: "connect-device"})
	if err != nil {
		return err
	}
	if req.Status == approval.StatusPending {
		req, err = updatedBroker.ApprovePairingRequest(ctx, req.ID, "cli")
		if err != nil {
			return err
		}
	}
	fmt.Fprintln(stdout, "Connect a device")
	fmt.Fprintln(stdout, "\nStep 1: Open OR3 on your phone or other device.")
	fmt.Fprintln(stdout, "Step 2: Enter this code:")
	fmt.Fprintf(stdout, "\n  %s\n\n", formatPairingCode(code))
	fmt.Fprintf(stdout, "Step 3: This device will be connected as: %s\n", uxcopy.DeviceRoleLabel(role))
	fmt.Fprintf(stdout, "Request ID: %d\n", req.ID)
	fmt.Fprintln(stdout, "After the remote device enters the code, use `or3-intern connect-device list` to review connected devices.")
	return nil
}

func runConnectDeviceList(ctx context.Context, database *db.DB, stdout io.Writer) error {
	if database == nil {
		return fmt.Errorf("device storage is not available")
	}
	items, err := database.ListPairedDevices(ctx, 100)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No connected devices yet.")
		return nil
	}
	views := uxstate.BuildDeviceViews(items)
	fmt.Fprintln(stdout, "Connected devices")
	for _, view := range views {
		fmt.Fprintf(stdout, "\n- %s\n", view.Name)
		fmt.Fprintf(stdout, "  Role: %s\n", view.RoleLabel)
		fmt.Fprintf(stdout, "  Status: %s\n", view.Status)
		fmt.Fprintf(stdout, "  Disconnect: or3-intern connect-device disconnect %s\n", view.DeviceID)
	}
	return nil
}

func ensureConnectDevicePrereqs(cfgPath string, cfg *config.Config, database *db.DB, broker *approval.Broker) (*approval.Broker, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	changed := false
	if !cfg.Service.Enabled {
		cfg.Service.Enabled = true
		changed = true
	}
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		secret, err := intdoctor.GenerateSecret()
		if err != nil {
			return nil, err
		}
		cfg.Service.Secret = secret
		changed = true
	}
	if !cfg.Security.Approvals.Enabled {
		cfg.Security.Approvals.Enabled = true
		changed = true
	}
	if cfg.Security.Approvals.Pairing.Mode == config.ApprovalModeDeny {
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeAsk
		changed = true
	}
	if strings.TrimSpace(cfg.Security.Approvals.KeyFile) == "" {
		cfg.Security.Approvals.KeyFile = config.Default().Security.Approvals.KeyFile
		changed = true
	}
	if _, err := security.LoadOrCreateKey(cfg.Security.Approvals.KeyFile); err != nil {
		return nil, err
	}
	if changed {
		if err := config.Save(cfgPath, *cfg); err != nil {
			return nil, err
		}
	}
	if broker != nil && broker.DB == database && strings.TrimSpace(broker.Config.KeyFile) == strings.TrimSpace(cfg.Security.Approvals.KeyFile) {
		broker.Config = cfg.Security.Approvals
		return broker, nil
	}
	key, err := os.ReadFile(cfg.Security.Approvals.KeyFile)
	if err != nil {
		return nil, err
	}
	return &approval.Broker{DB: database, Config: cfg.Security.Approvals, HostID: cfg.Security.Approvals.HostID, SignKey: key}, nil
}

func formatPairingCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return code
	}
	return code[:3] + "-" + code[3:]
}
