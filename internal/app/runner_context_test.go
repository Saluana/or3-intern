package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

func TestBuildNativeMemoryRefreshBoundedAndFlagged(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "native-refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := database.UpsertPinned(ctx, scope.GlobalMemoryScope, "fact", strings.Repeat("pinned ", 400)); err != nil {
		t.Fatal(err)
	}
	builder := NewRunnerContextBuilder(config.Default(), RunnerContextDeps{DB: database})
	refresh, debug := builder.BuildNativeMemoryRefresh(ctx, "cli:default", "task", RunnerBootstrapContext{
		StaticMemory: strings.Repeat("digest ", 400),
	})
	if refresh == "" {
		t.Fatal("expected non-empty native refresh")
	}
	if !strings.Contains(refresh, "memory_context:") || !strings.Contains(refresh, "pinned_memory:") {
		t.Fatalf("unexpected refresh: %q", refresh)
	}
	if strings.Contains(refresh, "indexed_docs:") {
		t.Fatal("native refresh must not include docs")
	}
	if !debug.NativeRefresh || !debug.PinnedNonEmpty || !debug.DigestNonEmpty {
		t.Fatalf("debug=%#v", debug)
	}
	if len(refresh) > 5000 {
		t.Fatalf("refresh should stay bounded, len=%d", len(refresh))
	}
}

func TestRunnerPromptCompilerSetsPassiveMemoryDebug(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "passive-debug.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.UpsertPinned(context.Background(), "cli:default", "k", "v"); err != nil {
		t.Fatal(err)
	}
	compiler := NewRunnerPromptCompiler(config.Default(), RunnerBootstrapContext{Soul: "soul"}, RunnerContextDeps{DB: database})
	out := compiler.Compile(context.Background(), RunnerPromptCompileInput{
		SessionKey:  "cli:default",
		UserTask:    "hello",
		TriggerKind: "user_message",
	})
	if !out.MemoryDebug.PassiveCompiled || !out.MemoryDebug.PinnedNonEmpty {
		t.Fatalf("memory debug=%#v", out.MemoryDebug)
	}
}
