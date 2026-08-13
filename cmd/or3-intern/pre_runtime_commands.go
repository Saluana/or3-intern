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
	"or3-intern/internal/skills"
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

func runPreRuntimeCommand(ctx context.Context, cmd, configPath string, cfg config.Config, database *db.DB, provider *providers.Client, audit *security.AuditLogger, broker *approval.Broker, args []string, stdout, stderr io.Writer) (bool, error) {
	switch cmd {
	case "approvals":
		return true, runApprovalsCommand(ctx, broker, args, stdout, stderr)
	case "capabilities":
		return true, runCapabilitiesCommand(cfg, broker, args, stdout, stderr)
	case "devices":
		return true, runDevicesCommand(ctx, broker, args, stdout, stderr)
	case "embeddings":
		cp := controlplane.NewLocal(cfg, database, provider, audit, broker)
		return true, runEmbeddingsCommand(ctx, cp, args, stdout, stderr)
	case "pairing":
		return true, runPairingCommand(ctx, broker, args, stdout, stderr)
	case "scope":
		cp := controlplane.NewLocal(cfg, database, provider, audit, broker)
		return true, runScopeCommand(ctx, cp, args, stdout, stderr)
	case "migrate-jsonl":
		if len(args) < 1 {
			return true, newUsageError("usage: or3-intern migrate-jsonl <jsonl_path> [session_key]")
		}
		sessionKey := "migrated:default"
		if len(args) >= 2 {
			sessionKey = args[1]
		}
		if err := migrateJSONL(ctx, database, args[0], sessionKey); err != nil {
			return true, err
		}
		fmt.Fprintln(stdout, "ok")
		return true, nil
	case "migrate-openclaw":
		return true, runMigrateOpenClawCommand(ctx, cfg, database, provider, args, stdout, stderr)
	case "memory":
		if _, err := ensureMemorySkillRegistered(configPath, &cfg); err != nil {
			return true, err
		}
		return true, runMemoryCommandWithDeps(ctx, cfg, database, args, memoryCommandDeps{Stdout: stdout, Stderr: stderr})
	case "skills":
		if _, err := ensureMemorySkillRegistered(configPath, &cfg); err != nil {
			return true, err
		}
		bundledDir, err := resolveBundledSkillsDir(configPath)
		if err != nil {
			return true, err
		}
		deps := skillsCommandDeps{
			Client: newClawHubClient(cfg),
			LoadToolNames: func(ctx context.Context, cfg config.Config) map[string]struct{} {
				return loadAvailableToolNamesWithManager(ctx, cfg, struct{}{})
			},
			LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
				return buildSkillsInventory(cfg, bundledDir, toolNames)
			},
			Audit: func(ctx context.Context, eventType string, payload any) error {
				if audit == nil {
					return nil
				}
				return audit.Record(ctx, eventType, "", "cli", payload)
			},
			Stdout: stdout,
			Stderr: stderr,
		}
		return true, runSkillsCommandWithDeps(ctx, cfg, args, deps)
	default:
		return false, nil
	}
}
