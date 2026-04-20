package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"or3-intern/internal/config"
)

var configureSections = []struct {
	Key         string
	Label       string
	Description string
}{
	{Key: "provider", Label: "Provider", Description: "Model endpoint, model names, and API key"},
	{Key: "storage", Label: "Storage", Description: "SQLite DB path and artifacts directory"},
	{Key: "workspace", Label: "Workspace", Description: "Workspace restriction and working directory"},
	{Key: "web", Label: "Web", Description: "Brave Search key and outbound proxy"},
	{Key: "channels", Label: "Channels", Description: "Telegram, Slack, Discord, WhatsApp, and Email"},
	{Key: "service", Label: "Service", Description: "Internal authenticated HTTP API listener"},
}

type configureArgs struct {
	Sections []string
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("section cannot be empty")
	}
	*s = append(*s, value)
	return nil
}

func runConfigure(cfgPath string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	if supportsInteractiveTUI(os.Stdin, os.Stdout) {
		return runConfigureWithTUI(cfgPathOrDefault(cfgPath), cwd, args, configureTUIOptions{})
	}
	return runConfigureWithIO(os.Stdin, os.Stdout, cfgPathOrDefault(cfgPath), cwd, args)
}

func runConfigureWithIO(in io.Reader, out io.Writer, cfgPath, cwd string, args []string) error {
	parsed, err := parseConfigureArgs(args)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(in)
	cfg, existed, loadWarning, err := loadConfigureConfig(cfgPath, cwd)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "or3-intern configure")
	if existed {
		fmt.Fprintln(out, "Loaded existing config. Press Enter to keep current values.")
	} else {
		fmt.Fprintln(out, "No config found yet. We'll create one with sensible defaults for local use.")
	}
	if loadWarning != "" {
		fmt.Fprintf(out, "Repair mode: %s\n", loadWarning)
	}
	fmt.Fprintf(out, "Config file: %s\n\n", cfgPath)
	printConfigureSummary(out, cfg)

	selectedSections := parsed.Sections
	if len(selectedSections) == 0 {
		return runConfigureInteractive(reader, out, cfgPath, cwd, cfg)
	}

	for _, section := range selectedSections {
		if err := runConfigureSection(reader, out, &cfg, section, cwd); err != nil {
			return err
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Saved %s settings.\n\n", section)
	}

	fmt.Fprintln(out, "Configuration complete.")
	fmt.Fprintf(out, "Saved config to %s\n", cfgPath)
	printConfigureSummary(out, cfg)
	return printConfigureNextSteps(out, cfg)
}

func parseConfigureArgs(args []string) (configureArgs, error) {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var sections stringSliceFlag
	fs.Var(&sections, "section", "configuration section to run")
	if err := fs.Parse(args); err != nil {
		return configureArgs{}, err
	}
	if fs.NArg() > 0 {
		return configureArgs{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	normalized, err := normalizeConfigureSections([]string(sections))
	if err != nil {
		return configureArgs{}, err
	}
	return configureArgs{Sections: normalized}, nil
}

func normalizeConfigureSections(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(configureSections))
	for _, section := range configureSections {
		allowed[section.Key] = struct{}{}
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(raw))
	for _, value := range raw {
		key := strings.ToLower(strings.TrimSpace(value))
		if _, ok := allowed[key]; !ok {
			options := make([]string, 0, len(configureSections))
			for _, section := range configureSections {
				options = append(options, section.Key)
			}
			return nil, fmt.Errorf("invalid --section %q (expected one of: %s)", value, strings.Join(options, ", "))
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	return normalized, nil
}

func loadConfigureConfig(cfgPath, cwd string) (config.Config, bool, string, error) {
	if _, err := os.Stat(cfgPath); err == nil {
		if cfg, loadErr := config.Load(cfgPath); loadErr == nil {
			return cfg, true, "", nil
		}
		cfg, repairErr := loadConfigureConfigLenient(cfgPath, cwd)
		if repairErr != nil {
			return config.Config{}, true, "", repairErr
		}
		return cfg, true, "existing config has validation issues; loaded raw values so you can repair them here.", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return config.Config{}, false, "", err
	}
	return initDefaults(cwd), false, "", nil
}

func loadConfigureConfigLenient(cfgPath, cwd string) (config.Config, error) {
	cfg := initDefaults(cwd)
	if err := readConfigFile(cfgPath, &cfg); err != nil {
		return config.Config{}, err
	}
	config.ApplyEnvOverrides(&cfg)
	return cfg, nil
}

func runConfigureInteractive(reader *bufio.Reader, out io.Writer, cfgPath, cwd string, cfg config.Config) error {
	defaultChoice := "1"
	ranAny := false
	for {
		options := make([]string, 0, len(configureSections)+1)
		for index, section := range configureSections {
			options = append(options, fmt.Sprintf("%d) %s — %s", index+1, section.Label, section.Description))
		}
		options = append(options, fmt.Sprintf("%d) Save and finish", len(configureSections)+1))
		choice, err := promptMenuChoice(reader, out, "Choose a section to configure", options, defaultChoice)
		if err != nil {
			return err
		}
		if choice == fmt.Sprintf("%d", len(configureSections)+1) {
			break
		}

		selectedIndex := int(choice[0] - '1')
		section := configureSections[selectedIndex].Key
		if err := runConfigureSection(reader, out, &cfg, section, cwd); err != nil {
			return err
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return err
		}
		ranAny = true
		fmt.Fprintf(out, "Saved %s settings.\n\n", section)
		printConfigureSummary(out, cfg)
		defaultChoice = fmt.Sprintf("%d", minInt(selectedIndex+2, len(configureSections)+1))
	}

	if !ranAny {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "No changes selected.")
		return nil
	}

	fmt.Fprintln(out, "Configuration complete.")
	fmt.Fprintf(out, "Saved config to %s\n", cfgPath)
	printConfigureSummary(out, cfg)
	return printConfigureNextSteps(out, cfg)
}

func promptMenuChoice(reader *bufio.Reader, out io.Writer, label string, options []string, defaultChoice string) (string, error) {
	fmt.Fprintln(out, label)
	valid := make(map[string]struct{}, len(options))
	for index, option := range options {
		choice := fmt.Sprintf("%d", index+1)
		valid[choice] = struct{}{}
		fmt.Fprintf(out, "  %s\n", option)
	}
	for {
		answer, err := promptString(reader, out, "Selection", defaultChoice)
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		if _, ok := valid[answer]; ok {
			return answer, nil
		}
		fmt.Fprintf(out, "Please choose a number between 1 and %d.\n", len(options))
	}
}

func runConfigureSection(reader *bufio.Reader, out io.Writer, cfg *config.Config, section, cwd string) error {
	switch section {
	case "provider":
		return configureProviderSection(reader, out, cfg)
	case "storage":
		return configureStorageSection(reader, out, cfg)
	case "workspace":
		return configureWorkspaceSection(reader, out, cfg, cwd)
	case "web":
		return configureWebSection(reader, out, cfg)
	case "channels":
		return configureChannelsSection(reader, out, cfg)
	case "service":
		return configureServiceSection(reader, out, cfg)
	default:
		return fmt.Errorf("unknown configure section %q", section)
	}
}

func configureProviderSection(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	fmt.Fprintln(out, "Provider configuration")
	choice, err := promptChoice(reader, out,
		"Choose your provider preset",
		[]string{"1) OpenAI", "2) OpenRouter", "3) Custom OpenAI-compatible"},
		defaultProviderChoice(cfg.Provider.APIBase),
	)
	if err != nil {
		return err
	}
	applyProviderPreset(cfg, choice)
	if cfg.Provider.APIBase, err = promptString(reader, out, "API base", cfg.Provider.APIBase); err != nil {
		return err
	}
	if cfg.Provider.Model, err = promptString(reader, out, "Chat model", cfg.Provider.Model); err != nil {
		return err
	}
	if cfg.Provider.EmbedModel, err = promptString(reader, out, "Embedding model", cfg.Provider.EmbedModel); err != nil {
		return err
	}
	cfg.Provider.APIKey, err = promptSecretString(reader, out, "API key", cfg.Provider.APIKey)
	return err
}

func configureStorageSection(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	fmt.Fprintln(out, "Storage configuration")
	var err error
	if cfg.DBPath, err = promptString(reader, out, "SQLite DB path", cfg.DBPath); err != nil {
		return err
	}
	if cfg.ArtifactsDir, err = promptString(reader, out, "Artifacts directory", cfg.ArtifactsDir); err != nil {
		return err
	}
	return nil
}

func configureWorkspaceSection(reader *bufio.Reader, out io.Writer, cfg *config.Config, cwd string) error {
	fmt.Fprintln(out, "Workspace configuration")
	defaultWorkspace := strings.TrimSpace(cfg.WorkspaceDir)
	if defaultWorkspace == "" {
		defaultWorkspace = strings.TrimSpace(cwd)
	}
	restrict, err := promptBool(reader, out, "Restrict file tools to a workspace?", cfg.Tools.RestrictToWorkspace)
	if err != nil {
		return err
	}
	cfg.Tools.RestrictToWorkspace = restrict
	if cfg.WorkspaceDir, err = promptString(reader, out, "Workspace directory", defaultWorkspace); err != nil {
		return err
	}
	return nil
}

func configureWebSection(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	fmt.Fprintln(out, "Web configuration")
	var err error
	if cfg.Tools.BraveAPIKey, err = promptSecretString(reader, out, "Brave Search API key (optional)", cfg.Tools.BraveAPIKey); err != nil {
		return err
	}
	if cfg.Tools.WebProxy, err = promptString(reader, out, "Web proxy URL (optional)", cfg.Tools.WebProxy); err != nil {
		return err
	}
	return nil
}

func configureChannelsSection(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	fmt.Fprintln(out, "Channel configuration")
	if err := configureTelegram(reader, out, cfg); err != nil {
		return err
	}
	if err := configureSlack(reader, out, cfg); err != nil {
		return err
	}
	if err := configureDiscord(reader, out, cfg); err != nil {
		return err
	}
	if err := configureWhatsApp(reader, out, cfg); err != nil {
		return err
	}
	if err := configureEmail(reader, out, cfg); err != nil {
		return err
	}
	return nil
}

func configureTelegram(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	enabled, err := promptBool(reader, out, "Enable Telegram?", cfg.Channels.Telegram.Enabled)
	if err != nil {
		return err
	}
	cfg.Channels.Telegram.Enabled = enabled
	if !enabled {
		return nil
	}
	if cfg.Channels.Telegram.Token, err = promptSecretString(reader, out, "Telegram bot token", cfg.Channels.Telegram.Token); err != nil {
		return err
	}
	if cfg.Channels.Telegram.DefaultChatID, err = promptString(reader, out, "Telegram default chat ID (optional)", cfg.Channels.Telegram.DefaultChatID); err != nil {
		return err
	}
	if err := configureInboundAccess(reader, out,
		"Telegram inbound access",
		"Telegram allowed chat IDs (comma-separated)",
		allowlistPromptDefault(cfg.Channels.Telegram.AllowedChatIDs, cfg.Channels.Telegram.DefaultChatID),
		&cfg.Channels.Telegram.InboundPolicy,
		&cfg.Channels.Telegram.OpenAccess,
		&cfg.Channels.Telegram.AllowedChatIDs,
		defaultInboundAccessChoice(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, len(cfg.Channels.Telegram.AllowedChatIDs) > 0, strings.TrimSpace(cfg.Channels.Telegram.DefaultChatID) != ""),
	); err != nil {
		return err
	}
	return nil
}

func configureSlack(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	enabled, err := promptBool(reader, out, "Enable Slack?", cfg.Channels.Slack.Enabled)
	if err != nil {
		return err
	}
	cfg.Channels.Slack.Enabled = enabled
	if !enabled {
		return nil
	}
	if cfg.Channels.Slack.AppToken, err = promptSecretString(reader, out, "Slack app token", cfg.Channels.Slack.AppToken); err != nil {
		return err
	}
	if cfg.Channels.Slack.BotToken, err = promptSecretString(reader, out, "Slack bot token", cfg.Channels.Slack.BotToken); err != nil {
		return err
	}
	if cfg.Channels.Slack.DefaultChannelID, err = promptString(reader, out, "Slack default channel ID (optional)", cfg.Channels.Slack.DefaultChannelID); err != nil {
		return err
	}
	if cfg.Channels.Slack.RequireMention, err = promptBool(reader, out, "Require mention in Slack?", cfg.Channels.Slack.RequireMention); err != nil {
		return err
	}
	if err := configureInboundAccess(reader, out,
		"Slack inbound access",
		"Slack allowed user IDs (comma-separated)",
		cfg.Channels.Slack.AllowedUserIDs,
		&cfg.Channels.Slack.InboundPolicy,
		&cfg.Channels.Slack.OpenAccess,
		&cfg.Channels.Slack.AllowedUserIDs,
		defaultInboundAccessChoice(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, len(cfg.Channels.Slack.AllowedUserIDs) > 0, false),
	); err != nil {
		return err
	}
	return nil
}

func configureDiscord(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	enabled, err := promptBool(reader, out, "Enable Discord?", cfg.Channels.Discord.Enabled)
	if err != nil {
		return err
	}
	cfg.Channels.Discord.Enabled = enabled
	if !enabled {
		return nil
	}
	if cfg.Channels.Discord.Token, err = promptSecretString(reader, out, "Discord bot token", cfg.Channels.Discord.Token); err != nil {
		return err
	}
	if cfg.Channels.Discord.DefaultChannelID, err = promptString(reader, out, "Discord default channel ID (optional)", cfg.Channels.Discord.DefaultChannelID); err != nil {
		return err
	}
	if cfg.Channels.Discord.RequireMention, err = promptBool(reader, out, "Require mention in Discord?", cfg.Channels.Discord.RequireMention); err != nil {
		return err
	}
	if err := configureInboundAccess(reader, out,
		"Discord inbound access",
		"Discord allowed user IDs (comma-separated)",
		cfg.Channels.Discord.AllowedUserIDs,
		&cfg.Channels.Discord.InboundPolicy,
		&cfg.Channels.Discord.OpenAccess,
		&cfg.Channels.Discord.AllowedUserIDs,
		defaultInboundAccessChoice(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, len(cfg.Channels.Discord.AllowedUserIDs) > 0, false),
	); err != nil {
		return err
	}
	return nil
}

func configureWhatsApp(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	enabled, err := promptBool(reader, out, "Enable WhatsApp bridge?", cfg.Channels.WhatsApp.Enabled)
	if err != nil {
		return err
	}
	cfg.Channels.WhatsApp.Enabled = enabled
	if !enabled {
		return nil
	}
	if cfg.Channels.WhatsApp.BridgeURL, err = promptString(reader, out, "WhatsApp bridge URL", cfg.Channels.WhatsApp.BridgeURL); err != nil {
		return err
	}
	if cfg.Channels.WhatsApp.BridgeToken, err = promptSecretString(reader, out, "WhatsApp bridge token (optional)", cfg.Channels.WhatsApp.BridgeToken); err != nil {
		return err
	}
	if cfg.Channels.WhatsApp.DefaultTo, err = promptString(reader, out, "WhatsApp default recipient (optional)", cfg.Channels.WhatsApp.DefaultTo); err != nil {
		return err
	}
	if err := configureInboundAccess(reader, out,
		"WhatsApp inbound access",
		"WhatsApp allowed sender IDs (comma-separated)",
		cfg.Channels.WhatsApp.AllowedFrom,
		&cfg.Channels.WhatsApp.InboundPolicy,
		&cfg.Channels.WhatsApp.OpenAccess,
		&cfg.Channels.WhatsApp.AllowedFrom,
		defaultInboundAccessChoice(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, len(cfg.Channels.WhatsApp.AllowedFrom) > 0, false),
	); err != nil {
		return err
	}
	return nil
}

func configureEmail(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	enabled, err := promptBool(reader, out, "Enable Email?", cfg.Channels.Email.Enabled)
	if err != nil {
		return err
	}
	cfg.Channels.Email.Enabled = enabled
	if !enabled {
		return nil
	}
	if cfg.Channels.Email.ConsentGranted, err = promptBool(reader, out, "Confirm you have consent to operate Email?", cfg.Channels.Email.ConsentGranted); err != nil {
		return err
	}
	if cfg.Channels.Email.IMAPHost, err = promptString(reader, out, "Email IMAP host", cfg.Channels.Email.IMAPHost); err != nil {
		return err
	}
	if cfg.Channels.Email.IMAPUsername, err = promptString(reader, out, "Email IMAP username", cfg.Channels.Email.IMAPUsername); err != nil {
		return err
	}
	if cfg.Channels.Email.IMAPPassword, err = promptSecretString(reader, out, "Email IMAP password", cfg.Channels.Email.IMAPPassword); err != nil {
		return err
	}
	if cfg.Channels.Email.SMTPHost, err = promptString(reader, out, "Email SMTP host", cfg.Channels.Email.SMTPHost); err != nil {
		return err
	}
	if cfg.Channels.Email.SMTPUsername, err = promptString(reader, out, "Email SMTP username", cfg.Channels.Email.SMTPUsername); err != nil {
		return err
	}
	if cfg.Channels.Email.SMTPPassword, err = promptSecretString(reader, out, "Email SMTP password", cfg.Channels.Email.SMTPPassword); err != nil {
		return err
	}
	if cfg.Channels.Email.FromAddress, err = promptString(reader, out, "Email from address", cfg.Channels.Email.FromAddress); err != nil {
		return err
	}
	if cfg.Channels.Email.DefaultTo, err = promptString(reader, out, "Email default recipient (optional)", cfg.Channels.Email.DefaultTo); err != nil {
		return err
	}
	if err := configureInboundAccess(reader, out,
		"Email inbound access",
		"Email allowed sender addresses (comma-separated)",
		cfg.Channels.Email.AllowedSenders,
		&cfg.Channels.Email.InboundPolicy,
		&cfg.Channels.Email.OpenAccess,
		&cfg.Channels.Email.AllowedSenders,
		defaultInboundAccessChoice(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, len(cfg.Channels.Email.AllowedSenders) > 0, false),
	); err != nil {
		return err
	}
	return nil
}

func configureServiceSection(reader *bufio.Reader, out io.Writer, cfg *config.Config) error {
	fmt.Fprintln(out, "Service configuration")
	enabled, err := promptBool(reader, out, "Enable internal service API?", cfg.Service.Enabled)
	if err != nil {
		return err
	}
	cfg.Service.Enabled = enabled
	if !enabled {
		return nil
	}
	if cfg.Service.Listen, err = promptString(reader, out, "Service listen address", cfg.Service.Listen); err != nil {
		return err
	}
	if cfg.Service.Secret, err = promptSecretString(reader, out, "Service shared secret", cfg.Service.Secret); err != nil {
		return err
	}
	return nil
}

func printConfigureSummary(out io.Writer, cfg config.Config) {
	providerLabel := configureProviderLabel(cfg.Provider.APIBase)
	channelNames := enabledChannelNames(cfg)
	channelSummary := "none enabled"
	if len(channelNames) > 0 {
		channelSummary = strings.Join(channelNames, ", ")
	}
	workspaceSummary := strings.TrimSpace(cfg.WorkspaceDir)
	if workspaceSummary == "" {
		workspaceSummary = "not set"
	}
	fmt.Fprintln(out, "Current settings:")
	fmt.Fprintf(out, "  Provider: %s (%s)\n", providerLabel, cfg.Provider.Model)
	fmt.Fprintf(out, "  Storage: db=%s artifacts=%s\n", cfg.DBPath, cfg.ArtifactsDir)
	fmt.Fprintf(out, "  Workspace: restrict=%t dir=%s\n", cfg.Tools.RestrictToWorkspace, workspaceSummary)
	fmt.Fprintf(out, "  Web: Brave key configured=%t proxy=%s\n", strings.TrimSpace(cfg.Tools.BraveAPIKey) != "", emptyAsNone(cfg.Tools.WebProxy))
	fmt.Fprintf(out, "  Channels: %s\n", channelSummary)
	if cfg.Service.Enabled {
		fmt.Fprintf(out, "  Service: enabled on %s\n", cfg.Service.Listen)
	} else {
		fmt.Fprintln(out, "  Service: disabled")
	}
	fmt.Fprintln(out)
}

func configureProviderLabel(apiBase string) string {
	base := strings.ToLower(strings.TrimSpace(apiBase))
	switch {
	case strings.Contains(base, "openrouter.ai"):
		return "OpenRouter"
	case strings.Contains(base, "api.openai.com"):
		return "OpenAI"
	case base == "":
		return "Not configured"
	default:
		return "Custom OpenAI-compatible"
	}
}

func enabledChannelNames(cfg config.Config) []string {
	names := make([]string, 0, 5)
	if cfg.Channels.Telegram.Enabled {
		names = append(names, "telegram("+channelAccessSummary(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, len(cfg.Channels.Telegram.AllowedChatIDs) > 0)+")")
	}
	if cfg.Channels.Slack.Enabled {
		names = append(names, "slack("+channelAccessSummary(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, len(cfg.Channels.Slack.AllowedUserIDs) > 0)+")")
	}
	if cfg.Channels.Discord.Enabled {
		names = append(names, "discord("+channelAccessSummary(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, len(cfg.Channels.Discord.AllowedUserIDs) > 0)+")")
	}
	if cfg.Channels.WhatsApp.Enabled {
		names = append(names, "whatsapp("+channelAccessSummary(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, len(cfg.Channels.WhatsApp.AllowedFrom) > 0)+")")
	}
	if cfg.Channels.Email.Enabled {
		names = append(names, "email("+channelAccessSummary(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, len(cfg.Channels.Email.AllowedSenders) > 0)+")")
	}
	sort.Strings(names)
	return names
}

func hasEnabledChannels(cfg config.Config) bool {
	return len(enabledChannelNames(cfg)) > 0
}

func emptyAsNone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return value
}

func readConfigFile(filePath string, cfg *config.Config) error {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, cfg)
}

func printConfigureNextSteps(out io.Writer, cfg config.Config) error {
	if err := validateConfigSnapshot(cfg); err != nil {
		fmt.Fprintln(out, "Next steps:")
		fmt.Fprintln(out, "  Configuration saved, but the config still has validation issues.")
		fmt.Fprintf(out, "  %v\n", err)
		fmt.Fprintln(out, "  Re-run: or3-intern configure --section channels")
		return nil
	}

	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  or3-intern chat")
	if hasEnabledChannels(cfg) || cfg.Service.Enabled {
		fmt.Fprintln(out, "  or3-intern doctor --strict")
	}
	if hasEnabledChannels(cfg) {
		fmt.Fprintln(out, "  or3-intern serve")
	}
	return nil
}

func validateConfigSnapshot(cfg config.Config) error {
	file, err := os.CreateTemp("", "or3-intern-config-*.json")
	if err != nil {
		return err
	}
	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	defer os.Remove(path)
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	_, err = config.Load(path)
	return err
}

func configureInboundAccess(reader *bufio.Reader, out io.Writer, label, allowlistLabel string, allowlist []string, policy *config.InboundPolicy, openAccess *bool, target *[]string, defaultChoice string) error {
	choice, err := promptMenuChoice(reader, out, label, []string{
		"1) Pairing (secure; inbound messages are allowed only after pairing)",
		"2) Allowlist (allow only the identities you enter now)",
		"3) Open access (allow any sender)",
		"4) Deny inbound (send-only)",
	}, defaultChoice)
	if err != nil {
		return err
	}

	switch choice {
	case "1":
		*policy = config.InboundPolicyPairing
		*openAccess = false
	case "2":
		items, err := promptRequiredCSV(reader, out, allowlistLabel, allowlist)
		if err != nil {
			return err
		}
		*target = items
		*policy = config.InboundPolicyAllowlist
		*openAccess = false
	case "3":
		*policy = ""
		*openAccess = true
	case "4":
		*policy = config.InboundPolicyDeny
		*openAccess = false
	default:
		return fmt.Errorf("unsupported inbound access choice %q", choice)
	}
	return nil
}

func defaultInboundAccessChoice(policy config.InboundPolicy, openAccess, hasAllowlist, preferAllowlist bool) string {
	switch config.EffectiveInboundPolicy(policy, openAccess, hasAllowlist) {
	case string(config.InboundPolicyPairing):
		return "1"
	case string(config.InboundPolicyAllowlist):
		return "2"
	case "open":
		return "3"
	case string(config.InboundPolicyDeny):
		return "4"
	}
	if preferAllowlist {
		return "2"
	}
	return "1"
}

func promptRequiredCSV(reader *bufio.Reader, out io.Writer, label string, current []string) ([]string, error) {
	defaultValue := strings.Join(current, ",")
	for {
		answer, err := promptString(reader, out, label, defaultValue)
		if err != nil {
			return nil, err
		}
		items := splitAndCompact(answer)
		if len(items) > 0 {
			return items, nil
		}
		fmt.Fprintln(out, "Enter at least one value.")
	}
}

func splitAndCompact(value string) []string {
	raw := strings.Split(value, ",")
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func allowlistPromptDefault(current []string, fallback string) []string {
	if len(current) > 0 {
		return current
	}
	if strings.TrimSpace(fallback) == "" {
		return nil
	}
	return []string{strings.TrimSpace(fallback)}
}

func channelAccessSummary(policy config.InboundPolicy, openAccess, hasAllowlist bool) string {
	switch config.EffectiveInboundPolicy(policy, openAccess, hasAllowlist) {
	case string(config.InboundPolicyPairing):
		return "pairing"
	case string(config.InboundPolicyAllowlist):
		return "allowlist"
	case "open":
		return "open"
	default:
		return "deny"
	}
}
