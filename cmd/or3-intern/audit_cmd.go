package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"or3-intern/internal/security"
)

func runAuditCommand(ctx context.Context, audit *security.AuditLogger, args []string, stdout io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if audit == nil {
		return fmt.Errorf("audit logger not configured")
	}
	if len(args) == 0 || args[0] == "verify" {
		if err := audit.Verify(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "[ok] audit chain verified")
		return nil
	}
	return fmt.Errorf("unknown audit subcommand: %s", args[0])
}
