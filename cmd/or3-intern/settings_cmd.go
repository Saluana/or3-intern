package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/safetymode"
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
		return runSettingsSection(bufio.NewReader(in), out, cfgPath, cwd, cfg, parsed.Section)
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
	for _, section := range view.Sections {
		if section.Advanced && !advanced {
			continue
		}
		fmt.Fprintf(out, "\n- %s\n", section.Title)
		fmt.Fprintf(out, "  %s\n", section.Summary)
		fmt.Fprintf(out, "  Change: %s\n", section.Action)
	}
	fmt.Fprintln(out, "\nUseful commands")
	for _, command := range view.Commands {
		fmt.Fprintf(out, "- %s\n", command)
	}
	if !advanced {
		fmt.Fprintln(out, "- or3-intern settings --advanced")
	}
}

func runSettingsSection(reader *bufio.Reader, out io.Writer, cfgPath, cwd string, cfg config.Config, section string) error {
	switch section {
	case "provider":
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "provider")
	case "workspace":
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "workspace")
	case "channels":
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "channels")
	case "tools":
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "tools")
	case "memory":
		return runConfigureSectionAndSave(reader, out, cfgPath, cwd, &cfg, "docindex")
	case "devices":
		fmt.Fprintln(out, "Connected Devices")
		fmt.Fprintln(out, "Use `or3-intern connect-device` to pair a device or `or3-intern connect-device list` to review connected devices.")
		return nil
	case "safety":
		mode, err := promptSetupMode(reader, out, safetymode.Infer(cfg).BaseMode)
		if err != nil {
			return err
		}
		safetymode.Apply(&cfg, mode)
		if err := config.Save(cfgPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Saved safety mode: %s\n", mode)
		return nil
	case "advanced":
		return runConfigureWithIO(reader, out, cfgPath, cwd, nil)
	default:
		return fmt.Errorf("unknown settings section %q", section)
	}
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
