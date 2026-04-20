package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"or3-intern/internal/agent"
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
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/mcp"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/security"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

const (
	schedulerMaxConsolidationPasses = 3
	gracefulShutdownTimeout         = 5 * time.Second
)

func main() {
	cfgPath, args, showHelp, err := parseRootCLIArgs(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if showHelp {
		if err := printHelpTopic(os.Stdout, helpTopicPath(args)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if handled, err := maybeHandleHelpRequest(args, os.Stdout); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	cmd := "chat"
	if len(args) > 0 {
		cmd = args[0]
	}
	if isHelpToken(cmd) {
		printRootHelp(os.Stdout)
		return
	}
	if cmd == "config-path" {
		fmt.Fprintln(os.Stdout, cfgPathOrDefault(cfgPath))
		return
	}
	if cmd == "init" {
		if err := runInit(cfgPath); err != nil {
			fmt.Fprintln(os.Stderr, "init error:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if cmd == "doctor" {
		if err := runDoctorCommand(cfg, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "doctor error:", err)
			os.Exit(1)
		}
		return
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.WorkspaceDir = cwd
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir db dir error:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir artifacts dir error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.SoulFile, agent.DefaultSoul); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap soul file error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.AgentsFile, agent.DefaultAgentInstructions); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap agents file error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.ToolsFile, agent.DefaultToolNotes); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap tools file error:", err)
		os.Exit(1)
	}
	// Bootstrap IDENTITY.md and MEMORY.md (silent fallback if missing)
	if cfg.IdentityFile != "" {
		_ = ensureFileIfMissing(cfg.IdentityFile, "# Identity\n")
	}
	if cfg.MemoryFile != "" {
		_ = ensureFileIfMissing(cfg.MemoryFile, "# Static Memory\n")
	}

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db error:", err)
		os.Exit(1)
	}
	defer d.Close()

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	cfg, secretManager, auditLogger, err := setupSecurity(ctx, cfg, d)
	if err != nil {
		fmt.Fprintln(os.Stderr, "security error:", err)
		os.Exit(1)
	}
	if err := validateStartupCommand(cmd, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if cmd == "secrets" {
		if secretManager == nil && cfg.Security.SecretStore.Enabled {
			key, keyErr := security.LoadOrCreateKey(cfg.Security.SecretStore.KeyFile)
			if keyErr != nil {
				fmt.Fprintln(os.Stderr, "secret key error:", keyErr)
				os.Exit(1)
			}
			secretManager = &security.SecretManager{DB: d, Key: key}
		}
		if err := runSecretsCommand(ctx, secretManager, auditLogger, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "secrets error:", err)
			os.Exit(1)
		}
		return
	}
	if cmd == "audit" {
		if err := runAuditCommand(ctx, auditLogger, args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "audit error:", err)
			os.Exit(1)
		}
		return
	}
	approvalBroker, err := setupApprovalBroker(cfg, d, auditLogger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "approval error:", err)
		os.Exit(1)
	}
	timeout := time.Duration(cfg.Provider.TimeoutSeconds) * time.Second
	prov := providers.New(cfg.Provider.APIBase, cfg.Provider.APIKey, timeout)
	prov.HostPolicy = buildHostPolicy(cfg)
	art := &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}

	b := bus.New(256)
	spinner := cli.NewSpinner()
	del := cli.Deliverer{Spinner: spinner}
	channelManager, err := buildChannelManager(cfg, del, art, cfg.MaxMediaBytes, approvalBroker)
	if err != nil {
		fmt.Fprintln(os.Stderr, "channel config error:", err)
		os.Exit(1)
	}

	var mcpManager *mcp.Manager
	if len(cfg.Tools.MCPServers) > 0 {
		mcpManager = mcp.NewManager(cfg.Tools.MCPServers)
		mcpManager.SetLogger(log.Printf)
		mcpManager.SetHostPolicy(buildHostPolicy(cfg))
		if err := mcpManager.Connect(ctx); err != nil {
			log.Printf("mcp setup failed: %v", err)
		}
	}

	// skills
	builtin := filepath.Join(filepath.Dir(cfgPathOrDefault(cfgPath)), "builtin_skills")
	toolNames := loadAvailableToolNamesWithManager(ctx, cfg, mcpManager)
	inv := buildSkillsInventory(cfg, builtin, toolNames)
	var cronSvc *cron.Service
	var subagentManager *agent.SubagentManager
	enableSubagents := subagentsEnabledForCommand(cmd, cfg)
	buildRuntimeTools := func() *tools.Registry {
		return buildToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, subagentManager, mcpManager, approvalBroker)
	}
	buildBackgroundTools := func() *tools.Registry {
		return buildBackgroundToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, mcpManager, approvalBroker)
	}

	ret := memory.NewRetriever(d)
	ret.VectorScanLimit = cfg.VectorScanLimit

	var docIndexer *memory.DocIndexer
	var docRetriever *memory.DocRetriever
	if cfg.DocIndex.Enabled && len(cfg.DocIndex.Roots) > 0 {
		docIndexer = &memory.DocIndexer{
			DB:         d,
			Provider:   prov,
			EmbedModel: cfg.Provider.EmbedModel,
			Config: memory.DocIndexConfig{
				Roots:          cfg.DocIndex.Roots,
				MaxFiles:       cfg.DocIndex.MaxFiles,
				MaxFileBytes:   cfg.DocIndex.MaxFileBytes,
				MaxChunks:      cfg.DocIndex.MaxChunks,
				EmbedMaxBytes:  cfg.DocIndex.EmbedMaxBytes,
				RefreshSeconds: cfg.DocIndex.RefreshSeconds,
				RetrieveLimit:  cfg.DocIndex.RetrieveLimit,
			},
		}
		docRetriever = &memory.DocRetriever{DB: d}
		// Initial sync in background (don't block startup)
		go func() {
			syncCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := docIndexer.SyncRoots(syncCtx, scope.GlobalMemoryScope); err != nil {
				log.Printf("doc index sync failed: %v", err)
			}
		}()
	}
	if docIndexer != nil && cfg.DocIndex.RefreshSeconds > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.DocIndex.RefreshSeconds) * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				syncCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				if err := docIndexer.SyncRoots(syncCtx, scope.GlobalMemoryScope); err != nil {
					log.Printf("doc index refresh failed: %v", err)
				}
				cancel()
			}
		}()
	}

	rt := &agent.Runtime{
		DB:          d,
		Provider:    prov,
		Model:       cfg.Provider.Model,
		Temperature: cfg.Provider.Temperature,
		Tools:       buildRuntimeTools(),
		Builder: &agent.Builder{
			DB:                     d,
			Artifacts:              art,
			Skills:                 inv,
			Mem:                    ret,
			Provider:               prov,
			EmbedModel:             cfg.Provider.EmbedModel,
			EnableVision:           cfg.Provider.EnableVision,
			Soul:                   loadBootstrapFile(cfg.SoulFile, cfg.WorkspaceDir, "SOUL.md", agent.DefaultSoul),
			AgentInstructions:      loadBootstrapFile(cfg.AgentsFile, cfg.WorkspaceDir, "AGENTS.md", agent.DefaultAgentInstructions),
			ToolNotes:              loadBootstrapFile(cfg.ToolsFile, cfg.WorkspaceDir, "TOOLS.md", agent.DefaultToolNotes),
			IdentityText:           loadBootstrapFile(cfg.IdentityFile, cfg.WorkspaceDir, "IDENTITY.md", ""),
			StaticMemory:           loadBootstrapFile(cfg.MemoryFile, cfg.WorkspaceDir, "MEMORY.md", ""),
			HeartbeatTasksFile:     cfg.Heartbeat.TasksFile,
			BootstrapMaxChars:      cfg.BootstrapMaxChars,
			BootstrapTotalMaxChars: cfg.BootstrapTotalMaxChars,
			HistoryMax:             cfg.HistoryMax,
			VectorK:                cfg.VectorK,
			FTSK:                   cfg.FTSK,
			TopK:                   cfg.MemoryRetrieve,
			DocRetriever:           docRetriever,
			DocRetrieveLimit:       cfg.DocIndex.RetrieveLimit,
			WorkspaceDir:           cfg.WorkspaceDir,
		},
		Artifacts:          art,
		MaxToolBytes:       cfg.MaxToolBytes,
		MaxToolLoops:       cfg.MaxToolLoops,
		Deliver:            delivererFunc(channelManager.Deliver),
		DefaultScopeKey:    cfg.DefaultSessionKey,
		LinkDirectMessages: cfg.Session.DirectMessagesShareDefault,
		IdentityScopeMap:   buildIdentityScopeMap(cfg),
		Hardening:          cfg.Hardening,
		AccessProfiles:     cfg.Security.Profiles,
		Audit:              auditLogger,
		ApprovalBroker:     approvalBroker,
	}
	var serviceJobs *agent.JobRegistry
	if cmd == "service" {
		serviceJobs = agent.NewJobRegistry(0, 0)
		rt.Deliver = nil
		rt.Streamer = nil
	}

	// cron service + tool
	if cfg.Cron.Enabled {
		cronSvc = cron.New(cfg.Cron.StorePath, agent.CronRunner(b, cfg.DefaultSessionKey))
		if err := cronSvc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "cron start error:", err)
			os.Exit(1)
		}
		rt.Tools = buildRuntimeTools()
	}

	if enableSubagents {
		subagentManager = &agent.SubagentManager{
			DB:            d,
			Runtime:       rt,
			Deliver:       delivererFunc(channelManager.Deliver),
			MaxConcurrent: cfg.Subagents.MaxConcurrent,
			MaxQueued:     cfg.Subagents.MaxQueued,
			TaskTimeout:   time.Duration(cfg.Subagents.TaskTimeoutSeconds) * time.Second,
			Jobs:          serviceJobs,
			BackgroundTools: func() *tools.Registry {
				return buildBackgroundTools()
			},
		}
		if cmd == "service" {
			subagentManager.Deliver = nil
		}
		if err := subagentManager.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "subagent manager error:", err)
			os.Exit(1)
		}
		rt.Tools = buildRuntimeTools()
	}
	if cfg.ConsolidationEnabled {
		rt.Consolidator = &memory.Consolidator{
			DB:                 d,
			Provider:           prov,
			EmbedModel:         cfg.Provider.EmbedModel,
			ChatModel:          cfg.Provider.Model,
			WindowSize:         cfg.ConsolidationWindowSize,
			MaxMessages:        cfg.ConsolidationMaxMessages,
			MaxInputChars:      cfg.ConsolidationMaxInputChars,
			CanonicalPinnedKey: "long_term_memory",
		}
		rt.ConsolidationScheduler = memory.NewSchedulerWithContext(
			ctx,
			time.Duration(cfg.ConsolidationAsyncTimeoutSeconds)*time.Second,
			func(runCtx context.Context, sessionKey string) {
				historyMax := cfg.HistoryMax
				if historyMax <= 0 {
					historyMax = 40
				}
				for i := 0; i < schedulerMaxConsolidationPasses; i++ {
					didWork, err := rt.Consolidator.RunOnce(runCtx, sessionKey, historyMax, memory.RunMode{})
					if err != nil {
						log.Printf("consolidation failed: session=%s err=%v", sessionKey, err)
						return
					}
					if !didWork {
						return
					}
				}
			},
		)
	}

	var heartbeatSvc *heartbeat.Service
	switch cmd {
	case "chat":
		rt.Streamer = del
		_ = channelManager.Start(ctx, "cli", b)
		runWorkers(ctx, b, rt, cfg.WorkerCount, spinner)
		ch := &cli.Channel{Bus: b, SessionKey: cfg.DefaultSessionKey, Spinner: spinner}
		if err := ch.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "cli error:", err)
		}
	case "serve":
		runWorkers(ctx, b, rt, cfg.WorkerCount, nil)
		if err := channelManager.StartAll(ctx, b); err != nil {
			fmt.Fprintln(os.Stderr, "channel start error:", err)
			os.Exit(1)
		}
		// start webhook server if configured
		webhookSrv := triggers.NewWebhookServer(cfg.Triggers.Webhook, b, cfg.DefaultSessionKey)
		if err := webhookSrv.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "webhook start error:", err)
			os.Exit(1)
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
		<-ctx.Done()
	case "service":
		runWorkers(ctx, b, rt, cfg.WorkerCount, nil)
		if err := runServiceCommandWithBroker(ctx, cfg, rt, subagentManager, serviceJobs, approvalBroker); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			os.Exit(1)
		}
	case "agent":
		// one-shot: or3-intern agent -m "hello"
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
			os.Exit(2)
		}
		agentCtx := tools.ContextWithApprovalToken(ctx, approvalToken)
		agentCtx = tools.ContextWithRequesterIdentity(agentCtx, "cli", approval.RoleOperator)
		if err := rt.Handle(agentCtx, bus.Event{Type: bus.EventUserMessage, SessionKey: session, Channel: "cli", From: "local", Message: msg}); err != nil {
			fmt.Fprintln(os.Stderr, "agent error:", err)
			os.Exit(1)
		}
	case "migrate-jsonl":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: or3-intern migrate-jsonl <jsonl_path> [session_key]")
			os.Exit(2)
		}
		sessionKey := "migrated:default"
		if len(args) >= 3 {
			sessionKey = args[2]
		}
		if err := migrateJSONL(ctx, d, args[1], sessionKey); err != nil {
			fmt.Fprintln(os.Stderr, "migration error:", err)
			os.Exit(1)
		}
		fmt.Println("ok")
	case "version":
		fmt.Println("or3-intern v1")
	case "skills":
		deps := skillsCommandDeps{
			Client: newClawHubClient(cfg),
			LoadToolNames: func(ctx context.Context, cfg config.Config) map[string]struct{} {
				return loadAvailableToolNamesWithManager(ctx, cfg, nil)
			},
			LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
				return buildSkillsInventory(cfg, builtin, toolNames)
			},
			Audit: func(ctx context.Context, eventType string, payload any) error {
				if auditLogger == nil {
					return nil
				}
				return auditLogger.Record(ctx, eventType, "", "cli", payload)
			},
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
		if err := runSkillsCommandWithDeps(ctx, cfg, args[1:], deps); err != nil {
			fmt.Fprintln(os.Stderr, "skills error:", err)
			os.Exit(1)
		}
	case "approvals":
		if err := runApprovalsCommand(ctx, approvalBroker, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "approvals error:", err)
			os.Exit(1)
		}
	case "devices":
		if err := runDevicesCommand(ctx, approvalBroker, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "devices error:", err)
			os.Exit(1)
		}
	case "pairing":
		if err := runPairingCommand(ctx, approvalBroker, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "pairing error:", err)
			os.Exit(1)
		}
	case "capabilities":
		if err := runCapabilitiesCommand(cfg, approvalBroker, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "capabilities error:", err)
			os.Exit(1)
		}
	case "scope":
		// or3-intern scope link <session-key> <scope-key>
		// or3-intern scope list <scope-key>
		fs := flag.NewFlagSet("scope", flag.ExitOnError)
		_ = fs.Parse(args[1:])
		scopeArgs := fs.Args()
		if len(scopeArgs) < 1 {
			fmt.Fprintln(os.Stderr, "usage: or3-intern scope <link|list> ...")
			os.Exit(2)
		}
		switch scopeArgs[0] {
		case "link":
			if len(scopeArgs) < 3 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope link <session-key> <scope-key>")
				os.Exit(2)
			}
			if err := d.LinkSession(ctx, scopeArgs[1], scopeArgs[2], nil); err != nil {
				fmt.Fprintln(os.Stderr, "scope link error:", err)
				os.Exit(1)
			}
			fmt.Printf("Linked session %q -> scope %q\n", scopeArgs[1], scopeArgs[2])
		case "list":
			if len(scopeArgs) < 2 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope list <scope-key>")
				os.Exit(2)
			}
			sessions, err := d.ListScopeSessions(ctx, scopeArgs[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "scope list error:", err)
				os.Exit(1)
			}
			if len(sessions) == 0 {
				fmt.Println("(no sessions linked to scope)")
			} else {
				for _, s := range sessions {
					fmt.Println(s)
				}
			}
		case "resolve":
			if len(scopeArgs) < 2 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope resolve <session-key>")
				os.Exit(2)
			}
			scopeKey, err := d.ResolveScopeKey(ctx, scopeArgs[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "scope resolve error:", err)
				os.Exit(1)
			}
			fmt.Println(scopeKey)
		default:
			fmt.Fprintln(os.Stderr, "unknown scope subcommand:", scopeArgs[0])
			os.Exit(2)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}

	if heartbeatSvc != nil {
		heartbeatSvc.Stop()
	}
	if mcpManager != nil {
		if err := mcpManager.Close(); err != nil {
			log.Printf("mcp shutdown failed: %v", err)
		}
	}
	if cronSvc != nil {
		cronSvc.Stop()
	}
	if subagentManager != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		if err := subagentManager.Stop(shutdownCtx); err != nil {
			log.Printf("subagent manager stop failed: %v", err)
		}
		cancel()
	}
	_ = channelManager.StopAll(context.Background())
}

func subagentsEnabledForCommand(cmd string, cfg config.Config) bool {
	if !cfg.Subagents.Enabled {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "service", "chat", "serve":
		return true
	default:
		return false
	}
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

type mcpToolRegistrar interface {
	RegisterTools(reg *tools.Registry) int
}

func buildToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer, mcpRegistrar mcpToolRegistrar, approvalBroker *approval.Broker) *tools.Registry {
	return buildToolRegistryWithOptions(cfg, d, prov, channelManager, inv, cronSvc, spawnManager, mcpRegistrar, approvalBroker, true)
}

func buildBackgroundToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, mcpRegistrar mcpToolRegistrar, approvalBroker *approval.Broker) *tools.Registry {
	return buildToolRegistryWithOptions(cfg, d, prov, channelManager, inv, cronSvc, nil, mcpRegistrar, approvalBroker, false)
}

func buildToolRegistryWithOptions(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer, mcpRegistrar mcpToolRegistrar, approvalBroker *approval.Broker, includeSendMessage bool) *tools.Registry {
	reg := tools.NewRegistry()
	fileRoot := allowedRoot(cfg)
	sandboxCfg := tools.BubblewrapConfig{Enabled: cfg.Hardening.Sandbox.Enabled, BubblewrapPath: cfg.Hardening.Sandbox.BubblewrapPath, AllowNetwork: cfg.Hardening.Sandbox.AllowNetwork, WritablePaths: append([]string{}, cfg.Hardening.Sandbox.WritablePaths...)}
	hostPolicy := buildHostPolicy(cfg)
	reg.Register(&tools.ExecTool{Timeout: time.Duration(cfg.Tools.ExecTimeoutSeconds) * time.Second, RestrictDir: fileRoot, PathAppend: cfg.Tools.PathAppend, AllowedPrograms: append([]string{}, cfg.Hardening.ExecAllowedPrograms...), ChildEnvAllowlist: append([]string{}, cfg.Hardening.ChildEnvAllowlist...), Sandbox: sandboxCfg, EnableLegacyShell: cfg.Hardening.EnableExecShell, ApprovalBroker: approvalBroker})
	reg.Register(&tools.ReadFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.EditFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.ListDir{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WebFetch{HostPolicy: hostPolicy})
	reg.Register(&tools.WebSearch{APIKey: cfg.Tools.BraveAPIKey, HostPolicy: hostPolicy})
	reg.Register(&tools.MemorySetPinned{DB: d})
	reg.Register(&tools.MemoryAddNote{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel})
	reg.Register(&tools.MemorySearch{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel, VectorK: cfg.VectorK, FTSK: cfg.FTSK, TopK: cfg.MemoryRetrieve, VectorScanLimit: cfg.VectorScanLimit})
	reg.Register(&tools.MemoryRecent{DB: d, DefaultLimit: 10, MaxLimit: cfg.HistoryMax, MaxChars: 240})
	reg.Register(&tools.MemoryGetPinned{DB: d, MaxChars: 400})
	if includeSendMessage {
		reg.Register(&tools.SendMessage{
			Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
				if channelManager == nil {
					return fmt.Errorf("channel manager not configured")
				}
				return channelManager.DeliverWithMeta(ctx, channel, to, text, meta)
			},
			AllowedRoot:   fileRoot,
			ArtifactsDir:  cfg.ArtifactsDir,
			MaxMediaBytes: cfg.MaxMediaBytes,
		})
	}
	if inv != nil {
		reg.Register(&tools.ReadSkill{Inventory: inv})
		reg.Register(&tools.RunSkillScript{Inventory: inv, Enabled: cfg.Skills.EnableExec, Timeout: time.Duration(cfg.Skills.MaxRunSeconds) * time.Second, ChildEnvAllowlist: append([]string{}, cfg.Hardening.ChildEnvAllowlist...), Sandbox: sandboxCfg, ApprovalBroker: approvalBroker})
	}
	if cronSvc != nil {
		reg.Register(&tools.CronTool{Svc: cronSvc})
	}
	if spawnManager != nil {
		reg.Register(&tools.SpawnSubagent{Manager: spawnManager})
	}
	if mcpRegistrar != nil {
		mcpRegistrar.RegisterTools(reg)
	}
	return reg
}

func buildChannelManager(cfg config.Config, cliDeliverer cli.Deliverer, art *artifacts.Store, maxMediaBytes int, approvalBroker *approval.Broker) (*rootchannels.Manager, error) {
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
	if cfg.Tools.RestrictToWorkspace {
		if cfg.WorkspaceDir != "" {
			return cfg.WorkspaceDir
		}
	}
	if cfg.AllowedDir != "" {
		return cfg.AllowedDir
	}
	return ""
}

func heartbeatServiceForCommand(cmd string, cfg config.Config, eventBus *bus.Bus) *heartbeat.Service {
	if cmd != "serve" || !cfg.Heartbeat.Enabled {
		return nil
	}
	return heartbeat.New(cfg.Heartbeat, cfg.WorkspaceDir, eventBus)
}

func runWorkers(ctx context.Context, b *bus.Bus, rt *agent.Runtime, n int, spinner *cli.Spinner) {
	if n <= 0 {
		n = 4
	}
	for i := 0; i < n; i++ {
		go func() {
			for ev := range b.Channel() {
				cctx, cancel := agent.WithTimeout(ctx, 120)
				if err := rt.Handle(cctx, ev); err != nil {
					if ev.Channel == "cli" {
						cli.ShowError(spinner, err)
					} else {
						log.Printf("handle event failed: type=%s session=%s err=%v", ev.Type, ev.SessionKey, err)
					}
				}
				cancel()
			}
		}()
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
