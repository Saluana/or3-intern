package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"or3-intern/internal/config"
)

const (
	MilestoneSetupComplete   = "setup_complete"
	MilestonePairingComplete = "pairing_complete"
	MilestoneFirstChatComplete = "first_chat_complete"
)

// MarkMilestone records a milestone completion in the config.
func MarkMilestone(cfg *config.Config, milestone string) {
	if cfg == nil {
		return
	}
	if cfg.Milestones == nil {
		cfg.Milestones = make(map[string]string)
	}
	cfg.Milestones[milestone] = time.Now().Format(time.RFC3339)
}

// HasMilestone checks if a milestone has been completed.
func HasMilestone(cfg config.Config, milestone string) bool {
	if cfg.Milestones == nil {
		return false
	}
	_, ok := cfg.Milestones[milestone]
	return ok
}

// PrintSetupSuccess prints the setup completion success message.
func PrintSetupSuccess(out io.Writer, cfg config.Config, readyToChat bool) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "You did it! OR3 is set up and ready to go.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "What you can do next:")
	fmt.Fprintln(out, "  1. Start chatting: `or3-intern chat`")
	fmt.Fprintln(out, "  2. Pair a phone or device: `or3-intern pair --auto`")
	fmt.Fprintln(out, "  3. Check system health: `or3-intern health`")
	fmt.Fprintln(out, "  4. Review settings: `or3-intern settings`")
}

// PrintPairingSuccess prints the pairing completion success message.
func PrintPairingSuccess(out io.Writer, deviceName, role string) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Device connected! %s is now linked as %s.\n", deviceName, role)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "What you can do next:")
	fmt.Fprintln(out, "  1. Open the OR3 app and start chatting")
	fmt.Fprintln(out, "  2. Check connected devices: `or3-intern connect-device list`")
}

// PrintFirstChatSuccess prints the first chat completion success message (one-time).
func PrintFirstChatSuccess(out io.Writer) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Nice! You just had your first conversation with OR3.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Tips:")
	fmt.Fprintln(out, "  - OR3 remembers your conversations for next time")
	fmt.Fprintln(out, "  - Use `or3-intern health` to check system status")
	fmt.Fprintln(out, "  - Use `or3-intern settings` to customize behavior")
}

// MilestoneCopy returns human-readable copy for a milestone.
func MilestoneCopy(milestone string) string {
	switch milestone {
	case MilestoneSetupComplete:
		return "Setup complete"
	case MilestonePairingComplete:
		return "Device paired"
	case MilestoneFirstChatComplete:
		return "First chat complete"
	default:
		return strings.ReplaceAll(milestone, "_", " ")
	}
}
