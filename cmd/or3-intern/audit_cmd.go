package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"or3-intern/internal/controlplane"
)

func runAuditCommand(ctx context.Context, cp *controlplane.Service, args []string, stdout io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if len(args) == 0 || args[0] == "verify" {
		if _, err := cp.VerifyAudit(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "[ok] audit chain verified")
		return nil
	}
	return fmt.Errorf("unknown audit subcommand: %s", args[0])
}
