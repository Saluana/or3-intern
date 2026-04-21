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

func runApprovalsCommand(ctx context.Context, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	if broker == nil {
		return fmt.Errorf("approval broker is not configured")
	}
	cp := controlplane.New(config.Config{}, nil, broker, nil, nil)
	if len(args) == 0 {
		return fmt.Errorf("usage: approvals <list|show|approve|deny|cancel|expire|allowlist>")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		if err := requireArgRange(args[1:], 0, 1, "approvals list [status]"); err != nil {
			return err
		}
		status := ""
		if len(args) > 1 {
			status = strings.TrimSpace(args[1])
		}
		items, err := cp.ListApprovalRequests(ctx, controlplane.ApprovalFilter{Status: status, Limit: 100})
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\n", item.ID, item.Status, item.Type, item.SubjectHash)
		}
		return nil
	case "show":
		if err := requireExactArgs(args[1:], 1, "approvals show <id>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid approval ID")
		}
		item, err := cp.GetApproval(ctx, id)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "id: %d\nstatus: %s\ntype: %s\nsubject_hash: %s\npolicy_mode: %s\nsubject_json: %s\n", item.ID, item.Status, item.Type, item.SubjectHash, item.PolicyMode, item.SubjectJSON)
		return nil
	case "approve":
		fs := flag.NewFlagSet("approvals approve", flag.ContinueOnError)
		fs.SetOutput(stderr)
		alwaysAllow := fs.Bool("allowlist", false, "create a matching allowlist rule")
		note := fs.String("note", "", "resolution note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 1, "approvals approve <id> [--allowlist] [--note text]"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(fs.Arg(0)), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid approval ID")
		}
		issued, err := cp.ApproveApproval(ctx, id, "cli", *alwaysAllow, *note)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "approved %d\ntoken: %s\n", id, issued.Token)
		if issued.AllowlistID > 0 {
			_, _ = fmt.Fprintf(stdout, "allowlist_id: %d\n", issued.AllowlistID)
		}
		return nil
	case "deny":
		fs := flag.NewFlagSet("approvals deny", flag.ContinueOnError)
		fs.SetOutput(stderr)
		note := fs.String("note", "", "resolution note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 1, "approvals deny <id> [--note text]"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(fs.Arg(0)), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid approval ID")
		}
		if err := cp.DenyApproval(ctx, id, "cli", *note); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "denied %d\n", id)
		return nil
	case "cancel":
		fs := flag.NewFlagSet("approvals cancel", flag.ContinueOnError)
		fs.SetOutput(stderr)
		note := fs.String("note", "", "resolution note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 1, "approvals cancel <id> [--note text]"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(fs.Arg(0)), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid approval ID")
		}
		if err := cp.CancelApproval(ctx, id, "cli", *note); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "canceled %d\n", id)
		return nil
	case "expire":
		if err := requireExactArgs(args[1:], 0, "approvals expire"); err != nil {
			return err
		}
		expired, err := cp.ExpireApprovals(ctx, "cli")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "expired %d pending request(s)\n", expired)
		return nil
	case "allowlist":
		return runApprovalAllowlistCommand(ctx, cp, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown approvals subcommand: %s", args[0])
	}
}

func runApprovalAllowlistCommand(ctx context.Context, cp *controlplane.Service, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: approvals allowlist <list|add|remove>")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		if err := requireArgRange(args[1:], 0, 1, "approvals allowlist list [domain]"); err != nil {
			return err
		}
		domain := ""
		if len(args) > 1 {
			domain = strings.TrimSpace(args[1])
		}
		items, err := cp.ListAllowlists(ctx, domain, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Fprintf(stdout, "%d\t%s\t%s\n", item.ID, item.Domain, item.ScopeJSON)
		}
		return nil
	case "remove":
		if err := requireExactArgs(args[1:], 1, "approvals allowlist remove <id>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid allowlist ID")
		}
		if err := cp.RemoveAllowlist(ctx, id, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "removed allowlist %d\n", id)
		return nil
	case "add":
		fs := flag.NewFlagSet("approvals allowlist add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		domain := fs.String("domain", "exec", "approval domain")
		hostID := fs.String("host", cp.Broker.HostID, "host scope")
		toolName := fs.String("tool", "", "tool scope")
		profile := fs.String("profile", "", "profile scope")
		agent := fs.String("agent", "", "agent scope")
		program := fs.String("program", "", "exec executable path")
		cwd := fs.String("cwd", "", "exec working directory")
		skillID := fs.String("skill", "", "skill identifier")
		version := fs.String("version", "", "skill version")
		origin := fs.String("origin", "", "skill origin")
		trust := fs.String("trust", "", "skill trust state")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "approvals allowlist add [--domain exec|skill_execution] [matcher flags]"); err != nil {
			return err
		}
		scope := approval.AllowlistScope{HostID: strings.TrimSpace(*hostID), Tool: strings.TrimSpace(*toolName), Profile: strings.TrimSpace(*profile), Agent: strings.TrimSpace(*agent)}
		var matcher any
		domainName := strings.TrimSpace(*domain)
		switch strings.TrimSpace(*domain) {
		case string(approval.SubjectExec):
			matcher = approval.ExecAllowlistMatcher{ExecutablePath: strings.TrimSpace(*program), WorkingDir: strings.TrimSpace(*cwd)}
		case string(approval.SubjectSkillExec):
			matcher = approval.SkillAllowlistMatcher{SkillID: strings.TrimSpace(*skillID), Version: strings.TrimSpace(*version), Origin: strings.TrimSpace(*origin), TrustState: strings.TrimSpace(*trust)}
		default:
			return fmt.Errorf("unsupported allowlist domain")
		}
		if err := approval.ValidateAllowlistMatcher(domainName, matcher); err != nil {
			return err
		}
		rec, err := cp.AddAllowlist(ctx, strings.TrimSpace(*domain), scope, matcher, "cli", 0)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "added allowlist %d\n", rec.ID)
		return nil
	default:
		return fmt.Errorf("unknown allowlist subcommand: %s", args[0])
	}
}
