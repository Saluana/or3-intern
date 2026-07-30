package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	remoteconnect "or3-intern/internal/connect"
	"or3-intern/internal/db"
)

type connectCommandOptions struct {
	CloudURL  string
	StateDir  string
	Name      string
	NoService bool
	NoBrowser bool
	LocalOnly bool
	Timeout   time.Duration
}

type connectSetupOperations struct {
	saveConfig         func(string, config.Config) error
	saveState          func(string, remoteconnect.State, remoteconnect.TunnelCredential) error
	currentServiceSpec func(string, string) (remoteconnect.ServiceSpec, error)
	installService     func(remoteconnect.ServiceSpec) error
	updateState        func(string, remoteconnect.State) error
	verifyOnline       func(context.Context, remoteconnect.State) error
}

func defaultConnectSetupOperations() connectSetupOperations {
	return connectSetupOperations{
		saveConfig:         config.Save,
		saveState:          remoteconnect.SaveState,
		currentServiceSpec: remoteconnect.CurrentServiceSpec,
		installService:     remoteconnect.InstallService,
		updateState:        remoteconnect.UpdateState,
		verifyOnline:       waitForRemoteConnectionOnline,
	}
}

const (
	connectSetupStageAuthorized        = "authorized"
	connectSetupStageLocalConfigured   = "local_configured"
	connectSetupStageServiceInstalling = "service_installing"
	connectSetupStageInstalled         = "installed"
	connectSetupStageOnline            = "online"
)

func runConnectCommand(ctx context.Context, cfgPath string, args []string, stdout, stderr io.Writer) error {
	subcommand := "setup"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	options, err := parseConnectOptions(subcommand, args)
	if err != nil {
		return err
	}
	if options.LocalOnly && subcommand != "uninstall" {
		return newUsageError("--local-only is only valid with `connect uninstall`")
	}
	switch subcommand {
	case "setup":
		return setupRemoteConnection(ctx, cfgPath, options, stdout, stderr)
	case "status":
		return printRemoteConnectionStatus(options.StateDir, stdout)
	case "doctor":
		return doctorRemoteConnection(ctx, options.StateDir, stdout)
	case "disconnect":
		return disconnectRemoteConnection(ctx, options, stdout)
	case "uninstall":
		return uninstallRemoteConnection(ctx, options, stdout)
	case "run":
		if os.Getenv(remoteconnect.ManagedServiceLogsEnv) == "1" {
			logs, logErr := remoteconnect.OpenManagedServiceLogs(options.StateDir)
			if logErr != nil {
				return fmt.Errorf("open bounded service logs: %w", logErr)
			}
			runErr := runRemoteConnectionService(ctx, cfgPath, options.StateDir, logs.Stdout, logs.Stderr)
			if runErr != nil {
				_, _ = fmt.Fprintf(logs.Stderr, "OR3 Connect stopped: %v\n", runErr)
			}
			if closeErr := logs.Close(); closeErr != nil && runErr == nil {
				return fmt.Errorf("close bounded service logs: %w", closeErr)
			}
			return runErr
		}
		return runRemoteConnectionService(ctx, cfgPath, options.StateDir, stdout, stderr)
	default:
		return newUsageError("unknown connect command %q", subcommand)
	}
}

func parseConnectOptions(subcommand string, args []string) (connectCommandOptions, error) {
	options := connectCommandOptions{
		CloudURL: remoteconnect.DefaultCloudURL,
		StateDir: remoteconnect.DefaultStateDir(),
		Timeout:  remoteconnect.DefaultTimeout,
	}
	fs := flag.NewFlagSet("connect "+subcommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&options.CloudURL, "cloud-url", envOrDefault("OR3_CONNECT_CLOUD_URL", options.CloudURL), "OR3 Cloud URL")
	fs.StringVar(&options.StateDir, "state-dir", options.StateDir, "remote connection state directory")
	fs.StringVar(&options.Name, "name", "", "computer name")
	fs.BoolVar(&options.NoService, "no-service", false, "keep the connection in this terminal")
	fs.BoolVar(&options.NoBrowser, "no-browser", false, "print the sign-in link without opening it")
	fs.BoolVar(&options.LocalOnly, "local-only", false, "remove local files without revoking cloud access")
	fs.DurationVar(&options.Timeout, "timeout", options.Timeout, "sign-in timeout")
	if err := fs.Parse(args); err != nil {
		return connectCommandOptions{}, newUsageError("%v", err)
	}
	if len(fs.Args()) > 0 {
		return connectCommandOptions{}, newUsageError("unexpected connect arguments: %s", strings.Join(fs.Args(), " "))
	}
	return options, nil
}

func setupRemoteConnection(ctx context.Context, cfgPath string, options connectCommandOptions, stdout, stderr io.Writer) error {
	return setupRemoteConnectionWithOperations(
		ctx,
		cfgPath,
		options,
		stdout,
		stderr,
		defaultConnectSetupOperations(),
	)
}

func setupRemoteConnectionWithOperations(
	ctx context.Context,
	cfgPath string,
	options connectCommandOptions,
	stdout,
	stderr io.Writer,
	operations connectSetupOperations,
) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("OR3 remote access currently supports macOS and Linux")
	}
	if existing, err := remoteconnect.LoadState(options.StateDir); err == nil {
		return resumeRemoteConnectionSetup(ctx, existing, options, stdout, operations)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("load saved remote connection for repair: %w", err)
	}
	cloudflaredPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return fmt.Errorf("cloudflared is required but was not found; install it and run this command again")
	}
	runtimeCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load OR3 settings: %w", err)
	}
	persistedCfg, err := config.LoadPersisted(cfgPath)
	if err != nil {
		return fmt.Errorf("load persisted OR3 settings: %w", err)
	}
	host, err := buildConnectHostMetadata(options.Name)
	if err != nil {
		return err
	}
	client := remoteconnect.NewClient(options.CloudURL)
	authorization, err := client.Start(ctx, host)
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, "OR3 Connect")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Sign in to connect this computer:")
	fmt.Fprintf(stdout, "  %s\n", authorization.VerificationURIComplete)
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Confirm this code in your browser:  %s\n", authorization.UserCode)
	if !options.NoBrowser {
		if err := openBrowser(authorization.VerificationURIComplete); err != nil {
			fmt.Fprintln(stderr, "Could not open the browser automatically. Use the link above.")
		}
	}

	credential, err := waitForDeviceCredential(ctx, client, authorization, host, options.Timeout)
	if err != nil {
		return err
	}
	if strings.TrimSpace(credential.ControlToken) == "" {
		return fmt.Errorf("OR3 Cloud approved the computer without returning an access credential")
	}
	if strings.TrimSpace(credential.Namespace) == "" {
		return fmt.Errorf("OR3 Cloud approved the computer without returning a workspace scope")
	}
	if strings.TrimSpace(credential.WorkspaceID) == "" {
		return fmt.Errorf("OR3 Cloud approved the computer without returning a workspace identity")
	}
	if strings.TrimSpace(credential.AccountID) == "" {
		return fmt.Errorf("OR3 Cloud approved the computer without returning an account identity")
	}
	previousService := snapshotConnectServiceConfig(persistedCfg)
	persistedCfg.Service.Enabled = true
	persistedCfg.Service.Listen = "127.0.0.1:9100"
	if cloudOrigin, err := url.Parse(client.BaseURL); err == nil && cloudOrigin.Scheme != "" && cloudOrigin.Host != "" {
		origin := cloudOrigin.Scheme + "://" + cloudOrigin.Host
		if !containsString(persistedCfg.Service.TrustedBrowserOrigins, origin) {
			persistedCfg.Service.TrustedBrowserOrigins = append(persistedCfg.Service.TrustedBrowserOrigins, origin)
		}
	}
	appliedService := snapshotConnectServiceConfig(persistedCfg)
	state := remoteconnect.State{
		CloudURL:        client.BaseURL,
		AccountID:       credential.AccountID,
		WorkspaceID:     credential.WorkspaceID,
		Namespace:       credential.Namespace,
		EnvironmentID:   credential.EnvironmentID,
		EnvironmentName: credential.EnvironmentName,
		Hostname:        credential.Tunnel.Hostname,
		ControlToken:    credential.ControlToken,
		CloudflaredPath: cloudflaredPath,
		ConfigPath:      cfgPath,
		PreviousService: &previousService,
		AppliedService:  &appliedService,
		Stage:           connectSetupStageAuthorized,
		TerminalOnly:    options.NoService,
		Installed:       false,
		ConnectedAt:     time.Now().UTC(),
	}
	if err := operations.saveState(options.StateDir, state, credential.Tunnel); err != nil {
		return connectSetupErrorWithRollback(
			err,
			rollbackNewRemoteConnectionSetup(ctx, client, runtimeCfg, state, options.StateDir, operations),
		)
	}
	if err := registerLocalConnectCredential(ctx, runtimeCfg, credential); err != nil {
		return connectSetupErrorWithRollback(
			err,
			rollbackNewRemoteConnectionSetup(ctx, client, runtimeCfg, state, options.StateDir, operations),
		)
	}
	if err := applyAuthorizedConnectConfig(state, operations.saveConfig); err != nil {
		return connectSetupErrorWithRollback(
			fmt.Errorf("save OR3 service settings: %w", err),
			rollbackNewRemoteConnectionSetup(ctx, client, runtimeCfg, state, options.StateDir, operations),
		)
	}
	state.Stage = connectSetupStageLocalConfigured
	if err := operations.updateState(options.StateDir, state); err != nil {
		return fmt.Errorf("record local connection configuration: %w", err)
	}

	if options.NoService {
		fmt.Fprintf(stdout, "\nConnected as %s. Keep this terminal open.\n", credential.EnvironmentName)
		return runRemoteConnectionService(ctx, cfgPath, options.StateDir, stdout, stderr)
	}
	return finishRemoteConnectionSetup(ctx, state, options.StateDir, stdout, operations, false)
}

func resumeRemoteConnectionSetup(
	ctx context.Context,
	state remoteconnect.State,
	options connectCommandOptions,
	stdout io.Writer,
	operations connectSetupOperations,
) error {
	state.Stage = normalizedConnectSetupStage(state)
	if state.Stage == connectSetupStageOnline || state.TerminalOnly {
		fmt.Fprintf(stdout, "This computer is already connected as %s.\n", state.EnvironmentName)
		fmt.Fprintln(stdout, "Run `npx or3 connect status` for details or `npx or3 connect disconnect` to replace it.")
		return nil
	}
	fmt.Fprintf(stdout, "Repairing the incomplete connection for %s.\n", state.EnvironmentName)
	if state.Stage == connectSetupStageAuthorized {
		if err := registerAuthorizedConnectCredential(ctx, state); err != nil {
			return err
		}
		if err := applyAuthorizedConnectConfig(state, operations.saveConfig); err != nil {
			return fmt.Errorf("resume OR3 service settings: %w", err)
		}
		state.Stage = connectSetupStageLocalConfigured
		if err := operations.updateState(options.StateDir, state); err != nil {
			return fmt.Errorf("record repaired local connection configuration: %w", err)
		}
	}
	return finishRemoteConnectionSetup(ctx, state, options.StateDir, stdout, operations, true)
}

func registerAuthorizedConnectCredential(ctx context.Context, state remoteconnect.State) error {
	if strings.TrimSpace(state.Namespace) == "" {
		return fmt.Errorf("saved authorization is missing its workspace namespace")
	}
	cfg, err := config.Load(state.ConfigPath)
	if err != nil {
		return fmt.Errorf("load OR3 settings to resume local credential registration: %w", err)
	}
	return registerLocalConnectCredential(ctx, cfg, remoteconnect.DeviceCredential{
		AccountID:       state.AccountID,
		WorkspaceID:     state.WorkspaceID,
		EnvironmentID:   state.EnvironmentID,
		EnvironmentName: state.EnvironmentName,
		Namespace:       state.Namespace,
		ControlToken:    state.ControlToken,
	})
}

func finishRemoteConnectionSetup(
	ctx context.Context,
	state remoteconnect.State,
	stateDir string,
	stdout io.Writer,
	operations connectSetupOperations,
	repairing bool,
) error {
	for {
		switch state.Stage {
		case connectSetupStageLocalConfigured:
			state.Stage = connectSetupStageServiceInstalling
			if err := operations.updateState(stateDir, state); err != nil {
				return fmt.Errorf("record background service installation start: %w", err)
			}
		case connectSetupStageServiceInstalling:
			spec, err := operations.currentServiceSpec(state.ConfigPath, stateDir)
			if err != nil {
				return err
			}
			if !repairing {
				fmt.Fprintln(stdout, "\nOne administrator approval installs the background service.")
			}
			if err := operations.installService(spec); err != nil {
				if repairing {
					return fmt.Errorf("repair background service: %w", err)
				}
				return fmt.Errorf("install background service: %w", err)
			}
			state.Installed = true
			state.Stage = connectSetupStageInstalled
			if err := operations.updateState(stateDir, state); err != nil {
				return fmt.Errorf("record background service installation: %w", err)
			}
		case connectSetupStageInstalled:
			if err := operations.verifyOnline(ctx, state); err != nil {
				return fmt.Errorf(
					"background service is installed, but remote access is not online yet: %w; run `npx or3 connect` to retry or `npx or3 connect doctor` for details",
					err,
				)
			}
			state.Installed = true
			state.Stage = connectSetupStageOnline
			if err := operations.updateState(stateDir, state); err != nil {
				return fmt.Errorf("record online remote connection: %w", err)
			}
		case connectSetupStageOnline:
			fmt.Fprintln(stdout)
			fmt.Fprintf(stdout, "Connected as %s\n", state.EnvironmentName)
			fmt.Fprintln(stdout, "OR3 will stay reachable after you log out or restart.")
			printConnectManagementCommands(stdout)
			return nil
		default:
			return fmt.Errorf("saved remote connection has unknown setup stage %q", state.Stage)
		}
	}
}

func normalizedConnectSetupStage(state remoteconnect.State) string {
	stage := strings.TrimSpace(state.Stage)
	if stage == "" {
		if state.Installed {
			return connectSetupStageInstalled
		}
		return connectSetupStageLocalConfigured
	}
	return stage
}

func applyAuthorizedConnectConfig(state remoteconnect.State, saveConfig func(string, config.Config) error) error {
	if state.PreviousService == nil || state.AppliedService == nil || strings.TrimSpace(state.ConfigPath) == "" {
		return fmt.Errorf("saved authorization is missing its service configuration checkpoint")
	}
	cfg, err := config.LoadPersisted(state.ConfigPath)
	if err != nil {
		return err
	}
	current := snapshotConnectServiceConfig(cfg)
	if equalConnectServiceSnapshots(current, *state.AppliedService) {
		return nil
	}
	if !connectServiceSnapshotCanResume(current, *state.PreviousService, *state.AppliedService) {
		return fmt.Errorf("service settings changed while Connect setup was incomplete; refusing to overwrite the newer settings")
	}
	next := persistedCfgWithServiceSnapshot(cfg, *state.AppliedService)
	return saveConfig(state.ConfigPath, next)
}

func connectServiceSnapshotCanResume(
	current,
	previous,
	applied remoteconnect.ServiceConfigSnapshot,
) bool {
	enabledSafe := current.Enabled == previous.Enabled || current.Enabled == applied.Enabled
	listenSafe := current.Listen == previous.Listen || current.Listen == applied.Listen
	originsSafe := equalStrings(current.TrustedBrowserOrigins, previous.TrustedBrowserOrigins) ||
		equalStrings(current.TrustedBrowserOrigins, applied.TrustedBrowserOrigins)
	return enabledSafe && listenSafe && originsSafe
}

func equalConnectServiceSnapshots(left, right remoteconnect.ServiceConfigSnapshot) bool {
	return left.Enabled == right.Enabled &&
		left.Listen == right.Listen &&
		equalStrings(left.TrustedBrowserOrigins, right.TrustedBrowserOrigins)
}

func rollbackNewRemoteConnectionSetup(
	ctx context.Context,
	client *remoteconnect.Client,
	runtimeCfg config.Config,
	state remoteconnect.State,
	stateDir string,
	operations connectSetupOperations,
) error {
	if state.PreviousService != nil && strings.TrimSpace(state.ConfigPath) != "" {
		if persisted, err := config.LoadPersisted(state.ConfigPath); err == nil {
			if err := operations.saveConfig(
				state.ConfigPath,
				persistedCfgWithServiceSnapshot(persisted, *state.PreviousService),
			); err != nil {
				return fmt.Errorf("restore OR3 service settings: %w", err)
			}
		} else {
			return fmt.Errorf("load OR3 service settings for rollback: %w", err)
		}
	}
	if err := revokeRegisteredConnectCredential(ctx, runtimeCfg, state.EnvironmentID, "connect:setup-rollback"); err != nil {
		return err
	}
	if err := client.Revoke(ctx, state); err != nil {
		return fmt.Errorf("revoke cloud environment during setup rollback: %w", err)
	}
	if err := remoteconnect.RemoveState(stateDir); err != nil {
		return fmt.Errorf("remove rolled-back connection state: %w", err)
	}
	return nil
}

func connectSetupErrorWithRollback(primary, rollback error) error {
	if rollback == nil {
		return primary
	}
	return fmt.Errorf(
		"%w; automatic rollback is incomplete: %v. Run `npx or3 connect` to resume from the saved checkpoint",
		primary,
		rollback,
	)
}

func waitForDeviceCredential(ctx context.Context, client *remoteconnect.Client, authorization remoteconnect.DeviceAuthorization, host remoteconnect.HostMetadata, timeout time.Duration) (remoteconnect.DeviceCredential, error) {
	if timeout <= 0 {
		timeout = remoteconnect.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	interval := time.Duration(authorization.Interval) * time.Second
	if interval <= 0 {
		interval = remoteconnect.DefaultPollInterval
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	transientFailures := 0
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return remoteconnect.DeviceCredential{}, fmt.Errorf("sign-in timed out; run `npx or3 connect` to try again")
			}
			return remoteconnect.DeviceCredential{}, ctx.Err()
		case <-timer.C:
			result, err := client.Poll(ctx, authorization.DeviceCode, host)
			if err != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return remoteconnect.DeviceCredential{}, fmt.Errorf("sign-in timed out; run `npx or3 connect` to try again")
				}
				if ctx.Err() != nil {
					return remoteconnect.DeviceCredential{}, ctx.Err()
				}
				if remoteconnect.IsRetryablePollError(err) {
					transientFailures++
					timer.Reset(connectPollRetryDelay(interval, transientFailures))
					continue
				}
				return remoteconnect.DeviceCredential{}, err
			}
			transientFailures = 0
			switch result.Status {
			case "approved":
				if result.Credential == nil {
					return remoteconnect.DeviceCredential{}, fmt.Errorf("OR3 Cloud approved the computer without returning its connection")
				}
				return *result.Credential, nil
			case "pending", "":
			case "slow_down":
				interval += time.Second
			case "denied":
				return remoteconnect.DeviceCredential{}, fmt.Errorf("connection was denied in the browser")
			case "expired":
				return remoteconnect.DeviceCredential{}, fmt.Errorf("the sign-in link expired; run `npx or3 connect` to try again")
			default:
				return remoteconnect.DeviceCredential{}, fmt.Errorf("OR3 Cloud returned an unknown sign-in state")
			}
			if result.RetryAfter > 0 {
				interval = time.Duration(result.RetryAfter) * time.Second
			}
			timer.Reset(interval)
		}
	}
}

func connectPollRetryDelay(base time.Duration, failures int) time.Duration {
	if base <= 0 {
		base = remoteconnect.DefaultPollInterval
	}
	delay := base
	for attempt := 1; attempt < failures && delay < 15*time.Second; attempt++ {
		delay *= 2
	}
	if delay > 15*time.Second {
		return 15 * time.Second
	}
	return delay
}

type connectOnlineProbeError struct {
	message   string
	retryable bool
}

func (err *connectOnlineProbeError) Error() string {
	return err.message
}

func waitForRemoteConnectionOnline(ctx context.Context, state remoteconnect.State) error {
	if strings.TrimSpace(state.Hostname) == "" ||
		strings.TrimSpace(state.ControlToken) == "" ||
		strings.TrimSpace(state.AccountID) == "" ||
		strings.TrimSpace(state.WorkspaceID) == "" ||
		strings.TrimSpace(state.EnvironmentID) == "" {
		return fmt.Errorf("saved connection is missing its scoped remote health credential")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	delay := 250 * time.Millisecond
	var lastErr error
	for {
		err := probeRemoteConnectionOnline(waitCtx, state)
		if err == nil {
			return nil
		}
		lastErr = err
		var probeErr *connectOnlineProbeError
		if errors.As(err, &probeErr) && !probeErr.retryable {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if lastErr != nil {
				return fmt.Errorf("remote health check timed out: %w", lastErr)
			}
			return waitCtx.Err()
		case <-timer.C:
		}
		if delay < 2*time.Second {
			delay *= 2
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
		}
	}
}

func probeRemoteConnectionOnline(ctx context.Context, state remoteconnect.State) error {
	return probeRemoteConnectionOnlineWithClient(ctx, http.DefaultClient, state)
}

func probeRemoteConnectionOnlineWithClient(
	ctx context.Context,
	client *http.Client,
	state remoteconnect.State,
) error {
	return probeRemoteConnectionOnlineWithOptions(ctx, client, state, connectOnlineProbeOptions{})
}

type connectOnlineProbeOptions struct {
	trace       *httptrace.ClientTrace
	bodyTimeout time.Duration
	phase       *connectDoctorPhaseTracker
}

func probeRemoteConnectionOnlineWithOptions(
	ctx context.Context,
	client *http.Client,
	state remoteconnect.State,
	options connectOnlineProbeOptions,
) error {
	hostname := strings.TrimSpace(state.Hostname)
	if strings.ContainsAny(hostname, "/?#@") {
		return &connectOnlineProbeError{
			message: "saved connection has an invalid remote hostname",
		}
	}
	target := (&url.URL{
		Scheme: "https",
		Host:   hostname,
		Path:   "/internal/v1/chat-runners",
	}).String()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return &connectOnlineProbeError{message: "build remote health check: " + err.Error()}
	}
	if options.trace != nil {
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), options.trace))
	}
	request.Header.Set("Authorization", "Bearer "+state.ControlToken)
	request.Header.Set("X-Or3-Auth-Method", "paired-device")
	response, err := client.Do(request)
	if err != nil {
		if options.phase != nil && connectNetworkTimeout(err, ctx) {
			return newConnectDoctorTimeoutError(options.phase.current())
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &connectOnlineProbeError{
			message:   "remote health check could not connect: " + err.Error(),
			retryable: true,
		}
	}
	defer response.Body.Close()
	if options.phase != nil {
		options.phase.set(connectDoctorPhaseBody)
	}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		body, readErr := readConnectProbeBody(ctx, response.Body, (256<<10)+1, options)
		if readErr != nil {
			var timeoutErr *connectDoctorTimeoutError
			if errors.As(readErr, &timeoutErr) {
				return timeoutErr
			}
			return &connectOnlineProbeError{
				message:   "read remote health response: " + readErr.Error(),
				retryable: true,
			}
		}
		if len(body) > 256<<10 {
			return &connectOnlineProbeError{
				message: "remote health response exceeded the expected size",
			}
		}
		var payload struct {
			Runners json.RawMessage `json:"runners"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || len(payload.Runners) == 0 {
			return &connectOnlineProbeError{
				message:   "remote health response did not match the OR3 runner API",
				retryable: true,
			}
		}
		var runnersPayload []json.RawMessage
		if err := json.Unmarshal(payload.Runners, &runnersPayload); err != nil {
			return &connectOnlineProbeError{
				message: "remote health response contained an invalid runner list",
			}
		}
		return nil
	}
	_, readErr := readConnectProbeBody(ctx, response.Body, 64<<10, options)
	if readErr != nil {
		var timeoutErr *connectDoctorTimeoutError
		if errors.As(readErr, &timeoutErr) {
			return timeoutErr
		}
	}
	retryable := response.StatusCode == http.StatusNotFound ||
		response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusTooEarly ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= http.StatusInternalServerError
	return &connectOnlineProbeError{
		message:   fmt.Sprintf("remote health check returned HTTP %d", response.StatusCode),
		retryable: retryable,
	}
}

func readConnectProbeBody(
	ctx context.Context,
	body io.ReadCloser,
	limit int64,
	options connectOnlineProbeOptions,
) ([]byte, error) {
	if options.bodyTimeout <= 0 {
		return io.ReadAll(io.LimitReader(body, limit))
	}
	type readResult struct {
		body []byte
		err  error
	}
	result := make(chan readResult, 1)
	go func() {
		payload, err := io.ReadAll(io.LimitReader(body, limit))
		result <- readResult{body: payload, err: err}
	}()
	timer := time.NewTimer(options.bodyTimeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return completed.body, completed.err
	case <-ctx.Done():
		_ = body.Close()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, newConnectDoctorTimeoutError(connectDoctorPhaseBody)
		}
		return nil, ctx.Err()
	case <-timer.C:
		_ = body.Close()
		return nil, newConnectDoctorTimeoutError(connectDoctorPhaseBody)
	}
}

const (
	connectDoctorPhaseDNS        = "DNS lookup"
	connectDoctorPhaseConnection = "connection"
	connectDoctorPhaseTLS        = "TLS handshake"
	connectDoctorPhaseHeaders    = "response headers"
	connectDoctorPhaseBody       = "response body"
)

type connectDoctorTimeouts struct {
	overall time.Duration
	dial    time.Duration
	tls     time.Duration
	headers time.Duration
	body    time.Duration
}

func defaultConnectDoctorTimeouts() connectDoctorTimeouts {
	return connectDoctorTimeouts{
		overall: 20 * time.Second,
		dial:    6 * time.Second,
		tls:     6 * time.Second,
		headers: 8 * time.Second,
		body:    5 * time.Second,
	}
}

type connectDoctorTimeoutError struct {
	phase string
}

func newConnectDoctorTimeoutError(phase string) *connectDoctorTimeoutError {
	if strings.TrimSpace(phase) == "" {
		phase = connectDoctorPhaseConnection
	}
	return &connectDoctorTimeoutError{phase: phase}
}

func (err *connectDoctorTimeoutError) Error() string {
	return fmt.Sprintf("timed out during %s", err.phase)
}

func (err *connectDoctorTimeoutError) Unwrap() error {
	return context.DeadlineExceeded
}

type connectDoctorPhaseTracker struct {
	mu    sync.Mutex
	phase string
}

func (tracker *connectDoctorPhaseTracker) set(phase string) {
	tracker.mu.Lock()
	tracker.phase = phase
	tracker.mu.Unlock()
}

func (tracker *connectDoctorPhaseTracker) current() string {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.phase
}

func (tracker *connectDoctorPhaseTracker) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			tracker.set(connectDoctorPhaseDNS)
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			tracker.set(connectDoctorPhaseConnection)
		},
		ConnectStart: func(_, _ string) {
			tracker.set(connectDoctorPhaseConnection)
		},
		TLSHandshakeStart: func() {
			tracker.set(connectDoctorPhaseTLS)
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			tracker.set(connectDoctorPhaseHeaders)
		},
		GotConn: func(httptrace.GotConnInfo) {
			tracker.set(connectDoctorPhaseHeaders)
		},
		GotFirstResponseByte: func() {
			tracker.set(connectDoctorPhaseBody)
		},
	}
}

func connectNetworkTimeout(err error, ctx context.Context) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

func newConnectDoctorHTTPClient(timeouts connectDoctorTimeouts) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeouts.dial,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   timeouts.tls,
			ResponseHeaderTimeout: timeouts.headers,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("remote health check refused an unexpected redirect")
		},
	}
}

func probeRemoteConnectionForDoctor(
	ctx context.Context,
	client *http.Client,
	state remoteconnect.State,
	timeouts connectDoctorTimeouts,
) error {
	if timeouts.overall <= 0 {
		timeouts.overall = defaultConnectDoctorTimeouts().overall
	}
	doctorCtx, cancel := context.WithTimeout(ctx, timeouts.overall)
	defer cancel()
	tracker := &connectDoctorPhaseTracker{phase: connectDoctorPhaseConnection}
	return probeRemoteConnectionOnlineWithOptions(doctorCtx, client, state, connectOnlineProbeOptions{
		trace:       tracker.trace(),
		bodyTimeout: timeouts.body,
		phase:       tracker,
	})
}

func runRemoteConnectionService(ctx context.Context, cfgPath, stateDir string, stdout, stderr io.Writer) error {
	state, err := remoteconnect.LoadState(stateDir)
	if err != nil {
		return fmt.Errorf("load saved remote connection: %w", err)
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	service := exec.CommandContext(ctx, binary, "--config", cfgPath, "service")
	service.Stdout, service.Stderr = stdout, stderr
	cloudflaredPath := strings.TrimSpace(state.CloudflaredPath)
	if cloudflaredPath == "" {
		cloudflaredPath = "cloudflared"
	}
	var tunnel *exec.Cmd
	if strings.TrimSpace(state.TunnelConfigFile) != "" {
		if _, err := os.Stat(state.TunnelConfigFile); err != nil {
			return fmt.Errorf("load tunnel configuration: %w", err)
		}
		if _, err := os.Stat(state.TunnelCredentialsFile); err != nil {
			return fmt.Errorf("load tunnel credential: %w", err)
		}
		tunnel = exec.CommandContext(
			ctx,
			cloudflaredPath,
			"tunnel",
			"--config",
			state.TunnelConfigFile,
			"run",
		)
	} else {
		if _, err := os.Stat(state.TunnelTokenFile); err != nil {
			return fmt.Errorf("load tunnel credential: %w", err)
		}
		tunnel = exec.CommandContext(ctx, cloudflaredPath, "tunnel", "run", "--token-file", state.TunnelTokenFile)
	}
	tunnel.Stdout, tunnel.Stderr = stdout, stderr
	if err := service.Start(); err != nil {
		return fmt.Errorf("start OR3 service: %w", err)
	}
	if err := tunnel.Start(); err != nil {
		_ = service.Process.Kill()
		_ = service.Wait()
		return fmt.Errorf("start secure tunnel: %w", err)
	}
	fmt.Fprintf(stdout, "OR3 remote connection ready: %s\n", state.EnvironmentName)

	type processResult struct {
		name string
		err  error
	}
	results := make(chan processResult, 2)
	go func() { results <- processResult{name: "OR3 service", err: service.Wait()} }()
	go func() { results <- processResult{name: "secure tunnel", err: tunnel.Wait()} }()
	result := <-results
	if service.Process != nil {
		_ = service.Process.Signal(syscall.SIGTERM)
	}
	if tunnel.Process != nil {
		_ = tunnel.Process.Signal(syscall.SIGTERM)
	}
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("%s stopped: %w", result.name, result.err)
}

func printRemoteConnectionStatus(stateDir string, stdout io.Writer) error {
	state, err := remoteconnect.LoadState(stateDir)
	if os.IsNotExist(err) {
		fmt.Fprintln(stdout, "Remote access is not connected.")
		fmt.Fprintln(stdout, "Run `npx or3 connect` to connect this computer to OR3 Cloud.")
		return nil
	}
	if err != nil {
		return err
	}
	stage := normalizedConnectSetupStage(state)
	mode := "background service"
	if state.TerminalOnly {
		mode = "terminal only"
	}
	status := stage
	if stage == connectSetupStageOnline {
		status = "online"
	} else if state.TerminalOnly {
		status = "runs while the connect command stays open"
	} else {
		status = "setup incomplete (" + stage + ")"
	}
	fmt.Fprintf(stdout, "Computer: %s\n", state.EnvironmentName)
	fmt.Fprintf(stdout, "Address:  %s\n", state.Hostname)
	fmt.Fprintf(stdout, "Mode:     %s\n", mode)
	fmt.Fprintf(stdout, "Status:   %s\n", status)
	fmt.Fprintf(stdout, "Cloud:    %s\n", state.CloudURL)
	if !state.TerminalOnly && stage != connectSetupStageOnline {
		fmt.Fprintln(stdout, "Next:     run `npx or3 connect` to resume safely")
	}
	return nil
}

func printConnectManagementCommands(stdout io.Writer) {
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Manage this connection:")
	fmt.Fprintln(stdout, "  npx or3 connect status")
	fmt.Fprintln(stdout, "  npx or3 connect doctor")
	fmt.Fprintln(stdout, "  npx or3 connect disconnect")
}

func doctorRemoteConnection(ctx context.Context, stateDir string, stdout io.Writer) error {
	writeDiagnostics := func() {
		if diagnostics := remoteconnect.RecentServiceDiagnostics(stateDir, 32<<10); diagnostics != "" {
			_, _ = io.WriteString(stdout, diagnostics)
		}
	}
	state, err := remoteconnect.LoadState(stateDir)
	if err != nil {
		writeDiagnostics()
		return fmt.Errorf("saved connection: %w", err)
	}
	fmt.Fprintln(stdout, "Saved connection: ready")
	cloudflaredPath := strings.TrimSpace(state.CloudflaredPath)
	if cloudflaredPath == "" {
		cloudflaredPath = "cloudflared"
	}
	if _, err := exec.LookPath(cloudflaredPath); err != nil {
		writeDiagnostics()
		return fmt.Errorf("cloudflared: not installed")
	}
	fmt.Fprintln(stdout, "Tunnel client: ready")
	timeouts := defaultConnectDoctorTimeouts()
	if err := probeRemoteConnectionForDoctor(ctx, newConnectDoctorHTTPClient(timeouts), state, timeouts); err != nil {
		writeDiagnostics()
		return fmt.Errorf("remote reachability: %w", err)
	}
	fmt.Fprintln(stdout, "Remote reachability: ready")
	return nil
}

func disconnectRemoteConnection(ctx context.Context, options connectCommandOptions, stdout io.Writer) error {
	state, err := remoteconnect.LoadState(options.StateDir)
	if os.IsNotExist(err) {
		fmt.Fprintln(stdout, "This computer is not connected to OR3 Cloud.")
		return nil
	}
	if err != nil {
		return err
	}
	if state.Installed {
		if err := remoteconnect.UninstallService(); err != nil {
			return fmt.Errorf("stop background service: %w", err)
		}
	}
	if err := remoteconnect.NewClient(state.CloudURL).Revoke(ctx, state); err != nil {
		return err
	}
	if err := revokeLocalConnectCredential(ctx, state); err != nil {
		return err
	}
	if err := restoreConnectServiceConfig(state); err != nil {
		return err
	}
	if err := remoteconnect.RemoveState(options.StateDir); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Remote access is disconnected. Local OR3 remains unchanged.")
	return nil
}

func snapshotConnectServiceConfig(cfg config.Config) remoteconnect.ServiceConfigSnapshot {
	return remoteconnect.ServiceConfigSnapshot{
		Enabled:               cfg.Service.Enabled,
		Listen:                cfg.Service.Listen,
		TrustedBrowserOrigins: append([]string(nil), cfg.Service.TrustedBrowserOrigins...),
	}
}

func persistedCfgWithServiceSnapshot(cfg config.Config, snapshot remoteconnect.ServiceConfigSnapshot) config.Config {
	cfg.Service.Enabled = snapshot.Enabled
	cfg.Service.Listen = snapshot.Listen
	cfg.Service.TrustedBrowserOrigins = append([]string(nil), snapshot.TrustedBrowserOrigins...)
	return cfg
}

func restoreConnectServiceConfig(state remoteconnect.State) error {
	if state.PreviousService == nil || state.AppliedService == nil || strings.TrimSpace(state.ConfigPath) == "" {
		return nil
	}
	cfg, err := config.LoadPersisted(state.ConfigPath)
	if err != nil {
		return fmt.Errorf("load persisted OR3 settings to restore service: %w", err)
	}
	changed := false
	if cfg.Service.Enabled == state.AppliedService.Enabled {
		cfg.Service.Enabled = state.PreviousService.Enabled
		changed = true
	}
	if cfg.Service.Listen == state.AppliedService.Listen {
		cfg.Service.Listen = state.PreviousService.Listen
		changed = true
	}
	if equalStrings(cfg.Service.TrustedBrowserOrigins, state.AppliedService.TrustedBrowserOrigins) {
		cfg.Service.TrustedBrowserOrigins = append([]string(nil), state.PreviousService.TrustedBrowserOrigins...)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := config.Save(state.ConfigPath, cfg); err != nil {
		return fmt.Errorf("restore OR3 service settings: %w", err)
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func registerLocalConnectCredential(ctx context.Context, cfg config.Config, credential remoteconnect.DeviceCredential) error {
	if strings.TrimSpace(cfg.DBPath) == "" {
		return fmt.Errorf("register Connect credential: database path is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
		return fmt.Errorf("register Connect credential: %w", err)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("register Connect credential: %w", err)
	}
	defer database.Close()
	broker := &approval.Broker{
		DB:     database,
		Config: cfg.Security.Approvals,
		HostID: cfg.Security.Approvals.HostID,
	}
	_, err = broker.RegisterDeviceToken(
		ctx,
		"or3-connect:"+strings.TrimSpace(credential.EnvironmentID),
		credential.ControlToken,
		approval.RoleConnect,
		credential.EnvironmentName,
		map[string]any{
			"connect_namespace":      strings.TrimSpace(credential.Namespace),
			"connect_account_id":     strings.TrimSpace(credential.AccountID),
			"connect_workspace_id":   strings.TrimSpace(credential.WorkspaceID),
			"connect_environment_id": strings.TrimSpace(credential.EnvironmentID),
		},
	)
	if err != nil {
		return fmt.Errorf("register Connect credential: %w", err)
	}
	return nil
}

func revokeLocalConnectCredential(ctx context.Context, state remoteconnect.State) error {
	cfg, err := config.Load(state.ConfigPath)
	if err != nil {
		return fmt.Errorf("load OR3 settings to revoke local credential: %w", err)
	}
	return revokeRegisteredConnectCredential(ctx, cfg, state.EnvironmentID, "connect:disconnect")
}

func revokeRegisteredConnectCredential(ctx context.Context, cfg config.Config, environmentID, actor string) error {
	if strings.TrimSpace(cfg.DBPath) == "" {
		return fmt.Errorf("revoke local Connect credential: database path is not configured")
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open OR3 database to revoke local credential: %w", err)
	}
	defer database.Close()
	broker := &approval.Broker{
		DB:     database,
		Config: cfg.Security.Approvals,
		HostID: cfg.Security.Approvals.HostID,
	}
	if err := broker.RevokeDevice(ctx, "or3-connect:"+strings.TrimSpace(environmentID), actor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("revoke local Connect credential: %w", err)
	}
	return nil
}

func uninstallRemoteConnection(ctx context.Context, options connectCommandOptions, stdout io.Writer) error {
	state, err := remoteconnect.LoadState(options.StateDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && !options.LocalOnly {
		if revokeErr := remoteconnect.NewClient(state.CloudURL).Revoke(ctx, state); revokeErr != nil {
			return fmt.Errorf("revoke cloud access before uninstall: %w; saved connection was preserved for retry", revokeErr)
		}
		if revokeErr := revokeLocalConnectCredential(ctx, state); revokeErr != nil {
			return revokeErr
		}
		if restoreErr := restoreConnectServiceConfig(state); restoreErr != nil {
			return restoreErr
		}
	}
	if err == nil && options.LocalOnly {
		fmt.Fprintln(stdout, "Warning: removing local files only; cloud access is not being revoked.")
	}
	if uninstallErr := remoteconnect.UninstallService(); uninstallErr != nil {
		return fmt.Errorf("remove background service: %w", uninstallErr)
	}
	if err == nil {
		if removeErr := remoteconnect.RemoveState(options.StateDir); removeErr != nil {
			return fmt.Errorf("remove saved connection: %w", removeErr)
		}
	}
	if options.LocalOnly {
		fmt.Fprintln(stdout, "Local OR3 Connect files were removed. Revoke the computer from OR3 Cloud separately.")
	} else {
		fmt.Fprintln(stdout, "OR3 remote access was revoked and removed from this computer.")
	}
	return nil
}

func buildConnectHostMetadata(name string) (remoteconnect.HostMetadata, error) {
	if strings.TrimSpace(name) == "" {
		var err error
		name, err = os.Hostname()
		if err != nil {
			return remoteconnect.HostMetadata{}, err
		}
	}
	return remoteconnect.HostMetadata{
		Name:          strings.TrimSpace(name),
		Platform:      runtime.GOOS,
		Architecture:  runtime.GOARCH,
		InternVersion: "v1",
	}, nil
}

func openBrowser(target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case "linux":
		command, args = "xdg-open", []string{target}
	default:
		return fmt.Errorf("browser opening is not supported on %s", runtime.GOOS)
	}
	return exec.Command(command, args...).Start()
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func connectSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
