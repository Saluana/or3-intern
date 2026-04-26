package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/safetymode"
	"or3-intern/internal/uxcopy"
	"or3-intern/internal/uxstate"
)

type settingsArgs struct {
	Section  string
	Export   string
	Advanced bool
}

func runSettings(cfgPath string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return runSettingsWithIO(os.Stdin, os.Stdout, cfgPathOrDefault(cfgPath), cwd, args)
}

func runSettingsWithIO(in io.Reader, out io.Writer, cfgPath, cwd string, args []string) error {
	parsed, err := parseSettingsArgs(args)
	if err != nil {
		return err
	}
	cfg, existed, loadWarning, err := loadConfigureConfig(cfgPath, cwd)
	if err != nil {
		return err
	}
	if parsed.Export != "" {
		return exportSettingsConfig(out, parsed.Export, cfg)
	}
	if parsed.Section != "" {
		return runSettingsSection(bufio.NewReader(in), out, cfgPath, cwd, cfg, parsed.Section, parsed.Advanced, supportsInteractiveTUI(in, out))
	}
	if supportsInteractiveTUI(in, out) {
		return runSettingsWithTUI(cfgPath, cwd)
	}
	fmt.Fprintln(out, "OR3 settings")
	if existed {
		fmt.Fprintln(out, "Loaded your saved setup.")
	} else {
		fmt.Fprintln(out, "No config found yet. Run `or3-intern setup` to create one.")
	}
	if strings.TrimSpace(loadWarning) != "" {
		fmt.Fprintf(out, "Repair mode: %s\n", loadWarning)
	}
	printSettingsHome(out, uxstate.BuildSettingsHomeView(cfg), parsed.Advanced)
	return nil
}

func runSettingsWithTUI(cfgPath, cwd string) error {
	return runConfigureWithTUI(cfgPath, cwd, nil, configureTUIOptions{
		Title: "or3-intern settings",
		Intro: []string{
			"Review and update OR3 settings using the full interactive configuration UI.",
			"Use `or3-intern settings --section safety` for the plain-language Safety Mode chooser.",
		},
	})
}

func parseSettingsArgs(args []string) (settingsArgs, error) {
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var parsed settingsArgs
	fs.StringVar(&parsed.Section, "section", "", "settings section")
	fs.StringVar(&parsed.Export, "export", "", "write current config JSON to a path, or '-' for stdout")
	fs.BoolVar(&parsed.Advanced, "advanced", false, "show advanced settings sections")
	if err := fs.Parse(args); err != nil {
		return settingsArgs{}, err
	}
	if fs.NArg() > 0 {
		return settingsArgs{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	parsed.Section = strings.ToLower(strings.TrimSpace(parsed.Section))
	return parsed, nil
}

func printSettingsHome(out io.Writer, view uxstate.SettingsHomeView, advanced bool) {
	fmt.Fprintln(out, "\nSettings home")
	index := 1
	for _, section := range view.Sections {
		if section.Advanced && !advanced {
			continue
		}
		fmt.Fprintf(out, "\n%d. %s\n", index, section.Title)
		fmt.Fprintf(out, "  %s\n", section.Summary)
		fmt.Fprintf(out, "  Change: %s\n", section.Action)
		index++
	}
	fmt.Fprintln(out, "\nUseful commands")
	for _, command := range view.Commands {
		fmt.Fprintf(out, "- %s\n", command)
	}
	if !advanced {
		fmt.Fprintln(out, "- or3-intern settings --advanced")
	}
}

func runSettingsSection(reader *bufio.Reader, out io.Writer, cfgPath, cwd string, cfg config.Config, section string, advanced bool, interactive bool) error {
	switch section {
	case "provider":
		if interactive {
			return runConfigureWithTUI(cfgPath, cwd, []string{"--section", "provider"}, settingsConfigureOptions("AI Provider"))
		}
		return runSettingsProvider(reader, out, cfgPath, &cfg)
	case "workspace":
		if interactive {
			return runConfigureWithTUI(cfgPath, cwd, []string{"--section", "workspace"}, settingsConfigureOptions("Workspace Folder"))
		}
		return runSettingsWorkspace(reader, out, cfgPath, cwd, &cfg)
	case "channels":
		if interactive {
			return runConfigureWithTUI(cfgPath, cwd, []string{"--section", "channels"}, settingsConfigureOptions("Channels"))
		}
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "channels")
	case "tools":
		if interactive {
			return runConfigureWithTUI(cfgPath, cwd, []string{"--section", "tools"}, settingsConfigureOptions("Tools"))
		}
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "tools")
	case "memory":
		if interactive {
			return runConfigureWithTUI(cfgPath, cwd, []string{"--section", "docindex"}, settingsConfigureOptions("Memory"))
		}
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "docindex")
	case "context":
		if interactive {
			return runConfigureWithTUI(cfgPath, cwd, []string{"--section", "context"}, settingsConfigureOptions("Context"))
		}
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "context")
	case "devices":
		return runSettingsDevices(reader, out, cfgPath, &cfg, interactive)
	case "safety":
		if interactive {
			return runSettingsSafetyWithTUI(cfgPath, cfg)
		}
		mode, err := promptSetupMode(reader, out, safetymode.Infer(cfg).BaseMode)
		if err != nil {
			return err
		}
		if err := applySafetyModeForSetup(reader, out, &cfg, mode, interactive); err != nil {
			return err
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return err
		}
		inference := safetymode.Infer(cfg)
		fmt.Fprintf(out, "Saved safety mode: %s\n", uxcopy.SafetyModeLabel(inference.Mode, inference.IsCustom, inference.BaseMode))
		return nil
	case "advanced":
		if advanced {
			fmt.Fprintf(out, "Raw config path: %s\n", cfgPath)
			fmt.Fprintln(out, "Advanced commands:")
			fmt.Fprintln(out, "- or3-intern settings --export -")
			fmt.Fprintln(out, "- or3-intern configure")
			fmt.Fprintln(out, "- or3-intern help advanced")
			return nil
		}
		if interactive {
			return runConfigureWithTUI(cfgPath, cwd, nil, settingsConfigureOptions("Advanced"))
		}
		return runConfigureWithIO(reader, out, cfgPath, cwd, nil)
	default:
		return fmt.Errorf("unknown settings section %q", section)
	}
}

func runSettingsDevices(reader *bufio.Reader, out io.Writer, cfgPath string, cfg *config.Config, interactive bool) error {
	fmt.Fprintln(out, "Connected Devices")
	if strings.TrimSpace(cfg.DBPath) == "" {
		return fmt.Errorf("device storage is not available")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return err
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()
	ctx := context.Background()
	if !interactive {
		return runConnectDeviceList(ctx, database, out)
	}
	choice, err := promptMenuChoice(reader, out, "Choose an action", []string{
		"1) Review connected devices",
		"2) Pair a new device",
		"3) Change device access",
		"4) Disconnect a device",
	}, "1")
	if err != nil {
		return err
	}
	switch choice {
	case "2":
		broker, err := ensureConnectDevicePrereqs(cfgPath, cfg, database, nil)
		if err != nil {
			return err
		}
		return runConnectDeviceCommand(ctx, cfgPath, cfg, database, broker, nil, out, out)
	case "3":
		deviceID, err := promptString(reader, out, "Device ID", "")
		if err != nil {
			return err
		}
		broker, err := ensureConnectDevicePrereqs(cfgPath, cfg, database, nil)
		if err != nil {
			return err
		}
		return runConnectDeviceRole(ctx, database, broker, deviceID, reader, out)
	case "4":
		deviceID, err := promptString(reader, out, "Device ID", "")
		if err != nil {
			return err
		}
		broker, err := ensureConnectDevicePrereqs(cfgPath, cfg, database, nil)
		if err != nil {
			return err
		}
		if err := broker.RevokeDevice(ctx, strings.TrimSpace(deviceID), "cli"); err != nil {
			return err
		}
		fmt.Fprintf(out, "Disconnected %s\n", strings.TrimSpace(deviceID))
		return nil
	default:
		return runConnectDeviceList(ctx, database, out)
	}
}

func settingsConfigureOptions(section string) configureTUIOptions {
	return configureTUIOptions{
		Title: "or3-intern settings",
		Intro: []string{"Review and update " + strings.ToLower(section) + " settings."},
	}
}

func runSettingsProvider(reader *bufio.Reader, out io.Writer, cfgPath string, cfg *config.Config) error {
	fmt.Fprintln(out, "AI Provider")
	fmt.Fprintf(out, "Current provider: %s\n\n", providerFriendlyName(cfg.Provider.APIBase))
	choice, err := promptChoice(reader, out, "Choose your AI provider", []string{
		"1) OpenAI",
		"2) OpenRouter",
		"3) Custom OpenAI-compatible provider",
	}, defaultProviderChoice(cfg.Provider.APIBase))
	if err != nil {
		return err
	}
	applyProviderPreset(cfg, choice)
	if choice == "3" {
		cfg.Provider.APIBase, err = promptString(reader, out, "API base", cfg.Provider.APIBase)
		if err != nil {
			return err
		}
	}
	fmt.Fprintln(out, providerAPIKeyHelp(choice))
	cfg.Provider.APIKey, err = promptSecretString(reader, out, "API key", cfg.Provider.APIKey)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Provider.APIKey) == "" && strings.TrimSpace(os.Getenv(providerAPIKeyEnv(choice))) == "" {
		fmt.Fprintln(out, "No API key found. Setup can be saved, but chat will not work until you add one.")
	}
	if err := config.Save(cfgPath, *cfg); err != nil {
		return err
	}
	fmt.Fprintln(out, "Saved AI provider settings.")
	return nil
}

func runSettingsWorkspace(reader *bufio.Reader, out io.Writer, cfgPath, cwd string, cfg *config.Config) error {
	fmt.Fprintln(out, "Workspace Folder")
	fmt.Fprintf(out, "Current folder: %s\n", firstNonEmptyString(cfg.WorkspaceDir, cwd))
	value, err := promptString(reader, out, "Workspace folder", firstNonEmptyString(cfg.WorkspaceDir, cwd))
	if err != nil {
		return err
	}
	cfg.WorkspaceDir = value
	cfg.Tools.RestrictToWorkspace = true
	choice, err := promptMenuChoice(reader, out, "Where should OR3 store its own data?", []string{
		"1) Recommended: OR3 app folder",
		"2) Inside this workspace folder",
	}, "1")
	if err != nil {
		return err
	}
	if choice == "2" && strings.TrimSpace(cfg.WorkspaceDir) != "" {
		cfg.DBPath = filepath.Join(cfg.WorkspaceDir, ".or3", "or3-intern.sqlite")
		cfg.ArtifactsDir = filepath.Join(cfg.WorkspaceDir, ".or3", "artifacts")
	}
	if err := config.Save(cfgPath, *cfg); err != nil {
		return err
	}
	fmt.Fprintln(out, "Saved workspace folder. File access stays limited to this folder.")
	return nil
}

func providerFriendlyName(apiBase string) string {
	switch defaultProviderChoice(apiBase) {
	case "2":
		return "OpenRouter"
	case "3":
		return "Custom OpenAI-compatible provider"
	default:
		return "OpenAI"
	}
}

func providerAPIKeyHelp(choice string) string {
	switch choice {
	case "1":
		return "Paste your OpenAI API key.\nYou can leave this blank if OPENAI_API_KEY is already set."
	case "2":
		return "Paste your OpenRouter API key.\nYou can leave this blank if OR3_API_KEY is already set."
	default:
		return "Paste the API key for your provider.\nYou can leave this blank if OR3_API_KEY is already set."
	}
}

func providerAPIKeyEnv(choice string) string {
	if choice == "1" {
		return "OPENAI_API_KEY"
	}
	return "OR3_API_KEY"
}

func runConfigureSectionAndSave(reader *bufio.Reader, out io.Writer, cfgPath, cwd string, cfg *config.Config, section string) error {
	if err := runConfigureSection(reader, out, cfg, section, cwd); err != nil {
		return err
	}
	if err := config.Save(cfgPath, *cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved %s settings.\n", section)
	return nil
}

func exportSettingsConfig(out io.Writer, target string, cfg config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if strings.TrimSpace(target) == "-" {
		_, err = out.Write(data)
		return err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(out, "Exported advanced config to %s\n", target)
	return nil
}
