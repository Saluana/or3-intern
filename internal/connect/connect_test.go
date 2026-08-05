package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientDeviceAuthorizationFlow(t *testing.T) {
	var pollHost HostMetadata
	var revokedScope map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/connect/device/start":
			_ = json.NewEncoder(w).Encode(DeviceAuthorization{
				DeviceCode:              "device-secret",
				UserCode:                "BLUE-MOON",
				VerificationURI:         serverURL(r) + "/connect",
				VerificationURIComplete: serverURL(r) + "/connect?code=BLUE-MOON",
				ExpiresIn:               600,
				Interval:                1,
			})
		case "/api/connect/device/token":
			var body struct {
				DeviceCode string       `json:"deviceCode"`
				Host       HostMetadata `json:"host"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pollHost = body.Host
			if body.DeviceCode != "device-secret" {
				t.Fatalf("unexpected device code %q", body.DeviceCode)
			}
			_ = json.NewEncoder(w).Encode(DeviceTokenResponse{
				Status: "approved",
				Credential: &DeviceCredential{
					AccountID:       "acct-1",
					WorkspaceID:     "workspace-1",
					EnvironmentID:   "env-1",
					EnvironmentName: "Studio Mac",
					Namespace:       "or3-chat:workspace-1:",
					ControlToken:    "control-secret",
					Tunnel: TunnelCredential{
						Token:    "tunnel-secret",
						Hostname: "studio.example.test",
					},
				},
			})
		case "/api/connect/environments/revoke":
			if r.Header.Get("Authorization") != "Bearer control-secret" {
				t.Fatalf("unexpected revoke authorization: %q", r.Header.Get("Authorization"))
			}
			if err := json.NewDecoder(r.Body).Decode(&revokedScope); err != nil {
				t.Fatalf("decode revoke scope: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"revoked": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	host := HostMetadata{Name: "Studio Mac", Platform: "darwin", Architecture: "arm64"}
	authorization, err := client.Start(context.Background(), host)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if authorization.UserCode != "BLUE-MOON" {
		t.Fatalf("unexpected user code %q", authorization.UserCode)
	}
	result, err := client.Poll(context.Background(), authorization.DeviceCode, host)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if result.Credential == nil || result.Credential.Tunnel.Token != "tunnel-secret" {
		t.Fatalf("unexpected credential: %#v", result.Credential)
	}
	if pollHost.Name != "Studio Mac" {
		t.Fatalf("host metadata was not sent: %#v", pollHost)
	}
	if err := client.Revoke(context.Background(), State{
		AccountID:     "acct-1",
		WorkspaceID:   "workspace-1",
		EnvironmentID: "env-1",
		ControlToken:  "control-secret",
	}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revokedScope["accountId"] != "acct-1" || revokedScope["workspaceId"] != "workspace-1" {
		t.Fatalf("unexpected revoke scope: %#v", revokedScope)
	}
}

func TestValidateCloudURLRequiresHTTPSExceptExactLoopback(t *testing.T) {
	for _, valid := range []string{
		"https://or3.example",
		"https://or3.example:8443/",
		"http://localhost:3000",
		"http://127.0.0.1:3000",
		"http://[::1]:3000",
	} {
		if _, err := ValidateCloudURL(valid); err != nil {
			t.Fatalf("ValidateCloudURL(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"http://or3.example",
		"http://localhost.example:3000",
		"https://user:secret@or3.example",
		"https://or3.example/base",
		"https://or3.example?token=secret",
		"https://or3.example/#fragment",
		"file:///tmp/or3",
	} {
		if _, err := ValidateCloudURL(invalid); err == nil {
			t.Fatalf("ValidateCloudURL(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestClientRejectsCrossOriginRedirect(t *testing.T) {
	receiverCalled := false
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverCalled = true
		http.Error(w, "must not receive enrollment body", http.StatusBadRequest)
	}))
	defer receiver.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, receiver.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	_, err := NewClient(redirector.URL).Start(context.Background(), HostMetadata{Name: "Mac"})
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect") {
		t.Fatalf("Start error = %v, want cross-origin redirect rejection", err)
	}
	if receiverCalled {
		t.Fatal("cross-origin receiver saw the enrollment request")
	}
}

func TestClientRejectsMismatchedVerificationOrigins(t *testing.T) {
	for _, field := range []string{"verification", "complete"} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				verification := serverURL(r) + "/connect"
				complete := verification + "?code=BLUE-MOON"
				if field == "verification" {
					verification = "https://evil.example/connect"
				} else {
					complete = "https://evil.example/connect?code=BLUE-MOON"
				}
				_ = json.NewEncoder(w).Encode(DeviceAuthorization{
					DeviceCode:              "device-secret",
					UserCode:                "BLUE-MOON",
					VerificationURI:         verification,
					VerificationURIComplete: complete,
				})
			}))
			defer server.Close()
			if _, err := NewClient(server.URL).Start(context.Background(), HostMetadata{Name: "Mac"}); err == nil {
				t.Fatal("mismatched verification origin unexpectedly succeeded")
			}
		})
	}
}

func TestSaveStateWritesOwnerOnlyLocallyManagedTunnelFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "connect")
	state := State{
		CloudURL:        "https://or3.chat",
		AccountID:       "acct-1",
		WorkspaceID:     "workspace-1",
		Namespace:       "or3-chat:workspace-1:",
		EnvironmentID:   "env-1",
		EnvironmentName: "Studio Mac",
		Hostname:        "studio.example.test",
		ControlToken:    "control-secret",
		ConnectedAt:     time.Now().UTC(),
	}
	if err := SaveState(dir, state, TunnelCredential{
		AccountTag:   "account-tag",
		TunnelID:     "11111111-2222-3333-4444-555555555555",
		TunnelSecret: "base64-tunnel-secret",
		Hostname:     "studio.example.test",
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	for _, path := range []string{
		StatePath(dir),
		TunnelConfigPath(dir),
		TunnelCredentialsPath(dir),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
	stateBody, _ := os.ReadFile(StatePath(dir))
	if strings.Contains(string(stateBody), "base64-tunnel-secret") {
		t.Fatal("state file must not contain the tunnel secret")
	}
	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.TunnelConfigFile != TunnelConfigPath(dir) {
		t.Fatalf("unexpected config path %q", loaded.TunnelConfigFile)
	}
	if loaded.TunnelCredentialsFile != TunnelCredentialsPath(dir) {
		t.Fatalf("unexpected credential path %q", loaded.TunnelCredentialsFile)
	}
	if loaded.AccountID != "acct-1" || loaded.WorkspaceID != "workspace-1" {
		t.Fatalf("state scope was not preserved: %#v", loaded)
	}
	if loaded.Namespace != "or3-chat:workspace-1:" {
		t.Fatalf("state namespace was not preserved: %#v", loaded)
	}
	// Exercise the setup caller's pre-SaveState value. UpdateState must recover
	// the durable local credential paths rather than looking for a legacy token.
	state.Installed = true
	if err := UpdateState(dir, state); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	updated, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState after update: %v", err)
	}
	if !updated.Installed {
		t.Fatal("updated state did not persist installation status")
	}
	credentialBody, _ := os.ReadFile(TunnelCredentialsPath(dir))
	if !strings.Contains(string(credentialBody), `"TunnelSecret": "base64-tunnel-secret"`) {
		t.Fatal("updating state must not replace the tunnel credential")
	}
	configBody, _ := os.ReadFile(TunnelConfigPath(dir))
	configText := string(configBody)
	for _, expected := range []string{
		`service: http://127.0.0.1:9100`,
		`httpHostHeader: 127.0.0.1`,
		`service: http_status:404`,
	} {
		if !strings.Contains(configText, expected) {
			t.Fatalf("local tunnel configuration missing %q", expected)
		}
	}
}

func TestSaveStateUsesValidatedExternalRuntimeIngress(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "connect")
	state := State{
		CloudURL:        "https://or3.chat",
		AccountID:       "acct-1",
		WorkspaceID:     "workspace-1",
		EnvironmentID:   "env-1",
		EnvironmentName: "OpenClaw",
		Hostname:        "openclaw.example.test",
		ControlToken:    "control-secret",
		Driver:          "runs",
		Runtime:         "openclaw",
		LocalOrigin:     "http://127.0.0.1:18789",
		BasePath:        "/or3/",
	}
	if err := SaveState(dir, state, TunnelCredential{
		AccountTag:   "account-tag",
		TunnelID:     "11111111-2222-3333-4444-555555555555",
		TunnelSecret: "base64-tunnel-secret",
		Hostname:     state.Hostname,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	configBody, err := os.ReadFile(TunnelConfigPath(dir))
	if err != nil {
		t.Fatalf("read tunnel config: %v", err)
	}
	if !strings.Contains(string(configBody), `service: http://127.0.0.1:18789`) {
		t.Fatalf("external runtime ingress was not persisted: %s", configBody)
	}
	if err := SaveState(dir, state, TunnelCredential{
		AccountTag:   "account-tag",
		TunnelID:     "11111111-2222-3333-4444-555555555555",
		TunnelSecret: "base64-tunnel-secret",
		Hostname:     state.Hostname,
	}); err != nil {
		t.Fatalf("repeat SaveState: %v", err)
	}

	invalidDir := filepath.Join(t.TempDir(), "invalid")
	state.LocalOrigin = "http://127.0.0.1:18789/or3/"
	if err := SaveState(invalidDir, state, TunnelCredential{
		AccountTag:   "account-tag",
		TunnelID:     "11111111-2222-3333-4444-555555555555",
		TunnelSecret: "base64-tunnel-secret",
		Hostname:     state.Hostname,
	}); err == nil {
		t.Fatal("path-bearing loopback target unexpectedly accepted")
	}
	if _, err := os.Stat(TunnelCredentialsPath(invalidDir)); !os.IsNotExist(err) {
		t.Fatalf("invalid target left tunnel credentials behind: %v", err)
	}

	nonLoopbackDir := filepath.Join(t.TempDir(), "non-loopback")
	state.LocalOrigin = "http://0.0.0.0:18789"
	if err := SaveState(nonLoopbackDir, state, TunnelCredential{
		AccountTag:   "account-tag",
		TunnelID:     "11111111-2222-3333-4444-555555555555",
		TunnelSecret: "base64-tunnel-secret",
		Hostname:     state.Hostname,
	}); err == nil {
		t.Fatal("non-loopback external runtime target unexpectedly accepted")
	}
	if _, err := os.Stat(TunnelCredentialsPath(nonLoopbackDir)); !os.IsNotExist(err) {
		t.Fatalf("non-loopback target left tunnel credentials behind: %v", err)
	}
}

func TestRenderServiceRunsAsInvokingUserAndUsesTokenFile(t *testing.T) {
	spec := ServiceSpec{
		Label:      "chat.or3.connect",
		User:       "brendon",
		Group:      "staff",
		WorkingDir: "/Users/brendon/.or3-intern",
		Binary:     "/usr/local/bin/or3-intern",
		ConfigPath: "/Users/brendon/.or3-intern/config.json",
		StateDir:   "/Users/brendon/.or3-intern/connect",
		StdoutPath: "/tmp/or3-connect.log",
		StderrPath: "/tmp/or3-connect-error.log",
		Path:       "/opt/homebrew/bin:/usr/bin:/bin",
		Home:       "/Users/brendon",
		TempDir:    "/tmp",
		WritablePaths: []string{
			"/Users/brendon/workspace",
			"/Users/brendon/.or3-intern",
		},
	}
	launchd, err := RenderService(spec, "darwin")
	if err != nil {
		t.Fatalf("RenderService(darwin): %v", err)
	}
	for _, expected := range []string{"<key>UserName</key><string>brendon</string>", "<string>connect</string>", "<string>run</string>", "<key>PATH</key><string>/opt/homebrew/bin:/usr/bin:/bin</string>"} {
		if !strings.Contains(launchd, expected) {
			t.Fatalf("launchd service missing %q", expected)
		}
	}
	if strings.Contains(launchd, "StandardOutPath") || strings.Contains(launchd, "StandardErrorPath") {
		t.Fatalf("launchd service must not write unbounded supervisor logs: %s", launchd)
	}
	if !strings.Contains(launchd, "<key>OR3_CONNECT_MANAGED_LOGS</key><string>1</string>") {
		t.Fatalf("launchd service did not enable bounded managed logs: %s", launchd)
	}
	systemd, err := RenderService(spec, "linux")
	if err != nil {
		t.Fatalf("RenderService(linux): %v", err)
	}
	for _, expected := range []string{"User=brendon", "NoNewPrivileges=true", "connect run", `Environment="PATH=/opt/homebrew/bin:/usr/bin:/bin"`, `"/Users/brendon/workspace"`} {
		if !strings.Contains(systemd, expected) {
			t.Fatalf("systemd service missing %q", expected)
		}
	}
}

func TestServiceExecutablePathDropsRelativeAndEmptyEntries(t *testing.T) {
	got := serviceExecutablePath("relative:/custom/bin::/usr/bin", "/Users/example")
	if strings.Contains(got, "relative") || strings.Contains(got, "::") {
		t.Fatalf("unsafe PATH entry survived: %q", got)
	}
	for _, expected := range []string{"/custom/bin", "/usr/bin", "/Users/example/.local/bin", "/opt/homebrew/bin"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("controlled PATH missing %q: %q", expected, got)
		}
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
