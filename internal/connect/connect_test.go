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
					EnvironmentID:   "env-1",
					EnvironmentName: "Studio Mac",
					ControlToken:    "control-secret",
					Tunnel: TunnelCredential{
						Token:    "tunnel-secret",
						Hostname: "studio.example.test",
					},
				},
			})
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
}

func TestSaveStateUsesOwnerOnlyFilesAndNeverEmbedsTunnelToken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "connect")
	state := State{
		CloudURL:        "https://or3.chat",
		EnvironmentID:   "env-1",
		EnvironmentName: "Studio Mac",
		Hostname:        "studio.example.test",
		ControlToken:    "control-secret",
		ConnectedAt:     time.Now().UTC(),
	}
	if err := SaveState(dir, state, "tunnel-secret"); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	for _, path := range []string{StatePath(dir), TunnelTokenPath(dir)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
	stateBody, _ := os.ReadFile(StatePath(dir))
	if strings.Contains(string(stateBody), "tunnel-secret") {
		t.Fatal("state file must not contain the tunnel token")
	}
	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.TunnelTokenFile != TunnelTokenPath(dir) {
		t.Fatalf("unexpected token path %q", loaded.TunnelTokenFile)
	}
	loaded.Installed = true
	if err := UpdateState(dir, loaded); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	updated, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState after update: %v", err)
	}
	if !updated.Installed {
		t.Fatal("updated state did not persist installation status")
	}
	tokenBody, _ := os.ReadFile(TunnelTokenPath(dir))
	if string(tokenBody) != "tunnel-secret\n" {
		t.Fatal("updating state must not replace the tunnel credential")
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
	}
	launchd, err := RenderService(spec, "darwin")
	if err != nil {
		t.Fatalf("RenderService(darwin): %v", err)
	}
	for _, expected := range []string{"<key>UserName</key><string>brendon</string>", "<string>connect</string>", "<string>run</string>"} {
		if !strings.Contains(launchd, expected) {
			t.Fatalf("launchd service missing %q", expected)
		}
	}
	systemd, err := RenderService(spec, "linux")
	if err != nil {
		t.Fatalf("RenderService(linux): %v", err)
	}
	for _, expected := range []string{"User=brendon", "NoNewPrivileges=true", "connect run"} {
		if !strings.Contains(systemd, expected) {
			t.Fatalf("systemd service missing %q", expected)
		}
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
