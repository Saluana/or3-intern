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
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "", "path to config.json")
	flag.Parse()

	args := flag.Args()
	cmd := "chat"
	if len(args) > 0 { cmd = args[0] }
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

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db error:", err)
		os.Exit(1)
	}
	defer d.Close()

	ctx := context.Background()
	timeout := time.Duration(cfg.Provider.TimeoutSeconds) * time.Second
	prov := providers.New(cfg.Provider.APIBase, cfg.Provider.APIKey, timeout)

	b := bus.New(256)
	del := cli.Deliverer{}

	art := &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}

	reg := tools.NewRegistry()
	reg.Register(&tools.ExecTool{Timeout: time.Duration(cfg.Tools.ExecTimeoutSeconds) * time.Second, RestrictDir: allowedRoot(cfg), PathAppend: cfg.Tools.PathAppend})
	fileRoot := allowedRoot(cfg)
	reg.Register(&tools.ReadFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.EditFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.ListDir{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WebFetch{})
	reg.Register(&tools.WebSearch{APIKey: cfg.Tools.BraveAPIKey})

	// memory tools
	reg.Register(&tools.MemorySetPinned{DB: d})
	reg.Register(&tools.MemoryAddNote{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel})
	reg.Register(&tools.MemorySearch{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel, VectorK: cfg.VectorK, FTSK: cfg.FTSK, TopK: cfg.MemoryRetrieve, VectorScanLimit: cfg.VectorScanLimit})

	// send_message tool
	reg.Register(&tools.SendMessage{Deliver: del.Deliver})

	// skills
	builtin := filepath.Join(filepath.Dir(cfgPathOrDefault(cfgPath)), "builtin_skills")
	workspace := filepath.Join(cfg.WorkspaceDir, "workspace_skills")
	inv := skills.Scan([]string{builtin, workspace})
	reg.Register(&tools.ReadSkill{Inventory: &inv})

	ret := memory.NewRetriever(d)
	ret.VectorScanLimit = cfg.VectorScanLimit

	rt := &agent.Runtime{
		DB: d,
		Provider: prov,
		Model: cfg.Provider.Model,
		Temperature: cfg.Provider.Temperature,
		Tools: reg,
		Builder: &agent.Builder{
			DB: d,
			Skills: inv,
			Mem: ret,
			Provider: prov,
			EmbedModel: cfg.Provider.EmbedModel,
			Soul: loadBootstrapFile(cfg.SoulFile, cfg.WorkspaceDir, "SOUL.md", agent.DefaultSoul),
			AgentInstructions: loadBootstrapFile(cfg.AgentsFile, cfg.WorkspaceDir, "AGENTS.md", agent.DefaultAgentInstructions),
			ToolNotes: loadBootstrapFile(cfg.ToolsFile, cfg.WorkspaceDir, "TOOLS.md", agent.DefaultToolNotes),
			BootstrapMaxChars: cfg.BootstrapMaxChars,
			BootstrapTotalMaxChars: cfg.BootstrapTotalMaxChars,
			HistoryMax: cfg.HistoryMax,
			VectorK: cfg.VectorK,
			FTSK: cfg.FTSK,
			TopK: cfg.MemoryRetrieve,
		},
		Artifacts: art,
		MaxToolBytes: cfg.MaxToolBytes,
		MaxToolLoops: cfg.MaxToolLoops,
		Deliver: del,
	}

	// cron service + tool
	var cronSvc *cron.Service
	if cfg.Cron.Enabled {
		cronSvc = cron.New(cfg.Cron.StorePath, agent.CronRunner(b, cfg.DefaultSessionKey))
		if err := cronSvc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "cron start error:", err)
			os.Exit(1)
		}
		reg.Register(&tools.CronTool{Svc: cronSvc})
	}

	switch cmd {
	case "chat":
		runWorkers(ctx, b, rt, cfg.WorkerCount)
		ch := &cli.Channel{Bus: b, SessionKey: cfg.DefaultSessionKey}
		if err := ch.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "cli error:", err)
		}
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
		// or3-intern migrate-jsonl /path/to/session.jsonl cli:default
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: or3-intern migrate-jsonl <jsonl_path> [session_key]")
			os.Exit(2)
		}
		sessionKey := "migrated:default"
		if len(args) >= 3 { sessionKey = args[2] }
		if err := migrateJSONL(ctx, d, args[1], sessionKey); err != nil {
			fmt.Fprintln(os.Stderr, "migration error:", err)
			os.Exit(1)
		}
		fmt.Println("ok")
	case "version":
		fmt.Println("or3-intern v1")
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}

	if cronSvc != nil { cronSvc.Stop() }
}

func cfgPathOrDefault(p string) string {
	if p != "" { return p }
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

func allowedRoot(cfg config.Config) string {
	if cfg.Tools.RestrictToWorkspace {
		if cfg.WorkspaceDir != "" { return cfg.WorkspaceDir }
	}
	if cfg.AllowedDir != "" { return cfg.AllowedDir }
	return ""
}

func runWorkers(ctx context.Context, b *bus.Bus, rt *agent.Runtime, n int) {
	if n <= 0 { n = 4 }
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
	if strings.TrimSpace(path) == "" { return nil }
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { return err }
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}
