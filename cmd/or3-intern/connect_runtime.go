package main

// Runtime-specific setup for the small set of agent runtimes supported by
// OR3 Connect. The connect command owns enrollment and cloudflared; these
// adapters only prepare the runtime's documented loopback API.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	remoteconnect "or3-intern/internal/connect"
)

const (
	openClawRunsPluginVersion = "0.1.0"
	openClawDefaultPort       = 18789
	hermesDefaultPort         = 8642
	runtimeCommandTimeout     = 90 * time.Second
	runtimeInstallTimeout     = 5 * time.Minute
)

type externalRuntimePlan struct {
	host          remoteconnect.HostMetadata
	localOrigin   string
	basePath      string
	cloudOrigin   string
	runtimeBinary string
	configure     func(context.Context, string) error
	verify        func(context.Context, string) error
	rollback      func(context.Context) error
}

type runtimePreparationError struct {
	nextStep string
	err      error
}

func (e *runtimePreparationError) Error() string {
	if e == nil || e.err == nil {
		return "runtime preparation is incomplete"
	}
	return e.err.Error()
}

func (e *runtimePreparationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func newRuntimePreparationError(nextStep string, err error) error {
	return &runtimePreparationError{nextStep: nextStep, err: err}
}

func setupExternalRuntimeConnection(
	ctx context.Context,
	runtimeName string,
	options connectCommandOptions,
	stdout, stderr io.Writer,
) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("external agent connections currently support macOS and Linux")
	}
	if existing, err := remoteconnect.LoadState(options.StateDir); err == nil {
		if existing.Runtime != runtimeName {
			return fmt.Errorf("this computer is already connected as %s; run `npx @or3/connect disconnect` before switching runtimes", existing.Runtime)
		}
		return resumeRemoteConnectionSetup(ctx, existing, options, stdout, defaultConnectSetupOperations())
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("load saved remote connection for repair: %w", err)
	}
	resumeCheckpoint, err := loadRuntimePreparation(options.StateDir)
	if err != nil {
		return err
	}
	if resumeCheckpoint != nil && string(resumeCheckpoint.Runtime) != runtimeName {
		return fmt.Errorf("an incomplete %s preparation is saved; resume it with `npx @or3/connect %s` or remove it with `npx @or3/connect disconnect`", resumeCheckpoint.Runtime, resumeCheckpoint.Runtime)
	}
	if resumeCheckpoint != nil {
		fmt.Fprintf(stdout, "Resuming incomplete %s preparation from its last checkpoint.\n", runtimeName)
	}

	printExternalRuntimePlan(stdout, runtimeName)
	if !confirmRuntimeAction(stdout, "Continue with this setup?") {
		return errors.New("external runtime setup was declined")
	}
	adapter, err := runtimeAdapterFor(runtimeName)
	if err != nil {
		return err
	}
	confirm := func(action string) (bool, error) {
		return confirmRuntimeAction(stdout, action), nil
	}
	plan, err := prepareExternalRuntimeWithAdapter(ctx, adapter, PrepareInput{
		CloudOrigin: options.CloudURL,
		StateDir:    options.StateDir,
		Confirm:     confirm,
		Resume:      resumeCheckpoint,
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if err != nil {
		var readiness *runtimePreparationError
		if errors.As(err, &readiness) {
			// Keep a non-secret checkpoint before waiting. If the user chooses
			// not to wait (or this process is interrupted), the next invocation
			// can resume without repeating the runtime setup explanation.
			if checkpointErr := saveRuntimePreparation(options.StateDir, AdapterState{
				Runtime: adapter.ID(),
				Stage:   "runtime_onboarding",
			}); checkpointErr != nil {
				return fmt.Errorf("save runtime preparation checkpoint: %w", checkpointErr)
			}
		}
		plan, err = waitForRuntimePreparation(ctx, adapter, PrepareInput{
			CloudOrigin: options.CloudURL,
			StateDir:    options.StateDir,
			Confirm:     confirm,
			Resume:      resumeCheckpoint,
			Stdout:      stdout,
			Stderr:      stderr,
		}, err, options.Timeout)
		if err != nil {
			return err
		}
	}
	if name := strings.TrimSpace(options.Name); name != "" {
		plan.host.Name = name
	}
	if err := saveRuntimePreparation(options.StateDir, AdapterState{
		Runtime:     adapter.ID(),
		Stage:       connectSetupStageLocalConfigured,
		LocalOrigin: plan.localOrigin,
		BasePath:    plan.basePath,
		Version:     plan.host.RuntimeVersion,
	}); err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			fmt.Errorf("save runtime preparation checkpoint: %w", err),
			rollbackExternalRuntimePreparation(context.Background(), options.StateDir, plan),
		)
	}
	client := remoteconnect.NewClient(options.CloudURL)
	authorization, err := client.Start(ctx, plan.host)
	if err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			err,
			rollbackExternalRuntimePreparation(context.Background(), options.StateDir, plan),
		)
	}
	fmt.Fprintln(stdout, "\nSign in to connect this runtime:")
	fmt.Fprintf(stdout, "  %s\n\nConfirm this code in your browser:  %s\n", authorization.VerificationURIComplete, authorization.UserCode)
	if !options.NoBrowser {
		if err := openBrowser(authorization.VerificationURIComplete); err != nil {
			fmt.Fprintln(stderr, "Could not open the browser automatically. Use the link above.")
		}
	}
	credential, err := waitForDeviceCredential(ctx, client, authorization, plan.host, options.Timeout)
	if err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			err,
			rollbackExternalRuntimePreparation(context.Background(), options.StateDir, plan),
		)
	}
	if err := validateDeviceCredential(credential); err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			err,
			rollbackExternalRuntimePreparation(context.Background(), options.StateDir, plan),
		)
	}
	state := remoteconnect.State{
		CloudURL:        client.BaseURL,
		AccountID:       credential.AccountID,
		WorkspaceID:     credential.WorkspaceID,
		Namespace:       credential.Namespace,
		EnvironmentID:   credential.EnvironmentID,
		EnvironmentName: credential.EnvironmentName,
		Hostname:        credential.Tunnel.Hostname,
		ControlToken:    credential.ControlToken,
		Driver:          plan.host.Driver,
		Runtime:         plan.host.Runtime,
		RuntimeVersion:  plan.host.RuntimeVersion,
		LocalOrigin:     plan.localOrigin,
		BasePath:        plan.basePath,
		CloudflaredPath: mustCloudflaredPath(),
		// Persist an authorized cleanup checkpoint before putting the credential
		// into the runtime. Any interrupted failure is then visible and retryable.
		Stage:        connectSetupStageCleanupPending,
		TerminalOnly: options.NoService,
		ConnectedAt:  time.Now().UTC(),
	}
	if err := remoteconnect.SaveState(options.StateDir, state, credential.Tunnel); err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			fmt.Errorf("save external runtime connection: %w", err),
			errors.Join(
				rollbackExternalRuntimePreparation(context.Background(), options.StateDir, plan),
				client.Revoke(context.Background(), state),
			),
		)
	}
	if err := plan.configure(ctx, credential.ControlToken); err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			fmt.Errorf("configure %s: %w", runtimeName, err),
			cleanupExternalRuntimeEnrollment(context.Background(), options.StateDir, state, plan),
		)
	}
	target := &RuntimeConnectionTarget{
		Driver:      ConnectDriver(plan.host.Driver),
		Runtime:     ConnectRuntimeID(plan.host.Runtime),
		LocalOrigin: plan.localOrigin,
		BasePath:    plan.basePath,
		AccessToken: credential.ControlToken,
		Version:     plan.host.RuntimeVersion,
		DisplayName: plan.host.Name,
		plan:        &plan,
	}
	verification, err := adapter.Verify(ctx, target)
	if err != nil {
		if _, ok := adapter.(externalRuntimeAdapter); ok && runtimeName == "hermes" {
			if recoverErr := recoverHermesSSECors(*target.plan, options.StateDir, err, stdout, confirm); recoverErr == nil {
				verification, err = adapter.Verify(ctx, target)
			} else {
				err = recoverErr
			}
		}
	}
	if err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			fmt.Errorf("verify %s: %w", runtimeName, err),
			cleanupExternalRuntimeEnrollment(context.Background(), options.StateDir, state, plan),
		)
	}
	state.Stage = connectSetupStageLocalConfigured
	if err := remoteconnect.UpdateState(options.StateDir, state); err != nil {
		return externalRuntimeSetupErrorWithCleanup(
			fmt.Errorf("record external runtime connection: %w", err),
			cleanupExternalRuntimeEnrollment(context.Background(), options.StateDir, state, plan),
		)
	}
	// From this point on, the durable state and cloud enrollment are present.
	// Keep the runtime configuration intact if service installation or the
	// terminal-only tunnel needs to be retried; the saved state is the resume
	// source of truth.
	if options.NoService {
		return runRemoteConnectionServiceWithVerification(ctx, "", options.StateDir, stdout, stderr, func(verifyCtx context.Context, saved remoteconnect.State) error {
			adapter, remoteTarget, targetErr := externalRuntimeTargetFromState(saved)
			if targetErr != nil {
				return targetErr
			}
			remoteVerification, verifyErr := adapter.Verify(verifyCtx, remoteTarget)
			if verifyErr != nil {
				return fmt.Errorf("verify remote %s connection: %w", runtimeName, verifyErr)
			}
			printExternalRuntimeCompletion(stdout, saved, remoteVerification)
			if err := removeRuntimePreparation(options.StateDir); err != nil {
				return fmt.Errorf("remove runtime preparation checkpoint after remote verification: %w", err)
			}
			return nil
		})
	}
	if err := finishRemoteConnectionSetup(ctx, state, options.StateDir, stdout, defaultConnectSetupOperations(), false); err != nil {
		return err
	}
	// The local check proves the runtime itself is configured. Once the
	// managed service and tunnel are online, repeat the same live capability,
	// SSE, and cancellation check through the exact HTTPS path the browser will
	// use. Do not report an active host until that path has passed.
	remoteTarget, targetErr := externalRuntimeRemoteTarget(target, state)
	if targetErr != nil {
		return externalRuntimeSetupErrorWithCleanup(
			targetErr,
			cleanupExternalRuntimeEnrollment(context.Background(), options.StateDir, state, plan),
		)
	}
	remoteVerification, verifyErr := adapter.Verify(ctx, remoteTarget)
	if verifyErr != nil {
		return externalRuntimeSetupErrorWithCleanup(
			fmt.Errorf("verify remote %s connection: %w", runtimeName, verifyErr),
			cleanupExternalRuntimeEnrollment(context.Background(), options.StateDir, state, plan),
		)
	}
	verification = remoteVerification
	printExternalRuntimeCompletion(stdout, state, verification)
	_ = removeRuntimePreparation(options.StateDir)
	return nil
}

func externalRuntimeRemoteTarget(
	local *RuntimeConnectionTarget,
	state remoteconnect.State,
) (*RuntimeConnectionTarget, error) {
	if local == nil || local.plan == nil {
		return nil, errors.New("runtime verification target is incomplete")
	}
	hostname := strings.TrimSpace(state.Hostname)
	if hostname == "" || strings.ContainsAny(hostname, "/?#@") {
		return nil, errors.New("OR3 Cloud returned an invalid tunnel hostname")
	}
	basePath := strings.TrimSpace(state.BasePath)
	if basePath == "" {
		basePath = "/"
	}
	if basePath != "/" && basePath != "/or3/" {
		return nil, fmt.Errorf("OR3 Cloud returned an invalid runtime base path %q", basePath)
	}
	remote := *local
	remote.LocalOrigin = "https://" + hostname
	remote.BasePath = basePath
	remote.AccessToken = state.ControlToken
	return &remote, nil
}

// externalRuntimeTargetFromState rebuilds the small adapter target needed by
// a resumable connection. The original preparation plan is intentionally not
// persisted (it can contain rollback closures), so only the immutable remote
// verification context is recreated here.
func externalRuntimeTargetFromState(state remoteconnect.State) (RuntimeAdapter, *RuntimeConnectionTarget, error) {
	adapter, err := runtimeAdapterFor(strings.TrimSpace(state.Runtime))
	if err != nil {
		return nil, nil, err
	}
	remoteBase, err := externalRuntimeRemoteTarget(&RuntimeConnectionTarget{
		Driver:      ConnectDriver(state.Driver),
		Runtime:     ConnectRuntimeID(state.Runtime),
		LocalOrigin: "https://" + strings.TrimSpace(state.Hostname),
		BasePath:    state.BasePath,
		AccessToken: state.ControlToken,
		Version:     state.RuntimeVersion,
		DisplayName: state.EnvironmentName,
		plan: &externalRuntimePlan{
			host: remoteconnect.HostMetadata{
				Name:           state.EnvironmentName,
				Runtime:        state.Runtime,
				RuntimeVersion: state.RuntimeVersion,
				Driver:         state.Driver,
				BasePath:       state.BasePath,
			},
			cloudOrigin: state.CloudURL,
		}}, state)
	if err != nil {
		return nil, nil, err
	}
	// externalRuntimeRemoteTarget preserves the plan pointer from the local
	// target. Fill in the cloud origin used for the CORS probe after the copy.
	if remoteBase.plan == nil {
		return nil, nil, errors.New("runtime verification target is incomplete")
	}
	remoteBase.plan.cloudOrigin = state.CloudURL
	return adapter, remoteBase, nil
}

func verifyExternalRuntimeState(ctx context.Context, state remoteconnect.State) (*Verification, error) {
	adapter, target, err := externalRuntimeTargetFromState(state)
	if err != nil {
		return nil, err
	}
	return adapter.Verify(ctx, target)
}

func prepareExternalRuntimeWithAdapter(ctx context.Context, adapter RuntimeAdapter, input PrepareInput) (externalRuntimePlan, error) {
	target, err := adapter.Prepare(ctx, input)
	if err != nil {
		return externalRuntimePlan{}, err
	}
	if target == nil || target.plan == nil {
		return externalRuntimePlan{}, fmt.Errorf("runtime adapter %q returned no connection target", adapter.ID())
	}
	return *target.plan, nil
}

func waitForRuntimePreparation(
	ctx context.Context,
	adapter RuntimeAdapter,
	input PrepareInput,
	err error,
	timeout time.Duration,
) (externalRuntimePlan, error) {
	if input.Stdout == nil {
		input.Stdout = io.Discard
	}
	if input.Stderr == nil {
		input.Stderr = io.Discard
	}
	if input.Confirm == nil {
		input.Confirm = func(action string) (bool, error) {
			return confirmRuntimeAction(input.Stdout, action), nil
		}
	}
	var readiness *runtimePreparationError
	if !errors.As(err, &readiness) {
		return externalRuntimePlan{}, err
	}
	if readiness.nextStep == "" {
		return externalRuntimePlan{}, err
	}
	if timeout <= 0 {
		timeout = remoteconnect.DefaultTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	fmt.Fprintf(input.Stdout, "\n%s\n", readiness.nextStep)
	confirmed, confirmErr := input.Confirm("Wait while you finish that setup; Connect will retry automatically?")
	if confirmErr != nil {
		return externalRuntimePlan{}, confirmErr
	}
	if !confirmed {
		return externalRuntimePlan{}, err
	}
	interval := 3 * time.Second
	for {
		if waitErr := waitCtx.Err(); waitErr != nil {
			return externalRuntimePlan{}, fmt.Errorf("wait for %s readiness: %w; run `npx @or3/connect %s` to resume", adapter.ID(), waitErr, adapter.ID())
		}
		select {
		case <-waitCtx.Done():
			return externalRuntimePlan{}, fmt.Errorf("wait for %s readiness: %w; run `npx @or3/connect %s` to resume", adapter.ID(), waitCtx.Err(), adapter.ID())
		case <-time.After(interval):
		}
		plan, retryErr := prepareExternalRuntimeWithAdapter(waitCtx, adapter, input)
		if retryErr == nil {
			return plan, nil
		}
		var retryReadiness *runtimePreparationError
		if !errors.As(retryErr, &retryReadiness) {
			return externalRuntimePlan{}, retryErr
		}
		if retryReadiness.nextStep != "" {
			fmt.Fprintf(input.Stdout, "Still waiting: %s\n", retryReadiness.nextStep)
		}
	}
}

func printExternalRuntimePlan(stdout io.Writer, runtimeName string) {
	fmt.Fprintln(stdout, "OR3 Connect will:")
	fmt.Fprintf(stdout, "  • check %s and its provider/model readiness\n", runtimeName)
	fmt.Fprintln(stdout, "  • configure its authenticated Runs API on loopback with OR3's exact browser origin")
	fmt.Fprintln(stdout, "  • ask you to approve this computer in OR3 Cloud")
	fmt.Fprintln(stdout, "  • create a named Cloudflare Tunnel and optional background service")
}

func validateDeviceCredential(credential remoteconnect.DeviceCredential) error {
	for label, value := range map[string]string{
		"access credential":  credential.ControlToken,
		"workspace scope":    credential.Namespace,
		"workspace identity": credential.WorkspaceID,
		"account identity":   credential.AccountID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("OR3 Cloud approved the runtime without returning its %s", label)
		}
	}
	return nil
}

func mustCloudflaredPath() string {
	if value := strings.TrimSpace(os.Getenv("OR3_CONNECT_CLOUDFLARED_BIN")); value != "" {
		return value
	}
	if value, err := exec.LookPath("cloudflared"); err == nil {
		return value
	}
	return "cloudflared"
}

func prepareExternalRuntime(
	ctx context.Context,
	runtimeName, cloudURL string,
	stdout, stderr io.Writer,
) (externalRuntimePlan, error) {
	adapter, err := runtimeAdapterFor(runtimeName)
	if err != nil {
		return externalRuntimePlan{}, err
	}
	return prepareExternalRuntimeWithAdapter(ctx, adapter, PrepareInput{
		CloudOrigin: cloudURL,
		Stdout:      stdout,
		Stderr:      stderr,
	})
}

func prepareOpenClaw(ctx context.Context, cloudURL string, stdout, stderr io.Writer) (externalRuntimePlan, error) {
	return prepareOpenClawWithInput(ctx, PrepareInput{
		CloudOrigin: cloudURL,
		Stdout:      stdout,
		Stderr:      stderr,
	})
}

func prepareOpenClawWithInput(ctx context.Context, input PrepareInput) (externalRuntimePlan, error) {
	cloudURL := input.CloudOrigin
	stdout, stderr := input.Stdout, input.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	confirm := input.Confirm
	origin, err := normalizedCloudOrigin(cloudURL)
	if err != nil {
		return externalRuntimePlan{}, err
	}
	bin, version, err := ensureRuntimeBinaryWithConfirmContext(ctx, "openclaw", "https://openclaw.ai/install.sh", stdout, stderr, confirm)
	if err != nil {
		return externalRuntimePlan{}, err
	}
	if !openClawVersionCompatible(version) {
		return externalRuntimePlan{}, fmt.Errorf("OpenClaw %s is outside the supported 2026.7.x compatibility line for @or3/openclaw@%s; update OpenClaw before connecting", firstLine(version), openClawRunsPluginVersion)
	}
	configPath := openClawConfigPath(bin)
	snapshot, snapshotErr := runtimeSnapshotForPreparation(input.StateDir, "openclaw", bin, configPath)
	if snapshotErr != nil {
		return externalRuntimePlan{}, snapshotErr
	}
	rollbackOnError := true
	defer func() {
		if rollbackOnError {
			if err := restoreRuntimeConfiguration(context.Background(), "openclaw", bin, snapshot); err == nil {
				_ = removeRuntimeConfigBackup(input.StateDir)
			}
		}
	}()
	run := func(args ...string) (string, error) {
		return runRuntimeContext(ctx, bin, args...)
	}
	if _, err := run("gateway", "status"); err != nil {
		if !runtimeConfirm(stdout, "OpenClaw Gateway is not ready. Run `openclaw gateway start`?", confirm) {
			return externalRuntimePlan{}, errors.New("OpenClaw Gateway is not running")
		}
		if _, startErr := run("gateway", "start"); startErr != nil {
			return externalRuntimePlan{}, fmt.Errorf("start OpenClaw Gateway: %w", startErr)
		}
	}
	modelsOutput, err := run("models", "list", "--json")
	if err != nil {
		return externalRuntimePlan{}, newRuntimePreparationError(
			"OpenClaw has no ready model/provider configuration. Finish `openclaw onboard` in another terminal; Connect will keep checking.",
			errors.New("OpenClaw has no ready model/provider configuration; finish `openclaw onboard` first"),
		)
	}
	if !hasUsableOpenClawModel(modelsOutput) {
		return externalRuntimePlan{}, newRuntimePreparationError(
			"OpenClaw has no available model/provider configuration. Finish `openclaw onboard` in another terminal; Connect will keep checking.",
			errors.New("OpenClaw has no available model/provider configuration; finish `openclaw onboard` first"),
		)
	}
	pluginInspection, inspectErr := run("plugins", "inspect", "or3-runs", "--json")
	if inspectErr != nil || !openClawPluginVersionMatches(pluginInspection, openClawRunsPluginVersion) {
		if !runtimeConfirm(stdout, "Install the pinned OR3 Runs plugin from npm?", confirm) {
			return externalRuntimePlan{}, errors.New("OR3 Runs plugin installation was declined")
		}
		if _, installErr := run("plugins", "install", "npm:@or3/openclaw@"+openClawRunsPluginVersion, "--pin"); installErr != nil {
			return externalRuntimePlan{}, fmt.Errorf("install @or3/openclaw: %w", installErr)
		}
	}
	if _, err := run("plugins", "enable", "or3-runs"); err != nil {
		return externalRuntimePlan{}, fmt.Errorf("enable OR3 Runs plugin: %w", err)
	}
	pluginInspection, inspectErr = run("plugins", "inspect", "or3-runs", "--json")
	if inspectErr != nil || !openClawPluginReady(pluginInspection, openClawRunsPluginVersion) {
		return externalRuntimePlan{}, errors.New("OR3 Runs plugin inspection did not confirm the expected npm source, pinned version, and enabled state")
	}
	port := openClawPort(configPath)
	if bind := openClawBind(configPath); bind != "" && !isLoopbackBind(bind) {
		return externalRuntimePlan{}, fmt.Errorf("OpenClaw Gateway bind %q is not loopback; set gateway.bind to loopback before connecting", bind)
	}
	if err := updateOpenClawConfig(configPath, origin, ""); err != nil {
		return externalRuntimePlan{}, err
	}
	if _, err := run("gateway", "restart"); err != nil {
		return externalRuntimePlan{}, fmt.Errorf("restart OpenClaw Gateway: %w", err)
	}
	local := fmt.Sprintf("http://127.0.0.1:%d", port)
	host := remoteconnect.HostMetadata{
		Name:           runtimeDisplayName("OpenClaw"),
		Platform:       runtime.GOOS,
		Architecture:   runtime.GOARCH,
		Runtime:        "openclaw",
		RuntimeVersion: version,
		Driver:         "runs",
		BasePath:       "/or3/",
	}
	plan := externalRuntimePlan{
		host: host, localOrigin: local, basePath: "/or3/", cloudOrigin: origin, runtimeBinary: bin,
		configure: func(configureCtx context.Context, token string) error {
			if err := updateOpenClawConfig(configPath, origin, token); err != nil {
				return err
			}
			if _, err := runRuntimeContext(configureCtx, bin, "gateway", "restart"); err != nil {
				return fmt.Errorf("restart OpenClaw Gateway after credential setup: %w", err)
			}
			return nil
		},
		verify: func(ctx context.Context, token string) error {
			return probeRunsHTTP(ctx, local+"/or3/", token)
		},
		rollback: func(rollbackCtx context.Context) error {
			return restoreRuntimeConfiguration(rollbackCtx, "openclaw", bin, snapshot)
		},
	}
	rollbackOnError = false
	return plan, nil
}

func prepareHermes(ctx context.Context, cloudURL string, stdout, stderr io.Writer) (externalRuntimePlan, error) {
	return prepareHermesWithInput(ctx, PrepareInput{
		CloudOrigin: cloudURL,
		Stdout:      stdout,
		Stderr:      stderr,
	})
}

func prepareHermesWithInput(ctx context.Context, input PrepareInput) (externalRuntimePlan, error) {
	cloudURL := input.CloudOrigin
	stdout, stderr := input.Stdout, input.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	confirm := input.Confirm
	origin, err := normalizedCloudOrigin(cloudURL)
	if err != nil {
		return externalRuntimePlan{}, err
	}
	bin, version, err := ensureRuntimeBinaryWithConfirmContext(ctx, "hermes", "https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh", stdout, stderr, confirm)
	if err != nil {
		return externalRuntimePlan{}, err
	}
	run := func(args ...string) (string, error) {
		return runRuntimeContext(ctx, bin, args...)
	}
	if _, err := run("doctor"); err != nil {
		return externalRuntimePlan{}, newRuntimePreparationError(
			"Hermes is not ready. Finish `hermes setup` or the provider setup shown by `hermes --help` in another terminal; Connect will keep checking.",
			errors.New("Hermes is not ready; finish `hermes setup` or provider setup first"),
		)
	}
	statusOutput, err := run("status")
	if err != nil || !hasHermesModel(statusOutput) {
		return externalRuntimePlan{}, newRuntimePreparationError(
			"Hermes has no configured model/provider. Finish `hermes setup` or `hermes model` in another terminal; Connect will keep checking.",
			errors.New("Hermes has no configured model/provider; finish `hermes model` or `hermes setup` first"),
		)
	}
	envPath := hermesEnvPath(bin)
	snapshot, snapshotErr := runtimeSnapshotForPreparation(input.StateDir, "hermes", bin, envPath)
	if snapshotErr != nil {
		return externalRuntimePlan{}, snapshotErr
	}
	rollbackOnError := true
	defer func() {
		if rollbackOnError {
			if err := restoreRuntimeConfiguration(context.Background(), "hermes", bin, snapshot); err == nil {
				_ = removeRuntimeConfigBackup(input.StateDir)
			}
		}
	}()
	port := hermesPortForRuntimeContext(ctx, bin)
	local := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := mergeHermesEnvAtPort(envPath, origin, "", port); err != nil {
		return externalRuntimePlan{}, err
	}
	if _, err := run("gateway", "restart"); err != nil {
		if _, startErr := run("gateway", "start"); startErr != nil {
			return externalRuntimePlan{}, fmt.Errorf("start Hermes gateway: %w", startErr)
		}
	}
	host := remoteconnect.HostMetadata{
		Name:           runtimeDisplayName("Hermes"),
		Platform:       runtime.GOOS,
		Architecture:   runtime.GOARCH,
		Runtime:        "hermes",
		RuntimeVersion: version,
		Driver:         "runs",
		BasePath:       "/",
	}
	plan := externalRuntimePlan{
		host: host, localOrigin: local, basePath: "/", cloudOrigin: origin, runtimeBinary: bin,
		configure: func(configureCtx context.Context, token string) error {
			if err := mergeHermesEnvAtPort(envPath, origin, token, port); err != nil {
				return err
			}
			if _, err := runRuntimeContext(configureCtx, bin, "gateway", "restart"); err != nil {
				if _, startErr := runRuntimeContext(configureCtx, bin, "gateway", "start"); startErr != nil {
					return fmt.Errorf("restart Hermes gateway: %w", err)
				}
			}
			return nil
		},
		verify: func(ctx context.Context, token string) error {
			return probeRunsHTTP(ctx, local+"/", token)
		},
		rollback: func(rollbackCtx context.Context) error {
			return restoreRuntimeConfiguration(rollbackCtx, "hermes", bin, snapshot)
		},
	}
	rollbackOnError = false
	return plan, nil
}

func ensureRuntimeBinary(name, installer string, stdout, stderr io.Writer) (string, string, error) {
	return ensureRuntimeBinaryWithConfirmContext(context.Background(), name, installer, stdout, stderr, nil)
}

func ensureRuntimeBinaryWithConfirm(
	name, installer string,
	stdout, stderr io.Writer,
	confirm func(string) (bool, error),
) (string, string, error) {
	return ensureRuntimeBinaryWithConfirmContext(context.Background(), name, installer, stdout, stderr, confirm)
}

func ensureRuntimeBinaryWithConfirmContext(
	ctx context.Context,
	name, installer string,
	stdout, stderr io.Writer,
	confirm func(string) (bool, error),
) (string, string, error) {
	bin, err := findRuntimeBinary(name)
	if err != nil {
		fmt.Fprintf(stdout, "`%s` is not installed. Official installer: %s\n", name, installer)
		if !runtimeConfirm(stdout, "Run the official installer now?", confirm) {
			return "", "", fmt.Errorf("%s is required", name)
		}
		command := fmt.Sprintf("curl -fsSL %s | bash", installer)
		installCtx, cancel := context.WithTimeout(ctx, runtimeInstallTimeout)
		defer cancel()
		cmd := exec.Command("sh", "-c", command)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
		if err := cmd.Start(); err != nil {
			return "", "", fmt.Errorf("start %s installer: %w", name, err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		var installErr error
		select {
		case installErr = <-done:
		case <-installCtx.Done():
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			<-done
			return "", "", fmt.Errorf("install %s timed out after %s: %w", name, runtimeInstallTimeout, installCtx.Err())
		}
		if installErr != nil {
			return "", "", fmt.Errorf("install %s: %w", name, installErr)
		}
		bin, err = findRuntimeBinary(name)
		if err != nil {
			return "", "", fmt.Errorf("%s was installed but is not on PATH; restart the terminal and run Connect again", name)
		}
	}
	output, err := runRuntimeContext(ctx, bin, "--version")
	if err != nil {
		return "", "", fmt.Errorf("%s is installed but could not run: %w", name, err)
	}
	return bin, firstLine(output), nil
}

func findRuntimeBinary(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", name),
		filepath.Join(home, ".vite-plus", "bin", name),
		filepath.Join(home, ".openclaw", "bin", name),
		filepath.Join(home, ".npm-global", "bin", name),
	}
	for _, candidate := range candidates {
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func hasUsableOpenClawModel(output string) bool {
	var payload struct {
		Models []struct {
			ID        string `json:"id"`
			Key       string `json:"key"`
			Available *bool  `json:"available"`
			Missing   bool   `json:"missing"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return false
	}
	for _, model := range payload.Models {
		if (model.ID != "" || model.Key != "") && (model.Available == nil || *model.Available) && !model.Missing {
			return true
		}
	}
	return false
}

func openClawPluginVersionMatches(output, expected string) bool {
	inspection, err := decodeOpenClawPluginInspection(output)
	if err != nil {
		return false
	}
	return inspection.ID == "or3-runs" && inspection.Version == strings.TrimSpace(expected) && inspection.sourceIsNPM()
}

type openClawPluginInspection struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
	Package string `json:"package"`
}

func decodeOpenClawPluginInspection(output string) (openClawPluginInspection, error) {
	var payload struct {
		Plugin openClawPluginInspection `json:"plugin"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return openClawPluginInspection{}, err
	}
	payload.Plugin.ID = strings.TrimSpace(payload.Plugin.ID)
	payload.Plugin.Version = strings.TrimSpace(payload.Plugin.Version)
	payload.Plugin.Source = strings.TrimSpace(payload.Plugin.Source)
	payload.Plugin.Package = strings.TrimSpace(payload.Plugin.Package)
	if payload.Plugin.ID == "" || payload.Plugin.Version == "" {
		return openClawPluginInspection{}, errors.New("plugin inspection is missing plugin.id or plugin.version")
	}
	return payload.Plugin, nil
}

func (inspection openClawPluginInspection) sourceIsNPM() bool {
	return strings.EqualFold(inspection.Source, "npm") ||
		strings.HasPrefix(strings.ToLower(inspection.Source), "npm:") ||
		strings.HasPrefix(inspection.Package, "@or3/openclaw@")
}

func openClawPluginReady(output, expected string) bool {
	inspection, err := decodeOpenClawPluginInspection(output)
	return err == nil && inspection.ID == "or3-runs" && inspection.Version == strings.TrimSpace(expected) && inspection.Enabled && inspection.sourceIsNPM()
}

func hasHermesModel(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), "model:") {
			continue
		}
		model := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "Model:"), "model:"))
		return model != "" && !strings.Contains(strings.ToLower(model), "not set")
	}
	return false
}

func openClawVersionCompatible(version string) bool {
	match := regexp.MustCompile(`(?:^|[^0-9])([0-9]{4})\.([0-9]+)\.([0-9]+)([-+][0-9A-Za-z.-]+)?`).FindStringSubmatch(version)
	if len(match) == 0 {
		return false
	}
	year, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	if year != 2026 || minor != 7 || patch < 1 {
		return false
	}
	if patch > 1 || match[4] == "" || strings.HasPrefix(match[4], "+") {
		return true
	}
	// The published plugin's peer range begins at 2026.7.1-2. Treat
	// prereleases before that point as incompatible while accepting later
	// numbered prereleases on the same patch line.
	firstPrerelease := strings.Split(strings.TrimPrefix(match[4], "-"), ".")[0]
	prerelease, err := strconv.Atoi(firstPrerelease)
	return err == nil && prerelease >= 2
}

func runRuntime(binary string, args ...string) (string, error) {
	return runRuntimeContext(context.Background(), binary, args...)
}

func runRuntimeContext(ctx context.Context, binary string, args ...string) (string, error) {
	timeout := runtimeCommandTimeout
	if len(args) >= 2 && args[0] == "plugins" && args[1] == "install" {
		timeout = runtimeInstallTimeout
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(binary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-commandCtx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		message := strings.TrimSpace(output.String())
		if message == "" {
			message = commandCtx.Err().Error()
		}
		return message, fmt.Errorf("runtime command %q timed out after %s: %w", strings.Join(append([]string{binary}, args...), " "), timeout, commandCtx.Err())
	}
	if err != nil {
		message := strings.TrimSpace(output.String())
		if message == "" {
			message = err.Error()
		}
		return message, errors.New(strings.TrimSpace(strings.ReplaceAll(message, "\n", " ")))
	}
	return output.String(), nil
}

func confirmRuntimeAction(stdout io.Writer, action string) bool {
	if runtimeConsentApproved(runtimeConsentForAction(action)) {
		return true
	}
	fmt.Fprintf(stdout, "%s [y/N] ", action)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return err == nil && strings.EqualFold(strings.TrimSpace(line), "y")
}

type runtimeConsent string

const (
	runtimeConsentSetup         runtimeConsent = "setup"
	runtimeConsentInstaller     runtimeConsent = "installer"
	runtimeConsentGateway       runtimeConsent = "gateway"
	runtimeConsentPluginInstall runtimeConsent = "plugin-install"
	runtimeConsentSourcePatch   runtimeConsent = "source-patch"
)

func runtimeConsentForAction(action string) runtimeConsent {
	switch {
	case strings.Contains(action, "Continue with this setup"):
		return runtimeConsentSetup
	case strings.Contains(action, "official installer"):
		return runtimeConsentInstaller
	case strings.Contains(action, "Gateway is not ready"):
		return runtimeConsentGateway
	case strings.Contains(action, "pinned OR3 Runs plugin"):
		return runtimeConsentPluginInstall
	case strings.Contains(action, "Hermes SSE CORS compatibility patch"):
		return runtimeConsentSourcePatch
	default:
		return ""
	}
}

func runtimeConsentApproved(required runtimeConsent) bool {
	if required == "" {
		return false
	}
	for _, value := range strings.Split(os.Getenv("OR3_CONNECT_APPROVE"), ",") {
		if runtimeConsent(strings.TrimSpace(strings.ToLower(value))) == required {
			return true
		}
	}
	return false
}

func runtimeConfirm(stdout io.Writer, action string, confirm func(string) (bool, error)) bool {
	if confirm != nil {
		confirmed, err := confirm(action)
		return err == nil && confirmed
	}
	return confirmRuntimeAction(stdout, action)
}

func normalizedCloudOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("OR3 Cloud URL must be an origin for runtime CORS")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", errors.New("OR3 Cloud URL must use HTTPS (or loopback HTTP) for runtime CORS")
	}
	if scheme == "http" {
		hostname := strings.ToLower(parsed.Hostname())
		if hostname != "localhost" && hostname != "127.0.0.1" && hostname != "::1" {
			return "", errors.New("OR3 Cloud URL must use HTTPS except for loopback development URLs")
		}
	}
	return strings.TrimRight(scheme+"://"+parsed.Host, "/"), nil
}

func firstLine(value string) string {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func runtimeDisplayName(runtimeName string) string {
	if name := strings.TrimSpace(os.Getenv("OR3_CONNECT_NAME")); name != "" {
		return name
	}
	host, _ := os.Hostname()
	if host == "" {
		return runtimeName
	}
	return runtimeName + " (" + host + ")"
}

func hermesPort() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HERMES_API_SERVER_PORT"))); err == nil && value >= 1 && value <= 65535 {
		return value
	}
	return hermesDefaultPort
}

func hermesPortForRuntime(bin string) int {
	return hermesPortForRuntimeContext(context.Background(), bin)
}

func hermesPortForRuntimeContext(ctx context.Context, bin string) int {
	if raw := strings.TrimSpace(os.Getenv("HERMES_API_SERVER_PORT")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 1 && value <= 65535 {
			return value
		}
	}
	if output, err := runRuntimeContext(ctx, bin, "config", "get", "platforms.api_server.port"); err == nil {
		for _, line := range strings.Split(output, "\n") {
			if value, parseErr := strconv.Atoi(strings.TrimSpace(line)); parseErr == nil && value >= 1 && value <= 65535 {
				return value
			}
		}
	}
	return hermesDefaultPort
}

func hermesEnvPath(bin string) string {
	if output, err := runRuntime(bin, "config", "env-path"); err == nil {
		if path := firstAbsolutePath(output, ".env"); path != "" {
			return path
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes", ".env")
}

func mergeHermesEnv(path, origin, token string) error {
	return mergeHermesEnvAtPort(path, origin, token, hermesPort())
}

func mergeHermesEnvAtPort(path, origin, token string, port int) error {
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read Hermes environment: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	corsOrigins, err := mergeHermesCorsOrigins("", origin)
	if err != nil {
		return err
	}
	values := map[string]string{
		"API_SERVER_ENABLED":      "true",
		"API_SERVER_HOST":         "127.0.0.1",
		"API_SERVER_PORT":         strconv.Itoa(port),
		"API_SERVER_CORS_ORIGINS": corsOrigins,
	}
	if token != "" {
		values["API_SERVER_KEY"] = token
	}
	seen := map[string]bool{}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if value, ok := values[key]; ok {
			if key == "API_SERVER_CORS_ORIGINS" {
				value, err = mergeHermesCorsOrigins(parts[1], origin)
				if err != nil {
					return err
				}
			}
			lines[index] = key + "=" + value
			seen[key] = true
		}
	}
	for _, key := range []string{"API_SERVER_ENABLED", "API_SERVER_HOST", "API_SERVER_PORT", "API_SERVER_CORS_ORIGINS", "API_SERVER_KEY"} {
		if value, ok := values[key]; ok && !seen[key] {
			lines = append(lines, key+"="+value)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".or3-hermes-env-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strings.Join(lines, "\n")); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Hermes is exposed to the browser only for the requested OR3 Cloud origin.
// The caller persists a rollback snapshot before replacing the allowlist.
func mergeHermesCorsOrigins(existing, required string) (string, error) {
	_ = existing
	required = strings.Trim(strings.TrimSpace(required), "\"'")
	if required == "" {
		return "", errors.New("Hermes CORS requires an explicit OR3 Cloud origin")
	}
	return required, nil
}

type runtimeFileSnapshot struct {
	path   string
	exists bool
	mode   os.FileMode
	body   []byte
}

func captureRuntimeFile(path string) (runtimeFileSnapshot, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return runtimeFileSnapshot{path: path}, nil
	}
	if err != nil {
		return runtimeFileSnapshot{}, err
	}
	if info.IsDir() {
		return runtimeFileSnapshot{}, fmt.Errorf("runtime configuration path %q is a directory", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return runtimeFileSnapshot{}, err
	}
	return runtimeFileSnapshot{path: path, exists: true, mode: info.Mode().Perm(), body: body}, nil
}

func (s runtimeFileSnapshot) Restore() error {
	if !s.exists {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return atomicReplaceRuntimeFile(s.path, s.body, s.mode)
}

const runtimeConfigBackupVersion = 1

type persistedRuntimeConfigBackup struct {
	Version         int    `json:"version"`
	Runtime         string `json:"runtime"`
	RuntimeBinary   string `json:"runtimeBinary"`
	ConfigPath      string `json:"configPath"`
	ConfigExisted   bool   `json:"configExisted"`
	ConfigMode      uint32 `json:"configMode"`
	ConfigBody      []byte `json:"configBody"`
	SourcePatchPath string `json:"sourcePatchPath,omitempty"`
	SourcePatchMode uint32 `json:"sourcePatchMode,omitempty"`
	SourcePatchBody []byte `json:"sourcePatchBody,omitempty"`
}

func runtimeSnapshotForPreparation(stateDir, runtimeName, runtimeBinary, configPath string) (runtimeFileSnapshot, error) {
	if strings.TrimSpace(stateDir) == "" {
		return captureRuntimeFile(configPath)
	}
	backupPath := remoteconnect.RuntimeConfigBackupPath(stateDir)
	body, err := os.ReadFile(backupPath)
	if err == nil {
		var backup persistedRuntimeConfigBackup
		if err := json.Unmarshal(body, &backup); err != nil {
			return runtimeFileSnapshot{}, fmt.Errorf("read runtime configuration backup: %w", err)
		}
		if backup.Version != runtimeConfigBackupVersion || backup.Runtime != runtimeName || backup.ConfigPath != configPath {
			return runtimeFileSnapshot{}, errors.New("saved runtime configuration backup does not match this connection; run `npx @or3/connect disconnect` to restore it before connecting a runtime")
		}
		return runtimeFileSnapshot{
			path:   backup.ConfigPath,
			exists: backup.ConfigExisted,
			mode:   os.FileMode(backup.ConfigMode),
			body:   append([]byte(nil), backup.ConfigBody...),
		}, nil
	}
	if !os.IsNotExist(err) {
		return runtimeFileSnapshot{}, fmt.Errorf("read runtime configuration backup: %w", err)
	}
	snapshot, err := captureRuntimeFile(configPath)
	if err != nil {
		return runtimeFileSnapshot{}, err
	}
	backup := persistedRuntimeConfigBackup{
		Version:       runtimeConfigBackupVersion,
		Runtime:       runtimeName,
		RuntimeBinary: runtimeBinary,
		ConfigPath:    snapshot.path,
		ConfigExisted: snapshot.exists,
		ConfigMode:    uint32(snapshot.mode),
		ConfigBody:    append([]byte(nil), snapshot.body...),
	}
	body, err = json.Marshal(backup)
	if err != nil {
		return runtimeFileSnapshot{}, fmt.Errorf("encode runtime configuration backup: %w", err)
	}
	if err := atomicReplaceRuntimeFile(backupPath, append(body, '\n'), 0o600); err != nil {
		return runtimeFileSnapshot{}, fmt.Errorf("save runtime configuration backup: %w", err)
	}
	return snapshot, nil
}

func removeRuntimeConfigBackup(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return nil
	}
	err := os.Remove(remoteconnect.RuntimeConfigBackupPath(stateDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func recordHermesSourcePatchBackup(stateDir, sourcePath string) error {
	if strings.TrimSpace(stateDir) == "" {
		return nil
	}
	body, err := os.ReadFile(remoteconnect.RuntimeConfigBackupPath(stateDir))
	if err != nil {
		return fmt.Errorf("read runtime configuration backup before source patch: %w", err)
	}
	var backup persistedRuntimeConfigBackup
	if err := json.Unmarshal(body, &backup); err != nil {
		return fmt.Errorf("read runtime configuration backup before source patch: %w", err)
	}
	if backup.Version != runtimeConfigBackupVersion || backup.Runtime != "hermes" {
		return errors.New("runtime configuration backup does not belong to Hermes")
	}
	if backup.SourcePatchPath != "" {
		if backup.SourcePatchPath != sourcePath {
			return errors.New("runtime configuration backup already tracks a different Hermes source patch")
		}
		return nil
	}
	snapshot, err := captureRuntimeFile(sourcePath)
	if err != nil {
		return fmt.Errorf("snapshot Hermes API server source: %w", err)
	}
	if !snapshot.exists {
		return errors.New("Hermes API server source is missing")
	}
	backup.SourcePatchPath = snapshot.path
	backup.SourcePatchMode = uint32(snapshot.mode)
	backup.SourcePatchBody = append([]byte(nil), snapshot.body...)
	body, err = json.Marshal(backup)
	if err != nil {
		return fmt.Errorf("encode runtime configuration backup after source patch: %w", err)
	}
	if err := atomicReplaceRuntimeFile(remoteconnect.RuntimeConfigBackupPath(stateDir), append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("save runtime configuration backup after source patch: %w", err)
	}
	return nil
}

func restoreRuntimeConfiguration(ctx context.Context, runtimeName, runtimeBinary string, snapshot runtimeFileSnapshot) error {
	if err := snapshot.Restore(); err != nil {
		return fmt.Errorf("restore %s runtime configuration: %w", runtimeName, err)
	}
	if strings.TrimSpace(runtimeBinary) == "" {
		return fmt.Errorf("restart %s after configuration restore: runtime binary is missing", runtimeName)
	}
	restartCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if _, err := runRuntimeContext(restartCtx, runtimeBinary, "gateway", "restart"); err == nil {
		return nil
	} else if runtimeName != "hermes" {
		return fmt.Errorf("restart %s after configuration restore: %w", runtimeName, err)
	}
	if _, err := runRuntimeContext(restartCtx, runtimeBinary, "gateway", "start"); err != nil {
		return fmt.Errorf("restart Hermes after configuration restore: %w", err)
	}
	return nil
}

func restoreExternalRuntimeConfiguration(ctx context.Context, stateDir string, state remoteconnect.State) error {
	body, err := os.ReadFile(remoteconnect.RuntimeConfigBackupPath(stateDir))
	if os.IsNotExist(err) {
		return errors.New("runtime configuration backup is missing; state was preserved to avoid leaving the runtime credential installed")
	}
	if err != nil {
		return fmt.Errorf("read runtime configuration backup: %w", err)
	}
	var backup persistedRuntimeConfigBackup
	if err := json.Unmarshal(body, &backup); err != nil {
		return fmt.Errorf("read runtime configuration backup: %w", err)
	}
	if backup.Version != runtimeConfigBackupVersion || backup.Runtime != state.Runtime || strings.TrimSpace(backup.RuntimeBinary) == "" || strings.TrimSpace(backup.ConfigPath) == "" {
		return errors.New("runtime configuration backup is invalid; state was preserved to avoid leaving the runtime credential installed")
	}
	return restorePersistedRuntimeConfigBackup(ctx, backup)
}

func restoreOrphanedExternalRuntimeConfiguration(ctx context.Context, stateDir string) (bool, error) {
	body, err := os.ReadFile(remoteconnect.RuntimeConfigBackupPath(stateDir))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read runtime configuration backup: %w", err)
	}
	var backup persistedRuntimeConfigBackup
	if err := json.Unmarshal(body, &backup); err != nil {
		return false, fmt.Errorf("read runtime configuration backup: %w", err)
	}
	if backup.Version != runtimeConfigBackupVersion || strings.TrimSpace(backup.Runtime) == "" || strings.TrimSpace(backup.RuntimeBinary) == "" || strings.TrimSpace(backup.ConfigPath) == "" {
		return false, errors.New("runtime configuration backup is invalid; it was preserved to avoid leaving the runtime credential installed")
	}
	return true, restorePersistedRuntimeConfigBackup(ctx, backup)
}

func restorePersistedRuntimeConfigBackup(ctx context.Context, backup persistedRuntimeConfigBackup) error {
	if backup.SourcePatchPath != "" {
		if err := atomicReplaceRuntimeFile(backup.SourcePatchPath, backup.SourcePatchBody, os.FileMode(backup.SourcePatchMode)); err != nil {
			return fmt.Errorf("restore Hermes SSE compatibility source: %w", err)
		}
	}
	return restoreRuntimeConfiguration(ctx, backup.Runtime, backup.RuntimeBinary, runtimeFileSnapshot{
		path:   backup.ConfigPath,
		exists: backup.ConfigExisted,
		mode:   os.FileMode(backup.ConfigMode),
		body:   append([]byte(nil), backup.ConfigBody...),
	})
}

func rollbackExternalRuntimePreparation(ctx context.Context, stateDir string, plan externalRuntimePlan) error {
	if plan.rollback == nil {
		return errors.Join(
			removeRuntimeConfigBackup(stateDir),
			removeRuntimePreparation(stateDir),
		)
	}
	if err := plan.rollback(ctx); err != nil {
		return err
	}
	return errors.Join(
		removeRuntimeConfigBackup(stateDir),
		removeRuntimePreparation(stateDir),
	)
}

func externalRuntimeSetupErrorWithCleanup(cause, cleanupErr error) error {
	if cleanupErr == nil {
		return cause
	}
	return fmt.Errorf("%w; automatic cleanup is incomplete: %v; the saved connection was preserved so run `npx @or3/connect disconnect` to retry", cause, cleanupErr)
}

func cleanupExternalRuntimeEnrollment(ctx context.Context, stateDir string, state remoteconnect.State, plan externalRuntimePlan) error {
	var cleanupErrs []error
	state.Stage = connectSetupStageCleanupPending
	if err := remoteconnect.UpdateState(stateDir, state); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("record cleanup checkpoint: %w", err))
	}
	if state.Installed {
		if err := remoteconnect.UninstallService(); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("stop background service: %w", err))
		} else {
			state.Installed = false
			if err := remoteconnect.UpdateState(stateDir, state); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("record stopped background service: %w", err))
			}
		}
	}
	if !state.RuntimeConfigRestored {
		var err error
		if strings.TrimSpace(stateDir) != "" {
			err = restoreExternalRuntimeConfiguration(ctx, stateDir, state)
		} else if plan.rollback != nil {
			err = plan.rollback(ctx)
		} else {
			err = errors.New("runtime configuration backup is missing; state was preserved to avoid leaving the runtime credential installed")
		}
		if err != nil {
			cleanupErrs = append(cleanupErrs, err)
		} else {
			state.RuntimeConfigRestored = true
			if err := remoteconnect.UpdateState(stateDir, state); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("record restored runtime configuration: %w", err))
			}
		}
	}
	if !state.CloudRevoked {
		if err := remoteconnect.NewClient(state.CloudURL).Revoke(ctx, state); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("revoke cloud access: %w", err))
		} else {
			state.CloudRevoked = true
			if err := remoteconnect.UpdateState(stateDir, state); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("record revoked cloud access: %w", err))
			}
		}
	}
	if len(cleanupErrs) > 0 {
		return errors.Join(cleanupErrs...)
	}
	if err := removeRuntimePreparation(stateDir); err != nil {
		return fmt.Errorf("remove runtime preparation checkpoint: %w", err)
	}
	if err := remoteconnect.RemoveState(stateDir); err != nil {
		return fmt.Errorf("remove saved connection: %w", err)
	}
	return nil
}

func atomicReplaceRuntimeFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".or3-runtime-restore-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func openClawConfigPath(bin string) string {
	if output, err := runRuntime(bin, "config", "file"); err == nil {
		if path := firstAbsolutePath(output, ".json"); path != "" {
			return path
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".openclaw", "openclaw.json")
}

func firstAbsolutePath(output, suffix string) string {
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		candidate := strings.TrimSpace(line)
		if index := strings.LastIndex(candidate, ":"); index >= 0 {
			candidate = strings.TrimSpace(candidate[index+1:])
		}
		candidate = strings.Trim(candidate, "`\"'")
		if filepath.IsAbs(candidate) && (suffix == "" || strings.HasSuffix(candidate, suffix)) {
			return candidate
		}
	}
	return ""
}

func openClawPort(path string) int {
	value, err := readJSONFile(path)
	if err == nil {
		if gateway, ok := value["gateway"].(map[string]any); ok {
			switch port := gateway["port"].(type) {
			case float64:
				if int(port) >= 1 && int(port) <= 65535 {
					return int(port)
				}
			case string:
				if parsed, parseErr := strconv.Atoi(strings.TrimSpace(port)); parseErr == nil && parsed >= 1 && parsed <= 65535 {
					return parsed
				}
			}
		}
	}
	return openClawDefaultPort
}

func openClawBind(path string) string {
	value, err := readJSONFile(path)
	if err != nil {
		return ""
	}
	if gateway, ok := value["gateway"].(map[string]any); ok {
		if bind, ok := gateway["bind"].(string); ok {
			return strings.TrimSpace(bind)
		}
	}
	return ""
}

func isLoopbackBind(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "loopback", "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func updateOpenClawConfig(path, origin, token string) error {
	config, err := readJSONFile(path)
	if os.IsNotExist(err) {
		config = map[string]any{}
	} else if err != nil {
		return fmt.Errorf("read OpenClaw config: %w", err)
	}
	if config == nil {
		return errors.New("OpenClaw config root must be a JSON object")
	}
	plugins, err := ensureMap(config, "plugins", "plugins")
	if err != nil {
		return err
	}
	allow, err := ensureStringSlice(plugins, "allow", "plugins.allow")
	if err != nil {
		return err
	}
	if !containsAnyString(allow, "or3-runs") {
		plugins["allow"] = append(allow, "or3-runs")
	}
	entries, err := ensureMap(plugins, "entries", "plugins.entries")
	if err != nil {
		return err
	}
	entry, err := ensureMap(entries, "or3-runs", "plugins.entries.or3-runs")
	if err != nil {
		return err
	}
	pluginConfig, err := ensureMap(entry, "config", "plugins.entries.or3-runs.config")
	if err != nil {
		return err
	}
	origins, err := ensureStringSlice(pluginConfig, "allowedOrigins", "plugins.entries.or3-runs.config.allowedOrigins")
	if err != nil {
		return err
	}
	if containsAnyString(origins, "*") {
		return errors.New("OpenClaw CORS cannot use wildcard origins; replace plugins.entries.or3-runs.config.allowedOrigins with explicit origins")
	}
	if !containsAnyString(origins, origin) {
		origins = append(origins, origin)
	}
	pluginConfig["allowedOrigins"] = origins
	if token != "" {
		pluginConfig["token"] = token
	}
	if gateway, ok := config["gateway"].(map[string]any); ok {
		if auth, ok := gateway["auth"].(map[string]any); ok {
			if gatewayToken, ok := auth["token"].(string); ok && strings.TrimSpace(gatewayToken) != "" {
				pluginConfig["gatewayToken"] = gatewayToken
			}
		}
	}
	return writeJSONFile(path, config)
}

func readJSONFile(path string) (map[string]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return value, nil
}

func writeJSONFile(path string, value map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".or3-openclaw-config-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func ensureMap(parent map[string]any, key, path string) (map[string]any, error) {
	existing, present := parent[key]
	if !present || existing == nil {
		value := map[string]any{}
		parent[key] = value
		return value, nil
	}
	value, ok := existing.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("OpenClaw config %s must be an object", path)
	}
	return value, nil
}

func ensureStringSlice(parent map[string]any, key, path string) ([]any, error) {
	existing, present := parent[key]
	if !present || existing == nil {
		return nil, nil
	}
	values, ok := existing.([]any)
	if !ok {
		return nil, fmt.Errorf("OpenClaw config %s must be an array", path)
	}
	for _, value := range values {
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("OpenClaw config %s must contain only strings", path)
		}
	}
	return values, nil
}

func containsAnyString(values []any, target string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.EqualFold(strings.TrimSpace(text), target) {
			return true
		}
	}
	return false
}
