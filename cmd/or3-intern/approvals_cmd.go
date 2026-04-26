package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/uxstate"
)

func runApprovalsCommand(ctx context.Context, broker *approval.Broker, args []string, stdout, stderr io.Writer) error {
	if broker == nil {
		return fmt.Errorf("approval broker is not configured")
	}
	appSvc := app.NewServiceApp(nil, nil, nil, newCLIControlplane(broker))
	if len(args) == 0 {
		return fmt.Errorf("usage: approvals <list|show|approve|deny|cancel|expire|allowlist>\n\nFor phone/browser device pairing requests, use `or3-intern pairing` instead of `or3-intern approvals`")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		fs := flag.NewFlagSet("approvals list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		advanced := fs.Bool("advanced", false, "show raw approval details")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireArgRange(fs.Args(), 0, 1, "approvals list [--advanced] [status]"); err != nil {
			return err
		}
		status := ""
		if fs.NArg() > 0 {
			status = strings.TrimSpace(fs.Arg(0))
		}
		items, err := appSvc.ListApprovalRequests(ctx, controlplane.ApprovalFilter{Status: status, Limit: 100})
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		fmt.Fprintln(stdout, "Pending permissions")
		for _, item := range items {
			view := uxstate.BuildApprovalPrompt(item)
			fmt.Fprintf(stdout, "\n%d. %s\n", item.ID, friendlyApprovalTitle(view))
			fmt.Fprintf(stdout, "   Action: %s\n", view.ActionSummary)
			fmt.Fprintf(stdout, "   Risk: %s. %s\n", view.RiskLabel, view.RiskExplanation)
			fmt.Fprintf(stdout, "   Review: or3-intern approvals show %d\n", item.ID)
			if *advanced {
				printApprovalAdvanced(stdout, item)
			}
		}
		return nil
	case "show":
		fs := flag.NewFlagSet("approvals show", flag.ContinueOnError)
		fs.SetOutput(stderr)
		advanced := fs.Bool("advanced", false, "show raw approval details")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 1, "approvals show [--advanced] <id>"); err != nil {
			return err
		}
		id, err := strconv.ParseInt(strings.TrimSpace(fs.Arg(0)), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid approval ID")
		}
		item, err := appSvc.GetApproval(ctx, id)
		if err != nil {
			return err
		}
		view := uxstate.BuildApprovalPrompt(item)
		printApprovalPrompt(stdout, item, view)
		if *advanced {
			printApprovalAdvanced(stdout, item)
		}
		return nil
	case "approve":
		fs := flag.NewFlagSet("approvals approve", flag.ContinueOnError)
		fs.SetOutput(stderr)
		alwaysAllow := fs.Bool("allowlist", false, "create a matching allowlist rule")
		remember := fs.Bool("remember", false, "always allow this kind of action")
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
		issued, err := appSvc.ApproveApproval(ctx, id, "cli", *alwaysAllow || *remember, *note)
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
		if err := appSvc.DenyApproval(ctx, id, "cli", *note); err != nil {
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
		if err := appSvc.CancelApproval(ctx, id, "cli", *note); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "canceled %d\n", id)
		return nil
	case "expire":
		if err := requireExactArgs(args[1:], 0, "approvals expire"); err != nil {
			return err
		}
		expired, err := appSvc.ExpireApprovals(ctx, "cli")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "expired %d pending request(s)\n", expired)
		return nil
	case "allowlist":
		return runApprovalAllowlistCommand(ctx, appSvc, broker.HostID, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown approvals subcommand: %s", args[0])
	}
}

func runApprovalAllowlistCommand(ctx context.Context, appSvc *app.ServiceApp, defaultHostID string, args []string, stdout, stderr io.Writer) error {
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
		items, err := appSvc.ListAllowlists(ctx, domain, 100)
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
		if err := appSvc.RemoveAllowlist(ctx, id, "cli"); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "removed allowlist %d\n", id)
		return nil
	case "add":
		fs := flag.NewFlagSet("approvals allowlist add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		domain := fs.String("domain", "exec", "approval domain")
		hostID := fs.String("host", defaultHostID, "host scope")
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
		rec, err := appSvc.AddAllowlist(ctx, strings.TrimSpace(*domain), scope, matcher, "cli", 0)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "added allowlist %d\n", rec.ID)
		return nil
	default:
		return fmt.Errorf("unknown allowlist subcommand: %s", args[0])
	}
}

func printApprovalPrompt(stdout io.Writer, item db.ApprovalRequestRecord, view uxstate.ApprovalPromptView) {
	fmt.Fprintln(stdout, "OR3 wants permission")
	fmt.Fprintln(stdout, "\nAction:")
	fmt.Fprintf(stdout, "  %s\n", friendlyApprovalAction(view))
	if strings.TrimSpace(view.ActionSummary) != "" {
		fmt.Fprintln(stdout, "\nCommand:")
		fmt.Fprintf(stdout, "  %s\n", view.ActionSummary)
	}
	fmt.Fprintln(stdout, "\nWhy:")
	fmt.Fprintf(stdout, "  %s\n", view.Why)
	fmt.Fprintln(stdout, "\nRisk:")
	fmt.Fprintf(stdout, "  %s. %s\n", view.RiskLabel, view.RiskExplanation)
	fmt.Fprintln(stdout, "\nChoices:")
	for index, choice := range view.ChoiceHints {
		fmt.Fprintf(stdout, "  %d. %s\n", index+1, choice)
	}
	fmt.Fprintln(stdout, "\nCommands:")
	fmt.Fprintf(stdout, "  or3-intern approvals approve %d\n", item.ID)
	fmt.Fprintf(stdout, "  or3-intern approvals approve %d --remember\n", item.ID)
	fmt.Fprintf(stdout, "  or3-intern approvals deny %d\n", item.ID)
}

func printApprovalAdvanced(stdout io.Writer, item db.ApprovalRequestRecord) {
	fmt.Fprintf(stdout, "   Advanced ID: %d\n", item.ID)
	fmt.Fprintf(stdout, "   Status: %s\n", item.Status)
	fmt.Fprintf(stdout, "   Type: %s\n", item.Type)
	fmt.Fprintf(stdout, "   Subject hash: %s\n", item.SubjectHash)
	fmt.Fprintf(stdout, "   Policy mode: %s\n", item.PolicyMode)
	fmt.Fprintf(stdout, "   Subject JSON: %s\n", item.SubjectJSON)
}

func friendlyApprovalTitle(view uxstate.ApprovalPromptView) string {
	title := strings.TrimSpace(view.Title)
	title = strings.TrimPrefix(title, "OR3 wants to ")
	title = strings.TrimPrefix(title, "OR3 wants ")
	if title == "" {
		return "Permission needed"
	}
	return strings.ToUpper(title[:1]) + title[1:]
}

func friendlyApprovalAction(view uxstate.ApprovalPromptView) string {
	title := strings.ToLower(strings.TrimSpace(view.Title))
	switch {
	case strings.Contains(title, "command"):
		return "Run a command"
	case strings.Contains(title, "skill"):
		return "Run a skill"
	case strings.Contains(title, "secret"):
		return "Use a secret"
	case strings.Contains(title, "message"):
		return "Send a message"
	case strings.Contains(title, "file"):
		return "Transfer a file"
	default:
		return "Complete an action"
	}
}
