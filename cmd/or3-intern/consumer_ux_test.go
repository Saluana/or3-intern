package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
	"or3-intern/internal/safetymode"
	"or3-intern/internal/uxstate"
)

func seedConsumerConfig(t *testing.T, cfgPath, root string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Provider.APIBase = "https://api.openai.com/v1"
	cfg.Provider.APIKey = "test-key"
	cfg.Provider.Model = "gpt-test"
	cfg.Provider.EmbedModel = "embed-test"
	cfg.WorkspaceDir = root
	cfg.DBPath = filepath.Join(root, ".or3", "or3-intern.sqlite")
	cfg.ArtifactsDir = filepath.Join(root, ".or3", "artifacts")
	cfg.Security.SecretStore.KeyFile = filepath.Join(root, "master.key")
	cfg.Security.Audit.KeyFile = filepath.Join(root, "audit.key")
	cfg.Security.Approvals.KeyFile = filepath.Join(root, "approvals.key")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	return cfg
}

func TestRunSetupWithIO_PreservesExistingProviderModels(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := seedConsumerConfig(t, cfgPath, tmp)
	cfg.Provider.Model = "custom-chat-model"
	cfg.Provider.EmbedModel = "custom-embed-model"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	input := strings.Join([]string{
		"",  // keep provider
		"",  // keep API key
		"",  // keep workspace
		"5", // advanced/manual scenario
		"2", // balanced safety mode
		"n", // do not start chat
		"",
	}, "\n")
	result, err := runSetupWithIO(strings.NewReader(input), &bytes.Buffer{}, cfgPath, tmp)
	if err != nil {
		t.Fatalf("runSetupWithIO: %v", err)
	}
	if result.Config.Provider.Model != "custom-chat-model" {
		t.Fatalf("expected chat model to be preserved, got %q", result.Config.Provider.Model)
	}
	if result.Config.Provider.EmbedModel != "custom-embed-model" {
		t.Fatalf("expected embed model to be preserved, got %q", result.Config.Provider.EmbedModel)
	}
}

func TestRunSetupWithIO_ScenarioToConfigResults(t *testing.T) {
	tests := []struct {
		name           string
		scenarioChoice string
		wantScenario   safetymode.Scenario
		wantService    bool
	}{
		{name: "solo", scenarioChoice: "1", wantScenario: safetymode.ScenarioSoloComputer, wantService: false},
		{name: "phone", scenarioChoice: "2", wantScenario: safetymode.ScenarioPhoneCompanion, wantService: true},
		{name: "private server", scenarioChoice: "3", wantScenario: safetymode.ScenarioPrivateServer, wantService: true},
		{name: "hosted", scenarioChoice: "4", wantScenario: safetymode.ScenarioHostedService, wantService: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			cfgPath := filepath.Join(tmp, "config.json")
			seedConsumerConfig(t, cfgPath, tmp)
			input := strings.Join([]string{"", "", "", tc.scenarioChoice, "", "n", ""}, "\n")
			result, err := runSetupWithIO(strings.NewReader(input), &bytes.Buffer{}, cfgPath, tmp)
			if err != nil {
				t.Fatalf("runSetupWithIO: %v", err)
			}
			if got := safetymode.InferScenario(result.Config); got != tc.wantScenario {
				t.Fatalf("expected scenario %q, got %q", tc.wantScenario, got)
			}
			if result.Config.Service.Enabled != tc.wantService {
				t.Fatalf("expected service enabled %t, got %t", tc.wantService, result.Config.Service.Enabled)
			}
			if result.StartChat {
				t.Fatal("expected setup completion without chat start")
			}
		})
	}
}

func TestRunSetupWithIO_NewSetupDefaultsStateOutsideWorkspace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	cfgPath := filepath.Join(tmp, "config.json")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	input := strings.Join([]string{
		"",        // provider default
		"testkey", // API key
		workspace, // workspace
		"",        // recommended app folder
		"5",       // advanced/manual scenario
		"2",       // balanced safety
		"n",       // do not start chat
		"",
	}, "\n")
	result, err := runSetupWithIO(strings.NewReader(input), &bytes.Buffer{}, cfgPath, tmp)
	if err != nil {
		t.Fatalf("runSetupWithIO: %v", err)
	}
	if strings.Contains(result.Config.DBPath, workspace) {
		t.Fatalf("DB path should default outside workspace, got %q", result.Config.DBPath)
	}
	if strings.Contains(result.Config.ArtifactsDir, workspace) {
		t.Fatalf("artifacts path should default outside workspace, got %q", result.Config.ArtifactsDir)
	}
}

func TestRunSetupWithIO_StartChatCopy(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	seedConsumerConfig(t, cfgPath, tmp)
	input := strings.Join([]string{"", "", "", "5", "2", "y", ""}, "\n")
	var out bytes.Buffer
	result, err := runSetupWithIO(strings.NewReader(input), &out, cfgPath, tmp)
	if err != nil {
		t.Fatalf("runSetupWithIO: %v", err)
	}
	if !result.StartChat || !strings.Contains(out.String(), "Starting chat now.") {
		t.Fatalf("expected start chat copy, got start=%t output=%s", result.StartChat, out.String())
	}

	input = strings.Join([]string{"", "", "", "5", "2", "n", ""}, "\n")
	out.Reset()
	result, err = runSetupWithIO(strings.NewReader(input), &out, cfgPath, tmp)
	if err != nil {
		t.Fatalf("runSetupWithIO: %v", err)
	}
	if result.StartChat || !strings.Contains(out.String(), "Next: run `or3-intern chat`.") {
		t.Fatalf("expected next chat copy, got start=%t output=%s", result.StartChat, out.String())
	}
}

func TestRunSetupWithIOOptions_AutoSetupContinuesOriginalCommand(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	input := strings.Join([]string{
		"",        // provider default
		"testkey", // API key
		workspace, // workspace
		"",        // recommended app folder
		"1",       // solo computer
		"2",       // balanced safety
		"",
	}, "\n")
	var out bytes.Buffer
	result, err := runSetupWithIOOptions(strings.NewReader(input), &out, cfgPath, tmp, setupOptions{
		CompletionNext: "continuing with `or3-intern status`",
		AutoInvoked:    true,
	})
	if err != nil {
		t.Fatalf("runSetupWithIOOptions: %v", err)
	}
	if result.StartChat {
		t.Fatal("auto setup for another command should not start chat")
	}
	text := out.String()
	for _, want := range []string{
		"No saved setup was found",
		"Step 1 of 4: AI provider",
		"Step 4 of 4: Safety level",
		"Next: continuing with `or3-intern status`.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output: %s", want, text)
		}
	}
}

func TestRunStatusCommand_HidesAndShowsAdvancedIDs(t *testing.T) {
	cfg := config.Default()
	var out bytes.Buffer
	if err := runStatusCommand(cfg, "validation failed", nil, &out, false); err != nil {
		t.Fatalf("runStatusCommand: %v", err)
	}
	if strings.Contains(out.String(), "Advanced ID:") {
		t.Fatalf("default status leaked advanced IDs: %s", out.String())
	}
	if !strings.Contains(out.String(), "What OR3 can access") {
		t.Fatalf("expected access dashboard, got %s", out.String())
	}
	out.Reset()
	if err := runStatusCommand(cfg, "validation failed", nil, &out, true); err != nil {
		t.Fatalf("runStatusCommand detailed: %v", err)
	}
	if !strings.Contains(out.String(), "Advanced ID:") {
		t.Fatalf("expected advanced IDs, got %s", out.String())
	}
}

func TestRunStatusCommandWithOptions_AppliesSingleAutomaticFix(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := seedConsumerConfig(t, cfgPath, tmp)
	cfg.Security.Approvals.Enabled = true
	cfg.Security.Approvals.KeyFile = filepath.Join(tmp, "missing-approval.key")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	var out bytes.Buffer
	if err := runStatusCommandWithOptions(cfgPath, cfg, "", nil, &out, statusArgs{Detailed: true, FixID: "approvals.key_path_missing"}); err != nil {
		t.Fatalf("runStatusCommandWithOptions: %v", err)
	}
	if !strings.Contains(out.String(), "Applied fix for approvals.key_path_missing") {
		t.Fatalf("expected applied fix output, got %s", out.String())
	}
	if _, err := os.Stat(cfg.Security.Approvals.KeyFile); err != nil {
		t.Fatalf("expected generated approval key: %v", err)
	}
}

func TestStatusSelectFixes_NumberAndAllAndLegacyID(t *testing.T) {
	findings := []intdoctor.Finding{
		{ID: "first", FixMode: intdoctor.FixModeAutomatic},
		{ID: "second", FixMode: intdoctor.FixModeManual},
		{ID: "third", FixMode: intdoctor.FixModeAutomatic},
	}
	selected, _, err := selectStatusFixes(findings, "1")
	if err != nil || len(selected) != 1 || selected[0].ID != "first" {
		t.Fatalf("number selection failed: %#v err=%v", selected, err)
	}
	selected, _, err = selectStatusFixes(findings, "all")
	if err != nil || len(selected) != 2 {
		t.Fatalf("all selection failed: %#v err=%v", selected, err)
	}
	selected, _, err = selectStatusFixes(findings, "third")
	if err != nil || len(selected) != 1 || selected[0].ID != "third" {
		t.Fatalf("legacy ID selection failed: %#v err=%v", selected, err)
	}
}

func TestRunSettingsWithIO_HomeExportAndSafetySection(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	seedConsumerConfig(t, cfgPath, tmp)
	var out bytes.Buffer
	if err := runSettingsWithIO(strings.NewReader(""), &out, cfgPath, tmp, nil); err != nil {
		t.Fatalf("settings home: %v", err)
	}
	if !strings.Contains(out.String(), "Settings home") || !strings.Contains(out.String(), "AI Provider") || !strings.Contains(out.String(), "Safety") {
		t.Fatalf("unexpected settings home: %s", out.String())
	}
	out.Reset()
	if err := runSettingsWithIO(strings.NewReader(""), &out, cfgPath, tmp, []string{"--export", "-"}); err != nil {
		t.Fatalf("settings export: %v", err)
	}
	if !strings.Contains(out.String(), "\"provider\"") {
		t.Fatalf("expected config JSON export, got %s", out.String())
	}
	out.Reset()
	if err := runSettingsWithIO(strings.NewReader("3\n"), &out, cfgPath, tmp, []string{"--section", "safety"}); err != nil {
		t.Fatalf("settings safety: %v", err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.Security.Approvals.Exec.Mode != config.ApprovalModeDeny {
		t.Fatalf("expected locked down safety to deny exec, got %q", loaded.Security.Approvals.Exec.Mode)
	}
}

func TestPrintSetupReview_UsesActualSafetySummary(t *testing.T) {
	for _, tc := range []struct {
		mode safetymode.Mode
		want string
	}{
		{safetymode.ModeRelaxed, "Fewer prompts"},
		{safetymode.ModeBalanced, "Ask before risky actions"},
		{safetymode.ModeLockedDown, "Blocks dangerous actions"},
	} {
		cfg := config.Default()
		safetymode.Apply(&cfg, tc.mode)
		status := uxstate.BuildStatusView(cfg, intdoctor.Report{}, 0, 0)
		var out bytes.Buffer
		printSetupReview(&out, status)
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("mode %s: expected %q in %s", tc.mode, tc.want, out.String())
		}
	}
}

func TestParseStatusArgs_AcceptsSubcommandAdvancedFlag(t *testing.T) {
	detailed, err := parseStatusArgs([]string{"--advanced"}, false)
	if err != nil {
		t.Fatalf("parseStatusArgs: %v", err)
	}
	if !detailed {
		t.Fatal("expected detailed status output")
	}
}

func TestRunConnectDeviceCommand_RejectsUnknownSubcommand(t *testing.T) {
	err := runConnectDeviceCommand(context.Background(), "", &config.Config{}, nil, nil, []string{"lisst"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected unknown subcommand error")
	}
	if !strings.Contains(err.Error(), "usage: connect-device") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestConnectDeviceListShowsFriendlyActions(t *testing.T) {
	tmp := t.TempDir()
	database, err := db.Open(filepath.Join(tmp, "devices.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, _, err := (&approval.Broker{DB: database, Config: config.Default().Security.Approvals, HostID: "test", SignKey: []byte("0123456789abcdef0123456789abcdef")}).RotateDeviceToken(context.Background(), "device-1", approval.RoleOperator, "Phone", nil); err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}
	var out bytes.Buffer
	if err := runConnectDeviceList(context.Background(), database, &out); err != nil {
		t.Fatalf("runConnectDeviceList: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Phone", "Chat and workspace files", "Last used:", "Change access:", "Disconnect:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output: %s", want, text)
		}
	}
}

func TestConnectDeviceRoleChangesAccess(t *testing.T) {
	tmp := t.TempDir()
	database, err := db.Open(filepath.Join(tmp, "devices.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	broker := &approval.Broker{DB: database, Config: config.Default().Security.Approvals, HostID: "test", SignKey: []byte("0123456789abcdef0123456789abcdef")}
	if _, _, err := broker.RotateDeviceToken(context.Background(), "device-1", approval.RoleViewer, "Phone", nil); err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}
	var out bytes.Buffer
	if err := runConnectDeviceRole(context.Background(), database, broker, "device-1", bufio.NewReader(strings.NewReader("2\n")), &out); err != nil {
		t.Fatalf("runConnectDeviceRole: %v", err)
	}
	device, err := database.GetPairedDevice(context.Background(), "device-1")
	if err != nil {
		t.Fatalf("GetPairedDevice: %v", err)
	}
	if device.Role != approval.RoleOperator {
		t.Fatalf("expected operator role, got %q", device.Role)
	}
	if !strings.Contains(out.String(), "Chat and workspace files") {
		t.Fatalf("expected friendly role output, got %s", out.String())
	}
}

func TestEnsureConnectDevicePrereqsRepairsConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := seedConsumerConfig(t, cfgPath, tmp)
	cfg.Service.Enabled = false
	cfg.Service.Secret = ""
	cfg.Security.Approvals.Enabled = false
	cfg.Security.Approvals.Pairing.Mode = config.ApprovalModeDeny
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	database, err := db.Open(filepath.Join(tmp, "devices.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := ensureConnectDevicePrereqs(cfgPath, &cfg, database, nil); err != nil {
		t.Fatalf("ensureConnectDevicePrereqs: %v", err)
	}
	if !cfg.Service.Enabled || strings.TrimSpace(cfg.Service.Secret) == "" || !cfg.Security.Approvals.Enabled || cfg.Security.Approvals.Pairing.Mode != config.ApprovalModeAsk {
		t.Fatalf("expected repaired config, got %#v", cfg)
	}
	if _, err := os.Stat(cfg.Security.Approvals.KeyFile); err != nil {
		t.Fatalf("expected approval key: %v", err)
	}
}

func TestFormatPairingCode(t *testing.T) {
	if got := formatPairingCode("123456"); got != "123-456" {
		t.Fatalf("unexpected formatted code: %q", got)
	}
}
