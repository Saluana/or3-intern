package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"or3-intern/internal/config"
	remoteconnect "or3-intern/internal/connect"
)

type connectCommandOptions struct {
	CloudURL  string
	StateDir  string
	Name      string
	NoService bool
	NoBrowser bool
	Timeout   time.Duration
}

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
		if err := remoteconnect.UninstallService(); err != nil {
			return fmt.Errorf("remove background service: %w", err)
		}
		if err := remoteconnect.RemoveState(options.StateDir); err != nil {
			return fmt.Errorf("remove saved connection: %w", err)
		}
		fmt.Fprintln(stdout, "OR3 remote access was removed from this computer.")
		return nil
	case "run":
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
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("OR3 remote access currently supports macOS and Linux")
	}
	if existing, err := remoteconnect.LoadState(options.StateDir); err == nil {
		fmt.Fprintf(stdout, "This computer is already connected as %s.\n", existing.EnvironmentName)
		fmt.Fprintln(stdout, "Run `or3-intern connect status` for details or `or3-intern connect disconnect` to replace it.")
		return nil
	}
	cloudflaredPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return fmt.Errorf("cloudflared is required but was not found; install it and run this command again")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load OR3 settings: %w", err)
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
		credential.ControlToken = randomSecret()
	}
	cfg.Service.Enabled = true
	cfg.Service.Listen = "127.0.0.1:9100"
	cfg.Service.Secret = credential.ControlToken
	if cloudOrigin, err := url.Parse(options.CloudURL); err == nil && cloudOrigin.Scheme != "" && cloudOrigin.Host != "" {
		origin := cloudOrigin.Scheme + "://" + cloudOrigin.Host
		if !containsString(cfg.Service.TrustedBrowserOrigins, origin) {
			cfg.Service.TrustedBrowserOrigins = append(cfg.Service.TrustedBrowserOrigins, origin)
		}
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Errorf("save OR3 service settings: %w", err)
	}
	state := remoteconnect.State{
		CloudURL:        options.CloudURL,
		AccountID:       credential.AccountID,
		EnvironmentID:   credential.EnvironmentID,
		EnvironmentName: credential.EnvironmentName,
		Hostname:        credential.Tunnel.Hostname,
		ControlToken:    credential.ControlToken,
		CloudflaredPath: cloudflaredPath,
		ConfigPath:      cfgPath,
		Installed:       false,
		ConnectedAt:     time.Now().UTC(),
	}
	if err := remoteconnect.SaveState(options.StateDir, state, credential.Tunnel.Token); err != nil {
		return err
	}

	if options.NoService {
		fmt.Fprintf(stdout, "\nConnected as %s. Keep this terminal open.\n", credential.EnvironmentName)
		return runRemoteConnectionService(ctx, cfgPath, options.StateDir, stdout, stderr)
	}
	spec, err := remoteconnect.CurrentServiceSpec(cfgPath, options.StateDir)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "\nOne administrator approval installs the background service.")
	if err := remoteconnect.InstallService(spec); err != nil {
		return fmt.Errorf("install background service: %w", err)
	}
	state.Installed = true
	if err := remoteconnect.UpdateState(options.StateDir, state); err != nil {
		return fmt.Errorf("record background service installation: %w", err)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Connected as %s\n", credential.EnvironmentName)
	fmt.Fprintln(stdout, "OR3 will stay reachable after you log out or restart.")
	return nil
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
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return remoteconnect.DeviceCredential{}, fmt.Errorf("sign-in timed out; run `or3-intern connect` to try again")
			}
			return remoteconnect.DeviceCredential{}, ctx.Err()
		case <-timer.C:
			result, err := client.Poll(ctx, authorization.DeviceCode, host)
			if err != nil {
				return remoteconnect.DeviceCredential{}, err
			}
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
				return remoteconnect.DeviceCredential{}, fmt.Errorf("the sign-in link expired; run `or3-intern connect` to try again")
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

func runRemoteConnectionService(ctx context.Context, cfgPath, stateDir string, stdout, stderr io.Writer) error {
	state, err := remoteconnect.LoadState(stateDir)
	if err != nil {
		return fmt.Errorf("load saved remote connection: %w", err)
	}
	if _, err := os.Stat(state.TunnelTokenFile); err != nil {
		return fmt.Errorf("load tunnel credential: %w", err)
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
	tunnel := exec.CommandContext(ctx, cloudflaredPath, "tunnel", "run", "--token-file", state.TunnelTokenFile)
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
		fmt.Fprintln(stdout, "Run `or3-intern connect` to connect this computer to OR3 Cloud.")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Computer: %s\n", state.EnvironmentName)
	fmt.Fprintf(stdout, "Address:  %s\n", state.Hostname)
	fmt.Fprintf(stdout, "Mode:     %s\n", map[bool]string{true: "background service", false: "terminal only"}[state.Installed])
	fmt.Fprintf(stdout, "Cloud:    %s\n", state.CloudURL)
	return nil
}

func doctorRemoteConnection(ctx context.Context, stateDir string, stdout io.Writer) error {
	state, err := remoteconnect.LoadState(stateDir)
	if err != nil {
		return fmt.Errorf("saved connection: %w", err)
	}
	fmt.Fprintln(stdout, "Saved connection: ready")
	cloudflaredPath := strings.TrimSpace(state.CloudflaredPath)
	if cloudflaredPath == "" {
		cloudflaredPath = "cloudflared"
	}
	if _, err := exec.LookPath(cloudflaredPath); err != nil {
		return fmt.Errorf("cloudflared: not installed")
	}
	fmt.Fprintln(stdout, "Tunnel client: ready")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+state.Hostname+"/internal/v1/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+state.ControlToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote reachability: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote reachability: HTTP %d", resp.StatusCode)
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
	if err := remoteconnect.RemoveState(options.StateDir); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Remote access is disconnected. Local OR3 remains unchanged.")
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

func randomSecret() string {
	body := make([]byte, 32)
	if _, err := rand.Read(body); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(body)
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
