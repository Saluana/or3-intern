package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	remoteconnect "or3-intern/internal/connect"
)

func TestMergeHermesEnvPreservesUnrelatedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PROVIDER_KEY=keep-me\nAPI_SERVER_PORT=9000\n# keep comments\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeHermesEnv(path, "https://or3.example", "or3-secret"); err != nil {
		t.Fatalf("mergeHermesEnv: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, expected := range []string{
		"PROVIDER_KEY=keep-me",
		"API_SERVER_ENABLED=true",
		"API_SERVER_HOST=127.0.0.1",
		"API_SERVER_PORT=8642",
		"API_SERVER_CORS_ORIGINS=https://or3.example",
		"API_SERVER_KEY=or3-secret",
		"# keep comments",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("merged Hermes environment missing %q: %s", expected, text)
		}
	}
	if mode := (func() os.FileMode { info, _ := os.Stat(path); return info.Mode().Perm() })(); mode != 0o600 {
		t.Fatalf("Hermes environment mode = %o, want 600", mode)
	}
}

func TestMergeHermesEnvUsesTheDiscoveredPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := mergeHermesEnvAtPort(path, "https://or3.example", "", 9123); err != nil {
		t.Fatalf("mergeHermesEnvAtPort: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "API_SERVER_PORT=9123") {
		t.Fatalf("custom Hermes port was not preserved: %s", body)
	}
}

func TestMergeHermesEnvRestrictsCorsOriginsToOR3Cloud(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("API_SERVER_CORS_ORIGINS=https://existing.example, https://or3.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeHermesEnvAtPort(path, "https://or3.example", "", 8642); err != nil {
		t.Fatalf("mergeHermesEnvAtPort: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); !strings.Contains(got, "API_SERVER_CORS_ORIGINS=https://or3.example") || strings.Contains(got, "existing.example") {
		t.Fatalf("Hermes CORS origins were not restricted to OR3 Cloud: %s", got)
	}
}

func TestMergeHermesEnvReplacesWildcardCorsOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("API_SERVER_CORS_ORIGINS=*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeHermesEnvAtPort(path, "https://or3.example", "", 8642); err != nil {
		t.Fatalf("mergeHermesEnvAtPort: %v", err)
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(body); !strings.Contains(got, "API_SERVER_CORS_ORIGINS=https://or3.example") || strings.Contains(got, "API_SERVER_CORS_ORIGINS=*") {
		t.Fatalf("wildcard Hermes CORS origin was not replaced: %q", body)
	}
}

func TestUpdateOpenClawConfigMergesRunsPluginWithoutDroppingPlugins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openclaw.json")
	original := map[string]any{
		"gateway": map[string]any{
			"auth": map[string]any{"token": "gateway-secret"},
		},
		"plugins": map[string]any{
			"allow": []any{"other-plugin"},
			"entries": map[string]any{
				"other-plugin": map[string]any{"config": map[string]any{"enabled": true}},
			},
		},
	}
	body, _ := json.Marshal(original)
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateOpenClawConfig(path, "https://or3.example", "or3-secret"); err != nil {
		t.Fatalf("updateOpenClawConfig: %v", err)
	}
	updated, err := readJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plugins := updated["plugins"].(map[string]any)
	allow := plugins["allow"].([]any)
	if len(allow) != 2 || allow[0] != "other-plugin" || allow[1] != "or3-runs" {
		t.Fatalf("plugin allow list was not merged: %#v", allow)
	}
	entries := plugins["entries"].(map[string]any)
	if entries["other-plugin"] == nil {
		t.Fatal("unrelated plugin was removed")
	}
	runsConfig := entries["or3-runs"].(map[string]any)["config"].(map[string]any)
	if runsConfig["token"] != "or3-secret" || runsConfig["gatewayToken"] != "gateway-secret" {
		t.Fatalf("Runs credentials were not configured safely: %#v", runsConfig)
	}
	if got := runsConfig["allowedOrigins"].([]any); len(got) != 1 || got[0] != "https://or3.example" {
		t.Fatalf("unexpected CORS origins: %#v", got)
	}
}

func TestUpdateOpenClawConfigPreservesExistingPluginOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openclaw.json")
	initial := `{"plugins":{"entries":{"or3-runs":{"config":{"allowedOrigins":["https://existing.example"]}}}}}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateOpenClawConfig(path, "https://or3.example", ""); err != nil {
		t.Fatalf("updateOpenClawConfig: %v", err)
	}
	updated, err := readJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	origins := updated["plugins"].(map[string]any)["entries"].(map[string]any)["or3-runs"].(map[string]any)["config"].(map[string]any)["allowedOrigins"].([]any)
	if len(origins) != 2 || origins[0] != "https://existing.example" || origins[1] != "https://or3.example" {
		t.Fatalf("existing plugin origins were not preserved: %#v", origins)
	}
}

func TestUpdateOpenClawConfigRejectsWildcardCorsOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openclaw.json")
	initial := `{"plugins":{"entries":{"or3-runs":{"config":{"allowedOrigins":["*"]}}}}}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateOpenClawConfig(path, "https://or3.example", ""); err == nil {
		t.Fatal("wildcard OpenClaw CORS origin was accepted")
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != initial {
		t.Fatalf("failed OpenClaw configuration was mutated: %q", body)
	}
}

func TestUpdateOpenClawConfigRejectsMalformedKnownTypesWithoutWriting(t *testing.T) {
	for name, initial := range map[string]string{
		"plugins":        `{"plugins":"unexpected"}`,
		"plugin allow":   `{"plugins":{"allow":"unexpected"}}`,
		"plugin entries": `{"plugins":{"entries":[]}}`,
		"plugin config":  `{"plugins":{"entries":{"or3-runs":{"config":false}}}}`,
		"origins":        `{"plugins":{"entries":{"or3-runs":{"config":{"allowedOrigins":"unexpected"}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "openclaw.json")
			if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := updateOpenClawConfig(path, "https://or3.example", ""); err == nil {
				t.Fatal("malformed OpenClaw configuration was accepted")
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != initial {
				t.Fatalf("malformed OpenClaw configuration was changed: %q", body)
			}
		})
	}
}

func TestOpenClawBindMustRemainLoopback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(path, []byte(`{"gateway":{"bind":"lan"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := openClawBind(path); got != "lan" {
		t.Fatalf("openClawBind = %q, want lan", got)
	}
	if isLoopbackBind("lan") || !isLoopbackBind("loopback") || !isLoopbackBind("127.0.0.1") {
		t.Fatal("loopback bind validation did not distinguish loopback from LAN")
	}
}

func TestOpenClawModelReadinessRequiresAnAvailableModel(t *testing.T) {
	if !hasUsableOpenClawModel(`{"models":[{"id":"openai/gpt-5","available":true,"missing":false}]}`) {
		t.Fatal("available OpenClaw model was rejected")
	}
	if !hasUsableOpenClawModel(`{"models":[{"key":"openai/gpt-5"}]}`) {
		t.Fatal("model without an availability flag was rejected")
	}
	for _, output := range []string{
		`{"models":[]}`,
		`{"models":[{"available":false,"missing":true}]}`,
		"not-json",
	} {
		if hasUsableOpenClawModel(output) {
			t.Fatalf("unusable OpenClaw models were accepted: %s", output)
		}
	}
}

func TestHermesModelReadinessRequiresAConfiguredModel(t *testing.T) {
	if !hasHermesModel("  Model: tencent/hy3:free\n") {
		t.Fatal("configured Hermes model was rejected")
	}
	for _, output := range []string{"Model: (not set)", "Provider: Nous Portal", ""} {
		if hasHermesModel(output) {
			t.Fatalf("unconfigured Hermes model was accepted: %s", output)
		}
	}
}

func TestHermesDoctorFailureIsResumableOnboarding(t *testing.T) {
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "hermes")
	script := `#!/bin/sh
case "$1" in
  --version) echo "Hermes 0.20.0" ;;
  doctor) echo "provider setup is incomplete" >&2; exit 1 ;;
  *) exit 0 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	_, err := prepareHermesWithInput(context.Background(), PrepareInput{
		CloudOrigin: "https://or3.example",
		Confirm:     func(string) (bool, error) { return true, nil },
		Stdout:      io.Discard,
		Stderr:      io.Discard,
	})
	var readiness *runtimePreparationError
	if !errors.As(err, &readiness) {
		t.Fatalf("Hermes doctor failure was not resumable: %v", err)
	}
	if !strings.Contains(readiness.nextStep, "hermes setup") {
		t.Fatalf("unexpected Hermes onboarding guidance: %q", readiness.nextStep)
	}
}

func TestOpenClawVersionCompatibilityMatchesPinnedPluginLine(t *testing.T) {
	for _, version := range []string{"OpenClaw 2026.7.1-2", "2026.7.1", "2026.7.9"} {
		if !openClawVersionCompatible(version) {
			t.Fatalf("compatible version rejected: %s", version)
		}
	}
	for _, version := range []string{"2026.7.1-1", "2026.7.0", "2026.8.0", "2025.12.1"} {
		if openClawVersionCompatible(version) {
			t.Fatalf("incompatible version accepted: %s", version)
		}
	}
	if openClawVersionCompatible("future banner without semver") {
		t.Fatal("unknown version banner was accepted")
	}
}

func TestOpenClawPluginVersionMustMatchThePinnedRelease(t *testing.T) {
	if !openClawPluginVersionMatches(`{"plugin":{"id":"or3-runs","version":"0.1.0","source":"npm","enabled":true}}`, "0.1.0") {
		t.Fatal("pinned plugin version was not detected")
	}
	if !openClawPluginReady(`{"plugin":{"id":"or3-runs","version":"0.1.0","source":"npm:@or3/openclaw@0.1.0","enabled":true}}`, "0.1.0") {
		t.Fatal("enabled pinned npm plugin was not detected")
	}
	for _, output := range []string{
		`{"plugin":{"id":"or3-runs","version":"0.0.9","source":"npm"}}`,
		`{"plugin":{"id":"other-plugin","version":"0.1.0","source":"npm"}}`,
		`{"plugin":{"id":"or3-runs","version":"0.1.0","source":"local"}}`,
		`{"plugin":{"id":"or3-runs","version":"0.1.0","source":"npm","enabled":false}}`,
		`not-json`,
	} {
		if openClawPluginReady(output, "0.1.0") {
			t.Fatalf("unmatched plugin inspection was accepted: %s", output)
		}
	}
}

func TestRuntimeConsentRequiresTheExactApprovedAction(t *testing.T) {
	t.Setenv("OR3_CONNECT_YES", "1")
	t.Setenv("OR3_CONNECT_APPROVE", "plugin-install, source-patch")
	if runtimeConsentApproved(runtimeConsentInstaller) {
		t.Fatal("installer was approved without its explicit consent token")
	}
	if !runtimeConsentApproved(runtimeConsentPluginInstall) || !runtimeConsentApproved(runtimeConsentSourcePatch) {
		t.Fatal("explicit runtime consent tokens were not accepted")
	}
}

func TestFirstAbsolutePathIgnoresRuntimeWarnings(t *testing.T) {
	got := firstAbsolutePath("warning: using defaults\nConfig file: /Users/test/.openclaw.json\n", ".json")
	if got != "/Users/test/.openclaw.json" {
		t.Fatalf("firstAbsolutePath = %q", got)
	}
}

func TestNormalizedCloudOriginRejectsNonOriginsAndInsecureRemoteURLs(t *testing.T) {
	if got, err := normalizedCloudOrigin("https://OR3.example/"); err != nil || got != "https://OR3.example" {
		t.Fatalf("normalizedCloudOrigin = %q, %v", got, err)
	}
	for _, value := range []string{
		"ftp://or3.example",
		"http://or3.example",
		"https://or3.example/connect",
		"https://or3.example/?token=secret",
	} {
		if _, err := normalizedCloudOrigin(value); err == nil {
			t.Fatalf("normalizedCloudOrigin accepted %q", value)
		}
	}
}

func TestExternalRuntimeCredentialValidationDoesNotAcceptIncompleteScopes(t *testing.T) {
	credential := remoteconnect.DeviceCredential{
		ControlToken: "token",
		Namespace:    "or3-chat:workspace:",
		WorkspaceID:  "workspace",
		AccountID:    "account",
	}
	if err := validateDeviceCredential(credential); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
	credential.ControlToken = ""
	if err := validateDeviceCredential(credential); err == nil {
		t.Fatal("incomplete credential unexpectedly accepted")
	}
}

func TestExternalRuntimeRemoteTargetValidatesTunnelMetadata(t *testing.T) {
	local := &RuntimeConnectionTarget{
		LocalOrigin: "http://127.0.0.1:8642",
		BasePath:    "/",
		plan:        &externalRuntimePlan{cloudOrigin: "https://or3.example"},
	}
	state := remoteconnect.State{Hostname: "runtime.example", BasePath: "/or3/", ControlToken: "secret"}
	remote, err := externalRuntimeRemoteTarget(local, state)
	if err != nil {
		t.Fatalf("externalRuntimeRemoteTarget: %v", err)
	}
	if remote.LocalOrigin != "https://runtime.example" || remote.BasePath != "/or3/" || remote.AccessToken != "secret" {
		t.Fatalf("remote target = %+v", remote)
	}
	for _, invalid := range []remoteconnect.State{
		{Hostname: "runtime.example/path", BasePath: "/or3/"},
		{Hostname: "runtime.example", BasePath: "/bad/"},
	} {
		if _, err := externalRuntimeRemoteTarget(local, invalid); err == nil {
			t.Fatalf("invalid remote state was accepted: %+v", invalid)
		}
	}
}

func TestRuntimeAdapterContractCarriesTargetAndConsent(t *testing.T) {
	confirmed := false
	fake := fakeRuntimeAdapter{
		id: "fake",
		prepare: func(_ context.Context, input PrepareInput) (externalRuntimePlan, error) {
			ok, err := input.Confirm("write fake config")
			confirmed = ok && err == nil
			return externalRuntimePlan{
				host: remoteconnect.HostMetadata{
					Driver:         "runs",
					Runtime:        "hermes",
					RuntimeVersion: "test",
				},
				localOrigin: "http://127.0.0.1:1234",
				basePath:    "/",
			}, nil
		},
	}
	plan, err := prepareExternalRuntimeWithAdapter(context.Background(), fake, PrepareInput{
		Confirm: func(string) (bool, error) { return true, nil },
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("prepareExternalRuntimeWithAdapter: %v", err)
	}
	if !confirmed || plan.localOrigin != "http://127.0.0.1:1234" || plan.basePath != "/" {
		t.Fatalf("adapter plan did not preserve consent/target: confirmed=%v plan=%+v", confirmed, plan)
	}
}

func TestRuntimePreparationWaitIsResumableAndStopsOnCancellation(t *testing.T) {
	fake := fakeRuntimeAdapter{id: "hermes"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitForRuntimePreparation(ctx, fake, PrepareInput{
		Confirm: func(string) (bool, error) { return true, nil },
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}, newRuntimePreparationError("finish Hermes setup", errors.New("not ready")), time.Second)
	if err == nil || !strings.Contains(err.Error(), "readiness") {
		t.Fatalf("cancelled readiness wait returned %v", err)
	}
}

func TestLiveSSEValidationDistinguishesPreflightAndStreamHeaders(t *testing.T) {
	origin := "https://or3.example"
	preflight := http.Header{}
	preflight.Set("Access-Control-Allow-Origin", origin)
	preflight.Set("Access-Control-Allow-Methods", "GET, POST")
	preflight.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	if err := validateCORSPreflight(http.StatusOK, preflight, origin); err != nil {
		t.Fatalf("valid preflight rejected: %v", err)
	}
	live := http.Header{}
	live.Set("Content-Type", "text/event-stream")
	live.Set("Access-Control-Allow-Origin", origin)
	ok, err := validateLiveSSEResponse(http.StatusOK, live, origin)
	if err != nil || !ok {
		t.Fatalf("valid live SSE rejected: ok=%v err=%v", ok, err)
	}
	missing := http.Header{}
	missing.Set("Content-Type", "text/event-stream")
	if _, err := validateLiveSSEResponse(http.StatusOK, missing, origin); err == nil {
		t.Fatal("live SSE without CORS was accepted")
	} else {
		var corsErr *hermesSSECorsError
		if !errors.As(err, &corsErr) {
			t.Fatalf("missing live CORS returned wrong error: %T %v", err, err)
		}
	}
	if err := validateCORSPreflight(http.StatusOK, missing, origin); err == nil {
		t.Fatal("preflight without CORS was accepted")
	}
}

func TestReadLiveRunEvidenceRequiresContentAndTerminalForTheRequestedRun(t *testing.T) {
	valid := "data: {\"run_id\":\"run-1\",\"event\":\"message.delta\",\"delta\":\"OK\"}\n\ndata: {\"run_id\":\"run-1\",\"event\":\"run.completed\",\"output\":\"OK\"}\n\n"
	if err := readLiveRunEvidence(context.Background(), strings.NewReader(valid), "run-1"); err != nil {
		t.Fatalf("valid live evidence was rejected: %v", err)
	}
	wrongRun := "data: {\"run_id\":\"other\",\"event\":\"message.delta\",\"delta\":\"OK\"}\n\ndata: {\"run_id\":\"other\",\"event\":\"run.completed\"}\n\n"
	if err := readLiveRunEvidence(context.Background(), strings.NewReader(wrongRun), "run-1"); err == nil {
		t.Fatal("events for another run were accepted")
	}
	noContent := "data: {\"run_id\":\"run-1\",\"event\":\"run.completed\"}\n\n"
	if err := readLiveRunEvidence(context.Background(), strings.NewReader(noContent), "run-1"); err == nil {
		t.Fatal("terminal-only evidence was accepted")
	}
}

func TestReadSSEFrameWaitsForACompleteFrame(t *testing.T) {
	frame, err := readSSEFrame(context.Background(), strings.NewReader(": keepalive\n\ndata: hello\n\n"))
	if err != nil {
		t.Fatalf("readSSEFrame: %v", err)
	}
	if frame != ": keepalive\n\n" {
		t.Fatalf("readSSEFrame returned %q, want first complete frame", frame)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := readSSEFrame(ctx, strings.NewReader("data: incomplete")); err == nil {
		t.Fatal("readSSEFrame accepted an incomplete frame")
	}
}

func TestHermesSSECorsPatchIsNarrowAndAtomic(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gateway", "platforms", "api_server.py")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	source := "async def _handle_run_events(request):\n        sse_headers = {}\n        response = web.StreamResponse(status=200, headers=sse_headers)\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OR3_HERMES_SOURCE_ROOT", root)
	if err := applyHermesSSECorsPatch("hermes", ""); err != nil {
		t.Fatalf("applyHermesSSECorsPatch: %v", err)
	}
	patched, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patched), "_cors_headers_for_origin") {
		t.Fatalf("patch did not add CORS handling: %s", patched)
	}
	if err := applyHermesSSECorsPatch("hermes", ""); err == nil {
		t.Fatal("already-patched source was edited a second time")
	}
}

func TestRuntimePreparationCheckpointContainsNoCredentialAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	state := AdapterState{
		Runtime:     connectRuntimeOpenClaw,
		Stage:       connectSetupStageLocalConfigured,
		LocalOrigin: "http://127.0.0.1:18789",
		BasePath:    "/or3/",
		Version:     "2026.7.1-2",
	}
	if err := saveRuntimePreparation(dir, state); err != nil {
		t.Fatalf("saveRuntimePreparation: %v", err)
	}
	body, err := os.ReadFile(runtimePreparationPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "token") || strings.Contains(string(body), "secret") {
		t.Fatalf("preparation checkpoint contains a credential-like field: %s", body)
	}
	loaded, err := loadRuntimePreparation(dir)
	if err != nil {
		t.Fatalf("loadRuntimePreparation: %v", err)
	}
	if loaded.Runtime != state.Runtime || loaded.Stage != state.Stage || loaded.BasePath != state.BasePath {
		t.Fatalf("checkpoint did not round-trip: %+v", loaded)
	}
	if err := removeRuntimePreparation(dir); err != nil {
		t.Fatalf("removeRuntimePreparation: %v", err)
	}
	if _, err := os.Stat(runtimePreparationPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("checkpoint remains after removal: %v", err)
	}
}

func TestExternalRuntimeCleanupRestoresConfigurationAndRemovesItsRetryState(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.json")
	original := []byte(`{"gateway":{"bind":"loopback"}}\n`)
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := writeRuntimeGatewayScript(t, 0)
	if _, err := runtimeSnapshotForPreparation(stateDir, "openclaw", binary, configPath); err != nil {
		t.Fatalf("runtimeSnapshotForPreparation: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"plugins":{"entries":{"or3-runs":{"config":{"token":"secret"}}}}}\n`), 0o600); err != nil {
		t.Fatal(err)
	}
	cloud, revokes := newRuntimeCleanupCloud(t)
	defer cloud.Close()
	state := runtimeCleanupState(cloud.URL, "openclaw")
	if err := remoteconnect.SaveState(stateDir, state, runtimeCleanupTunnel()); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if err := cleanupExternalRuntimeEnrollment(context.Background(), stateDir, state, externalRuntimePlan{}); err != nil {
		t.Fatalf("cleanupExternalRuntimeEnrollment: %v", err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("runtime config was not restored: %q", got)
	}
	if revokes() != 1 {
		t.Fatalf("cloud revoke count = %d, want 1", revokes())
	}
	if _, err := remoteconnect.LoadState(stateDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup retained state: %v", err)
	}
	if _, err := os.Stat(remoteconnect.RuntimeConfigBackupPath(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("cleanup retained runtime backup: %v", err)
	}
}

func TestExternalRuntimeCleanupKeepsStateWhenRuntimeRestartFails(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(configPath, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := writeRuntimeGatewayScript(t, 1)
	if _, err := runtimeSnapshotForPreparation(stateDir, "openclaw", binary, configPath); err != nil {
		t.Fatalf("runtimeSnapshotForPreparation: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("or3-token-installed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cloud, revokes := newRuntimeCleanupCloud(t)
	defer cloud.Close()
	state := runtimeCleanupState(cloud.URL, "openclaw")
	if err := remoteconnect.SaveState(stateDir, state, runtimeCleanupTunnel()); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if err := cleanupExternalRuntimeEnrollment(context.Background(), stateDir, state, externalRuntimePlan{}); err == nil {
		t.Fatal("cleanup succeeded despite a failed runtime restart")
	}
	checkpoint, err := remoteconnect.LoadState(stateDir)
	if err != nil {
		t.Fatalf("LoadState after failed cleanup: %v", err)
	}
	if checkpoint.Stage != connectSetupStageCleanupPending || checkpoint.RuntimeConfigRestored || !checkpoint.CloudRevoked {
		t.Fatalf("cleanup checkpoint = %#v", checkpoint)
	}
	if revokes() != 1 {
		t.Fatalf("cloud revoke count = %d, want 1", revokes())
	}
	if _, err := os.Stat(remoteconnect.RuntimeConfigBackupPath(stateDir)); err != nil {
		t.Fatalf("cleanup lost the retry backup: %v", err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := cleanupExternalRuntimeEnrollment(context.Background(), stateDir, checkpoint, externalRuntimePlan{}); err != nil {
		t.Fatalf("retry cleanup: %v", err)
	}
	if _, err := remoteconnect.LoadState(stateDir); !os.IsNotExist(err) {
		t.Fatalf("retry cleanup retained state: %v", err)
	}
}

func TestExternalRuntimeCleanupRestoresTrackedHermesSourcePatch(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(configPath, []byte("API_SERVER_KEY=before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := writeRuntimeGatewayScript(t, 0)
	if _, err := runtimeSnapshotForPreparation(stateDir, "hermes", binary, configPath); err != nil {
		t.Fatalf("runtimeSnapshotForPreparation: %v", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "api_server.py")
	source := []byte("before source patch\n")
	if err := os.WriteFile(sourcePath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recordHermesSourcePatchBackup(stateDir, sourcePath); err != nil {
		t.Fatalf("recordHermesSourcePatchBackup: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("or3 source patch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("API_SERVER_KEY=or3-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cloud, _ := newRuntimeCleanupCloud(t)
	defer cloud.Close()
	state := runtimeCleanupState(cloud.URL, "hermes")
	if err := remoteconnect.SaveState(stateDir, state, runtimeCleanupTunnel()); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if err := cleanupExternalRuntimeEnrollment(context.Background(), stateDir, state, externalRuntimePlan{}); err != nil {
		t.Fatalf("cleanupExternalRuntimeEnrollment: %v", err)
	}
	got, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(source) {
		t.Fatalf("Hermes source was not restored: %q", got)
	}
}

func TestDisconnectRestoresAnOrphanedExternalRuntimeBackup(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "openclaw.json")
	original := []byte("before\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := writeRuntimeGatewayScript(t, 0)
	if _, err := runtimeSnapshotForPreparation(stateDir, "openclaw", binary, configPath); err != nil {
		t.Fatalf("runtimeSnapshotForPreparation: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("or3-token-installed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := disconnectRemoteConnection(context.Background(), connectCommandOptions{StateDir: stateDir}, &stdout); err != nil {
		t.Fatalf("disconnectRemoteConnection: %v", err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("orphaned runtime config was not restored: %q", got)
	}
	if _, err := os.Stat(remoteconnect.RuntimeConfigBackupPath(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("disconnect retained runtime backup: %v", err)
	}
	if !strings.Contains(stdout.String(), "Interrupted external runtime setup was restored") {
		t.Fatalf("unexpected disconnect output: %s", stdout.String())
	}
}

func writeRuntimeGatewayScript(t *testing.T, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func newRuntimeCleanupCloud(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	revokes := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/connect/environments/revoke" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		revokes++
		response.WriteHeader(http.StatusOK)
	}))
	return server, func() int { return revokes }
}

func runtimeCleanupState(cloudURL, runtimeName string) remoteconnect.State {
	return remoteconnect.State{
		CloudURL:        cloudURL,
		AccountID:       "account-a",
		WorkspaceID:     "workspace-a",
		Namespace:       "or3-chat:workspace:workspace-a",
		EnvironmentID:   "environment-a",
		EnvironmentName: "Runtime test",
		Hostname:        "runtime.example.test",
		ControlToken:    "control-token",
		Driver:          "runs",
		Runtime:         runtimeName,
		LocalOrigin:     "http://127.0.0.1:8642",
		BasePath:        "/",
		Stage:           connectSetupStageCleanupPending,
	}
}

func runtimeCleanupTunnel() remoteconnect.TunnelCredential {
	return remoteconnect.TunnelCredential{
		AccountTag:   "account-a",
		TunnelID:     "tunnel-a",
		TunnelSecret: "tunnel-secret",
		Hostname:     "runtime.example.test",
	}
}

type fakeRuntimeAdapter struct {
	id      ConnectRuntimeID
	prepare func(context.Context, PrepareInput) (externalRuntimePlan, error)
}

func (f fakeRuntimeAdapter) ID() ConnectRuntimeID { return f.id }

func (f fakeRuntimeAdapter) Detect(context.Context) (bool, string, error) {
	return true, "test", nil
}

func (f fakeRuntimeAdapter) Prepare(ctx context.Context, input PrepareInput) (*RuntimeConnectionTarget, error) {
	plan, err := f.prepare(ctx, input)
	if err != nil {
		return nil, err
	}
	return &RuntimeConnectionTarget{plan: &plan}, nil
}

func (f fakeRuntimeAdapter) Verify(context.Context, *RuntimeConnectionTarget) (*Verification, error) {
	return &Verification{Streaming: "verified"}, nil
}
