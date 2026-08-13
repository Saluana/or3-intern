package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"or3-intern/internal/requestctx"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/channels/discord"
	"or3-intern/internal/channels/email"
	"or3-intern/internal/channels/slack"
	"or3-intern/internal/channels/telegram"
	"or3-intern/internal/channels/whatsapp"
	"or3-intern/internal/config"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/cron"
	"or3-intern/internal/cronrunner"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/runners"
	"or3-intern/internal/security"
	"or3-intern/internal/serviceerrors"
	"or3-intern/internal/streaming"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

const (
	schedulerMaxConsolidationPasses = 3
	gracefulShutdownTimeout         = 5 * time.Second
	consolidationTimeoutBuffer      = 5 * time.Second
)

func effectiveConsolidationTimeout(cfg config.Config) time.Duration {
	role := cfg.ModelRole(config.ModelRoleSummarization)
	timeoutSeconds := providerTimeoutSeconds(cfg, role.Primary.Provider, cfg.Provider.TimeoutSeconds)
	asyncTimeout := time.Duration(cfg.ConsolidationAsyncTimeoutSeconds) * time.Second
	if asyncTimeout <= 0 {
		asyncTimeout = 30 * time.Second
	}
	providerTimeout := time.Duration(timeoutSeconds) * time.Second
	if providerTimeout <= 0 {
		providerTimeout = 60 * time.Second
	}
	minimum := providerTimeout + consolidationTimeoutBuffer
	if asyncTimeout < minimum {
		return minimum
	}
	return asyncTimeout
}

func currentEmbedFingerprint(cfg config.Config) string {
	role := cfg.ModelRole(config.ModelRoleEmbeddings)
	profile, ok := cfg.ProviderProfile(role.Primary.Provider)
	if !ok {
		return providers.EmbeddingFingerprint(cfg.Provider.APIBase, cfg.Provider.EmbedModel, cfg.Provider.EmbedDimensions)
	}
	dimensions := role.EmbedDimensions
	if dimensions <= 0 {
		dimensions = profile.DefaultDimensions
	}
	return providers.EmbeddingFingerprint(profile.APIBase, role.Primary.Model, dimensions)
}

func effectiveConsolidationModel(cfg config.Config) string {
	if model := strings.TrimSpace(cfg.ConsolidationModel); model != "" {
		return model
	}
	roleModel := strings.TrimSpace(cfg.ModelRole(config.ModelRoleSummarization).Primary.Model)
	defaultModel := config.Default().ModelRouting.Summarization.Primary.Model
	if roleModel != "" && roleModel != defaultModel {
		return roleModel
	}
	return cfg.Provider.Model
}

func newProviderClient(cfg config.Config) *providers.Client {
	return newRoleProviderClient(cfg, config.ModelRoleChat)
}

func newConsolidationProviderClient(cfg config.Config) *providers.Client {
	return newRoleProviderClientWithTimeout(cfg, config.ModelRoleSummarization, effectiveConsolidationTimeout(cfg))
}

func newEmbeddingProviderClient(cfg config.Config) *providers.Client {
	return newRoleProviderClient(cfg, config.ModelRoleEmbeddings)
}

func newRoleProviderClient(cfg config.Config, roleName string) *providers.Client {
	role := cfg.ModelRole(roleName)
	return newRoleProviderClientWithTimeout(cfg, roleName, time.Duration(providerTimeoutSeconds(cfg, role.Primary.Provider, cfg.Provider.TimeoutSeconds))*time.Second)
}

func newRoleProviderClientWithTimeout(cfg config.Config, roleName string, timeout time.Duration) *providers.Client {
	role := cfg.ModelRole(roleName)
	prov := newModelRefClient(cfg, role.Primary, timeout)
	if prov == nil {
		return nil
	}
	if roleName == config.ModelRoleEmbeddings && role.EmbedDimensions > 0 {
		prov.EmbedDimensions = role.EmbedDimensions
	}
	for _, fallback := range append(role.Fallbacks, cfg.ModelRole(config.ModelRoleFallback).Fallbacks...) {
		fallbackClient := newModelRefClient(cfg, fallback, timeout)
		if fallbackClient == nil {
			continue
		}
		if roleName == config.ModelRoleEmbeddings && role.EmbedDimensions > 0 {
			fallbackClient.EmbedDimensions = role.EmbedDimensions
		}
		prov.Fallbacks = append(prov.Fallbacks, providers.Fallback{Client: fallbackClient, Model: fallback.Model})
	}
	return prov
}

func newModelRefClient(cfg config.Config, ref config.ModelRef, timeout time.Duration) *providers.Client {
	profile, ok := cfg.ProviderProfile(ref.Provider)
	if !ok || strings.TrimSpace(profile.APIBase) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = time.Duration(profile.TimeoutSeconds) * time.Second
	}
	prov := providers.New(strings.TrimRight(profile.APIBase, "/"), profile.APIKey, timeout)
	prov.EmbedDimensions = profile.DefaultDimensions
	prov.HostPolicy = buildHostPolicy(cfg)
	return prov
}

func providerTimeoutSeconds(cfg config.Config, provider string, fallback int) int {
	if profile, ok := cfg.ProviderProfile(provider); ok && profile.TimeoutSeconds > 0 {
		return profile.TimeoutSeconds
	}
	if fallback > 0 {
		return fallback
	}
	return 60
}

func roleTemperatureOrDefault(role config.ModelRoleConfig, fallback float64) float64 {
	if role.Temperature != nil {
		return *role.Temperature
	}
	return fallback
}

func roleProviderVision(cfg config.Config, provider string) bool {
	profile, ok := cfg.ProviderProfile(provider)
	return ok && profile.EnableVision
}

func main() {
	os.Exit(run())
}

func run() int {
	cfgPath, args, showHelp, unsafeDev, err := parseRootCLIArgs(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if showHelp {
		if err := printHelpTopic(os.Stdout, helpTopicPath(args)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}
	if handled, err := maybeHandleHelpRequest(args, os.Stdout); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}

	cmd := "chat"
	if len(args) > 0 {
		cmd = args[0]
	}
	if isHelpToken(cmd) {
		printRootHelp(os.Stdout)
		return 0
	}
	if !isKnownRootCommand(cmd) {
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		return 2
	}
	if commandHandledBeforeConfigLoad(cmd) {
		switch cmd {
		case "config-path":
			if err := requireExactArgs(args[1:], 0, "or3-intern config-path"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			fmt.Fprintln(os.Stdout, cfgPathOrDefault(cfgPath))
		case "version":
			if err := requireExactArgs(args[1:], 0, "or3-intern version"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			fmt.Println(buildVersionString())
		case "configure":
			if err := runConfigure(cfgPath, args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "configure error:", err)
				return 1
			}
		case "init":
			if err := requireExactArgs(args[1:], 0, "or3-intern init"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			if err := runInit(cfgPath); err != nil {
				fmt.Fprintln(os.Stderr, "init error:", err)
				return 1
			}
		case "settings":
			if err := runSettings(cfgPath, args[1:]); err != nil {
				if translated := translateAndPrintError(err, os.Stderr); translated != nil {
					fmt.Fprintln(os.Stderr, "settings error:", err)
				}
				return 1
			}
		case "setup":
			if err := requireExactArgs(args[1:], 0, "or3-intern setup"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			result, err := runSetup(cfgPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, "setup error:", err)
				return 1
			}
			if !result.StartChat {
				return 0
			}
			cmd = "chat"
			args = []string{"chat"}
		case "connect":
			ctx, cancel := connectSignalContext()
			defer cancel()
			if err := runConnectCommand(ctx, cfgPathOrDefault(cfgPath), args[1:], os.Stdout, os.Stderr); err != nil {
				if isUsageError(err) {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				fmt.Fprintln(os.Stderr, "connect error:", err)
				return 1
			}
		}
		if cmd != "chat" {
			return 0
		}
	}
	if setupRan, err := maybeRunFirstRunSetup(cfgPath, cmd, args); err != nil {
		fmt.Fprintln(os.Stderr, "setup error:", err)
		return 1
	} else if setupRan {
		if cmd == "chat" {
			return 0
		}
	}
	if cmd == "doctor" || cmd == "health" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = ""
		}
		cfg, validationError, loadErr := loadDoctorConfig(cfgPathOrDefault(cfgPath), cwd)
		if loadErr != nil {
			fmt.Fprintln(os.Stderr, cmd+" error:", loadErr)
			return 1
		}
		if cmd == "health" {
			if err := runHealthCommand(cfgPathOrDefault(cfgPath), cfg, validationError, args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "health error:", err)
				return 1
			}
		} else {
			if err := runDoctorCommand(cfgPathOrDefault(cfgPath), cfg, validationError, args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "doctor error:", err)
				return 1
			}
		}
		return 0
	}
	if cmd == "status" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = ""
		}
		cfg, validationError, loadErr := loadDoctorConfig(cfgPathOrDefault(cfgPath), cwd)
		if loadErr != nil {
			fmt.Fprintln(os.Stderr, "status error:", loadErr)
			return 1
		}
		var database *db.DB
		if strings.TrimSpace(cfg.DBPath) != "" {
			_ = os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755)
			if opened, openErr := db.Open(cfg.DBPath); openErr == nil {
				database = opened
				defer database.Close()
			}
		}
		statusOptions, err := parseStatusCommandArgs(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "status error:", err)
			return 2
		}
		if err := runStatusCommandWithOptions(cfgPathOrDefault(cfgPath), cfg, validationError, database, os.Stdout, statusOptions); err != nil {
			if translated := translateAndPrintError(err, os.Stderr); translated != nil {
				fmt.Fprintln(os.Stderr, "status error:", err)
			}
			return 1
		}
		return 0
	}
	if cmd == "cron" && (len(args) < 2 || args[1] != "run") {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = ""
		}
		cfg, _, loadErr := loadDoctorConfig(cfgPathOrDefault(cfgPath), cwd)
		if loadErr != nil {
			fmt.Fprintln(os.Stderr, "cron error:", loadErr)
			return 1
		}
		storePath := strings.TrimSpace(cfg.Cron.StorePath)
		if storePath == "" {
			fmt.Fprintln(os.Stderr, "cron error: cron store path not configured")
			return 1
		}
		if err := runCronCommand(context.Background(), storePath, args[1:], os.Stdout, os.Stderr); err != nil {
			if isUsageError(err) {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			fmt.Fprintln(os.Stderr, "cron error:", err)
			return 1
		}
		return 0
	}
	if cmd == "access" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = ""
		}
		cfg, _, loadErr := loadDoctorConfig(cfgPathOrDefault(cfgPath), cwd)
		if loadErr != nil {
			fmt.Fprintln(os.Stderr, "access error:", loadErr)
			return 1
		}
		if err := runAccessCommand(context.Background(), cfgPathOrDefault(cfgPath), cfg, args[1:], os.Stdout, os.Stderr); err != nil {
			if isUsageError(err) {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			fmt.Fprintln(os.Stderr, "access error:", err)
			return 1
		}
		return 0
	}

	loadedRuntimeConfig, err := loadRuntimeConfig(cfgPath)
	if err != nil {
		if translated := translateAndPrintError(err, os.Stderr); translated != nil {
			fmt.Fprintln(os.Stderr, "config error:", err)
		}
		if hint := configErrorHint(err); hint != "" {
			fmt.Fprintln(os.Stderr, hint)
		}
		return 1
	}
	cfg := loadedRuntimeConfig.Config
	if err := prepareRuntimeStorage(&cfg, cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "runtime storage error:", err)
		return 1
	}

	d, err := openRuntimeDatabase(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer d.Close()

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancelRuntime := context.WithCancel(ctx)
	defer cancelRuntime()
	securedRuntime, err := buildRuntimeSecurity(ctx, cfg, d)
	if err != nil {
		if translated := translateAndPrintError(err, os.Stderr); translated != nil {
			fmt.Fprintln(os.Stderr, "security error:", err)
		}
		return 1
	}
	cfg = securedRuntime.Config
	if cfg.Security.Profiles.Enabled {
		config.EnsureBuiltinAccessProfiles(&cfg.Security.Profiles)
	}
	secretManager := securedRuntime.Secrets
	auditLogger := securedRuntime.Audit
	if err := validateRuntimeStartupCommand(cmd, cfg, unsafeDev); err != nil {
		if translated := translateAndPrintError(err, os.Stderr); translated != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}
	if cmd == "secrets" {
		if secretManager == nil && cfg.Security.SecretStore.Enabled {
			key, keyErr := security.LoadOrCreateKey(cfg.Security.SecretStore.KeyFile)
			if keyErr != nil {
				fmt.Fprintln(os.Stderr, "secret key error:", keyErr)
				return 1
			}
			secretManager = &security.SecretManager{DB: d, Key: key}
		}
		if err := runSecretsCommand(ctx, secretManager, auditLogger, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "secrets error:", err)
			return 1
		}
		return 0
	}
	if cmd == "audit" {
		cp := controlplane.NewLocal(cfg, d, nil, auditLogger, nil)
		if err := runAuditCommand(ctx, cp, args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "audit error:", err)
			return 1
		}
		return 0
	}
	approvalRuntime, err := buildRuntimeApprovalSecurity(cfg, d, auditLogger)
	if err != nil {
		if translated := translateAndPrintError(err, os.Stderr); translated != nil {
			fmt.Fprintln(os.Stderr, "approval error:", err)
		}
		return 1
	}
	approvalBroker := approvalRuntime.ApprovalBroker
	if commandHandledBeforeRuntimeBootstrap(cmd) {
		var prov *providers.Client
		if cmd == "embeddings" {
			prov = newEmbeddingProviderClient(cfg)
		} else if cmd == "migrate-openclaw" {
			prov = newProviderClient(cfg)
		}
		handled, err := runPreRuntimeCommand(ctx, cmd, cfgPath, cfg, d, prov, auditLogger, approvalBroker, args[1:], os.Stdout, os.Stderr)
		if handled {
			if err != nil {
				if isUsageError(err) {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				fmt.Fprintln(os.Stderr, cmd+" error:", err)
				return 1
			}
			return 0
		}
	}
	embedProv := newEmbeddingProviderClient(cfg)
	art := &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}
	if err := art.Reconcile(ctx, time.Hour); err != nil {
		log.Printf("artifact reconciliation failed: %v", err)
	}

	b := bus.New(256)
	spinner := cli.NewSpinner()
	del := &cli.Deliverer{Spinner: spinner}
	channelManager, err := buildChannelManager(cfg, del, art, cfg.MaxMediaBytes, approvalBroker)
	if err != nil {
		fmt.Fprintln(os.Stderr, "channel config error:", err)
		return 1
	}

	var cronSvc *cron.Service
	var runnerManager *runners.Manager
	var heartbeatSvc *heartbeat.Service
	var workerWG *sync.WaitGroup
	defer func() {
		cancelRuntime()
		if heartbeatSvc != nil {
			heartbeatSvc.Stop()
		}
		if cronSvc != nil {
			cronSvc.Stop()
		}
		_ = channelManager.StopAll(context.Background())
		b.Close()
		if workerWG != nil {
			workerWG.Wait()
		}
		if runnerManager != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
			defer cancel()
			if err := runnerManager.Stop(stopCtx); err != nil {
				log.Printf("runner manager stop failed: %v", err)
			}
		}
	}()

	ret := memory.NewRetriever(d)
	ret.EmbedFingerprint = currentEmbedFingerprint(cfg)
	ret.VectorScanLimit = cfg.VectorScanLimit

	var docRetriever *memory.DocRetriever

	serviceHost := serviceHostDeps{
		DB:            d,
		Audit:         auditLogger,
		Artifacts:     art,
		Mem:           ret,
		DocRetriever:  docRetriever,
		EmbedProvider: embedProv,
	}
	runtimeJobs := buildRuntimeJobRegistry()

	runnerManager = buildRuntimeRunnerManager(cfg, d, runtimeJobs)
	if runnerManager != nil && cmd != "service" {
		if err := startRuntimeRunnerManager(ctx, runnerManager); err != nil {
			fmt.Fprintln(os.Stderr, "runner manager error:", err)
			return 1
		}
	}
	chatManager := buildRuntimeChatManager(cfg, d, runnerManager, runtimeJobs, approvalBroker)
	turnOrchestrator := buildRunnerTurnOrchestrator(cfg, chatManager, d, ret, docRetriever, embedProv)

	cronSvc = buildRuntimeCronService(cfg, b, runnerManager, turnOrchestrator)
	if cronSvc != nil && cmd != "cron" {
		if err := cronSvc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "cron start error:", err)
			return 1
		}
	}
	if consolidator, scheduler := startMemoryConsolidation(ctx, cfg, d, del); consolidator != nil {
		_ = consolidator
		if chatManager != nil && scheduler != nil {
			chatManager.OnSuccessfulTurn = scheduler.Trigger
		}
	}

	switch cmd {
	case "chat":
		_ = channelManager.Start(ctx, "cli", b)
		workerWG = runWorkers(ctx, b, turnOrchestrator, cfg.WorkerCount, del, channelManager, nil, &channelCommandHandler{Config: cfg, DB: d, RunnerManager: runnerManager, Channels: channelManager, CLI: del})
		ch := &cli.Channel{Bus: b, SessionKey: cfg.DefaultSessionKey, Spinner: spinner, Deliverer: del, History: d}
		if err := ch.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "cli error:", err)
		}
	case "serve":
		workerWG = runWorkers(ctx, b, turnOrchestrator, cfg.WorkerCount, nil, channelManager, &channelApprovalHandler{Config: cfg, Jobs: runtimeJobs, Broker: approvalBroker, Channels: channelManager, ChatManager: chatManager}, &channelCommandHandler{Config: cfg, DB: d, RunnerManager: runnerManager, Channels: channelManager})
		if err := channelManager.StartAll(ctx, b); err != nil {
			fmt.Fprintln(os.Stderr, "channel start error:", err)
			return 1
		}
		// start webhook server if configured
		webhookSrv := triggers.NewWebhookServer(cfg.Triggers.Webhook, b, cfg.DefaultSessionKey)
		if err := webhookSrv.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "webhook start error:", err)
			return 1
		}
		defer func() {
			_ = webhookSrv.Stop(context.Background())
		}()
		// start file watcher if configured
		fileWatcher := triggers.NewFileWatcher(cfg.Triggers.FileWatch, b, cfg.DefaultSessionKey)
		fileWatcher.Start(ctx)
		defer fileWatcher.Stop()
		heartbeatSvc = heartbeatServiceForCommand(cmd, cfg, b)
		if heartbeatSvc != nil {
			heartbeatSvc.Start(ctx)
		}
		fmt.Println("or3-intern serve: channels running. Ctrl+C to stop.")
		select {
		case <-ctx.Done():
		case err := <-webhookSrv.Errors():
			fmt.Fprintln(os.Stderr, "webhook server error:", err)
			return 1
		}
	case "service":
		workerWG = runWorkers(ctx, b, turnOrchestrator, cfg.WorkerCount, nil, channelManager, &channelApprovalHandler{Config: cfg, Jobs: runtimeJobs, Broker: approvalBroker, Channels: channelManager, ChatManager: chatManager}, &channelCommandHandler{Config: cfg, DB: d, RunnerManager: runnerManager, Channels: channelManager})
		if err := channelManager.StartAll(ctx, b); err != nil {
			fmt.Fprintln(os.Stderr, "channel start error:", err)
			return 1
		}
		if err := runServiceCommandWithBrokerOptionsCronMCPAndChannels(ctx, cfg, serviceHost, runnerManager, chatManager, turnOrchestrator, runtimeJobs, approvalBroker, unsafeDev, cronSvc, channelManager); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			return 1
		}
	case "agent":
		// one-shot: or3-intern agent -m "hello" (runner-backed; built-in agent loop deprecated)
		fs := flag.NewFlagSet("agent", flag.ExitOnError)
		var msg string
		var session string
		var approvalToken string
		fs.StringVar(&msg, "m", "", "message")
		fs.StringVar(&session, "s", cfg.DefaultSessionKey, "session key")
		fs.StringVar(&approvalToken, "approval-token", "", "one-shot approval token")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(msg) == "" {
			fmt.Fprintln(os.Stderr, "missing -m message")
			return 2
		}
		agentCtx := requestctx.ContextWithApprovalToken(ctx, approvalToken)
		agentCtx = requestctx.ContextWithRequesterIdentity(agentCtx, "cli", approval.RoleOperator)
		if turnOrchestrator == nil {
			fmt.Fprintln(os.Stderr, "agent error: runner orchestration unavailable; enable runners and configure a default runner")
			return 1
		}
		result, err := turnOrchestrator.StartTurn(agentCtx, app.RunnerTurnRequest{
			SessionKey:    session,
			Channel:       "cli",
			From:          "local",
			Message:       msg,
			TriggerKind:   "user_message",
			ApprovalToken: approvalToken,
			Actor:         "cli",
			Role:          approval.RoleOperator,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent error:", err)
			return 1
		}
		var abort func(context.Context, string) error
		if chatManager != nil {
			abort = chatManager.AbortTurn
		}
		final, err := waitForForegroundAgentTurn(
			agentCtx,
			time.Duration(cfg.Runners.DefaultTimeoutSeconds)*time.Second,
			result,
			turnOrchestrator.WaitForTurnResult,
			abort,
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent error:", err)
			return 1
		}
		if text := channelTurnDeliveryText(final); strings.TrimSpace(text) != "" {
			fmt.Fprintln(os.Stdout, text)
		}
		if !foregroundAgentTurnSucceeded(final) {
			return 1
		}
	case "cron":
		runner := foregroundCronRunner(d, cronrunner.NewWithPreparer(b, cfg.DefaultSessionKey, runnerManager, turnOrchestrator))
		if err := runCronCommandWithRunner(ctx, cfg.Cron.StorePath, args[1:], os.Stdout, os.Stderr, runner); err != nil {
			fmt.Fprintln(os.Stderr, "cron error:", err)
			if isUsageError(err) {
				return 2
			}
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		return 2
	}

	return 0
}

func foregroundCronRunner(database *db.DB, dispatch cron.Runner) cron.Runner {
	return func(ctx context.Context, job cron.CronJob) (cron.RunResult, error) {
		result, err := dispatch(ctx, job)
		if err != nil || strings.TrimSpace(result.EnqueuedRunID) == "" || database == nil {
			return result, err
		}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			run, ok, err := database.GetRunnerRun(ctx, result.EnqueuedRunID)
			if err != nil {
				return result, err
			}
			if !ok {
				return result, fmt.Errorf("runner run %s disappeared", result.EnqueuedRunID)
			}
			switch run.Status {
			case db.RunnerRunStatusSucceeded:
				return result, nil
			case db.RunnerRunStatusFailed, db.RunnerRunStatusAborted, db.RunnerRunStatusTimedOut, db.RunnerRunStatusApprovalRequired:
				return result, fmt.Errorf("runner run %s ended with status %s: %s", run.ID, run.Status, run.ErrorMessage)
			}
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func loadDoctorConfig(cfgPath, cwd string) (config.Config, string, error) {
	if cfg, err := config.Load(cfgPath); err == nil {
		return cfg, "", nil
	} else {
		cfg, repairErr := loadConfigureConfigLenient(cfgPath, cwd)
		if repairErr != nil {
			return config.Config{}, "", repairErr
		}
		return cfg, err.Error(), nil
	}
}

func commandHandledBeforeConfigLoad(cmd string) bool {
	switch cmd {
	case "config-path", "version", "configure", "init", "setup", "settings", "connect":
		return true
	default:
		return false
	}
}

func maybeRunFirstRunSetup(cfgPath, cmd string, args []string) (bool, error) {
	path := cfgPathOrDefault(cfgPath)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	next := firstRunNextStep(cmd, args)
	result, err := runSetupWithIOOptions(os.Stdin, os.Stdout, path, currentWorkingDir(), setupOptions{
		AskStartChat:     cmd == "chat",
		StartChatDefault: true,
		CompletionNext:   next,
		AutoInvoked:      true,
	})
	if err != nil {
		return true, err
	}
	if cmd == "chat" && !result.StartChat {
		return true, nil
	}
	return false, nil
}

func firstRunNextStep(cmd string, args []string) string {
	if cmd == "" {
		cmd = "chat"
	}
	if cmd == "chat" {
		return "run `or3-intern chat`"
	}
	if len(args) == 0 {
		return fmt.Sprintf("continuing with `or3-intern %s`", cmd)
	}
	return fmt.Sprintf("continuing with `or3-intern %s`", strings.Join(args, " "))
}

func currentWorkingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func commandHandledBeforeRuntimeBootstrap(cmd string) bool {
	switch cmd {
	case "approvals", "capabilities", "devices", "embeddings", "pairing", "scope", "migrate-jsonl", "migrate-openclaw", "memory", "skills":
		return true
	default:
		return false
	}
}

func configErrorHint(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, " enabled: set ") && strings.Contains(message, "inboundpolicy=pairing") {
		return "hint: run `or3-intern configure --section channels` and choose an inbound access mode for the enabled channel"
	}
	return ""
}

func buildIdentityScopeMap(cfg config.Config) map[string]string {
	out := map[string]string{}
	for _, link := range cfg.Session.IdentityLinks {
		canonical := strings.TrimSpace(link.Canonical)
		if canonical == "" {
			continue
		}
		for _, peer := range link.Peers {
			peer = strings.TrimSpace(peer)
			if peer == "" {
				continue
			}
			out[peer] = canonical
		}
	}
	return out
}

type delivererFunc func(ctx context.Context, channel, to, text string) error

func (f delivererFunc) Deliver(ctx context.Context, channel, to, text string) error {
	return f(ctx, channel, to, text)
}

func shouldRegisterExecTool(cfg config.Config) bool {
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		return false
	}
	spec := config.ProfileSpec(cfg.RuntimeProfile)
	if spec.ForbidPrivilegedTools {
		return false
	}
	return true
}

func buildChannelManager(cfg config.Config, cliDeliverer *cli.Deliverer, art *artifacts.Store, maxMediaBytes int, approvalBroker *approval.Broker) (*rootchannels.Manager, error) {
	mgr := rootchannels.NewManager()
	if err := mgr.Register(cli.Service{Deliverer: cliDeliverer}); err != nil {
		return nil, err
	}
	if cfg.Channels.Telegram.Enabled {
		if err := mgr.Register(&telegram.Channel{Config: cfg.Channels.Telegram, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers, ApprovalBroker: approvalBroker}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Slack.Enabled {
		if err := mgr.Register(&slack.Channel{Config: cfg.Channels.Slack, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers, ApprovalBroker: approvalBroker}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Discord.Enabled {
		if err := mgr.Register(&discord.Channel{Config: cfg.Channels.Discord, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers, ApprovalBroker: approvalBroker}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.WhatsApp.Enabled {
		cfg.Channels.WhatsApp.BridgeURL = whatsapp.BridgeURL(cfg.Channels.WhatsApp.BridgeURL)
		if err := mgr.Register(&whatsapp.Channel{Config: cfg.Channels.WhatsApp, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers, ApprovalBroker: approvalBroker}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Email.Enabled {
		var database *db.DB
		if art != nil {
			database = art.DB
		}
		if err := mgr.Register(&email.Channel{Config: cfg.Channels.Email, DB: database, ApprovalBroker: approvalBroker}); err != nil {
			return nil, err
		}
	}
	return mgr, nil
}

func cfgPathOrDefault(p string) string {
	if p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

func allowedRoot(cfg config.Config) string {
	if cfg.WorkspaceDir != "" {
		return cfg.WorkspaceDir
	}
	if cfg.AllowedDir != "" {
		return cfg.AllowedDir
	}
	return ""
}

func allowedReadRoot(cfg config.Config) string {
	return allowedRoot(cfg)
}

func heartbeatServiceForCommand(cmd string, cfg config.Config, eventBus *bus.Bus) *heartbeat.Service {
	if cmd != "serve" || !cfg.Heartbeat.Enabled {
		return nil
	}
	return heartbeat.New(cfg.Heartbeat, cfg.WorkspaceDir, eventBus)
}

func runWorkers(ctx context.Context, b *bus.Bus, turnOrchestrator *app.RunnerTurnOrchestrator, n int, cliDeliverer *cli.Deliverer, channelManager *rootchannels.Manager, approvalHandler *channelApprovalHandler, commandHandler *channelCommandHandler) *sync.WaitGroup {
	if n <= 0 {
		n = 4
	}
	events := b.Channel()
	wg := &sync.WaitGroup{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ev := range events {
				cctx, cancel := context.WithTimeout(ctx, time.Duration(channelWorkerTimeoutSeconds(ev))*time.Second)
				cctx = streaming.ContextWithConversationSession(cctx, ev.SessionKey)
				if handled, err := approvalHandler.Handle(cctx, ev); handled {
					if err != nil {
						log.Printf("channel approval command failed: channel=%s session=%s err=%v", ev.Channel, ev.SessionKey, err)
					}
					cancel()
					continue
				}
				if next, handled, err := commandHandler.Handle(cctx, ev); handled {
					if err != nil {
						log.Printf("channel command failed: channel=%s session=%s err=%v", ev.Channel, ev.SessionKey, err)
					}
					cancel()
					continue
				} else {
					ev = next
				}
				stopTyping := func() {}
				if ev.Channel != "cli" && channelManager != nil {
					stopTyping = channelManager.StartTyping(cctx, ev.Channel, "", ev.Meta)
				}
				if ev.Channel == "cli" && cliDeliverer != nil {
					if observer := cliDeliverer.Observer(); observer != nil {
						cctx = streaming.ContextWithConversationObserver(cctx, observer)
					}
				}
				var err error
				var result app.RunnerTurnResult
				if turnOrchestrator != nil {
					result, err = turnOrchestrator.StartBusEventTurn(cctx, ev)
				} else {
					err = app.ErrRunnerRuntimeUnavailable
				}
				if err != nil {
					if ev.Channel == "cli" {
						if cliDeliverer != nil {
							cliDeliverer.ShowErrorForSession(ev.SessionKey, err)
						}
					} else {
						deliverChannelRuntimeError(cctx, channelManager, ev, err)
						log.Printf("handle event failed: type=%s session=%s err=%v", ev.Type, ev.SessionKey, err)
					}
				} else if ev.Channel != "cli" && turnOrchestrator != nil {
					deliverChannelTurnResult(cctx, channelManager, ev, turnOrchestrator, result)
				}
				stopTyping()
				cancel()
			}
		}()
	}
	return wg
}

func channelWorkerTimeoutSeconds(ev bus.Event) int {
	if isApprovalExternalChannel(ev.Channel) {
		return 900
	}
	return 120
}

func deliverChannelRuntimeError(ctx context.Context, channelManager *rootchannels.Manager, ev bus.Event, err error) {
	if channelManager == nil || err == nil {
		return
	}
	var approvalErr *tools.ApprovalRequiredError
	if errors.As(err, &approvalErr) {
		return
	}
	code := serviceerrors.PublicErrorCode(err)
	if code == "" {
		code = serviceerrors.PublicErrorUnknown
	}
	text := "I hit a problem while handling that request (" + code + "). Please retry, or review the details in the OR3 app."
	if derr := channelManager.DeliverWithMeta(ctx, ev.Channel, channelEventTarget(ev), text, rootchannels.ReplyMeta(ev.Meta)); derr != nil {
		log.Printf("channel error delivery failed: channel=%s session=%s err=%v", ev.Channel, ev.SessionKey, derr)
	}
}

func deliverChannelTurnResult(ctx context.Context, channelManager *rootchannels.Manager, ev bus.Event, turnOrchestrator *app.RunnerTurnOrchestrator, result app.RunnerTurnResult) {
	if channelManager == nil || turnOrchestrator == nil || strings.TrimSpace(result.RunnerChatTurnID) == "" {
		return
	}
	final, ok := turnOrchestrator.WaitForTurnResult(ctx, result)
	if !ok {
		log.Printf("channel turn delivery skipped: channel=%s session=%s turn=%s reason=timeout_or_missing", ev.Channel, ev.SessionKey, result.RunnerChatTurnID)
		return
	}
	text := channelTurnDeliveryText(final)
	if strings.TrimSpace(text) == "" {
		return
	}
	if derr := channelManager.DeliverWithMeta(ctx, ev.Channel, channelEventTarget(ev), text, rootchannels.ReplyMeta(ev.Meta)); derr != nil {
		log.Printf("channel turn delivery failed: channel=%s session=%s turn=%s err=%v", ev.Channel, ev.SessionKey, result.RunnerChatTurnID, derr)
	}
}

func channelTurnDeliveryText(final app.RunnerTurnFinalResult) string {
	if text := strings.TrimSpace(final.FinalText); text != "" {
		return text
	}
	if errMessage := strings.TrimSpace(final.ErrorMessage); errMessage != "" {
		status := strings.ReplaceAll(strings.TrimSpace(final.Status), "_", " ")
		if status == "" {
			status = "failed"
		}
		return "Runner turn " + status + ": " + errMessage
	}
	switch final.Status {
	case db.RunnerChatTurnStatusSucceeded:
		return "(no output)"
	case db.RunnerChatTurnStatusApprovalRequired:
		return "OR3 needs your approval before the runner can continue. Review the request in the app, or reply with the approval command if one was shown."
	case db.RunnerChatTurnStatusTimedOut:
		return "Runner turn timed out. Check the OR3 app for details."
	case db.RunnerChatTurnStatusAborted:
		return "Runner turn was aborted."
	case db.RunnerChatTurnStatusFailed:
		return "Runner turn failed. Check the OR3 app for details."
	default:
		return ""
	}
}

func loadBootstrapFile(configPath, workspaceDir, baseName, fallback string) string {
	paths := []string{}
	if strings.TrimSpace(workspaceDir) != "" {
		paths = append(paths,
			filepath.Join(workspaceDir, baseName),
			filepath.Join(workspaceDir, strings.ToLower(baseName)),
		)
	}
	if strings.TrimSpace(configPath) != "" {
		paths = append(paths, configPath)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return fallback
}

func ensureFileIfMissing(path, content string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}
