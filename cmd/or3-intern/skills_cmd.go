package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"or3-intern/internal/clawhub"
	"or3-intern/internal/config"
	"or3-intern/internal/mcp"
	"or3-intern/internal/skills"
)

type skillsCommandDeps struct {
	Client        *clawhub.Client
	LoadToolNames func(context.Context, config.Config) map[string]struct{}
	LoadInventory func(toolNames map[string]struct{}) skills.Inventory
	Audit         func(context.Context, string, any) error
	Stdout        io.Writer
	Stderr        io.Writer
}

func runSkillsCommand(ctx context.Context, cfg config.Config, bundledDir string, args []string, stdout, stderr io.Writer) error {
	deps := skillsCommandDeps{
		Client: newClawHubClient(cfg),
		LoadToolNames: func(ctx context.Context, cfg config.Config) map[string]struct{} {
			return loadAvailableToolNamesWithManager(ctx, cfg, nil)
		},
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			return buildSkillsInventory(cfg, bundledDir, toolNames)
		},
		Stdout: stdout,
		Stderr: stderr,
	}
	return runSkillsCommandWithDeps(ctx, cfg, args, deps)
}

func runSkillsCommandWithDeps(ctx context.Context, cfg config.Config, args []string, deps skillsCommandDeps) error {
	if deps.Client == nil {
		deps.Client = newClawHubClient(cfg)
	}
	if deps.LoadToolNames == nil {
		deps.LoadToolNames = func(ctx context.Context, cfg config.Config) map[string]struct{} {
			return loadAvailableToolNamesWithManager(ctx, cfg, nil)
		}
	}
	if deps.LoadInventory == nil {
		return fmt.Errorf("skills inventory loader not configured")
	}
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: or3-intern skills <list|info|check|search|install|update|remove> ...")
	}

	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("skills list", flag.ContinueOnError)
		fs.SetOutput(deps.Stderr)
		eligibleOnly := fs.Bool("eligible", false, "show only eligible skills")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		inv := deps.LoadInventory(deps.LoadToolNames(ctx, cfg))
		if len(inv.Skills) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no skills found)")
			return nil
		}
		for _, skill := range inv.Skills {
			if *eligibleOnly && !skill.Eligible {
				continue
			}
			status := "eligible"
			switch {
			case skill.ParseError != "":
				status = "parse-error"
			case skill.Disabled:
				status = "disabled"
			case !skill.Eligible:
				status = "ineligible"
			case skill.Hidden:
				status = "hidden"
			}
			permissionState := strings.TrimSpace(skill.PermissionState)
			if permissionState == "" {
				permissionState = "approved"
			}
			_, _ = fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\t%s\t%s\n", skill.Name, status, permissionState, skill.Source, skill.Dir)
		}
		return nil
	case "info":
		if len(args) < 2 {
			return fmt.Errorf("usage: or3-intern skills info <name>")
		}
		inv := deps.LoadInventory(deps.LoadToolNames(ctx, cfg))
		skill, ok := inv.Get(args[1])
		if !ok {
			return fmt.Errorf("skill not found: %s", args[1])
		}
		_, _ = fmt.Fprintf(deps.Stdout, "Name: %s\n", skill.Name)
		_, _ = fmt.Fprintf(deps.Stdout, "Description: %s\n", skill.Description)
		_, _ = fmt.Fprintf(deps.Stdout, "Source: %s\n", skill.Source)
		_, _ = fmt.Fprintf(deps.Stdout, "Location: %s\n", skill.Dir)
		if skill.Homepage != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Homepage: %s\n", skill.Homepage)
		}
		_, _ = fmt.Fprintf(deps.Stdout, "Eligible: %t\n", skill.Eligible)
		_, _ = fmt.Fprintf(deps.Stdout, "User Invocable: %t\n", skill.UserInvocable)
		if strings.TrimSpace(skill.PermissionState) != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Permission State: %s\n", skill.PermissionState)
			_, _ = fmt.Fprintf(deps.Stdout, "Declared Permissions: %s\n", skill.Permissions.Summary())
			printReasons(deps.Stdout, "Permission Notes", skill.PermissionNotes)
		}
		if skill.Hidden {
			_, _ = fmt.Fprintln(deps.Stdout, "Model Visibility: hidden")
		}
		if skill.CommandDispatch != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Command Dispatch: %s\n", skill.CommandDispatch)
			_, _ = fmt.Fprintf(deps.Stdout, "Command Tool: %s\n", skill.CommandTool)
			_, _ = fmt.Fprintf(deps.Stdout, "Command Arg Mode: %s\n", skill.CommandArgMode)
		}
		printReasons(deps.Stdout, "Missing", skill.Missing)
		printReasons(deps.Stdout, "Unsupported", skill.Unsupported)
		if skill.ParseError != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Parse Error: %s\n", skill.ParseError)
		}
		return nil
	case "check":
		inv := deps.LoadInventory(deps.LoadToolNames(ctx, cfg))
		if len(inv.Skills) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no skills found)")
			return nil
		}
		for _, skill := range inv.Skills {
			if skill.PermissionState == "quarantined" {
				_, _ = fmt.Fprintf(deps.Stdout, "[quarantined] %s: %s\n", skill.Name, strings.Join(skill.PermissionNotes, "; "))
				continue
			}
			if skill.PermissionState == "blocked" {
				reasons := append([]string{}, skill.PermissionNotes...)
				reasons = append(reasons, skill.Missing...)
				reasons = append(reasons, skill.Unsupported...)
				if skill.ParseError != "" {
					reasons = append(reasons, skill.ParseError)
				}
				_, _ = fmt.Fprintf(deps.Stdout, "[blocked] %s: %s\n", skill.Name, strings.Join(reasons, "; "))
				continue
			}
			if skill.Eligible {
				_, _ = fmt.Fprintf(deps.Stdout, "[ok] %s\n", skill.Name)
				continue
			}
			reasons := append([]string{}, skill.Missing...)
			reasons = append(reasons, skill.Unsupported...)
			if skill.ParseError != "" {
				reasons = append(reasons, skill.ParseError)
			}
			_, _ = fmt.Fprintf(deps.Stdout, "[blocked] %s: %s\n", skill.Name, strings.Join(reasons, "; "))
		}
		return nil
	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: or3-intern skills search <query>")
		}
		results, err := deps.Client.Search(ctx, strings.Join(args[1:], " "), 10)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no results)")
			return nil
		}
		for _, result := range results {
			version := result.Version
			if version == "" {
				version = "latest"
			}
			_, _ = fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", result.Slug, version, strings.TrimSpace(result.DisplayName))
		}
		return nil
	case "install":
		fs := flag.NewFlagSet("skills install", flag.ContinueOnError)
		fs.SetOutput(deps.Stderr)
		version := fs.String("version", "", "skill version")
		force := fs.Bool("force", false, "overwrite local modifications")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: or3-intern skills install <slug> [--version v]")
		}
		result, err := deps.Client.Install(ctx, fs.Arg(0), *version, resolveInstallRoot(cfg), clawhub.InstallOptions{Force: *force})
		if err != nil {
			return err
		}
		if deps.Audit != nil {
			if err := deps.Audit(ctx, "skill.install", map[string]any{"slug": result.Slug, "version": result.Version, "path": result.Path}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(deps.Stdout, "installed\t%s\t%s\t%s\n", result.Slug, result.Version, result.Path)
		return nil
	case "update":
		fs := flag.NewFlagSet("skills update", flag.ContinueOnError)
		fs.SetOutput(deps.Stderr)
		all := fs.Bool("all", false, "update all installed skills")
		version := fs.String("version", "", "target version")
		force := fs.Bool("force", false, "overwrite local modifications")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		root := resolveInstallRoot(cfg)
		installed, err := clawhub.ListInstalled(root)
		if err != nil {
			return err
		}
		targets := installed
		if !*all {
			if fs.NArg() < 1 {
				return fmt.Errorf("usage: or3-intern skills update <name>|--all")
			}
			match, matchErr := findInstalledSkill(installed, fs.Arg(0))
			if matchErr != nil {
				return matchErr
			}
			targets = []clawhub.InstalledSkill{match}
		}
		if len(targets) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no installed skills)")
			return nil
		}
		for _, item := range targets {
			info, err := deps.Client.Inspect(ctx, item.Origin.Slug, *version)
			if err != nil {
				return err
			}
			targetVersion := strings.TrimSpace(*version)
			if targetVersion == "" {
				targetVersion = info.LatestVersion
			}
			if targetVersion == "" {
				return fmt.Errorf("could not resolve latest version for %s", item.Origin.Slug)
			}
			if item.Origin.InstalledVersion == targetVersion {
				_, _ = fmt.Fprintf(deps.Stdout, "up-to-date\t%s\t%s\n", item.Origin.Slug, targetVersion)
				continue
			}
			if _, err := deps.Client.Install(ctx, item.Origin.Slug, targetVersion, root, clawhub.InstallOptions{Force: *force}); err != nil {
				return err
			}
			if deps.Audit != nil {
				if err := deps.Audit(ctx, "skill.update", map[string]any{"slug": item.Origin.Slug, "version": targetVersion}); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(deps.Stdout, "updated\t%s\t%s\n", item.Origin.Slug, targetVersion)
		}
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: or3-intern skills remove <name>")
		}
		root := resolveInstallRoot(cfg)
		installed, err := clawhub.ListInstalled(root)
		if err != nil {
			return err
		}
		match, err := findInstalledSkill(installed, args[1])
		if err != nil {
			return err
		}
		if err := clawhub.RemoveSkill(root, match.Name); err != nil {
			return err
		}
		if deps.Audit != nil {
			if err := deps.Audit(ctx, "skill.remove", map[string]any{"name": match.Name}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(deps.Stdout, "removed\t%s\n", match.Name)
		return nil
	default:
		return fmt.Errorf("unknown skills subcommand: %s", args[0])
	}
}

func buildSkillsInventory(cfg config.Config, bundledDir string, toolNames map[string]struct{}) skills.Inventory {
	approved := make(map[string]struct{}, len(cfg.Skills.Policy.Approved)*2)
	for _, name := range cfg.Skills.Policy.Approved {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		approved[trimmed] = struct{}{}
		approved[strings.ToLower(trimmed)] = struct{}{}
	}
	return skills.ScanWithOptions(skills.LoadOptions{
		Roots:          buildSkillRoots(cfg, bundledDir),
		Entries:        skillEntries(cfg),
		GlobalConfig:   configMap(cfg),
		Env:            envMap(),
		AvailableTools: toolNames,
		ApprovalPolicy: skills.ApprovalPolicy{QuarantineByDefault: cfg.Skills.Policy.QuarantineByDefault, ApprovedSkills: approved},
	})
}

func loadAvailableToolNames(ctx context.Context, cfg config.Config) map[string]struct{} {
	return loadAvailableToolNamesWithManager(ctx, cfg, nil)
}

func loadAvailableToolNamesWithManager(ctx context.Context, cfg config.Config, manager *mcp.Manager) map[string]struct{} {
	toolNames := availableToolNames(cfg.Cron.Enabled, cfg.Subagents.Enabled)
	if len(cfg.Tools.MCPServers) == 0 {
		return toolNames
	}
	if manager != nil {
		for _, name := range manager.ToolNames() {
			toolNames[name] = struct{}{}
		}
		return toolNames
	}
	manager = mcp.NewManager(cfg.Tools.MCPServers)
	manager.SetLogger(log.Printf)
	if err := manager.Connect(ctx); err != nil {
		log.Printf("mcp setup failed: %v", err)
		return toolNames
	}
	defer func() {
		if err := manager.Close(); err != nil {
			log.Printf("mcp close failed: %v", err)
		}
	}()
	for _, name := range manager.ToolNames() {
		toolNames[name] = struct{}{}
	}
	return toolNames
}

func buildSkillRoots(cfg config.Config, bundledDir string) []skills.Root {
	var roots []skills.Root
	for _, extra := range cfg.Skills.Load.ExtraDirs {
		if strings.TrimSpace(extra) == "" {
			continue
		}
		roots = append(roots, skills.Root{Path: extra, Source: skills.SourceExtra})
	}
	if strings.TrimSpace(bundledDir) != "" {
		roots = append(roots, skills.Root{Path: bundledDir, Source: skills.SourceBundled})
	}
	if strings.TrimSpace(cfg.Skills.ManagedDir) != "" {
		roots = append(roots, skills.Root{Path: cfg.Skills.ManagedDir, Source: skills.SourceManaged})
	}
	if strings.TrimSpace(cfg.WorkspaceDir) != "" {
		roots = append(roots,
			skills.Root{Path: filepath.Join(cfg.WorkspaceDir, "workspace_skills"), Source: skills.SourceExtra, Priority: 35},
			skills.Root{Path: filepath.Join(cfg.WorkspaceDir, "skills"), Source: skills.SourceWorkspace},
		)
	}
	return roots
}

func skillEntries(cfg config.Config) map[string]skills.EntryConfig {
	out := make(map[string]skills.EntryConfig, len(cfg.Skills.Entries))
	for key, entry := range cfg.Skills.Entries {
		out[key] = skills.EntryConfig{
			Enabled: entry.Enabled,
			APIKey:  entry.APIKey,
			Env:     entry.Env,
			Config:  entry.Config,
		}
	}
	return out
}

func configMap(cfg config.Config) map[string]any {
	buf, _ := json.Marshal(cfg)
	out := map[string]any{}
	_ = json.Unmarshal(buf, &out)
	return out
}

func envMap() map[string]string {
	out := map[string]string{}
	for _, raw := range os.Environ() {
		key, value, ok := strings.Cut(raw, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func resolveInstallRoot(cfg config.Config) string {
	installDir := strings.TrimSpace(cfg.Skills.ClawHub.InstallDir)
	if installDir == "" {
		installDir = "skills"
	}
	if filepath.IsAbs(installDir) {
		return installDir
	}
	if strings.TrimSpace(cfg.Skills.ManagedDir) != "" {
		return cfg.Skills.ManagedDir
	}
	return filepath.Join(filepath.Dir(config.DefaultPath()), installDir)
}

func availableToolNames(includeCron, includeSubagents bool) map[string]struct{} {
	names := []string{
		"exec",
		"read_file",
		"write_file",
		"edit_file",
		"list_dir",
		"web_fetch",
		"web_search",
		"memory_set_pinned",
		"memory_add_note",
		"memory_search",
		"send_message",
		"read_skill",
		"run_skill_script",
	}
	if includeCron {
		names = append(names, "cron")
	}
	if includeSubagents {
		names = append(names, "spawn_subagent")
	}
	sort.Strings(names)
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}

func newClawHubClient(cfg config.Config) *clawhub.Client {
	return clawhub.New(cfg.Skills.ClawHub.SiteURL, cfg.Skills.ClawHub.RegistryURL)
}

func findInstalledSkill(installed []clawhub.InstalledSkill, raw string) (clawhub.InstalledSkill, error) {
	target := strings.TrimSpace(raw)
	for _, item := range installed {
		if item.Name == target || item.Origin.Slug == target {
			return item, nil
		}
	}
	return clawhub.InstalledSkill{}, fmt.Errorf("installed skill not found: %s", raw)
}

func printReasons(w io.Writer, label string, values []string) {
	if len(values) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "%s: %s\n", label, strings.Join(values, "; "))
}
