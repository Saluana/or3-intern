package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
)

type usageError struct {
	message string
}

func (e usageError) Error() string {
	return e.message
}

func newUsageError(format string, args ...any) error {
	return usageError{message: fmt.Sprintf(format, args...)}
}

func isUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}

func runPreRuntimeCommand(ctx context.Context, cmd string, cfg config.Config, database *db.DB, provider *providers.Client, audit *security.AuditLogger, broker *approval.Broker, args []string, stdout, stderr io.Writer) (bool, error) {
	switch cmd {
	case "capabilities":
		return true, runCapabilitiesCommand(cfg, broker, args, stdout, stderr)
	case "embeddings":
		cp := controlplane.NewLocal(cfg, database, provider, audit, broker)
		return true, runEmbeddingsCommand(ctx, cp, args, stdout, stderr)
	case "scope":
		cp := controlplane.NewLocal(cfg, database, provider, audit, broker)
		return true, runScopeCommand(ctx, cp, args, stdout, stderr)
	default:
		return false, nil
	}
}
