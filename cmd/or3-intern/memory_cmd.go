package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memorysvc"
)

type memoryCommandDeps struct {
	NewService func(config.Config, *db.DB) *memorysvc.Service
	Stdout     io.Writer
	Stderr     io.Writer
}

func runMemoryCommandWithDeps(ctx context.Context, cfg config.Config, database *db.DB, args []string, deps memoryCommandDeps) error {
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: or3-intern memory <search|add-note|pinned> ...")
	}
	newService := deps.NewService
	if newService == nil {
		newService = newCLIMemoryService
	}
	svc := newService(cfg, database)
	if svc == nil {
		return fmt.Errorf("memory service unavailable")
	}

	switch args[0] {
	case "search":
		return runMemorySearch(ctx, svc, args[1:], deps)
	case "add-note":
		return runMemoryAddNote(ctx, svc, args[1:], deps)
	case "pinned":
		return runMemoryPinned(ctx, svc, args[1:], deps)
	default:
		return fmt.Errorf("unknown memory subcommand: %s", args[0])
	}
}

func newCLIMemoryService(cfg config.Config, database *db.DB) *memorysvc.Service {
	if database == nil {
		return nil
	}
	return memorysvc.New(cfg, database, newEmbeddingProviderClient(cfg), currentEmbedFingerprint(cfg))
}

func runMemorySearch(ctx context.Context, svc *memorysvc.Service, args []string, deps memoryCommandDeps) error {
	fs := flag.NewFlagSet("memory search", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	session := fs.String("session", "", "session key")
	global := fs.Bool("global", false, "search global memory only")
	topK := fs.Int("top-k", 0, "maximum hits")
	format := fs.String("format", "json", "output format: json or text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireExactFlagArgs(fs, 1, "or3-intern memory search --session <key> [--global] [--top-k N] <query>"); err != nil {
		return err
	}
	sessionKey := strings.TrimSpace(*session)
	query := strings.TrimSpace(fs.Arg(0))
	if sessionKey == "" || query == "" {
		return fmt.Errorf("session and query are required")
	}
	resp, err := svc.Search(ctx, memorysvc.SearchRequest{
		SessionKey: sessionKey,
		Query:      query,
		TopK:       *topK,
		GlobalOnly: *global,
	})
	if err != nil {
		return err
	}
	return writeMemorySearchOutput(deps.Stdout, deps.Stderr, *format, resp)
}

func runMemoryAddNote(ctx context.Context, svc *memorysvc.Service, args []string, deps memoryCommandDeps) error {
	fs := flag.NewFlagSet("memory add-note", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	session := fs.String("session", "", "session key")
	global := fs.Bool("global", false, "store in global memory only")
	tags := fs.String("tags", "", "comma-separated tags")
	format := fs.String("format", "json", "output format: json or text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireExactFlagArgs(fs, 1, "or3-intern memory add-note --session <key> [--global] [--tags a,b] <text>"); err != nil {
		return err
	}
	sessionKey := strings.TrimSpace(*session)
	text := strings.TrimSpace(fs.Arg(0))
	if sessionKey == "" || text == "" {
		return fmt.Errorf("session and text are required")
	}
	resp, err := svc.AddNote(ctx, memorysvc.AddNoteRequest{
		SessionKey: sessionKey,
		Text:       text,
		Tags:       *tags,
		GlobalOnly: *global,
	})
	if err != nil {
		return err
	}
	return writeMemoryAddNoteOutput(deps.Stdout, deps.Stderr, *format, resp)
}

func runMemoryPinned(ctx context.Context, svc *memorysvc.Service, args []string, deps memoryCommandDeps) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: or3-intern memory pinned <get|set> ...")
	}
	switch args[0] {
	case "get":
		return runMemoryPinnedGet(ctx, svc, args[1:], deps)
	case "set":
		return runMemoryPinnedSet(ctx, svc, args[1:], deps)
	default:
		return fmt.Errorf("unknown pinned subcommand: %s", args[0])
	}
}

func runMemoryPinnedGet(ctx context.Context, svc *memorysvc.Service, args []string, deps memoryCommandDeps) error {
	fs := flag.NewFlagSet("memory pinned get", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	session := fs.String("session", "", "session key")
	global := fs.Bool("global", false, "read global pinned memory only")
	key := fs.String("key", "", "single pinned key")
	format := fs.String("format", "json", "output format: json or text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireExactFlagArgs(fs, 0, "or3-intern memory pinned get --session <key> [--global] [--key <k>]"); err != nil {
		return err
	}
	sessionKey := strings.TrimSpace(*session)
	if sessionKey == "" {
		return fmt.Errorf("session is required")
	}
	entries, err := svc.GetPinned(ctx, sessionKey, strings.TrimSpace(*key), *global)
	if err != nil {
		return err
	}
	return writeMemoryPinnedGetOutput(deps.Stdout, *format, entries)
}

func runMemoryPinnedSet(ctx context.Context, svc *memorysvc.Service, args []string, deps memoryCommandDeps) error {
	fs := flag.NewFlagSet("memory pinned set", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	session := fs.String("session", "", "session key")
	global := fs.Bool("global", false, "store in global pinned memory only")
	key := fs.String("key", "", "pinned key")
	format := fs.String("format", "json", "output format: json or text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireExactFlagArgs(fs, 1, "or3-intern memory pinned set --session <key> [--global] --key <k> <content>"); err != nil {
		return err
	}
	sessionKey := strings.TrimSpace(*session)
	pinKey := strings.TrimSpace(*key)
	content := strings.TrimSpace(fs.Arg(0))
	if sessionKey == "" || pinKey == "" || content == "" {
		return fmt.Errorf("session, key, and content are required")
	}
	if err := svc.SetPinned(ctx, memorysvc.SetPinnedRequest{
		SessionKey: sessionKey,
		Key:        pinKey,
		Content:    content,
		GlobalOnly: *global,
	}); err != nil {
		return err
	}
	return writeMemoryPinnedSetOutput(deps.Stdout, *format)
}

func writeMemorySearchOutput(stdout, stderr io.Writer, format string, resp memorysvc.SearchResponse) error {
	if strings.EqualFold(format, "text") {
		if resp.Warning != "" {
			_, _ = fmt.Fprintf(stderr, "warning: %s\n", resp.Warning)
		}
		if len(resp.Hits) == 0 {
			_, _ = fmt.Fprintln(stdout, "(no hits)")
			return nil
		}
		for i, hit := range resp.Hits {
			_, _ = fmt.Fprintf(stdout, "%d)\t%s\t%.3f\t%s\n", i+1, hit.Source, hit.Score, hit.Text)
		}
		return nil
	}
	return writeMemoryJSON(stdout, resp)
}

func writeMemoryAddNoteOutput(stdout, stderr io.Writer, format string, resp memorysvc.AddNoteResponse) error {
	if strings.EqualFold(format, "text") {
		if resp.Warning != "" {
			_, _ = fmt.Fprintf(stderr, "warning: %s\n", resp.Warning)
		}
		_, _ = fmt.Fprintf(stdout, "note_id=%d\n", resp.ID)
		return nil
	}
	return writeMemoryJSON(stdout, resp)
}

func writeMemoryPinnedGetOutput(stdout io.Writer, format string, entries []memorysvc.PinnedEntry) error {
	if strings.EqualFold(format, "text") {
		if len(entries) == 0 {
			_, _ = fmt.Fprintln(stdout, "(no entries)")
			return nil
		}
		for _, entry := range entries {
			_, _ = fmt.Fprintf(stdout, "%s\t%s\n", entry.Key, entry.Content)
		}
		return nil
	}
	return writeMemoryJSON(stdout, map[string]any{"entries": entries})
}

func writeMemoryPinnedSetOutput(stdout io.Writer, format string) error {
	if strings.EqualFold(format, "text") {
		_, _ = fmt.Fprintln(stdout, "ok")
		return nil
	}
	return writeMemoryJSON(stdout, map[string]any{"ok": true})
}

func writeMemoryJSON(stdout io.Writer, value any) error {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
