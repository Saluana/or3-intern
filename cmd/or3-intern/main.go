package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/channels/discord"
	"or3-intern/internal/channels/slack"
	"or3-intern/internal/channels/telegram"
	"or3-intern/internal/channels/whatsapp"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

const schedulerMaxConsolidationPasses = 3

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "", "path to config.json")
	flag.Parse()

	args := flag.Args()
	cmd := "chat"
	if len(args) > 0 {
		cmd = args[0]
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

	ctx := context.Background()
	timeout := time.Duration(cfg.Provider.TimeoutSeconds) * time.Second
	prov := providers.New(cfg.Provider.APIBase, cfg.Provider.APIKey, timeout)
	art := &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}

	b := bus.New(256)
	del := cli.Deliverer{}
	channelManager, err := buildChannelManager(cfg, del, art, cfg.MaxMediaBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "channel config error:", err)
		os.Exit(1)
	}

	// skills
	builtin := filepath.Join(filepath.Dir(cfgPathOrDefault(cfgPath)), "builtin_skills")
	workspace := filepath.Join(cfg.WorkspaceDir, "workspace_skills")
	inv := skills.Scan([]string{builtin, workspace})
	var cronSvc *cron.Service
	var subagentManager *agent.SubagentManager
	buildRuntimeTools := func() *tools.Registry {
		return buildToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, subagentManager)
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
			HeartbeatText:          loadBootstrapFile(cfg.Heartbeat.TasksFile, cfg.WorkspaceDir, "HEARTBEAT.md", ""),
			BootstrapMaxChars:      cfg.BootstrapMaxChars,
			BootstrapTotalMaxChars: cfg.BootstrapTotalMaxChars,
			HistoryMax:             cfg.HistoryMax,
				VectorK:                cfg.VectorK,
				FTSK:                   cfg.FTSK,
				TopK:                   cfg.MemoryRetrieve,
				DocRetriever:           docRetriever,
				DocRetrieveLimit:       cfg.DocIndex.RetrieveLimit,
			},
		Artifacts:    art,
		MaxToolBytes: cfg.MaxToolBytes,
		MaxToolLoops: cfg.MaxToolLoops,
		Deliver:      delivererFunc(channelManager.Deliver),
	}
	if cfg.Subagents.Enabled {
		subagentManager = &agent.SubagentManager{
			DB:            d,
			Runtime:       rt,
			Deliver:       delivererFunc(channelManager.Deliver),
			MaxConcurrent: cfg.Subagents.MaxConcurrent,
			MaxQueued:     cfg.Subagents.MaxQueued,
			TaskTimeout:   time.Duration(cfg.Subagents.TaskTimeoutSeconds) * time.Second,
			BackgroundTools: func() *tools.Registry {
				return buildToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, nil)
			},
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

	// cron service + tool
	if cfg.Cron.Enabled {
		cronSvc = cron.New(cfg.Cron.StorePath, agent.CronRunner(b, cfg.DefaultSessionKey))
		if err := cronSvc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "cron start error:", err)
			os.Exit(1)
		}
		rt.Tools = buildRuntimeTools()
	}

	switch cmd {
	case "chat":
		rt.Streamer = del
		_ = channelManager.Start(ctx, "cli", b)
		runWorkers(ctx, b, rt, cfg.WorkerCount)
		ch := &cli.Channel{Bus: b, SessionKey: cfg.DefaultSessionKey}
		if err := ch.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "cli error:", err)
		}
	case "serve":
		runWorkers(ctx, b, rt, cfg.WorkerCount)
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
		defer webhookSrv.Stop(context.Background())
		// start file watcher if configured
		fileWatcher := triggers.NewFileWatcher(cfg.Triggers.FileWatch, b, cfg.DefaultSessionKey)
		fileWatcher.Start(ctx)
		defer fileWatcher.Stop()
		fmt.Println("or3-intern serve: channels running. Ctrl+C to stop.")
		select {}
	case "agent":
		// one-shot: or3-intern agent -m "hello"
		fs := flag.NewFlagSet("agent", flag.ExitOnError)
		var msg string
		var session string
		fs.StringVar(&msg, "m", "", "message")
		fs.StringVar(&session, "s", cfg.DefaultSessionKey, "session key")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(msg) == "" {
			fmt.Fprintln(os.Stderr, "missing -m message")
			os.Exit(2)
		}
		if err := rt.Handle(ctx, bus.Event{Type: bus.EventUserMessage, SessionKey: session, Channel: "cli", From: "local", Message: msg}); err != nil {
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

	if cronSvc != nil {
		cronSvc.Stop()
	}
	if subagentManager != nil {
		if err := subagentManager.Stop(context.Background()); err != nil {
			log.Printf("subagent manager stop failed: %v", err)
		}
	}
	_ = channelManager.StopAll(context.Background())
}

type delivererFunc func(ctx context.Context, channel, to, text string) error

func (f delivererFunc) Deliver(ctx context.Context, channel, to, text string) error {
	return f(ctx, channel, to, text)
}

func buildToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer) *tools.Registry {
	reg := tools.NewRegistry()
	fileRoot := allowedRoot(cfg)
	reg.Register(&tools.ExecTool{Timeout: time.Duration(cfg.Tools.ExecTimeoutSeconds) * time.Second, RestrictDir: fileRoot, PathAppend: cfg.Tools.PathAppend})
	reg.Register(&tools.ReadFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.EditFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.ListDir{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WebFetch{})
	reg.Register(&tools.WebSearch{APIKey: cfg.Tools.BraveAPIKey})
	reg.Register(&tools.MemorySetPinned{DB: d})
	reg.Register(&tools.MemoryAddNote{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel})
	reg.Register(&tools.MemorySearch{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel, VectorK: cfg.VectorK, FTSK: cfg.FTSK, TopK: cfg.MemoryRetrieve, VectorScanLimit: cfg.VectorScanLimit})
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
	if inv != nil {
		reg.Register(&tools.ReadSkill{Inventory: inv})
	}
	if cronSvc != nil {
		reg.Register(&tools.CronTool{Svc: cronSvc})
	}
	if spawnManager != nil {
		reg.Register(&tools.SpawnSubagent{Manager: spawnManager})
	}
	return reg
}

func buildChannelManager(cfg config.Config, cliDeliverer cli.Deliverer, art *artifacts.Store, maxMediaBytes int) (*rootchannels.Manager, error) {
	mgr := rootchannels.NewManager()
	if err := mgr.Register(cli.Service{Deliverer: cliDeliverer}); err != nil {
		return nil, err
	}
	if cfg.Channels.Telegram.Enabled {
		if err := mgr.Register(&telegram.Channel{Config: cfg.Channels.Telegram, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Slack.Enabled {
		if err := mgr.Register(&slack.Channel{Config: cfg.Channels.Slack, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Discord.Enabled {
		if err := mgr.Register(&discord.Channel{Config: cfg.Channels.Discord, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.WhatsApp.Enabled {
		cfg.Channels.WhatsApp.BridgeURL = whatsapp.BridgeURL(cfg.Channels.WhatsApp.BridgeURL)
		if err := mgr.Register(&whatsapp.Channel{Config: cfg.Channels.WhatsApp, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
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

func runWorkers(ctx context.Context, b *bus.Bus, rt *agent.Runtime, n int) {
	if n <= 0 {
		n = 4
	}
	for i := 0; i < n; i++ {
		go func() {
			for ev := range b.Channel() {
				cctx, cancel := agent.WithTimeout(ctx, 120)
				if err := rt.Handle(cctx, ev); err != nil {
					log.Printf("handle event failed: type=%s session=%s err=%v", ev.Type, ev.SessionKey, err)
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
