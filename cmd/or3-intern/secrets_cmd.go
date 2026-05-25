package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/security"

	"golang.org/x/term"
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
		prompt := fs.Bool("prompt", false, "Prompt for the secret value with hidden input")
		stdinFlag := fs.Bool("stdin", false, "Read the secret value from stdin")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		name := fs.Arg(0)
		if name == "" {
			return fmt.Errorf("usage: or3-intern secrets set <name> [--prompt | --stdin | <value>]")
		}

		var value string
		switch {
		case *prompt:
			// Interactive prompt with hidden input
			stdinTTY, _ := stdioIsTerminal(os.Stdin, stdout)
			if !stdinTTY {
				return fmt.Errorf("--prompt requires an interactive terminal")
			}
			fmt.Fprintf(stdout, "Enter secret value for %s: ", name)
			val, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(stdout)
			if err != nil {
				return fmt.Errorf("failed to read secret: %w", err)
			}
			value = string(val)
		case *stdinFlag:
			// Read from stdin
			reader := bufio.NewReader(os.Stdin)
			val, err := reader.ReadString('\n')
			if err != nil && len(val) == 0 {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}
			value = strings.TrimRight(val, "\n\r")
		default:
			// Positional argument (backward compatible)
			value = fs.Arg(1)
			if value == "" {
				return fmt.Errorf("usage: or3-intern secrets set <name> [--prompt | --stdin | <value>]")
			}
		}

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
		fs := flag.NewFlagSet("secrets delete", flag.ContinueOnError)
		fs.SetOutput(stderr)
		force := fs.Bool("force", false, "Skip confirmation prompt")
		internal := fs.Bool("internal", false, "Allow deleting internal secrets (e.g., secure-connection keys)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 1, "or3-intern secrets delete <name> [--force] [--internal]"); err != nil {
			return err
		}
		name := fs.Arg(0)

		// Check if this is an internal secret
		if security.IsInternalSecret(name) && !*internal {
			return fmt.Errorf("cannot delete internal secret %q without --internal flag (this may break secure connections)", name)
		}

		if err := validateStrictAuditBeforeMutation(audit); err != nil {
			return err
		}
		stdinTTY, stdoutTTY := stdioIsTerminal(os.Stdin, stdout)
		ok, err := confirmDestructiveAction(destructiveConfirmation{
			Action:      "Delete stored secret",
			ItemName:    name,
			Consequence: "Any provider or integration using this secret may stop working.",
			Undo:        "There is no undo. Store the secret again if you still have the value.",
			Force:       *force,
			Stdin:       os.Stdin,
			Stdout:      stdout,
			StdinTTY:    stdinTTY,
			StdoutTTY:   stdoutTTY,
		})
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(stdout, "Canceled.")
			return nil
		}
		if err := mgr.Delete(ctx, name); err != nil {
			return err
		}
		if audit != nil {
			if err := audit.Record(ctx, "secret.delete", "", "cli", map[string]any{"name": name}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(stdout, "deleted\t%s\n", name)
		return nil
	case "migrate-config":
		fs := flag.NewFlagSet("secrets migrate-config", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dryRun := fs.Bool("dry-run", false, "Show what would be migrated without making changes")
		force := fs.Bool("force", false, "Overwrite existing secrets")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "or3-intern secrets migrate-config [--dry-run] [--force]"); err != nil {
			return err
		}
		if err := validateStrictAuditBeforeMutation(audit); err != nil {
			return err
		}

		// Get config path
		configPath, err := getConfigPath()
		if err != nil {
			return fmt.Errorf("failed to get config path: %w", err)
		}

		// Load config
		cfg, err := loadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Find secrets to migrate
		secretsToMigrate := findSecretsToMigrate(cfg)
		if len(secretsToMigrate) == 0 {
			_, _ = fmt.Fprintln(stdout, "No plaintext secrets found in config.")
			return nil
		}

		// Show what would be migrated
		_, _ = fmt.Fprintf(stdout, "Found %d secrets to migrate:\n", len(secretsToMigrate))
		for _, s := range secretsToMigrate {
			_, _ = fmt.Fprintf(stdout, "  %s -> %s\n", s.ConfigPath, s.SecretName)
		}

		if *dryRun {
			_, _ = fmt.Fprintln(stdout, "\nDry run - no changes made.")
			return nil
		}
		if !*force {
			for _, s := range secretsToMigrate {
				if _, ok, err := mgr.DB.GetSecret(ctx, s.SecretName); err != nil {
					return fmt.Errorf("check existing secret %s: %w", s.SecretName, err)
				} else if ok {
					return fmt.Errorf("secret %q already exists; rerun with --force to overwrite it", s.SecretName)
				}
			}
		}

		// Confirm migration
		stdinTTY, stdoutTTY := stdioIsTerminal(os.Stdin, stdout)
		ok, err := confirmDestructiveAction(destructiveConfirmation{
			Action:      "Migrate plaintext secrets to encrypted store",
			ItemName:    fmt.Sprintf("%d secrets", len(secretsToMigrate)),
			Consequence: "Plaintext values in config will be replaced with secret references.",
			Undo:        "Restore from backup or manually set values back.",
			Force:       *force,
			Stdin:       os.Stdin,
			Stdout:      stdout,
			StdinTTY:    stdinTTY,
			StdoutTTY:   stdoutTTY,
		})
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(stdout, "Canceled.")
			return nil
		}

		// Perform migration
		migrated := 0
		for _, s := range secretsToMigrate {
			if err := mgr.Put(ctx, s.SecretName, s.Value); err != nil {
				_, _ = fmt.Fprintf(stderr, "Failed to migrate %s: %v\n", s.ConfigPath, err)
				continue
			}
			if err := setConfigValue(cfg, s.ConfigPath, "secret:"+s.SecretName); err != nil {
				_, _ = fmt.Fprintf(stderr, "Failed to update config for %s: %v\n", s.ConfigPath, err)
				continue
			}
			migrated++
			if audit != nil {
				if err := audit.Record(ctx, "secret.migrate", "", "cli", map[string]any{
					"configPath": s.ConfigPath,
					"secretName": s.SecretName,
				}); err != nil {
					return err
				}
			}
		}

		if migrated > 0 {
			// Save updated config
			if err := saveConfig(configPath, cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			_, _ = fmt.Fprintf(stdout, "\nSuccessfully migrated %d secrets.\n", migrated)
			_, _ = fmt.Fprintln(stdout, "Config updated with secret references.")
		} else {
			_, _ = fmt.Fprintln(stdout, "\nNo secrets were migrated.")
		}

		return nil
	case "check":
		fs := flag.NewFlagSet("secrets check", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "or3-intern secrets check"); err != nil {
			return err
		}

		names, err := mgr.List(ctx)
		if err != nil {
			return err
		}

		if len(names) == 0 {
			_, _ = fmt.Fprintln(stdout, "No secrets stored.")
			return nil
		}

		_, _ = fmt.Fprintf(stdout, "Checking %d secrets...\n", len(names))
		failed := 0
		for _, name := range names {
			_, _, err := mgr.Get(ctx, name)
			if err != nil {
				_, _ = fmt.Fprintf(stdout, "FAIL\t%s: %v\n", name, err)
				failed++
			} else {
				_, _ = fmt.Fprintf(stdout, "OK\t%s\n", name)
			}
		}

		if failed > 0 {
			return fmt.Errorf("%d secrets failed to decrypt", failed)
		}
		_, _ = fmt.Fprintf(stdout, "\nAll %d secrets OK.\n", len(names))
		return nil
	case "export":
		fs := flag.NewFlagSet("secrets export", flag.ContinueOnError)
		fs.SetOutput(stderr)
		encrypted := fs.Bool("encrypted", false, "Export encrypted values")
		plaintext := fs.Bool("plaintext", false, "Export decrypted plaintext values")
		force := fs.Bool("force", false, "Skip plaintext export confirmation prompt")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "or3-intern secrets export [--encrypted | --plaintext] [--force]"); err != nil {
			return err
		}
		if *encrypted && *plaintext {
			return fmt.Errorf("usage: or3-intern secrets export [--encrypted | --plaintext] [--force]")
		}

		names, err := mgr.List(ctx)
		if err != nil {
			return err
		}

		if len(names) == 0 {
			_, _ = fmt.Fprintln(stdout, "No secrets to export.")
			return nil
		}

		if *plaintext {
			stdinTTY, stdoutTTY := stdioIsTerminal(os.Stdin, stdout)
			ok, err := confirmDestructiveAction(destructiveConfirmation{
				Action:      "Export decrypted secrets",
				ItemName:    fmt.Sprintf("%d secrets", len(names)),
				Consequence: "Plaintext secret values will be written to stdout.",
				Undo:        "There is no undo for copied terminal output, logs, or redirected files.",
				Force:       *force,
				Stdin:       os.Stdin,
				Stdout:      stdout,
				StdinTTY:    stdinTTY,
				StdoutTTY:   stdoutTTY,
			})
			if err != nil {
				return err
			}
			if !ok {
				_, _ = fmt.Fprintln(stdout, "Canceled.")
				return nil
			}
		}

		_, _ = fmt.Fprintf(stdout, "Exporting %d secrets...\n", len(names))
		for _, name := range names {
			if !*plaintext {
				// Export encrypted (raw database records)
				record, ok, err := mgr.DB.GetSecret(ctx, name)
				if err != nil || !ok {
					_, _ = fmt.Fprintf(stderr, "Failed to export %s\n", name)
					continue
				}
				_, _ = fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", name, base64.StdEncoding.EncodeToString(record.Ciphertext), base64.StdEncoding.EncodeToString(record.Nonce), record.KeyVersion)
			} else {
				val, ok, err := mgr.Get(ctx, name)
				if err != nil || !ok {
					_, _ = fmt.Fprintf(stderr, "Failed to export %s\n", name)
					continue
				}
				_, _ = fmt.Fprintf(stdout, "%s\t%s\n", name, val)
			}
		}
		return nil
	case "list":
		fs := flag.NewFlagSet("secrets list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		advanced := fs.Bool("advanced", false, "Show internal secrets (e.g., secure-connection keys)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireExactFlagArgs(fs, 0, "or3-intern secrets list [--advanced]"); err != nil {
			return err
		}

		var names []string
		var err error
		if *advanced {
			names, err = mgr.List(ctx)
		} else {
			names, err = mgr.ListUserSecrets(ctx)
		}
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

type secretMigration struct {
	ConfigPath string
	SecretName string
	Value      string
}

func getConfigPath() (string, error) {
	if envPath := os.Getenv("OR3_CONFIG"); envPath != "" {
		return envPath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.config/or3-intern/config.json", nil
}

func loadConfig(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func findSecretsToMigrate(cfg *config.Config) []secretMigration {
	var migrations []secretMigration

	// Main provider API key
	if cfg.Provider.APIKey != "" && !strings.HasPrefix(cfg.Provider.APIKey, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "provider.apiKey",
			SecretName: "provider-api-key",
			Value:      cfg.Provider.APIKey,
		})
	}

	// Provider profiles
	for name, profile := range cfg.Providers {
		if profile.APIKey != "" && !strings.HasPrefix(profile.APIKey, "secret:") {
			migrations = append(migrations, secretMigration{
				ConfigPath: fmt.Sprintf("providers.%s.apiKey", name),
				SecretName: fmt.Sprintf("provider-%s-api-key", name),
				Value:      profile.APIKey,
			})
		}
	}

	// Tools API keys
	if cfg.Tools.BraveAPIKey != "" && !strings.HasPrefix(cfg.Tools.BraveAPIKey, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "tools.braveApiKey",
			SecretName: "brave-api-key",
			Value:      cfg.Tools.BraveAPIKey,
		})
	}

	// Channel tokens
	if cfg.Channels.Telegram.Token != "" && !strings.HasPrefix(cfg.Channels.Telegram.Token, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "channels.telegram.token",
			SecretName: "telegram-token",
			Value:      cfg.Channels.Telegram.Token,
		})
	}
	if cfg.Channels.Slack.AppToken != "" && !strings.HasPrefix(cfg.Channels.Slack.AppToken, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "channels.slack.appToken",
			SecretName: "slack-app-token",
			Value:      cfg.Channels.Slack.AppToken,
		})
	}
	if cfg.Channels.Slack.BotToken != "" && !strings.HasPrefix(cfg.Channels.Slack.BotToken, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "channels.slack.botToken",
			SecretName: "slack-bot-token",
			Value:      cfg.Channels.Slack.BotToken,
		})
	}
	if cfg.Channels.Discord.Token != "" && !strings.HasPrefix(cfg.Channels.Discord.Token, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "channels.discord.token",
			SecretName: "discord-token",
			Value:      cfg.Channels.Discord.Token,
		})
	}
	if cfg.Channels.WhatsApp.BridgeToken != "" && !strings.HasPrefix(cfg.Channels.WhatsApp.BridgeToken, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "channels.whatsApp.bridgeToken",
			SecretName: "whatsapp-bridge-token",
			Value:      cfg.Channels.WhatsApp.BridgeToken,
		})
	}
	if cfg.Channels.Email.IMAPPassword != "" && !strings.HasPrefix(cfg.Channels.Email.IMAPPassword, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "channels.email.imapPassword",
			SecretName: "email-imap-password",
			Value:      cfg.Channels.Email.IMAPPassword,
		})
	}
	if cfg.Channels.Email.SMTPPassword != "" && !strings.HasPrefix(cfg.Channels.Email.SMTPPassword, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "channels.email.smtpPassword",
			SecretName: "email-smtp-password",
			Value:      cfg.Channels.Email.SMTPPassword,
		})
	}

	// Webhook secret
	if cfg.Triggers.Webhook.Secret != "" && !strings.HasPrefix(cfg.Triggers.Webhook.Secret, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "triggers.webhook.secret",
			SecretName: "webhook-secret",
			Value:      cfg.Triggers.Webhook.Secret,
		})
	}

	// Service secret
	if cfg.Service.Secret != "" && !strings.HasPrefix(cfg.Service.Secret, "secret:") {
		migrations = append(migrations, secretMigration{
			ConfigPath: "service.secret",
			SecretName: "service-secret",
			Value:      cfg.Service.Secret,
		})
	}

	// MCP server headers and env
	for serverName, server := range cfg.Tools.MCPServers {
		for key, val := range server.Headers {
			if !strings.HasPrefix(val, "secret:") {
				migrations = append(migrations, secretMigration{
					ConfigPath: fmt.Sprintf("tools.mcpServers.%s.headers.%s", serverName, key),
					SecretName: fmt.Sprintf("mcp-%s-header-%s", serverName, key),
					Value:      val,
				})
			}
		}
		for key, val := range server.Env {
			if !strings.HasPrefix(val, "secret:") && isSensitiveEnvKey(key) {
				migrations = append(migrations, secretMigration{
					ConfigPath: fmt.Sprintf("tools.mcpServers.%s.env.%s", serverName, key),
					SecretName: fmt.Sprintf("mcp-%s-env-%s", serverName, key),
					Value:      val,
				})
			}
		}
	}

	return migrations
}

func isSensitiveEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	sensitivePatterns := []string{"TOKEN", "SECRET", "PASSWORD", "KEY", "API", "CREDENTIAL", "AUTH"}
	for _, pattern := range sensitivePatterns {
		if strings.Contains(upper, pattern) {
			return true
		}
	}
	return false
}

func setConfigValue(cfg *config.Config, path, value string) error {
	switch path {
	case "provider.apiKey":
		cfg.Provider.APIKey = value
	case "tools.braveApiKey":
		cfg.Tools.BraveAPIKey = value
	case "channels.telegram.token":
		cfg.Channels.Telegram.Token = value
	case "channels.slack.appToken":
		cfg.Channels.Slack.AppToken = value
	case "channels.slack.botToken":
		cfg.Channels.Slack.BotToken = value
	case "channels.discord.token":
		cfg.Channels.Discord.Token = value
	case "channels.whatsApp.bridgeToken":
		cfg.Channels.WhatsApp.BridgeToken = value
	case "channels.email.imapPassword":
		cfg.Channels.Email.IMAPPassword = value
	case "channels.email.smtpPassword":
		cfg.Channels.Email.SMTPPassword = value
	case "triggers.webhook.secret":
		cfg.Triggers.Webhook.Secret = value
	case "service.secret":
		cfg.Service.Secret = value
	default:
		if strings.HasPrefix(path, "providers.") && strings.HasSuffix(path, ".apiKey") {
			name := strings.TrimSuffix(strings.TrimPrefix(path, "providers."), ".apiKey")
			profile, ok := cfg.Providers[name]
			if !ok {
				return fmt.Errorf("provider profile not found: %s", name)
			}
			profile.APIKey = value
			cfg.Providers[name] = profile
			return nil
		}
		if strings.HasPrefix(path, "tools.mcpServers.") {
			rest := strings.TrimPrefix(path, "tools.mcpServers.")
			section := ""
			sectionIndex := -1
			for _, candidate := range []string{".headers.", ".env."} {
				if idx := strings.Index(rest, candidate); idx >= 0 {
					section = strings.Trim(candidate, ".")
					sectionIndex = idx
					break
				}
			}
			if sectionIndex < 0 {
				return fmt.Errorf("unsupported config path: %s", path)
			}
			serverName := rest[:sectionIndex]
			key := rest[sectionIndex+len(section)+2:]
			server, ok := cfg.Tools.MCPServers[serverName]
			if !ok {
				return fmt.Errorf("mcp server not found: %s", serverName)
			}
			switch section {
			case "headers":
				if server.Headers == nil {
					server.Headers = map[string]string{}
				}
				server.Headers[key] = value
			case "env":
				if server.Env == nil {
					server.Env = map[string]string{}
				}
				server.Env[key] = value
			default:
				return fmt.Errorf("unsupported config path: %s", path)
			}
			cfg.Tools.MCPServers[serverName] = server
			return nil
		}
		return fmt.Errorf("unsupported config path: %s", path)
	}
	return nil
}
