package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"or3-intern/internal/controlplane"
)

func runScopeCommand(ctx context.Context, cp *controlplane.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("scope", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return newUsageError("%s", err)
	}
	scopeArgs := fs.Args()
	if len(scopeArgs) < 1 {
		return newUsageError("usage: or3-intern scope <link|list|resolve> ...")
	}
	switch scopeArgs[0] {
	case "link":
		if len(scopeArgs) < 3 {
			return newUsageError("usage: or3-intern scope link <session-key> <scope-key>")
		}
		linked, err := cp.LinkSessionScope(ctx, controlplane.ScopeLinkInput{SessionKey: scopeArgs[1], ScopeKey: scopeArgs[2]})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Linked session %q -> scope %q\n", linked.SessionKey, linked.ScopeKey)
		return nil
	case "list":
		if len(scopeArgs) < 2 {
			return newUsageError("usage: or3-intern scope list <scope-key>")
		}
		sessions, err := cp.ListScopeSessions(ctx, scopeArgs[1])
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			_, _ = fmt.Fprintln(stdout, "(no sessions linked to scope)")
			return nil
		}
		for _, sessionKey := range sessions {
			_, _ = fmt.Fprintln(stdout, sessionKey)
		}
		return nil
	case "resolve":
		if len(scopeArgs) < 2 {
			return newUsageError("usage: or3-intern scope resolve <session-key>")
		}
		scopeKey, err := cp.ResolveScopeKey(ctx, scopeArgs[1])
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, scopeKey)
		return nil
	default:
		return newUsageError("unknown scope subcommand: %s", scopeArgs[0])
	}
}
