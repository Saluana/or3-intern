package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	remoteconnect "or3-intern/internal/connect"
	"or3-intern/internal/db"
)

type connectRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn connectRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestConfirmRuntimeActionReusesCommandReader(t *testing.T) {
	t.Setenv("OR3_CONNECT_APPROVE", "")
	reader := bufio.NewReader(strings.NewReader("y\ny\n"))
	for _, action := range []string{"First confirmation?", "Second confirmation?"} {
		if !confirmRuntimeAction(reader, io.Discard, action) {
			t.Fatalf("confirmation %q was not read from the shared reader", action)
		}
	}
}

func TestProcessStoppedErrorHandlesCleanExit(t *testing.T) {
	if got := processStoppedError("secure tunnel", nil).Error(); got != "secure tunnel stopped unexpectedly" {
		t.Fatalf("clean exit error = %q", got)
	}
	stopped := processStoppedError("OR3 service", errors.New("exit status 1"))
	if !strings.Contains(stopped.Error(), "OR3 service stopped: exit status 1") {
		t.Fatalf("failed process error = %v", stopped)
	}
}

func TestParseConnectOptionsWithholdsUnconfiguredRemoteSetup(t *testing.T) {
	t.Setenv("OR3_CONNECT_CLOUD_URL", "")
	for _, subcommand := range []string{"setup", "openclaw", "hermes"} {
		_, err := parseConnectOptions(subcommand, nil)
		if err == nil || !strings.Contains(err.Error(), "not enabled by default") {
			t.Fatalf("parseConnectOptions(%q) error = %v, want withheld message", subcommand, err)
		}
	}
}

func TestParseConnectOptionsAllowsExplicitVerifiedEndpoint(t *testing.T) {
	t.Setenv("OR3_CONNECT_CLOUD_URL", "")
	options, err := parseConnectOptions("setup", []string{"--cloud-url", "https://staging.example.test"})
	if err != nil {
		t.Fatalf("parseConnectOptions explicit endpoint: %v", err)
	}
	if options.CloudURL != "https://staging.example.test" {
		t.Fatalf("CloudURL = %q", options.CloudURL)
	}
}

func TestParseConnectOptionsAllowsManagementWithoutCloudURL(t *testing.T) {
	t.Setenv("OR3_CONNECT_CLOUD_URL", "")
	for _, subcommand := range []string{"status", "doctor", "disconnect", "uninstall", "run"} {
		if _, err := parseConnectOptions(subcommand, nil); err != nil {
			t.Fatalf("parseConnectOptions(%q): %v", subcommand, err)
		}
	}
}

func TestRequiredExternalRuntimeVerificationRequiresCapabilities(t *testing.T) {
	if _, err := requiredExternalRuntimeVerification(nil); err == nil {
		t.Fatal("nil verification was accepted")
	}
	if _, err := requiredExternalRuntimeVerification(&Verification{}); err == nil {
		t.Fatal("verification without capabilities was accepted")
	}
	verification, err := requiredExternalRuntimeVerification(&Verification{Capabilities: map[string]any{}})
	if err != nil || verification.Capabilities == nil {
		t.Fatalf("valid verification = %#v, %v", verification, err)
	}
}

type connectSetupCloudFixture struct {
	server       *httptest.Server
	starts       atomic.Int32
	polls        atomic.Int32
	revokes      atomic.Int32
	revokeStatus atomic.Int32
}

func newConnectSetupCloudFixture(t *testing.T) *connectSetupCloudFixture {
	t.Helper()
	fixture := &connectSetupCloudFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/connect/device/start":
			fixture.starts.Add(1)
			_ = json.NewEncoder(w).Encode(remoteconnect.DeviceAuthorization{
				DeviceCode:              "device-code",
				UserCode:                "ABCD-EFGH",
				VerificationURI:         "http://" + request.Host + "/connect",
				VerificationURIComplete: "http://" + request.Host + "/connect?code=ABCD-EFGH",
				Interval:                1,
			})
		case "/api/connect/device/token":
			fixture.polls.Add(1)
			_ = json.NewEncoder(w).Encode(remoteconnect.DeviceTokenResponse{
				Status: "approved",
				Credential: &remoteconnect.DeviceCredential{
					AccountID:       "account-a",
					WorkspaceID:     "workspace-a",
					EnvironmentID:   "environment-a",
					EnvironmentName: "Studio Mac",
					Namespace:       "or3-chat:workspace-a:",
					ControlToken:    "control-token-that-is-at-least-thirty-two-bytes",
					Tunnel: remoteconnect.TunnelCredential{
						AccountTag:   "account-tag",
						TunnelID:     "11111111-2222-3333-4444-555555555555",
						TunnelSecret: "base64-tunnel-secret",
						Hostname:     "studio.example.test",
					},
				},
			})
		case "/api/connect/environments/revoke":
			fixture.revokes.Add(1)
			status := fixture.revokeStatus.Load()
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(int(status))
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

type connectSetupTestFixture struct {
	configPath   string
	stateDir     string
	original     config.Config
	options      connectCommandOptions
	cloud        *connectSetupCloudFixture
	operations   connectSetupOperations
	controlToken string
}

func newConnectSetupTestFixture(t *testing.T) *connectSetupTestFixture {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}
	cloudflaredPath := filepath.Join(binDir, "cloudflared")
	if err := os.WriteFile(cloudflaredPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write cloudflared stub: %v", err)
	}
	t.Setenv("PATH", binDir)

	cfg := config.Default()
	cfg.DBPath = filepath.Join(root, "or3.sqlite")
	cfg.ArtifactsDir = filepath.Join(root, "artifacts")
	cfg.Service.Enabled = false
	cfg.Service.Listen = "127.0.0.1:7777"
	cfg.Service.Secret = "existing-service-secret"
	cfg.Service.TrustedBrowserOrigins = []string{"https://local.example"}
	configPath := filepath.Join(root, "config.json")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	cloud := newConnectSetupCloudFixture(t)
	operations := defaultConnectSetupOperations()
	operations.currentServiceSpec = func(configPath, stateDir string) (remoteconnect.ServiceSpec, error) {
		return remoteconnect.ServiceSpec{
			Label:      "chat.or3.connect",
			ConfigPath: configPath,
			StateDir:   stateDir,
		}, nil
	}
	operations.installService = func(remoteconnect.ServiceSpec) error { return nil }
	operations.verifyOnline = func(context.Context, remoteconnect.State) error { return nil }

	return &connectSetupTestFixture{
		configPath: configPath,
		stateDir:   filepath.Join(root, "connect"),
		original:   cfg,
		options: connectCommandOptions{
			CloudURL:  cloud.server.URL,
			StateDir:  filepath.Join(root, "connect"),
			Name:      "Studio Mac",
			NoBrowser: true,
			Timeout:   5 * time.Second,
		},
		cloud:        cloud,
		operations:   operations,
		controlToken: "control-token-that-is-at-least-thirty-two-bytes",
	}
}

func (fixture *connectSetupTestFixture) run(t *testing.T) error {
	t.Helper()
	return setupRemoteConnectionWithOperations(
		context.Background(),
		fixture.configPath,
		fixture.options,
		&bytes.Buffer{},
		&bytes.Buffer{},
		fixture.operations,
	)
}

func (fixture *connectSetupTestFixture) assertFullyRolledBack(t *testing.T) {
	t.Helper()
	if _, err := remoteconnect.LoadState(fixture.stateDir); !os.IsNotExist(err) {
		t.Fatalf("expected no resumable state after rollback, got %v", err)
	}
	persisted, err := config.LoadPersisted(fixture.configPath)
	if err != nil {
		t.Fatalf("LoadPersisted: %v", err)
	}
	if got := snapshotConnectServiceConfig(persisted); !equalConnectServiceSnapshot(got, snapshotConnectServiceConfig(fixture.original)) {
		t.Fatalf("service config was not restored: got %#v, want %#v", got, snapshotConnectServiceConfig(fixture.original))
	}
	database, err := db.Open(fixture.original.DBPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	broker := &approval.Broker{DB: database}
	if _, err := broker.AuthenticateDeviceToken(context.Background(), fixture.controlToken, approval.RoleConnect); err == nil {
		t.Fatal("local Connect credential remained active after rollback")
	}
	if fixture.cloud.revokes.Load() != 1 {
		t.Fatalf("cloud revoke count = %d, want 1", fixture.cloud.revokes.Load())
	}
}

func equalConnectServiceSnapshot(left, right remoteconnect.ServiceConfigSnapshot) bool {
	if left.Enabled != right.Enabled || left.Listen != right.Listen {
		return false
	}
	if len(left.TrustedBrowserOrigins) != len(right.TrustedBrowserOrigins) {
		return false
	}
	for index := range left.TrustedBrowserOrigins {
		if left.TrustedBrowserOrigins[index] != right.TrustedBrowserOrigins[index] {
			return false
		}
	}
	return true
}

func (fixture *connectSetupTestFixture) stagedState(stage string) remoteconnect.State {
	previous := snapshotConnectServiceConfig(fixture.original)
	appliedConfig := fixture.original
	appliedConfig.Service.Enabled = true
	appliedConfig.Service.Listen = "127.0.0.1:9100"
	if !containsString(appliedConfig.Service.TrustedBrowserOrigins, fixture.cloud.server.URL) {
		appliedConfig.Service.TrustedBrowserOrigins = append(
			appliedConfig.Service.TrustedBrowserOrigins,
			fixture.cloud.server.URL,
		)
	}
	applied := snapshotConnectServiceConfig(appliedConfig)
	return remoteconnect.State{
		CloudURL:        fixture.cloud.server.URL,
		AccountID:       "account-a",
		WorkspaceID:     "workspace-a",
		Namespace:       "or3-chat:workspace-a:",
		EnvironmentID:   "environment-a",
		EnvironmentName: "Studio Mac",
		Hostname:        "studio.example.test",
		ControlToken:    fixture.controlToken,
		ConfigPath:      fixture.configPath,
		PreviousService: &previous,
		AppliedService:  &applied,
		Stage:           stage,
		Installed: stage == connectSetupStageInstalled ||
			stage == connectSetupStageOnline,
		ConnectedAt: time.Now().UTC(),
	}
}

func (fixture *connectSetupTestFixture) seedStage(
	t *testing.T,
	stage string,
	registerCredential bool,
) remoteconnect.State {
	t.Helper()
	state := fixture.stagedState(stage)
	if stage != connectSetupStageAuthorized {
		persisted := persistedCfgWithServiceSnapshot(
			fixture.original,
			*state.AppliedService,
		)
		if err := config.Save(fixture.configPath, persisted); err != nil {
			t.Fatalf("save applied config: %v", err)
		}
	}
	if err := remoteconnect.SaveState(
		fixture.stateDir,
		state,
		remoteconnect.TunnelCredential{
			AccountTag:   "account-tag",
			TunnelID:     "11111111-2222-3333-4444-555555555555",
			TunnelSecret: "base64-tunnel-secret",
			Hostname:     state.Hostname,
		},
	); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if registerCredential {
		if err := registerAuthorizedConnectCredential(context.Background(), state); err != nil {
			t.Fatalf("registerAuthorizedConnectCredential: %v", err)
		}
	}
	return state
}

func (fixture *connectSetupTestFixture) assertCredentialActive(t *testing.T) {
	t.Helper()
	database, err := db.Open(fixture.original.DBPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	broker := &approval.Broker{DB: database}
	if _, err := broker.AuthenticateDeviceToken(
		context.Background(),
		fixture.controlToken,
		approval.RoleConnect,
	); err != nil {
		t.Fatalf("Connect credential is not active: %v", err)
	}
}

func TestWaitForDeviceCredentialRetriesDroppedApprovedResponse(t *testing.T) {
	polls := 0
	client := remoteconnect.NewClient("https://or3.test")
	client.HTTPClient = &http.Client{
		Transport: connectRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/api/connect/device/token" {
				t.Fatalf("unexpected path %q", request.URL.Path)
			}
			polls++
			response := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Request:    request,
			}
			if polls == 1 {
				response.Body = io.NopCloser(strings.NewReader(""))
				return response, nil
			}
			body, err := json.Marshal(remoteconnect.DeviceTokenResponse{
				Status: "approved",
				Credential: &remoteconnect.DeviceCredential{
					AccountID:       "account-a",
					WorkspaceID:     "workspace-a",
					EnvironmentID:   "environment-a",
					EnvironmentName: "Studio Mac",
					Namespace:       "or3-chat:workspace-a:",
					ControlToken:    "control-token",
					Tunnel: remoteconnect.TunnelCredential{
						Token:    "tunnel-token",
						Hostname: "studio.example.test",
					},
				},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			response.Body = io.NopCloser(bytes.NewReader(body))
			return response, nil
		}),
	}

	credential, err := waitForDeviceCredential(
		context.Background(),
		client,
		remoteconnect.DeviceAuthorization{
			DeviceCode: "device-code",
			Interval:   1,
		},
		remoteconnect.HostMetadata{
			Name:          "Studio Mac",
			Platform:      "darwin",
			Architecture:  "arm64",
			InternVersion: "1.0.0",
		},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("waitForDeviceCredential: %v", err)
	}
	if polls != 2 {
		t.Fatalf("poll count = %d, want 2", polls)
	}
	if credential.ControlToken != "control-token" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
}

func TestProbeRemoteConnectionOnlineUsesPairedCredential(t *testing.T) {
	var request *http.Request
	client := &http.Client{
		Transport: connectRoundTripFunc(func(received *http.Request) (*http.Response, error) {
			request = received
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"runners":[]}`)),
				Request:    received,
			}, nil
		}),
	}
	state := remoteconnect.State{
		AccountID:     "account-a",
		WorkspaceID:   "workspace-a",
		EnvironmentID: "environment-a",
		Hostname:      "studio.example.test",
		ControlToken:  "scoped-control-token",
	}
	if err := probeRemoteConnectionOnlineWithClient(context.Background(), client, state); err != nil {
		t.Fatalf("probeRemoteConnectionOnlineWithClient: %v", err)
	}
	if request == nil || request.URL.String() != "https://studio.example.test/internal/v1/chat-runners" {
		t.Fatalf("unexpected probe request: %#v", request)
	}
	if request.Header.Get("Authorization") != "Bearer scoped-control-token" ||
		request.Header.Get("X-Or3-Auth-Method") != "paired-device" {
		t.Fatalf("probe omitted paired credential headers: %#v", request.Header)
	}
}

func TestConnectDoctorReportsBoundedNetworkTimeoutPhases(t *testing.T) {
	state := remoteconnect.State{
		AccountID:     "account-a",
		WorkspaceID:   "workspace-a",
		EnvironmentID: "environment-a",
		Hostname:      "studio.example.test",
		ControlToken:  "scoped-control-token",
	}
	timeouts := connectDoctorTimeouts{
		overall: 40 * time.Millisecond,
		body:    20 * time.Millisecond,
	}
	tests := []struct {
		name  string
		phase string
		mark  func(*httptrace.ClientTrace)
	}{
		{
			name:  "dns",
			phase: connectDoctorPhaseDNS,
			mark: func(trace *httptrace.ClientTrace) {
				trace.DNSStart(httptrace.DNSStartInfo{Host: "studio.example.test"})
			},
		},
		{
			name:  "connect",
			phase: connectDoctorPhaseConnection,
			mark: func(trace *httptrace.ClientTrace) {
				trace.ConnectStart("tcp", "studio.example.test:443")
			},
		},
		{
			name:  "headers",
			phase: connectDoctorPhaseHeaders,
			mark: func(trace *httptrace.ClientTrace) {
				trace.GotConn(httptrace.GotConnInfo{})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: connectRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(request.Context())
				if trace == nil {
					t.Fatal("Doctor request did not install an HTTP trace")
				}
				test.mark(trace)
				<-request.Context().Done()
				return nil, request.Context().Err()
			})}
			started := time.Now()
			err := probeRemoteConnectionForDoctor(context.Background(), client, state, timeouts)
			var timeoutErr *connectDoctorTimeoutError
			if !errors.As(err, &timeoutErr) {
				t.Fatalf("error = %v, want connectDoctorTimeoutError", err)
			}
			if timeoutErr.phase != test.phase {
				t.Fatalf("timeout phase = %q, want %q", timeoutErr.phase, test.phase)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("Doctor timeout took %s, want a predictable bounded exit", elapsed)
			}
		})
	}
}

func TestConnectDoctorBoundsStalledAndPartialResponseBodies(t *testing.T) {
	state := remoteconnect.State{
		AccountID:     "account-a",
		WorkspaceID:   "workspace-a",
		EnvironmentID: "environment-a",
		Hostname:      "studio.example.test",
		ControlToken:  "scoped-control-token",
	}
	for _, partial := range []bool{false, true} {
		name := "stalled"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			body := newStallingConnectBody(partial)
			client := &http.Client{Transport: connectRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       body,
					Request:    request,
				}, nil
			})}
			err := probeRemoteConnectionForDoctor(context.Background(), client, state, connectDoctorTimeouts{
				overall: 200 * time.Millisecond,
				body:    20 * time.Millisecond,
			})
			var timeoutErr *connectDoctorTimeoutError
			if !errors.As(err, &timeoutErr) || timeoutErr.phase != connectDoctorPhaseBody {
				t.Fatalf("error = %v, want response body timeout", err)
			}
			select {
			case <-body.closed:
			default:
				t.Fatal("Doctor did not close the stalled response body")
			}
		})
	}
}

func TestConnectDoctorSurfacesOnlyBoundedRedactedServiceDiagnostics(t *testing.T) {
	stateDir := t.TempDir()
	if err := remoteconnect.SaveState(stateDir, remoteconnect.State{
		CloudURL:        "https://or3.chat",
		EnvironmentName: "Studio Mac",
		Hostname:        "studio.example.test",
		CloudflaredPath: filepath.Join(stateDir, "missing-cloudflared"),
		ControlToken:    "stored-control-secret",
	}, remoteconnect.TunnelCredential{Token: "stored-tunnel-secret"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	diagnosticSecret := strings.Repeat("diagnostic-secret", 8)
	if err := os.WriteFile(
		filepath.Join(stateDir, "connect-error.log"),
		[]byte(strings.Repeat("ordinary diagnostic line\n", 2_000)+"Authorization: Bearer "+diagnosticSecret+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := doctorRemoteConnection(context.Background(), stateDir, &output)
	if err == nil || !strings.Contains(err.Error(), "cloudflared: not installed") {
		t.Fatalf("doctorRemoteConnection error = %v", err)
	}
	if output.Len() > (40 << 10) {
		t.Fatalf("Doctor diagnostics were not bounded: %d bytes", output.Len())
	}
	if strings.Contains(output.String(), diagnosticSecret) ||
		!strings.Contains(output.String(), "<redacted>") {
		t.Fatalf("Doctor diagnostics leaked credentials: %s", output.String())
	}
}

type stallingConnectBody struct {
	partial bool
	sent    bool
	closed  chan struct{}
	once    sync.Once
}

func newStallingConnectBody(partial bool) *stallingConnectBody {
	return &stallingConnectBody{partial: partial, closed: make(chan struct{})}
}

func (body *stallingConnectBody) Read(destination []byte) (int, error) {
	if body.partial && !body.sent {
		body.sent = true
		return copy(destination, `{"runners":[`), nil
	}
	<-body.closed
	return 0, io.ErrClosedPipe
}

func (body *stallingConnectBody) Close() error {
	body.once.Do(func() { close(body.closed) })
	return nil
}

func TestSetupRemoteConnectionRollsBackAnAmbiguousConfigSaveFailure(t *testing.T) {
	fixture := newConnectSetupTestFixture(t)
	saveCalls := 0
	fixture.operations.saveConfig = func(path string, cfg config.Config) error {
		saveCalls++
		if saveCalls == 1 {
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			return errors.New("injected config save acknowledgement failure")
		}
		return config.Save(path, cfg)
	}

	err := fixture.run(t)
	if err == nil || !strings.Contains(err.Error(), "save OR3 service settings") {
		t.Fatalf("setup error = %v, want config save failure", err)
	}
	fixture.assertFullyRolledBack(t)
	if fixture.cloud.starts.Load() != 1 || fixture.cloud.polls.Load() != 1 {
		t.Fatalf("unexpected authorization calls: starts=%d polls=%d", fixture.cloud.starts.Load(), fixture.cloud.polls.Load())
	}
}

func TestSetupRemoteConnectionKeepsCheckpointWhenRollbackCannotRevokeCloud(t *testing.T) {
	fixture := newConnectSetupTestFixture(t)
	fixture.cloud.revokeStatus.Store(http.StatusServiceUnavailable)
	saveCalls := 0
	fixture.operations.saveConfig = func(path string, cfg config.Config) error {
		saveCalls++
		if saveCalls == 1 {
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			return errors.New("injected config save acknowledgement failure")
		}
		return config.Save(path, cfg)
	}

	err := fixture.run(t)
	if err == nil || !strings.Contains(err.Error(), "automatic rollback is incomplete") {
		t.Fatalf("setup error = %v, want incomplete rollback guidance", err)
	}
	checkpoint, err := remoteconnect.LoadState(fixture.stateDir)
	if err != nil {
		t.Fatalf("LoadState after rollback failure: %v", err)
	}
	if checkpoint.Stage != connectSetupStageAuthorized {
		t.Fatalf("rollback failure lost authorized checkpoint: %#v", checkpoint)
	}

	fixture.cloud.revokeStatus.Store(http.StatusOK)
	if err := fixture.run(t); err != nil {
		t.Fatalf("resume after rollback failure: %v", err)
	}
	assertConnectSetupInstalled(t, fixture.stateDir)
	fixture.assertCredentialActive(t)
	if fixture.cloud.starts.Load() != 1 || fixture.cloud.polls.Load() != 1 {
		t.Fatalf("resume after rollback failure unexpectedly reauthorized")
	}
}

func TestSetupRemoteConnectionRollsBackAnAmbiguousStateSaveFailure(t *testing.T) {
	fixture := newConnectSetupTestFixture(t)
	fixture.operations.saveState = func(dir string, state remoteconnect.State, tunnel remoteconnect.TunnelCredential) error {
		if _, err := os.Stat(fixture.original.DBPath); !os.IsNotExist(err) {
			t.Fatalf("local credential store exists before authorized checkpoint: %v", err)
		}
		if state.Stage != connectSetupStageAuthorized || state.Namespace == "" {
			t.Fatalf("first durable checkpoint is incomplete: %#v", state)
		}
		if err := remoteconnect.SaveState(dir, state, tunnel); err != nil {
			return err
		}
		return errors.New("injected state save acknowledgement failure")
	}

	err := fixture.run(t)
	if err == nil || !strings.Contains(err.Error(), "injected state save") {
		t.Fatalf("setup error = %v, want state save failure", err)
	}
	fixture.assertFullyRolledBack(t)
	for _, path := range []string{
		remoteconnect.TunnelConfigPath(fixture.stateDir),
		remoteconnect.TunnelCredentialsPath(fixture.stateDir),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("rollback left tunnel artifact %s: %v", path, statErr)
		}
	}
}

func TestSetupRemoteConnectionResumesAuthorizedCheckpointBeforeLocalRegistration(t *testing.T) {
	fixture := newConnectSetupTestFixture(t)
	fixture.seedStage(t, connectSetupStageAuthorized, false)
	installCalls := 0
	verifyCalls := 0
	fixture.operations.installService = func(remoteconnect.ServiceSpec) error {
		installCalls++
		return nil
	}
	fixture.operations.verifyOnline = func(context.Context, remoteconnect.State) error {
		verifyCalls++
		return nil
	}

	if err := fixture.run(t); err != nil {
		t.Fatalf("resume authorized setup: %v", err)
	}
	assertConnectSetupInstalled(t, fixture.stateDir)
	fixture.assertCredentialActive(t)
	if installCalls != 1 || verifyCalls != 1 {
		t.Fatalf("resume calls = install:%d verify:%d, want 1 each", installCalls, verifyCalls)
	}
	if fixture.cloud.starts.Load() != 0 || fixture.cloud.polls.Load() != 0 {
		t.Fatalf(
			"authorized restart unexpectedly reauthorized: starts=%d polls=%d",
			fixture.cloud.starts.Load(),
			fixture.cloud.polls.Load(),
		)
	}
}

func TestSetupRemoteConnectionResumesEveryDurableCheckpointUpdate(t *testing.T) {
	tests := []struct {
		name          string
		failedStage   string
		expectedStage string
		wantInstalls  int
		wantVerifies  int
	}{
		{
			name:          "local configuration checkpoint",
			failedStage:   connectSetupStageLocalConfigured,
			expectedStage: connectSetupStageAuthorized,
			wantInstalls:  1,
			wantVerifies:  1,
		},
		{
			name:          "service installing checkpoint",
			failedStage:   connectSetupStageServiceInstalling,
			expectedStage: connectSetupStageLocalConfigured,
			wantInstalls:  1,
			wantVerifies:  1,
		},
		{
			name:          "online checkpoint",
			failedStage:   connectSetupStageOnline,
			expectedStage: connectSetupStageInstalled,
			wantInstalls:  1,
			wantVerifies:  2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newConnectSetupTestFixture(t)
			failed := false
			installCalls := 0
			verifyCalls := 0
			fixture.operations.installService = func(remoteconnect.ServiceSpec) error {
				installCalls++
				return nil
			}
			fixture.operations.verifyOnline = func(context.Context, remoteconnect.State) error {
				verifyCalls++
				return nil
			}
			fixture.operations.updateState = func(dir string, state remoteconnect.State) error {
				if state.Stage == test.failedStage && !failed {
					failed = true
					return errors.New("injected hard-crash boundary")
				}
				return remoteconnect.UpdateState(dir, state)
			}

			err := fixture.run(t)
			if err == nil || !strings.Contains(err.Error(), "injected hard-crash boundary") {
				t.Fatalf("first setup error = %v, want injected checkpoint failure", err)
			}
			checkpoint, err := remoteconnect.LoadState(fixture.stateDir)
			if err != nil {
				t.Fatalf("LoadState after checkpoint failure: %v", err)
			}
			if checkpoint.Stage != test.expectedStage {
				t.Fatalf("durable stage = %q, want %q", checkpoint.Stage, test.expectedStage)
			}

			if err := fixture.run(t); err != nil {
				t.Fatalf("restart setup: %v", err)
			}
			assertConnectSetupInstalled(t, fixture.stateDir)
			fixture.assertCredentialActive(t)
			if fixture.cloud.starts.Load() != 1 || fixture.cloud.polls.Load() != 1 {
				t.Fatalf(
					"restart unexpectedly reauthorized: starts=%d polls=%d",
					fixture.cloud.starts.Load(),
					fixture.cloud.polls.Load(),
				)
			}
			if installCalls != test.wantInstalls || verifyCalls != test.wantVerifies {
				t.Fatalf(
					"checkpoint calls = install:%d verify:%d, want %d/%d",
					installCalls,
					verifyCalls,
					test.wantInstalls,
					test.wantVerifies,
				)
			}
		})
	}
}

func TestSetupRemoteConnectionResumesAfterServiceInstallFailure(t *testing.T) {
	fixture := newConnectSetupTestFixture(t)
	installCalls := 0
	fixture.operations.installService = func(remoteconnect.ServiceSpec) error {
		installCalls++
		if installCalls == 1 {
			return errors.New("injected service install failure")
		}
		return nil
	}

	err := fixture.run(t)
	if err == nil || !strings.Contains(err.Error(), "install background service") {
		t.Fatalf("first setup error = %v, want install failure", err)
	}
	incomplete, err := remoteconnect.LoadState(fixture.stateDir)
	if err != nil {
		t.Fatalf("LoadState after install failure: %v", err)
	}
	if incomplete.Installed || incomplete.Stage != connectSetupStageServiceInstalling {
		t.Fatalf("unexpected incomplete state: %#v", incomplete)
	}
	if fixture.cloud.revokes.Load() != 0 {
		t.Fatal("resumable install failure must not revoke the cloud environment")
	}

	if err := fixture.run(t); err != nil {
		t.Fatalf("repair setup: %v", err)
	}
	assertConnectSetupInstalled(t, fixture.stateDir)
	if installCalls != 2 {
		t.Fatalf("install calls = %d, want one failed attempt plus one repair", installCalls)
	}
	if fixture.cloud.starts.Load() != 1 || fixture.cloud.polls.Load() != 1 {
		t.Fatalf("repair unexpectedly re-authorized: starts=%d polls=%d", fixture.cloud.starts.Load(), fixture.cloud.polls.Load())
	}
}

func TestSetupRemoteConnectionRetriesHealthWithoutReinstalling(t *testing.T) {
	fixture := newConnectSetupTestFixture(t)
	installCalls := 0
	verifyCalls := 0
	fixture.operations.installService = func(remoteconnect.ServiceSpec) error {
		installCalls++
		return nil
	}
	fixture.operations.verifyOnline = func(context.Context, remoteconnect.State) error {
		verifyCalls++
		if verifyCalls == 1 {
			return errors.New("injected remote health failure")
		}
		return nil
	}

	err := fixture.run(t)
	if err == nil || !strings.Contains(err.Error(), "remote access is not online yet") {
		t.Fatalf("first setup error = %v, want online verification failure", err)
	}
	checkpoint, err := remoteconnect.LoadState(fixture.stateDir)
	if err != nil {
		t.Fatalf("LoadState after health failure: %v", err)
	}
	if !checkpoint.Installed || checkpoint.Stage != connectSetupStageInstalled {
		t.Fatalf("health failure did not preserve installed checkpoint: %#v", checkpoint)
	}

	if err := fixture.run(t); err != nil {
		t.Fatalf("retry online verification: %v", err)
	}
	assertConnectSetupInstalled(t, fixture.stateDir)
	if installCalls != 1 || verifyCalls != 2 {
		t.Fatalf("retry calls = install:%d verify:%d, want install 1 verify 2", installCalls, verifyCalls)
	}
	if fixture.cloud.starts.Load() != 1 || fixture.cloud.polls.Load() != 1 {
		t.Fatalf("health retry unexpectedly reauthorized")
	}
}

func TestSetupRemoteConnectionResumesAfterInstallationStateUpdateFailure(t *testing.T) {
	fixture := newConnectSetupTestFixture(t)
	installCalls := 0
	updateCalls := 0
	installedUpdateFailures := 0
	fixture.operations.installService = func(remoteconnect.ServiceSpec) error {
		installCalls++
		return nil
	}
	fixture.operations.updateState = func(dir string, state remoteconnect.State) error {
		updateCalls++
		if state.Stage == connectSetupStageInstalled && installedUpdateFailures == 0 {
			installedUpdateFailures++
			return errors.New("injected state update failure")
		}
		return remoteconnect.UpdateState(dir, state)
	}

	err := fixture.run(t)
	if err == nil || !strings.Contains(err.Error(), "record background service installation") {
		t.Fatalf("first setup error = %v, want state update failure", err)
	}
	incomplete, err := remoteconnect.LoadState(fixture.stateDir)
	if err != nil {
		t.Fatalf("LoadState after update failure: %v", err)
	}
	if incomplete.Installed || incomplete.Stage != connectSetupStageServiceInstalling {
		t.Fatalf("state update failure was not resumable: %#v", incomplete)
	}

	if err := fixture.run(t); err != nil {
		t.Fatalf("repair setup: %v", err)
	}
	assertConnectSetupInstalled(t, fixture.stateDir)
	if installCalls != 2 || installedUpdateFailures != 1 || updateCalls < 5 {
		t.Fatalf(
			"repair calls = install:%d update:%d installed failures:%d",
			installCalls,
			updateCalls,
			installedUpdateFailures,
		)
	}
	if fixture.cloud.starts.Load() != 1 || fixture.cloud.polls.Load() != 1 {
		t.Fatalf("repair unexpectedly re-authorized: starts=%d polls=%d", fixture.cloud.starts.Load(), fixture.cloud.polls.Load())
	}
}

func assertConnectSetupInstalled(t *testing.T, stateDir string) {
	t.Helper()
	state, err := remoteconnect.LoadState(stateDir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !state.Installed || state.Stage != connectSetupStageOnline {
		t.Fatalf("connection did not converge to online: %#v", state)
	}
}

func TestConnectStatusIsFriendlyWhenNotConnected(t *testing.T) {
	var stdout bytes.Buffer
	err := runConnectCommand(context.Background(), filepath.Join(t.TempDir(), "config.json"), []string{"status", "--state-dir", t.TempDir()}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runConnectCommand: %v", err)
	}
	if !strings.Contains(stdout.String(), "Remote access is not connected") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestConnectStatusDoesNotPrintCredentials(t *testing.T) {
	stateDir := t.TempDir()
	if err := remoteconnect.SaveState(stateDir, remoteconnect.State{
		CloudURL:        "https://or3.chat",
		EnvironmentName: "Studio Mac",
		Hostname:        "studio.example.test",
		ControlToken:    "control-secret",
	}, remoteconnect.TunnelCredential{Token: "tunnel-secret"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	var stdout bytes.Buffer
	if err := runConnectCommand(context.Background(), "", []string{"status", "--state-dir", stateDir}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runConnectCommand: %v", err)
	}
	output := stdout.String()
	if strings.Contains(output, "control-secret") || strings.Contains(output, "tunnel-secret") {
		t.Fatalf("status leaked credentials: %s", output)
	}
	if !strings.Contains(output, "Studio Mac") {
		t.Fatalf("status omitted computer name: %s", output)
	}
}

func TestConnectStatusExplainsIncompleteCheckpoint(t *testing.T) {
	stateDir := t.TempDir()
	if err := remoteconnect.SaveState(stateDir, remoteconnect.State{
		CloudURL:        "https://or3.chat",
		EnvironmentName: "Studio Mac",
		Hostname:        "studio.example.test",
		Stage:           connectSetupStageAuthorized,
	}, remoteconnect.TunnelCredential{Token: "tunnel-secret"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	var stdout bytes.Buffer
	if err := printRemoteConnectionStatus(stateDir, &stdout); err != nil {
		t.Fatalf("printRemoteConnectionStatus: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "Mode:     background service") ||
		!strings.Contains(output, "Status:   setup incomplete (authorized)") ||
		!strings.Contains(output, "run `or3-intern connect` to resume safely") {
		t.Fatalf("incomplete status did not explain repair: %s", output)
	}
}

func TestConnectHelpIsDiscoverable(t *testing.T) {
	var output bytes.Buffer
	if err := printHelpTopic(&output, []string{"connect"}); err != nil {
		t.Fatalf("printHelpTopic: %v", err)
	}
	if !strings.Contains(output.String(), "npx @or3/connect intern") || !strings.Contains(output.String(), "./scripts/install-cli.sh") || !strings.Contains(output.String(), "or3-intern connect --cloud-url") {
		t.Fatalf("connect help missing: %s", output.String())
	}
}

func TestConnectSuccessPrintsCLIManagementCommands(t *testing.T) {
	var output bytes.Buffer
	err := finishRemoteConnectionSetup(
		context.Background(),
		remoteconnect.State{
			EnvironmentName: "Studio Mac",
			Stage:           connectSetupStageOnline,
		},
		t.TempDir(),
		&output,
		connectSetupOperations{},
		false,
	)
	if err != nil {
		t.Fatalf("finishRemoteConnectionSetup: %v", err)
	}
	for _, command := range []string{
		"or3-intern connect status",
		"or3-intern connect doctor",
		"or3-intern connect disconnect",
	} {
		if !strings.Contains(output.String(), command) {
			t.Fatalf("success output omitted %q: %s", command, output.String())
		}
	}
}

func TestDefaultConnectStateRespectsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "state")
	t.Setenv("OR3_CONNECT_HOME", override)
	if got := remoteconnect.DefaultStateDir(); got != override {
		t.Fatalf("DefaultStateDir() = %q, want %q", got, override)
	}
	if _, err := os.Stat(override); !os.IsNotExist(err) {
		t.Fatalf("state lookup should not create the directory")
	}
}

func TestRegisterLocalConnectCredentialUsesPairedDeviceStore(t *testing.T) {
	cfg := config.Default()
	cfg.DBPath = filepath.Join(t.TempDir(), "or3.sqlite")
	credential := remoteconnect.DeviceCredential{
		AccountID:       "account-a",
		WorkspaceID:     "workspace-a",
		EnvironmentID:   "env-a",
		EnvironmentName: "Studio Mac",
		Namespace:       "or3-chat:workspace-a:",
		ControlToken:    "connect-token-that-is-at-least-thirty-two-bytes-long",
	}
	if err := registerLocalConnectCredential(context.Background(), cfg, credential); err != nil {
		t.Fatalf("registerLocalConnectCredential: %v", err)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	broker := &approval.Broker{DB: database}
	device, err := broker.AuthenticateDeviceToken(context.Background(), credential.ControlToken, approval.RoleConnect)
	if err != nil {
		t.Fatalf("AuthenticateDeviceToken: %v", err)
	}
	if device.DeviceID != "or3-connect:env-a" {
		t.Fatalf("device id = %q", device.DeviceID)
	}
	if device.Metadata["connect_namespace"] != credential.Namespace {
		t.Fatalf("namespace metadata = %#v", device.Metadata["connect_namespace"])
	}
}

func TestRestoreConnectServiceConfigRestoresOnlyConnectOwnedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Provider.Model = "custom-model"
	cfg.Service.Enabled = true
	cfg.Service.Listen = "127.0.0.1:7777"
	cfg.Service.Secret = "existing-service-secret"
	cfg.Service.TrustedBrowserOrigins = []string{"https://local.example"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save original: %v", err)
	}
	previous := snapshotConnectServiceConfig(cfg)
	cfg.Service.Listen = "127.0.0.1:9100"
	cfg.Service.TrustedBrowserOrigins = append(cfg.Service.TrustedBrowserOrigins, "https://or3.chat")
	applied := snapshotConnectServiceConfig(cfg)
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save applied: %v", err)
	}

	if err := restoreConnectServiceConfig(remoteconnect.State{
		ConfigPath:      path,
		PreviousService: &previous,
		AppliedService:  &applied,
	}); err != nil {
		t.Fatalf("restoreConnectServiceConfig: %v", err)
	}
	restored, err := config.LoadPersisted(path)
	if err != nil {
		t.Fatalf("LoadPersisted: %v", err)
	}
	if restored.Service.Listen != "127.0.0.1:7777" ||
		restored.Service.Secret != "existing-service-secret" ||
		len(restored.Service.TrustedBrowserOrigins) != 1 ||
		restored.Service.TrustedBrowserOrigins[0] != "https://local.example" {
		t.Fatalf("service settings were not restored: %#v", restored.Service)
	}
	if restored.Provider.Model != "custom-model" {
		t.Fatalf("unrelated setting changed: %q", restored.Provider.Model)
	}
}

func TestRestoreConnectServiceConfigPreservesConcurrentListenEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Service.Listen = "127.0.0.1:7777"
	previous := snapshotConnectServiceConfig(cfg)
	cfg.Service.Enabled = true
	cfg.Service.Listen = "127.0.0.1:9100"
	applied := snapshotConnectServiceConfig(cfg)
	cfg.Service.Listen = "127.0.0.1:9200"
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save concurrent config: %v", err)
	}

	if err := restoreConnectServiceConfig(remoteconnect.State{
		ConfigPath:      path,
		PreviousService: &previous,
		AppliedService:  &applied,
	}); err != nil {
		t.Fatalf("restoreConnectServiceConfig: %v", err)
	}
	restored, err := config.LoadPersisted(path)
	if err != nil {
		t.Fatalf("LoadPersisted: %v", err)
	}
	if restored.Service.Listen != "127.0.0.1:9200" {
		t.Fatalf("concurrent listen edit was overwritten: %q", restored.Service.Listen)
	}
}
