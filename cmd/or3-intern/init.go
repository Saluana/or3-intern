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
	return runInitWithIO(os.Stdin, os.Stdout, cfgPathOrDefault(cfgPath), cwd)
}

func runInitWithIO(in io.Reader, out io.Writer, cfgPath, cwd string) error {
	reader := bufio.NewReader(in)
	cfg := initDefaults(cwd)

	fmt.Fprintln(out, "or3-intern setup")
	fmt.Fprintln(out, "We'll create a config file and pick defaults that work well for local testing.")
	fmt.Fprintf(out, "Config file: %s\n\n", cfgPath)

	providerChoice, err := promptChoice(reader, out,
		"Choose your provider",
		[]string{"1) OpenAI", "2) OpenRouter", "3) Custom OpenAI-compatible"},
		defaultProviderChoice(cfg.Provider.APIBase),
	)
	if err != nil {
		return err
	}
	applyProviderPreset(&cfg, providerChoice)

	cfg.Provider.APIBase, err = promptString(reader, out, "API base", cfg.Provider.APIBase)
	if err != nil {
		return err
	}
	cfg.Provider.Model, err = promptString(reader, out, "Chat model", cfg.Provider.Model)
	if err != nil {
		return err
	}
	cfg.Provider.EmbedModel, err = promptString(reader, out, "Embedding model", cfg.Provider.EmbedModel)
	if err != nil {
		return err
	}

	saveKey, err := promptBool(reader, out, "Save API key in config.json (stored locally with restricted permissions; env vars are safer)?", strings.TrimSpace(cfg.Provider.APIKey) != "")
	if err != nil {
		return err
	}
	if saveKey {
		cfg.Provider.APIKey, err = promptString(reader, out, "API key", cfg.Provider.APIKey)
		if err != nil {
			return err
		}
	} else {
		cfg.Provider.APIKey = ""
	}

	cfg.DBPath, err = promptString(reader, out, "SQLite DB path", cfg.DBPath)
	if err != nil {
		return err
	}
	cfg.ArtifactsDir, err = promptString(reader, out, "Artifacts directory", cfg.ArtifactsDir)
	if err != nil {
		return err
	}

	restrictWorkspace, err := promptBool(reader, out, "Restrict file tools to the current workspace?", cfg.Tools.RestrictToWorkspace)
	if err != nil {
		return err
	}
	cfg.Tools.RestrictToWorkspace = restrictWorkspace
	if restrictWorkspace {
		cfg.WorkspaceDir = cwd
	} else if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		cfg.WorkspaceDir = cwd
	}

	cfg.Tools.BraveAPIKey, err = promptString(reader, out, "Brave Search API key (optional)", cfg.Tools.BraveAPIKey)
	if err != nil {
		return err
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Saved config to %s\n", cfgPath)
	fmt.Fprintf(out, "Provider: %s\n", initProviderPresets[providerChoice].label)
	fmt.Fprintf(out, "DB: %s\n", cfg.DBPath)
	fmt.Fprintf(out, "Artifacts: %s\n", cfg.ArtifactsDir)
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) != "" {
		fmt.Fprintf(out, "Workspace restriction: enabled (%s)\n", cfg.WorkspaceDir)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next step:")
	fmt.Fprintln(out, "  go run ./cmd/or3-intern chat")
	return nil
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
