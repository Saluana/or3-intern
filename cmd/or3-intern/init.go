package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
)

type initProviderPreset struct {
	label      string
	apiBase    string
	model      string
	embedModel string
}

var initProviderPresets = map[string]initProviderPreset{
	"1": {
		label:      "OpenAI",
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
	"2": {
		label:      "OpenRouter",
		apiBase:    "https://openrouter.ai/api/v1",
		model:      "openai/gpt-4o-mini",
		embedModel: "text-embedding-3-small",
	},
	"3": {
		label:      "Custom OpenAI-compatible",
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
}

func runInit(cfgPath string) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	if supportsInteractiveTUI(os.Stdin, os.Stdout) {
		return runConfigureWithTUI(cfgPathOrDefault(cfgPath), cwd, []string{}, configureTUIOptions{
			Title:      "or3-intern init",
			Intro:      []string{"First-run setup with the essential provider, storage, workspace, and tools sections."},
			Restricted: []string{"provider", "storage", "workspace", "tools"},
		})
	}
	return runInitWithIO(os.Stdin, os.Stdout, cfgPathOrDefault(cfgPath), cwd)
}

func runInitWithIO(in io.Reader, out io.Writer, cfgPath, cwd string) error {
	fmt.Fprintln(out, "or3-intern init")
	fmt.Fprintln(out, "`init` now uses the same configure wizard under the hood with the original first-run sections.")
	fmt.Fprintln(out)
	return runConfigureWithIO(in, out, cfgPath, cwd, []string{
		"--section", "provider",
		"--section", "storage",
		"--section", "workspace",
		"--section", "tools",
	})
}

func initDefaults(cwd string) config.Config {
	cfg := config.Default()
	config.ApplyEnvOverrides(&cfg)
	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		cfg.WorkspaceDir = cwd
		cfg.DBPath = filepath.Join(cwd, ".or3", "or3-intern.sqlite")
		cfg.ArtifactsDir = filepath.Join(cwd, ".or3", "artifacts")
		cfg.Tools.RestrictToWorkspace = true
	}
	return cfg
}

func defaultProviderChoice(apiBase string) string {
	if strings.Contains(strings.ToLower(apiBase), "openrouter.ai") {
		return "2"
	}
	return "1"
}

func applyProviderPreset(cfg *config.Config, choice string) {
	preset, ok := initProviderPresets[choice]
	if !ok || cfg == nil {
		return
	}
	cfg.Provider.APIBase = preset.apiBase
	cfg.Provider.Model = preset.model
	cfg.Provider.EmbedModel = preset.embedModel
}

func promptChoice(reader *bufio.Reader, out io.Writer, label string, options []string, defaultChoice string) (string, error) {
	fmt.Fprintln(out, label)
	for _, option := range options {
		fmt.Fprintf(out, "  %s\n", option)
	}
	for {
		answer, err := promptString(reader, out, "Selection", defaultChoice)
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		if _, ok := initProviderPresets[answer]; ok {
			return answer, nil
		}
		fmt.Fprintln(out, "Please choose 1, 2, or 3.")
	}
}

func promptBool(reader *bufio.Reader, out io.Writer, label string, defaultValue bool) (bool, error) {
	defaultText := "n"
	if defaultValue {
		defaultText = "y"
	}
	for {
		answer, err := promptString(reader, out, label+" (y/n)", defaultText)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Please answer y or n.")
		}
	}
}

func promptString(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

func promptSecretString(reader *bufio.Reader, out io.Writer, label, currentValue string) (string, error) {
	if strings.TrimSpace(currentValue) == "" {
		return promptString(reader, out, label, "")
	}
	_, _ = fmt.Fprintf(out, "%s [leave blank to keep current, type clear to remove]: ", label)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return currentValue, nil
	case strings.EqualFold(line, configureSecretClearKeyword):
		return "", nil
	default:
		return line, nil
	}
}
