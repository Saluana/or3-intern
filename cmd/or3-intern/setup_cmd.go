package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/config"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/providers"
	"or3-intern/internal/safetymode"
	"or3-intern/internal/security"
	"or3-intern/internal/uxcopy"
	"or3-intern/internal/uxstate"
)

type setupResult struct {
	StartChat bool
	Config    config.Config
}

type setupOptions struct {
	AskStartChat     bool
	StartChatDefault bool
	CompletionNext   string
	AutoInvoked      bool
	ProviderProbe    setupProviderProbeFunc
	ProbeTimeout     time.Duration
}

type setupProviderProbeFunc func(context.Context, config.Config) setupProviderProbeReport

type setupProviderProbeReport struct {
	Checks []setupProviderProbeCheck
	Ready  bool
}

type setupProviderProbeCheck struct {
	Name    string
	OK      bool
	Message string
}

func runSetup(cfgPath string) (setupResult, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return runSetupWithIOOptions(os.Stdin, os.Stdout, cfgPathOrDefault(cfgPath), cwd, setupOptions{
		AskStartChat:     true,
		StartChatDefault: true,
		CompletionNext:   "run `or3-intern chat`",
	})
}

func runSetupWithIO(in io.Reader, out io.Writer, cfgPath, cwd string) (setupResult, error) {
	return runSetupWithIOOptions(in, out, cfgPath, cwd, setupOptions{
		AskStartChat:     true,
		StartChatDefault: true,
		CompletionNext:   "run `or3-intern chat`",
	})
}

func runSetupWithIOOptions(in io.Reader, out io.Writer, cfgPath, cwd string, options setupOptions) (setupResult, error) {
	reader := bufio.NewReader(in)
	cfg, existed, _, err := loadConfigureConfig(cfgPath, cwd)
	if err != nil {
		return setupResult{}, err
	}
	fmt.Fprintln(out, "OR3 setup")
	if options.AutoInvoked {
		fmt.Fprintln(out, "No saved setup was found, so OR3 will create one before continuing.")
		fmt.Fprintln(out, "This only needs to happen once. You can change these choices later with `or3-intern settings`.")
	}
	if existed {
		fmt.Fprintln(out, "Loaded your current settings. Leave answers blank to keep what you already have.")
	} else {
		fmt.Fprintln(out, "We'll choose three basics: the AI service to use, the folder OR3 may work in, and how cautious OR3 should be.")
	}
	fmt.Fprintf(out, "Config file: %s\n", cfgPath)
	fmt.Fprintln(out)

	previousAPIBase := cfg.Provider.APIBase
	previousModel := cfg.Provider.Model
	previousEmbedModel := cfg.Provider.EmbedModel
	fmt.Fprintln(out, "Step 1 of 4: AI provider")
	fmt.Fprintln(out, "The provider is the outside AI service OR3 sends your messages to. OR3 keeps the rest of its working files on this computer.")
	providerChoice, err := promptChoice(reader, out, "Choose your AI provider", []string{
		"1) OpenAI",
		"2) OpenRouter",
		"3) Custom OpenAI-compatible provider",
	}, defaultProviderChoice(cfg.Provider.APIBase))
	if err != nil {
		return setupResult{}, err
	}
	applyProviderPreset(&cfg, providerChoice)
	if existed && providerChoice == defaultProviderChoice(previousAPIBase) {
		cfg.Provider.Model = previousModel
		cfg.Provider.EmbedModel = previousEmbedModel
	}
	if providerChoice == "3" {
		cfg.Provider.APIBase, err = promptString(reader, out, "Provider API base", cfg.Provider.APIBase)
		if err != nil {
			return setupResult{}, err
		}
	}
	fmt.Fprintln(out, providerAPIKeyHelp(providerChoice))
	envKeyName := providerAPIKeyEnv(providerChoice)
	envKey := strings.TrimSpace(os.Getenv(envKeyName))
	promptDefaultKey := cfg.Provider.APIKey
	if !existed && envKey != "" && promptDefaultKey == envKey {
		promptDefaultKey = ""
		fmt.Fprintf(out, "Found %s in your environment. Leave the API key blank to use that without saving it in config.\n", envKeyName)
	}
	fmt.Fprintln(out, "This key is like a password for billing and access to that AI service. Prefer an environment variable or secret store; paste it here only for local-only config storage on this computer.")
	cfg.Provider.APIKey, err = promptSecretString(reader, out, "API key", promptDefaultKey)
	if err != nil {
		return setupResult{}, err
	}
	if !existed && envKey != "" && strings.TrimSpace(cfg.Provider.APIKey) == "" {
		clearSetupProviderProfileKey(&cfg)
	}
	effectiveCfg := cfg
	if strings.TrimSpace(effectiveCfg.Provider.APIKey) == "" && envKey != "" {
		effectiveCfg.Provider.APIKey = envKey
	}
	probeReport := setupProviderProbeReport{Ready: true}
	if strings.TrimSpace(effectiveCfg.Provider.APIKey) == "" {
		fmt.Fprintln(out, "No API key found. Setup can still be saved, but chat will not be able to contact the AI provider until you add one.")
		probeReport = setupProviderProbeReport{Ready: false, Checks: []setupProviderProbeCheck{
			{Name: "API key", OK: false, Message: "Missing provider key."},
		}}
	} else {
		probeReport = runSetupProviderProbe(in, out, effectiveCfg, options)
		printSetupProviderProbe(out, probeReport)
	}

	fmt.Fprintln(out, "\nStep 2 of 4: Workspace folder")
	fmt.Fprintln(out, "The workspace is the folder OR3 is allowed to read and edit. Choosing a specific folder gives OR3 a clear boundary.")
	cfg.WorkspaceDir, err = promptString(reader, out, "Workspace folder", firstNonEmptyString(cfg.WorkspaceDir, cwd))
	if err != nil {
		return setupResult{}, err
	}
	cfg.Tools.RestrictToWorkspace = true
	fmt.Fprintf(out, "File access will be limited to: %s\n", cfg.WorkspaceDir)
	if !existed {
		fmt.Fprintln(out, "\nOR3 also needs a small private place for its database, logs, and generated files.")
		storageChoice, err := promptMenuChoice(reader, out, "Where should OR3 store its own data?", []string{
			"1) Recommended: OR3 app folder",
			"2) Inside this workspace folder",
		}, "1")
		if err != nil {
			return setupResult{}, err
		}
		if storageChoice == "2" && strings.TrimSpace(cfg.WorkspaceDir) != "" {
			cfg.DBPath = filepath.Join(cfg.WorkspaceDir, ".or3", "or3-intern.sqlite")
			cfg.ArtifactsDir = filepath.Join(cfg.WorkspaceDir, ".or3", "artifacts")
		}
	} else if strings.TrimSpace(cfg.DBPath) == "" && strings.TrimSpace(cfg.WorkspaceDir) != "" {
		cfg.DBPath = filepath.Join(cfg.WorkspaceDir, ".or3", "or3-intern.sqlite")
		cfg.ArtifactsDir = filepath.Join(cfg.WorkspaceDir, ".or3", "artifacts")
	}
	fmt.Fprintf(out, "OR3 data will be stored at: %s\n", cfg.DBPath)

	fmt.Fprintln(out, "\nStep 3 of 4: How OR3 will be used")
	fmt.Fprintln(out, "This decides whether OR3 should prepare for only this computer, a paired phone, or a service that other devices can reach.")
	scenario, err := promptSetupScenario(reader, out)
	if err != nil {
		return setupResult{}, err
	}
	fmt.Fprintln(out, "\nStep 4 of 4: Safety level")
	fmt.Fprintln(out, "The safety level controls when OR3 asks before doing sensitive things, such as running local commands, using secrets, or sending messages.")
	mode, err := promptSetupMode(reader, out, safetymode.RecommendMode(scenario))
	if err != nil {
		return setupResult{}, err
	}
	safetymode.ApplyScenario(&cfg, scenario)
	if err := applySafetyModeForSetup(reader, out, &cfg, mode, !isNonInteractiveIO(in, out)); err != nil {
		return setupResult{}, err
	}
	if err := ensureSetupSecurityAssets(&cfg); err != nil {
		return setupResult{}, err
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return setupResult{}, err
	}

	report := intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeConfigurePostSave, ConfigPath: cfgPath})
	if applied, err := intdoctor.ApplyAutomaticFixes(cfgPath, &cfg, report); err != nil {
		return setupResult{}, err
	} else if len(applied) > 0 {
		fmt.Fprintln(out, "\nApplied safe repairs")
		for _, fix := range applied {
			fmt.Fprintf(out, "- %s: %s\n", fix.ID, fix.Summary)
		}
		report = intdoctor.Evaluate(cfg, intdoctor.Options{Mode: intdoctor.ModeConfigurePostSave, ConfigPath: cfgPath})
	}
	status := uxstate.BuildStatusView(cfg, report, 0, 0)
	printSetupReview(out, status)
	effectiveReadyCfg := cfg
	if strings.TrimSpace(effectiveReadyCfg.Provider.APIKey) == "" && envKey != "" {
		effectiveReadyCfg.Provider.APIKey = envKey
	}
	readiness := config.EvaluateReadiness(effectiveReadyCfg, config.ReadinessOptions{Command: "chat"})
	readyToChat := probeReport.Ready && (readiness.State == config.ReadinessReady || readiness.State == config.ReadinessAdvancedCustom)
	startChat := false
	if options.AskStartChat && readyToChat {
		startChat, err = promptBool(reader, out, "Start chat next", options.StartChatDefault)
		if err != nil {
			return setupResult{}, err
		}
	}
	if readyToChat {
		MarkMilestone(&cfg, MilestoneSetupComplete)
		if err := config.Save(cfgPath, cfg); err != nil {
			return setupResult{}, err
		}
		PrintSetupSuccess(out, cfg, readyToChat)
	} else {
		fmt.Fprintln(out, "\nSaved draft setup.")
		fmt.Fprintln(out, "Chat will be available after the provider settings pass setup checks.")
		fmt.Fprintln(out, "Next: run `or3-intern setup` after adding provider credentials, or run `or3-intern health` for repair guidance.")
	}
	if startChat {
		fmt.Fprintln(out, "Starting chat now.")
	}
	return setupResult{StartChat: startChat, Config: cfg}, nil
}

func runSetupProviderProbe(in io.Reader, out io.Writer, cfg config.Config, options setupOptions) setupProviderProbeReport {
	timeout := options.ProbeTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if options.ProviderProbe != nil {
		return options.ProviderProbe(ctx, cfg)
	}
	if isNonInteractiveIO(in, out) {
		return staticSetupProviderProbe(cfg)
	}
	return defaultSetupProviderProbe(ctx, cfg)
}

func staticSetupProviderProbe(cfg config.Config) setupProviderProbeReport {
	checks := []setupProviderProbeCheck{
		{Name: "Provider endpoint", OK: strings.TrimSpace(cfg.Provider.APIBase) != "", Message: "Endpoint is configured."},
		{Name: "API key", OK: strings.TrimSpace(cfg.Provider.APIKey) != "", Message: "Provider key is available."},
		{Name: "Chat model", OK: strings.TrimSpace(cfg.Provider.Model) != "", Message: "Chat model is configured."},
	}
	if strings.TrimSpace(cfg.Provider.EmbedModel) != "" {
		checks = append(checks, setupProviderProbeCheck{Name: "Embedding model", OK: true, Message: "Embedding model is configured."})
	}
	return setupProviderProbeReport{Checks: checks, Ready: setupProbeChecksReady(checks)}
}

func defaultSetupProviderProbe(ctx context.Context, cfg config.Config) setupProviderProbeReport {
	report := staticSetupProviderProbe(cfg)
	if !report.Ready {
		return report
	}
	client := providers.New(strings.TrimRight(cfg.Provider.APIBase, "/"), cfg.Provider.APIKey, 8*time.Second)
	client.EmbedDimensions = cfg.Provider.EmbedDimensions
	if _, err := client.Chat(ctx, providers.ChatCompletionRequest{
		Model: cfg.Provider.Model,
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "Reply with OK."},
		},
		Temperature: 0,
	}); err != nil {
		report.Checks = append(report.Checks, setupProviderProbeCheck{Name: "Chat model probe", OK: false, Message: err.Error()})
		report.Ready = false
		return report
	}
	report.Checks = append(report.Checks, setupProviderProbeCheck{Name: "Chat model probe", OK: true, Message: "Provider accepted a short chat request."})
	if strings.TrimSpace(cfg.Provider.EmbedModel) != "" {
		if _, err := client.Embed(ctx, cfg.Provider.EmbedModel, "OR3 setup probe"); err != nil {
			report.Checks = append(report.Checks, setupProviderProbeCheck{Name: "Embedding model probe", OK: false, Message: err.Error()})
			report.Ready = false
			return report
		}
		report.Checks = append(report.Checks, setupProviderProbeCheck{Name: "Embedding model probe", OK: true, Message: "Provider accepted a short embedding request."})
	}
	return report
}

func printSetupProviderProbe(out io.Writer, report setupProviderProbeReport) {
	fmt.Fprintln(out, "\nProvider checks")
	for _, check := range report.Checks {
		mark := "OK"
		if !check.OK {
			mark = "Needs attention"
		}
		fmt.Fprintf(out, "- %s: %s", check.Name, mark)
		if strings.TrimSpace(check.Message) != "" {
			fmt.Fprintf(out, " — %s", check.Message)
		}
		fmt.Fprintln(out)
	}
}

func setupProbeChecksReady(checks []setupProviderProbeCheck) bool {
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

func clearSetupProviderProfileKey(cfg *config.Config) {
	if cfg == nil || cfg.Providers == nil {
		return
	}
	key := configureProviderKeyFromBase(cfg.Provider.APIBase)
	profile, ok := cfg.Providers[key]
	if !ok {
		return
	}
	profile.APIKey = ""
	cfg.Providers[key] = profile
}

func promptSetupScenario(reader *bufio.Reader, out io.Writer) (safetymode.Scenario, error) {
	options := safetymode.ScenarioOptions()
	menu := make([]string, 0, len(options))
	for index, option := range options {
		menu = append(menu, fmt.Sprintf("%d) %s", index+1, option.Label))
	}
	choice, err := promptMenuChoice(reader, out, "Where are you using OR3?", menu, "1")
	if err != nil {
		return safetymode.ScenarioAdvanced, err
	}
	selected := options[0].Scenario
	for index, option := range options {
		if choice == fmt.Sprintf("%d", index+1) {
			selected = option.Scenario
			break
		}
	}
	return selected, nil
}

func promptSetupMode(reader *bufio.Reader, out io.Writer, recommended safetymode.Mode) (safetymode.Mode, error) {
	defaultChoice := "2"
	if recommended == safetymode.ModeLockedDown {
		defaultChoice = "3"
	}
	choice, err := promptMenuChoice(reader, out, "Choose a safety mode", []string{
		"1) Relaxed — Good for local testing. Fewer prompts.",
		"2) Balanced — Recommended. Ask before risky actions.",
		"3) Locked Down — Best for servers or shared devices.",
	}, defaultChoice)
	if err != nil {
		return safetymode.ModeBalanced, err
	}
	switch choice {
	case "1":
		return safetymode.ModeRelaxed, nil
	case "3":
		return safetymode.ModeLockedDown, nil
	default:
		return safetymode.ModeBalanced, nil
	}
}

func ensureSetupSecurityAssets(cfg *config.Config) error {
	if cfg.Security.SecretStore.Enabled && strings.TrimSpace(cfg.Security.SecretStore.KeyFile) != "" {
		if _, err := security.LoadOrCreateKey(cfg.Security.SecretStore.KeyFile); err != nil {
			return err
		}
	}
	if cfg.Security.Audit.Enabled && strings.TrimSpace(cfg.Security.Audit.KeyFile) != "" {
		if _, err := security.LoadOrCreateKey(cfg.Security.Audit.KeyFile); err != nil {
			return err
		}
	}
	if cfg.Security.Approvals.Enabled && strings.TrimSpace(cfg.Security.Approvals.KeyFile) != "" {
		if _, err := security.LoadOrCreateKey(cfg.Security.Approvals.KeyFile); err != nil {
			return err
		}
	}
	if cfg.Service.Enabled && strings.TrimSpace(cfg.Service.Secret) == "" {
		secret, err := intdoctor.GenerateSecret()
		if err != nil {
			return err
		}
		cfg.Service.Secret = secret
	}
	return nil
}

func applySafetyModeForSetup(reader *bufio.Reader, out io.Writer, cfg *config.Config, mode safetymode.Mode, interactive bool) error {
	if mode != safetymode.ModeLockedDown {
		safetymode.Apply(cfg, mode)
		return nil
	}
	defaultSandboxPath := strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath)
	if defaultSandboxPath == "" {
		defaultSandboxPath = config.Default().Hardening.Sandbox.BubblewrapPath
	}
	if sandboxToolAvailable(defaultSandboxPath) {
		safetymode.Apply(cfg, mode)
		return nil
	}
	if interactive {
		fmt.Fprintln(out, "\nLocked Down works best with command isolation.")
		fmt.Fprintln(out, "This system does not appear to have the required sandbox tool.")
		choice, err := promptMenuChoice(reader, out, "Choose", []string{
			"1) Block local commands instead",
			"2) Use sandboxing anyway",
			"3) Choose Balanced instead",
		}, "1")
		if err != nil {
			return err
		}
		switch choice {
		case "2":
			safetymode.Apply(cfg, mode)
		case "3":
			safetymode.Apply(cfg, safetymode.ModeBalanced)
		default:
			applyLockedDownNoSandbox(cfg)
			fmt.Fprintln(out, "Local commands will be blocked instead.")
		}
		return nil
	}
	applyLockedDownNoSandbox(cfg)
	fmt.Fprintln(out, "Locked Down works best with command isolation.")
	fmt.Fprintln(out, "This system does not appear to have the required sandbox tool, so local commands will be blocked instead.")
	return nil
}

func applyLockedDownNoSandbox(cfg *config.Config) {
	safetymode.Apply(cfg, safetymode.ModeLockedDown)
	cfg.Hardening.Sandbox.Enabled = false
	cfg.Security.Approvals.Exec.Mode = config.ApprovalModeDeny
	cfg.Tools.RestrictToWorkspace = true
}

func sandboxToolAvailable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if strings.Contains(path, string(os.PathSeparator)) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
		return false
	}
	_, err := exec.LookPath(path)
	return err == nil
}

func isNonInteractiveIO(in io.Reader, out io.Writer) bool {
	return !supportsInteractiveTUI(in, out)
}

func printSetupReview(out io.Writer, status uxstate.StatusView) {
	fmt.Fprintln(out, "\nSetup review")
	fmt.Fprintf(out, "- Safety: %s\n", status.SafetyLabel)
	fmt.Fprintf(out, "- Files: %s\n", status.Workspace)
	fmt.Fprintf(out, "- Commands: %s\n", status.Commands)
	fmt.Fprintf(out, "- Internet: %s\n", status.Internet)
	fmt.Fprintf(out, "- Devices: %s\n", status.Devices)
	fmt.Fprintf(out, "- Activity log: %s\n", status.ActivityLog)
	if len(status.Problems) > 0 {
		fmt.Fprintln(out, "- A few things still need attention:")
		for _, problem := range status.Problems {
			fmt.Fprintf(out, "  - %s — %s\n", problem.Title, problem.RecommendedAction)
		}
	} else {
		inferenceMode := safetymode.NormalizeMode(status.SafetyLabel)
		if inferenceMode == safetymode.ModeCustom {
			inferenceMode = safetymode.ModeBalanced
			if strings.Contains(strings.ToLower(status.SafetyLabel), "relaxed") {
				inferenceMode = safetymode.ModeRelaxed
			}
			if strings.Contains(strings.ToLower(status.SafetyLabel), "locked") {
				inferenceMode = safetymode.ModeLockedDown
			}
		}
		fmt.Fprintf(out, "- %s\n", uxcopy.SafetyModeSummary(inferenceMode))
	}
}
