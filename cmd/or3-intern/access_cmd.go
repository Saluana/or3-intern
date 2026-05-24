package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/config"
)

func runAccessCommand(ctx context.Context, cfgPath string, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	_ = ctx
	_ = stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if len(args) == 0 || isHelpToken(args[0]) {
		printAccessUsage(stdout)
		return nil
	}
	next := cfg
	changed := false
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "show":
		printAccessSummary(stdout, next)
		return nil
	case "defaults":
		config.EnsureBuiltinAccessProfiles(&next.Security.Profiles)
		next.Security.Profiles.Enabled = true
		changed = true
		if len(args) > 1 {
			if !config.SetDefaultAccessLevel(&next.Security.Profiles, args[1]) {
				return newUsageError("invalid access level %q; use reader, operator, or admin", args[1])
			}
			applyAccessLevelRuntimeRequirements(&next, args[1])
		}
	case "channel":
		if len(args) != 3 {
			return newUsageError("usage: or3-intern access channel <channel> <reader|operator|admin>")
		}
		if !config.SetChannelAccessLevel(&next.Security.Profiles, args[1], args[2]) {
			return newUsageError("invalid access command; channel and level are required")
		}
		applyAccessLevelRuntimeRequirements(&next, args[2])
		changed = true
	case "default":
		if len(args) != 2 {
			return newUsageError("usage: or3-intern access default <reader|operator|admin>")
		}
		if !config.SetDefaultAccessLevel(&next.Security.Profiles, args[1]) {
			return newUsageError("invalid access level %q; use reader, operator, or admin", args[1])
		}
		applyAccessLevelRuntimeRequirements(&next, args[1])
		changed = true
	default:
		return newUsageError("unknown access command %q", args[0])
	}
	if !changed {
		return nil
	}
	if strings.TrimSpace(cfgPath) == "" {
		return fmt.Errorf("config path required")
	}
	if err := config.Save(cfgPath, next); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Access settings updated.")
	printAccessSummary(stdout, next)
	return nil
}

func printAccessUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  or3-intern access show")
	fmt.Fprintln(out, "  or3-intern access defaults [reader|operator|admin]")
	fmt.Fprintln(out, "  or3-intern access default <reader|operator|admin>")
	fmt.Fprintln(out, "  or3-intern access channel <telegram|discord|slack|email|whatsapp|service> <reader|operator|admin>")
}

func printAccessSummary(out io.Writer, cfg config.Config) {
	profiles := cfg.Security.Profiles
	fmt.Fprintf(out, "access_profiles: enabled=%t default=%s\n", profiles.Enabled, strings.TrimSpace(profiles.Default))
	for _, channel := range []string{"service", "telegram", "discord", "slack", "email", "whatsapp"} {
		if level := strings.TrimSpace(profiles.Channels[channel]); level != "" {
			fmt.Fprintf(out, "  %s: %s\n", channel, level)
		}
	}
}

func applyAccessLevelRuntimeRequirements(cfg *config.Config, level string) {
	switch config.NormalizeAccessLevel(level) {
	case config.AccessLevelAdmin:
		cfg.Service.MaxCapability = "privileged"
		cfg.Hardening.GuardedTools = true
		cfg.Hardening.PrivilegedTools = true
		cfg.Tools.EnableExec = true
	case config.AccessLevelOperator:
		cfg.Service.MaxCapability = "guarded"
		cfg.Hardening.GuardedTools = true
		cfg.Tools.EnableExec = true
	}
}
