package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

type helpCommand struct {
	Usage       string
	Summary     string
	Description []string
	Subcommands []helpItem
	Flags       []helpItem
	Examples    []string
}

type helpItem struct {
	Name        string
	Description string
}

var rootHelpSections = []struct {
	Title string
	Items []helpItem
}{
	{
		Title: "Simple commands",
		Items: []helpItem{
			{Name: "setup", Description: "Guided first-run setup with scenario and safety choices"},
			{Name: "chat", Description: "Start chatting with OR3"},
			{Name: "status", Description: "Check what OR3 can access and what needs attention"},
			{Name: "settings", Description: "Review and update your settings"},
			{Name: "connect-device", Description: "Pair a phone or other device"},
			{Name: "help", Description: "Show help for simple or advanced commands"},
		},
	},
	{
		Title: "Advanced commands",
		Items: []helpItem{
			{Name: "configure", Description: "Interactive configuration wizard for setup and later edits"},
			{Name: "init", Description: "Guided first-run setup for config and provider settings"},
			{Name: "config-path", Description: "Print the resolved config.json path"},
			{Name: "chat", Description: "Interactive CLI session"},
			{Name: "serve", Description: "Run enabled channels, triggers, heartbeat, cron, and workers"},
			{Name: "service", Description: "Run the authenticated internal HTTP API"},
			{Name: "agent", Description: "Run a one-shot foreground turn"},
			{Name: "version", Description: "Print the binary version"},
		},
	},
	{
		Title: "Operator tools",
		Items: []helpItem{
			{Name: "doctor", Description: "Diagnose readiness issues, explain risk, and repair safe local problems"},
			{Name: "capabilities", Description: "Inspect runtime posture, ingress policy, approvals, and profiles"},
			{Name: "embeddings", Description: "Inspect or rebuild stored memory and doc embeddings after provider/model changes"},
			{Name: "secrets", Description: "Manage encrypted secret references stored in SQLite"},
			{Name: "audit", Description: "Inspect or verify the append-only audit chain"},
			{Name: "skills", Description: "List, inspect, search, install, update, check, and remove skills"},
			{Name: "approvals", Description: "Inspect and resolve approval requests and allowlists"},
			{Name: "devices", Description: "Inspect paired devices and legacy pairing request helpers"},
			{Name: "pairing", Description: "Manage first-class pairing workflows"},
			{Name: "scope", Description: "Link session keys to a shared history scope"},
			{Name: "migrate-jsonl", Description: "Import legacy session history from JSONL"},
			{Name: "migrate-openclaw", Description: "Import a local OpenClaw agent into or3-intern files, daily notes, and dreams"},
		},
	},
}

var helpTopics = map[string]helpCommand{
	"configure": {
		Usage:   "or3-intern configure [--section provider|storage|workspace|web|channels|service] ...",
		Summary: "Interactive configuration wizard for first-run setup and later edits.",
		Description: []string{
			"Loads the active config when present, shows a short summary, and prompts only for the sections you want to change.",
			"When stdin and stdout are terminals, configure opens the Bubble Tea setup UI with arrow-key navigation, enter to select, space to toggle, s to save, and q to quit.",
			"When either side is non-interactive, configure stays in the plain text prompt flow so pipes, redirected input, and scripts keep working.",
			"The provider section can also set an optional embedding dimensions override for providers/models that support truncated embedding vectors.",
			"Use repeatable --section flags for targeted updates, or run without flags to choose sections interactively.",
		},
		Flags: []helpItem{
			{Name: "--section <name>", Description: "Repeatable section filter: provider, storage, workspace, web, channels, service"},
		},
		Examples: []string{"or3-intern configure", "or3-intern configure --section provider --section web", "or3-intern configure --section channels"},
	},
	"setup": {
		Usage:   "or3-intern setup",
		Summary: "Guided setup using plain-language scenario and safety choices.",
		Description: []string{
			"Setup asks where you are using OR3, how careful it should be, and which folder it should stay inside.",
			"It translates those answers into the existing runtime profile, approvals, audit, service, and hardening settings.",
		},
		Examples: []string{"or3-intern setup"},
	},
	"settings": {
		Usage:   "or3-intern settings [--section provider|workspace|devices|safety|channels|tools|memory|advanced] [--export path|-] [--advanced]",
		Summary: "Open the settings flow for reviewing and updating your setup.",
		Description: []string{
			"Shows a task-based settings home for AI Provider, Workspace Folder, Connected Devices, Safety Level, Channels, Tools, Memory, and Advanced.",
			"Use --section to jump to a task, or --export to write the current advanced JSON config without making JSON editing the default path.",
		},
		Flags: []helpItem{
			{Name: "--section <name>", Description: "Open a task section: provider, workspace, devices, safety, channels, tools, memory, advanced"},
			{Name: "--export <path|->", Description: "Export current config JSON to a file or stdout"},
			{Name: "--advanced", Description: "Show advanced settings actions on the home screen"},
		},
		Examples: []string{"or3-intern settings", "or3-intern settings --section safety", "or3-intern settings --export config.json"},
	},
	"status": {
		Usage:   "or3-intern status [--advanced] [--fix number|all|finding-id]",
		Summary: "Show a friendly safety and access summary, plus problems that need attention.",
		Flags: []helpItem{
			{Name: "--advanced", Description: "Include internal finding IDs in the output"},
			{Name: "--fix <number|all|finding-id>", Description: "Apply a safe automatic repair"},
		},
		Examples: []string{"or3-intern status", "or3-intern status --fix 1", "or3-intern status --fix all"},
	},
	"connect-device": {
		Usage:    "or3-intern connect-device [list|disconnect <device-id>|role <device-id>]",
		Summary:  "Pair a phone or other device using a short code and simple access levels.",
		Examples: []string{"or3-intern connect-device", "or3-intern connect-device list"},
	},
	"init": {
		Usage:   "or3-intern init",
		Summary: "Guided first-run setup alias.",
		Description: []string{
			"Runs the same configure wizard with the original first-run sections: provider, storage, workspace, and web.",
			"Like configure, it opens the Bubble Tea UI only on an interactive terminal and falls back to plain text prompts when used non-interactively.",
			"Use `or3-intern configure` directly when you want channels, service, or custom section selection.",
		},
		Examples: []string{"or3-intern init"},
	},
	"config-path": {
		Usage:   "or3-intern config-path",
		Summary: "Print the resolved path to config.json.",
		Description: []string{
			"Respects --config when provided; otherwise prints the default path under ~/.or3-intern/.",
		},
		Examples: []string{"or3-intern config-path", "or3-intern --config /tmp/or3.json config-path"},
	},
	"chat": {
		Usage:   "or3-intern chat",
		Summary: "Start the interactive terminal chat UI.",
		Description: []string{
			"This is the default command when no command is provided.",
			"Inside chat, use /new to archive the current session into memory and then clear the live message history for a fresh conversation.",
			"Use /status to inspect message counts, consolidation distance, context token pressure, retrieval settings, and tool limits.",
		},
		Examples: []string{"or3-intern chat"},
	},
	"serve": {
		Usage:    "or3-intern serve",
		Summary:  "Run enabled channels, triggers, heartbeat jobs, cron, and the shared worker runtime.",
		Examples: []string{"or3-intern serve"},
	},
	"service": {
		Usage:    "or3-intern service",
		Summary:  "Run the authenticated internal HTTP API used by OR3 Net.",
		Examples: []string{"or3-intern service"},
	},
	"agent": {
		Usage:   "or3-intern agent -m <message> [-s session] [--approval-token token]",
		Summary: "Run a one-shot foreground turn without entering interactive chat.",
		Flags: []helpItem{
			{Name: "-m <message>", Description: "Message to send to the agent"},
			{Name: "-s <session>", Description: "Session key to use"},
			{Name: "--approval-token <token>", Description: "One-shot approval token to attach to the request"},
		},
		Examples: []string{"or3-intern agent -m \"hello\"", "or3-intern agent -m \"summarize this repo\" -s review"},
	},
	"doctor": {
		Usage:   "or3-intern doctor [--strict] [--json] [--fix] [--interactive] [--probe] [--area name] [--severity level] [--fixable-only]",
		Summary: "Diagnose readiness and safety issues, then repair the safe ones.",
		Description: []string{
			"Doctor is the main readiness command for config, local runtime prerequisites, ingress posture, and hardened startup checks.",
			"Use --fix for deterministic automatic repairs such as missing directories, key files, or conservative channel ingress defaults.",
			"Use --fix --interactive when multiple valid repairs exist, such as choosing pairing vs allowlist for an enabled channel.",
		},
		Flags: []helpItem{
			{Name: "--strict", Description: "Exit non-zero when warnings are found"},
			{Name: "--json", Description: "Emit a structured JSON report"},
			{Name: "--fix", Description: "Apply safe automatic fixes where available"},
			{Name: "--interactive", Description: "Prompt for guided fixes on ambiguous findings"},
			{Name: "--probe", Description: "Run bounded local runtime probes"},
			{Name: "--area <name>", Description: "Repeatable area filter"},
			{Name: "--severity <level>", Description: "Minimum severity filter: info, warn, error, block"},
			{Name: "--fixable-only", Description: "Show only findings with available fixes"},
		},
		Examples: []string{"or3-intern doctor", "or3-intern doctor --strict", "or3-intern doctor --fix", "or3-intern doctor --fix --interactive", "or3-intern doctor --json --severity error"},
	},
	"capabilities": {
		Usage:   "or3-intern capabilities [--channel name] [--trigger name] [--json]",
		Summary: "Inspect the effective runtime posture, ingress policy, approvals, and access profiles.",
		Flags: []helpItem{
			{Name: "--channel <name>", Description: "Filter report to a specific channel"},
			{Name: "--trigger <name>", Description: "Filter report to a specific trigger"},
			{Name: "--json", Description: "Emit JSON instead of human-readable text"},
		},
		Examples: []string{"or3-intern capabilities", "or3-intern capabilities --channel slack --json"},
	},
	"embeddings": {
		Usage:   "or3-intern embeddings <status|rebuild> [memory|docs|all]",
		Summary: "Inspect or rebuild persisted embedding state after provider or embedding-model changes.",
		Description: []string{
			"Use status to compare the stored memory-vector fingerprint against the current provider API base and embedding model.",
			"Use rebuild memory after changing embedding providers or models so existing long-term memory vectors are regenerated in the new embedding space.",
			"Use rebuild docs or rebuild all when document indexing is enabled and you want indexed file embeddings refreshed too.",
		},
		Subcommands: []helpItem{
			{Name: "status", Description: "Show stored vector dims, stored embedding fingerprint, current fingerprint, and mismatch status"},
			{Name: "rebuild [memory|docs|all]", Description: "Regenerate persisted embeddings for memory notes, indexed docs, or both"},
		},
		Examples: []string{"or3-intern embeddings status", "or3-intern embeddings rebuild memory", "or3-intern embeddings rebuild all"},
	},
	"secrets": {
		Usage:   "or3-intern secrets <set|delete|list> ...",
		Summary: "Manage encrypted secret references stored in SQLite.",
		Subcommands: []helpItem{
			{Name: "set <name> <value>", Description: "Store or replace a secret"},
			{Name: "delete <name>", Description: "Delete a stored secret"},
			{Name: "list", Description: "List stored secret names"},
		},
		Examples: []string{"or3-intern secrets set provider.openai sk-...", "or3-intern secrets list"},
	},
	"audit": {
		Usage:       "or3-intern audit [verify]",
		Summary:     "Inspect or verify the append-only audit chain.",
		Subcommands: []helpItem{{Name: "verify", Description: "Verify the audit chain and report integrity status"}},
		Examples:    []string{"or3-intern audit", "or3-intern audit verify"},
	},
	"approvals": {
		Usage:   "or3-intern approvals <list|show|approve|deny|allowlist> ...",
		Summary: "Inspect and resolve approval requests and allowlist rules.",
		Description: []string{
			"All approval subcommands work directly against the local SQLite database.",
			"Use this for command, secret, skill, and message permission prompts.",
			"For phone/browser device connection requests, use `or3-intern pairing`, not `or3-intern approvals`.",
		},
		Subcommands: []helpItem{
			{Name: "list [status]", Description: "List approval requests, optionally filtered by status"},
			{Name: "show <id>", Description: "Show one approval request in detail"},
			{Name: "approve <id> [--allowlist] [--note text]", Description: "Approve a request and optionally create a matching allowlist rule"},
			{Name: "deny <id> [--note text]", Description: "Deny a request"},
			{Name: "allowlist <list|add|remove>", Description: "Manage persistent allowlist rules"},
		},
		Examples: []string{"or3-intern approvals list pending", "or3-intern approvals approve 42 --allowlist", "or3-intern pairing list pending"},
	},
	"approvals approve": {
		Usage:   "or3-intern approvals approve <id> [--allowlist] [--note text]",
		Summary: "Approve a pending approval request.",
		Flags: []helpItem{
			{Name: "--allowlist", Description: "Create a matching persistent allowlist rule"},
			{Name: "--note <text>", Description: "Attach a free-text resolution note"},
		},
		Examples: []string{"or3-intern approvals approve 42", "or3-intern approvals approve 42 --allowlist --note \"reviewed\""},
	},
	"approvals deny": {
		Usage:    "or3-intern approvals deny <id> [--note text]",
		Summary:  "Deny a pending approval request.",
		Flags:    []helpItem{{Name: "--note <text>", Description: "Attach a free-text resolution note"}},
		Examples: []string{"or3-intern approvals deny 42", "or3-intern approvals deny 42 --note \"blocked\""},
	},
	"approvals allowlist": {
		Usage:   "or3-intern approvals allowlist <list|add|remove> ...",
		Summary: "Manage persistent approval allowlist rules.",
		Subcommands: []helpItem{
			{Name: "list [domain]", Description: "List active allowlist rules"},
			{Name: "add [flags]", Description: "Create a new allowlist rule"},
			{Name: "remove <id>", Description: "Disable an allowlist rule by ID"},
		},
		Examples: []string{"or3-intern approvals allowlist list exec", "or3-intern approvals allowlist add --domain exec --program /bin/echo"},
	},
	"approvals allowlist add": {
		Usage:   "or3-intern approvals allowlist add [--domain exec|skill_execution] [flags]",
		Summary: "Create a persistent allowlist rule.",
		Flags: []helpItem{
			{Name: "--domain <name>", Description: "Approval domain; defaults to exec"},
			{Name: "--host <id>", Description: "Host scope; defaults to the current host ID"},
			{Name: "--tool <name>", Description: "Tool scope"},
			{Name: "--profile <name>", Description: "Access-profile scope"},
			{Name: "--agent <id>", Description: "Agent scope"},
			{Name: "--program <path>", Description: "Exact executable path for exec rules"},
			{Name: "--cwd <path>", Description: "Working-directory constraint for exec rules"},
			{Name: "--skill <id>", Description: "Skill identifier for skill_execution rules"},
			{Name: "--version <v>", Description: "Skill version constraint"},
			{Name: "--origin <registry>", Description: "Skill origin or registry constraint"},
			{Name: "--trust <state>", Description: "Skill trust-state constraint"},
		},
		Examples: []string{"or3-intern approvals allowlist add --domain exec --program /usr/bin/git", "or3-intern approvals allowlist add --domain skill_execution --skill demo --version 1.0.0"},
	},
	"devices": {
		Usage:   "or3-intern devices <list|requests|approve|deny|rotate|revoke> ...",
		Summary: "Inspect paired devices and legacy pairing request helpers.",
		Description: []string{
			"Use `devices list` after pairing is complete to review or manage connected phones, tablets, or apps.",
			"For the actual pairing approval step, `or3-intern pairing` is the clearest command group.",
		},
		Subcommands: []helpItem{
			{Name: "list", Description: "List paired devices"},
			{Name: "requests [status]", Description: "List pairing requests, optionally filtered by status"},
			{Name: "approve <pairing-request-id>", Description: "Approve a pairing request"},
			{Name: "deny <pairing-request-id>", Description: "Deny a pairing request"},
			{Name: "rotate <device-id>", Description: "Rotate a device token and print the new token"},
			{Name: "revoke <device-id>", Description: "Revoke a paired device immediately"},
		},
		Examples: []string{"or3-intern devices list", "or3-intern devices rotate dev_123"},
	},
	"pairing": {
		Usage:   "or3-intern pairing <list|request|approve-code|approve|deny|exchange> ...",
		Summary: "Manage first-class pairing workflows, including channel-bound identities.",
		Description: []string{
			"Use this when the OR3 app or another device asks to connect.",
			"Easiest browser/app approval flow:",
			"1) In the app, tap 'Get pairing code'.",
			"2) On the computer, run `or3-intern pairing approve-code 123456` using the code shown in the app.",
			"3) Go back to the app. It should finish connecting by itself.",
			"Advanced fallback: run `or3-intern pairing list pending`, then `or3-intern pairing approve <request-id>`.",
		},
		Subcommands: []helpItem{
			{Name: "list [status]", Description: "List pairing requests"},
			{Name: "request [flags]", Description: "Create a pairing request"},
			{Name: "approve-code <6-digit-code>", Description: "Approve the waiting device using the code shown in the app"},
			{Name: "approve <request-id>", Description: "Approve a pairing request"},
			{Name: "deny <request-id>", Description: "Deny a pairing request"},
			{Name: "exchange <request-id> <code>", Description: "Exchange a pairing code for a device token"},
		},
		Examples: []string{"or3-intern pairing approve-code 123456", "or3-intern pairing list pending", "or3-intern pairing approve 12", "or3-intern pairing request --channel slack --identity U42 --name \"Slack User\""},
	},
	"pairing request": {
		Usage:   "or3-intern pairing request [--role role] [--name text] [--origin text] [--device id] [--channel name --identity id]",
		Summary: "Create a new pairing request.",
		Flags: []helpItem{
			{Name: "--role <role>", Description: "Device role; defaults to operator"},
			{Name: "--name <text>", Description: "Display name"},
			{Name: "--origin <text>", Description: "Origin description"},
			{Name: "--device <id>", Description: "Explicit device ID"},
			{Name: "--channel <name>", Description: "Channel name to bind"},
			{Name: "--identity <id>", Description: "Channel identity to bind"},
		},
		Examples: []string{"or3-intern pairing request --name \"Laptop\"", "or3-intern pairing request --channel slack --identity U42 --name \"Slack User\""},
	},
	"skills": {
		Usage:   "or3-intern skills <list|info|check|search|install|update|remove> ...",
		Summary: "List, inspect, search, install, update, check, and remove skills.",
		Subcommands: []helpItem{
			{Name: "list [--eligible]", Description: "List discovered skills"},
			{Name: "info <name>", Description: "Show metadata, permission state, and policy notes"},
			{Name: "check", Description: "Validate available skills and report policy state"},
			{Name: "search <query>", Description: "Search configured skill registries"},
			{Name: "install <slug> [--version v] [--force]", Description: "Install a skill into the managed directory"},
			{Name: "update <name>|--all [--version v] [--force]", Description: "Update one or more managed skill installs"},
			{Name: "remove <name>", Description: "Remove a managed install"},
		},
		Examples: []string{"or3-intern skills list --eligible", "or3-intern skills install demo --version 1.0.0"},
	},
	"skills list": {
		Usage:    "or3-intern skills list [--eligible]",
		Summary:  "List discovered skills.",
		Flags:    []helpItem{{Name: "--eligible", Description: "Show only eligible skills"}},
		Examples: []string{"or3-intern skills list", "or3-intern skills list --eligible"},
	},
	"skills install": {
		Usage:   "or3-intern skills install <slug> [--version v] [--force]",
		Summary: "Install a skill into the managed directory.",
		Flags: []helpItem{
			{Name: "--version <v>", Description: "Install a specific skill version"},
			{Name: "--force", Description: "Overwrite local modifications when installing"},
		},
		Examples: []string{"or3-intern skills install demo", "or3-intern skills install demo --version 1.0.0 --force"},
	},
	"skills update": {
		Usage:   "or3-intern skills update <name>|--all [--version v] [--force]",
		Summary: "Update one or more managed skill installs.",
		Flags: []helpItem{
			{Name: "--all", Description: "Update all installed skills"},
			{Name: "--version <v>", Description: "Target version to install"},
			{Name: "--force", Description: "Overwrite local modifications when updating"},
		},
		Examples: []string{"or3-intern skills update demo", "or3-intern skills update --all"},
	},
	"scope": {
		Usage:   "or3-intern scope <link|list|resolve> ...",
		Summary: "Link multiple session keys to a shared history scope.",
		Subcommands: []helpItem{
			{Name: "link <session-key> <scope-key>", Description: "Link a session key to a scope"},
			{Name: "list <scope-key>", Description: "List session keys attached to a scope"},
			{Name: "resolve <session-key>", Description: "Resolve the scope for a session key"},
		},
		Examples: []string{"or3-intern scope link session-a team-alpha", "or3-intern scope resolve session-a"},
	},
	"migrate-jsonl": {
		Usage:    "or3-intern migrate-jsonl <path-to-session.jsonl> [session_key]",
		Summary:  "Import legacy session history from a JSONL transcript.",
		Examples: []string{"or3-intern migrate-jsonl /tmp/session.jsonl", "or3-intern migrate-jsonl /tmp/session.jsonl imported:demo"},
	},
	"migrate-openclaw": {
		Usage:   "or3-intern migrate-openclaw [--scope <scope-key>] [--embed-max-bytes <n>] <openclaw-agent-dir>",
		Summary: "Import SOUL.md, IDENTITY.md, MEMORY.md, USER.md, daily notes, and dreams from a local OpenClaw agent.",
		Description: []string{
			"Core bootstrap files are copied into the configured or3-intern destinations.",
			"OpenClaw memory/*.md files plus DREAMS.md and memory/.dreams/* are imported as durable memory notes using conservative chunking so embedding requests stay bounded.",
			"The command is repeatable: previously imported OpenClaw daily notes for the same source agent are replaced before new notes are inserted.",
		},
		Flags: []helpItem{
			{Name: "--scope <scope-key>", Description: "Memory scope for imported daily notes; defaults to global shared memory"},
			{Name: "--embed-max-bytes <n>", Description: "Maximum bytes per imported memory chunk before embedding"},
		},
		Examples: []string{"or3-intern migrate-openclaw ~/.openclaw/agents/main", "or3-intern migrate-openclaw --scope global ~/agent-export"},
	},
	"version": {
		Usage:    "or3-intern version",
		Summary:  "Print the binary version.",
		Examples: []string{"or3-intern version"},
	},
}

func parseRootCLIArgs(argv []string, stderr io.Writer) (string, []string, bool, bool, bool, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	fs := flag.NewFlagSet("or3-intern", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {}
	var cfgPath string
	var showHelp bool
	var unsafeDev bool
	var advanced bool
	fs.StringVar(&cfgPath, "config", "", "path to config.json")
	fs.BoolVar(&showHelp, "help", false, "show help")
	fs.BoolVar(&showHelp, "h", false, "show help")
	fs.BoolVar(&unsafeDev, "unsafe-dev", false, "bypass startup safety gates for local development")
	fs.BoolVar(&advanced, "advanced", false, "accepted for compatibility; root help is always complete")
	if err := fs.Parse(argv); err != nil {
		return "", nil, false, false, false, err
	}
	return cfgPath, fs.Args(), showHelp, unsafeDev, advanced, nil
}

func maybeHandleHelpRequest(args []string, stdout io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "help") {
		return true, printHelpTopic(stdout, helpTopicPath(args[1:]))
	}
	for _, arg := range args[1:] {
		if isHelpToken(arg) {
			return true, printHelpTopic(stdout, helpTopicPath(args))
		}
	}
	return false, nil
}

func isHelpToken(arg string) bool {
	switch strings.TrimSpace(arg) {
	case "-h", "--help":
		return true
	default:
		return false
	}
}

func helpTopicPath(args []string) []string {
	path := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == "" || isHelpToken(arg) || strings.HasPrefix(arg, "-") {
			break
		}
		path = append(path, arg)
	}
	return path
}

func printHelpTopic(w io.Writer, path []string) error {
	if w == nil {
		w = io.Discard
	}
	if len(path) == 0 {
		printRootHelp(w)
		return nil
	}
	if len(path) == 1 && path[0] == "advanced" {
		printAdvancedRootHelp(w)
		return nil
	}
	key, ok := bestHelpTopicKey(path)
	if !ok {
		return fmt.Errorf("unknown help topic: %s", strings.Join(path, " "))
	}
	renderHelpTopic(w, helpTopics[key])
	return nil
}

func bestHelpTopicKey(path []string) (string, bool) {
	for i := len(path); i >= 1; i-- {
		key := strings.Join(path[:i], " ")
		if _, ok := helpTopics[key]; ok {
			return key, true
		}
	}
	return "", false
}

func printRootHelp(w io.Writer) {
	printRootHelpMode(w)
}

func printAdvancedRootHelp(w io.Writer) {
	printRootHelpMode(w)
}

func printRootHelpMode(w io.Writer) {
	_, _ = fmt.Fprintln(w, "or3-intern is a local-first agent runtime with chat, channels, memory, approvals, and service APIs.")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  or3-intern [--config path] [--unsafe-dev] <command> [options]")
	_, _ = fmt.Fprintln(w, "  or3-intern")
	_, _ = fmt.Fprintln(w, "  or3-intern help [command]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Without a command, or3-intern starts interactive chat.")
	for _, section := range rootHelpSections {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "%s:\n", section.Title)
		printHelpItems(w, section.Items)
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Global flags:")
	printHelpItems(w, []helpItem{{Name: "--config <path>", Description: "Path to config.json"}, {Name: "--unsafe-dev", Description: "Bypass startup safety gates for local development"}, {Name: "--advanced", Description: "Accepted for compatibility; root help is always complete"}, {Name: "-h, --help", Description: "Show help for the root command or a subcommand"}})
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Examples:")
	for _, example := range []string{"or3-intern setup", "or3-intern chat", "or3-intern status", "or3-intern connect-device", "or3-intern approvals list pending"} {
		_, _ = fmt.Fprintf(w, "  %s\n", example)
	}
}

func renderHelpTopic(w io.Writer, cmd helpCommand) {
	_, _ = fmt.Fprintf(w, "Usage:\n  %s\n", cmd.Usage)
	if strings.TrimSpace(cmd.Summary) != "" {
		_, _ = fmt.Fprintf(w, "\n%s\n", cmd.Summary)
	}
	if len(cmd.Description) > 0 {
		_, _ = fmt.Fprintln(w)
		for _, line := range cmd.Description {
			_, _ = fmt.Fprintln(w, line)
		}
	}
	if len(cmd.Subcommands) > 0 {
		_, _ = fmt.Fprintln(w, "\nSubcommands:")
		printHelpItems(w, cmd.Subcommands)
	}
	if len(cmd.Flags) > 0 {
		_, _ = fmt.Fprintln(w, "\nFlags:")
		printHelpItems(w, cmd.Flags)
	}
	if len(cmd.Examples) > 0 {
		_, _ = fmt.Fprintln(w, "\nExamples:")
		for _, example := range cmd.Examples {
			_, _ = fmt.Fprintf(w, "  %s\n", example)
		}
	}
}

func printHelpItems(w io.Writer, items []helpItem) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, item := range items {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", item.Name, item.Description)
	}
	_ = tw.Flush()
}
