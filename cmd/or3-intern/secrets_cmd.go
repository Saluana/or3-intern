package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"or3-intern/internal/security"
)

func validateStrictAuditBeforeMutation(audit *security.AuditLogger) error {
	if audit == nil || !audit.Strict {
		return nil
	}
	if audit.DB == nil || len(audit.Key) == 0 {
		return fmt.Errorf("audit logger unavailable")
	}
	return nil
}

func runSecretsCommand(ctx context.Context, mgr *security.SecretManager, audit *security.AuditLogger, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if mgr == nil {
		return fmt.Errorf("secret store not configured")
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: or3-intern secrets <set|delete|list> ...")
	}
	switch args[0] {
	case "set":
		fs := flag.NewFlagSet("secrets set", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 2, "or3-intern secrets set <name> <value>"); err != nil {
			return err
		}
		name, value := fs.Arg(0), fs.Arg(1)
		if err := validateStrictAuditBeforeMutation(audit); err != nil {
			return err
		}
		if err := mgr.Put(ctx, name, value); err != nil {
			return err
		}
		if audit != nil {
			if err := audit.Record(ctx, "secret.set", "", "cli", map[string]any{"name": name}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(stdout, "stored\t%s\n", name)
		return nil
	case "delete":
		if err := requireExactArgs(args[1:], 1, "or3-intern secrets delete <name>"); err != nil {
			return err
		}
		if err := validateStrictAuditBeforeMutation(audit); err != nil {
			return err
		}
		if err := mgr.Delete(ctx, args[1]); err != nil {
			return err
		}
		if audit != nil {
			if err := audit.Record(ctx, "secret.delete", "", "cli", map[string]any{"name": args[1]}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(stdout, "deleted\t%s\n", args[1])
		return nil
	case "list":
		if err := requireExactArgs(args[1:], 0, "or3-intern secrets list"); err != nil {
			return err
		}
		names, err := mgr.List(ctx)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			_, _ = fmt.Fprintln(stdout, "(no secrets stored)")
			return nil
		}
		for _, name := range names {
			_, _ = fmt.Fprintln(stdout, name)
		}
		return nil
	default:
		return fmt.Errorf("unknown secrets subcommand: %s", args[0])
	}
}
