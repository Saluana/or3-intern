package runners

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOpenCodeAdapter_DefaultArgs(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "hello world",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	want := []string{"run", "--format", "json", "hello world"}
	assertArgsEqual(t, want, cmd.Args)
}

func TestOpenCodeAdapter_WithModel(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task:  "fix bug",
		Model: "gpt-5",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	want := []string{"run", "--format", "json", "--model", "gpt-5", "fix bug"}
	assertArgsEqual(t, want, cmd.Args)
}

func TestOpenCodeAdapter_SandboxAuto(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "do it",
		Mode: "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	found := false
	for _, a := range cmd.Args {
		if a == "--dangerously-skip-permissions" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --dangerously-skip-permissions, got %v", cmd.Args)
	}
}

func TestOpenCodeAdapter_NoDangerousInSafe(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "do it",
		Mode: "safe_edit",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	for _, a := range cmd.Args {
		if a == "--dangerously-skip-permissions" {
			t.Errorf("unexpected --dangerously-skip-permissions in safe_edit mode")
		}
	}
}

func TestCodexAdapter_SafeEdit(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "fix tests",
		Cwd:  "/workspace",
		Mode: "safe_edit",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	args := cmd.Args
	wantPrefix := []string{"--ask-for-approval", "never", "-c", "mcp_servers={}", "exec", "--json", "--color", "never", "--skip-git-repo-check"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("expected codex args prefix %v, got %v", wantPrefix, args)
	}
	assertArgsEqual(t, wantPrefix, args[:len(wantPrefix)])
	if !containsArg(args, "--sandbox", "workspace-write") {
		t.Errorf("expected --sandbox workspace-write, got %v", args)
	}
	if !containsArg(args, "--ask-for-approval", "never") {
		t.Errorf("expected --ask-for-approval never, got %v", args)
	}
	if !containsArg(args, "--color", "never") {
		t.Errorf("expected --color never, got %v", args)
	}
	if !containsArg(args, "--json") {
		t.Errorf("expected --json flag, got %v", args)
	}
	if !contains(args, "--skip-git-repo-check") {
		t.Errorf("expected --skip-git-repo-check flag, got %v", args)
	}
	if !containsArg(args, "--cd", "/workspace") {
		t.Errorf("expected --cd /workspace, got %v", args)
	}
}

func TestCodexAdapter_ReviewMode(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "review code",
		Cwd:  "/workspace",
		Mode: "review",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--sandbox", "read-only") {
		t.Errorf("expected --sandbox read-only, got %v", cmd.Args)
	}
}

func TestCodexAdapter_NoCdWhenCwdEmpty(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "hello",
		Mode: "safe_edit",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	for _, a := range cmd.Args {
		if a == "--cd" {
			t.Errorf("unexpected --cd when cwd is empty: %v", cmd.Args)
		}
	}
	if !containsArg(cmd.Args, "--json") {
		t.Errorf("expected --json flag, got %v", cmd.Args)
	}
}

func TestCodexAdapter_NoFullAuto(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "anything",
		Mode: "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	for _, a := range cmd.Args {
		if a == "--full-auto" {
			t.Errorf("--full-auto must never be emitted")
		}
	}
}

func TestCodexAdapter_DangerousBypassFlag(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{
		Task: "do danger",
		Mode: "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !contains(cmd.Args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("expected --dangerously-bypass-approvals-and-sandbox in sandbox_auto, got %v", cmd.Args)
	}
	if contains(cmd.Args, "--ask-for-approval") {
		t.Errorf("sandbox_auto should rely on Codex dangerous bypass only, got %v", cmd.Args)
	}
}

func TestShellMetacharactersRemainOneArgvElement(t *testing.T) {
	tests := []struct {
		name    string
		adapter RunnerAdapter
		task    string
	}{
		{"opencode semicolon", &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}, `fix"; rm -rf /"`},
		{"codex backticks", &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}, "run `evil`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := tt.adapter.BuildCommand(RunnerRunRequest{
				Task: tt.task,
			})
			if err != nil {
				t.Fatalf("BuildCommand: %v", err)
			}
			foundTask := false
			for _, a := range cmd.Args {
				if a == tt.task {
					foundTask = true
					break
				}
			}
			if !foundTask {
				t.Errorf("task %q not found as single arg in %v", tt.task, cmd.Args)
			}
		})
	}
}

func TestNewDefaultRegistry_HasAllAdapters(t *testing.T) {
	reg := NewDefaultRegistry()
	for _, id := range []RunnerID{RunnerOpenCode, RunnerCodex} {
		adapter, ok := reg.Adapter(id)
		if !ok {
			t.Errorf("NewDefaultRegistry missing adapter for %q", id)
			continue
		}
		if adapter.ID() != id {
			t.Errorf("adapter ID mismatch: got %q, want %q", adapter.ID(), id)
		}
		if adapter.Spec().Binary == "" {
			t.Errorf("adapter %q has empty Binary", id)
		}
	}
	// Verify BuildCommand works without manual spec wiring
	for _, id := range []RunnerID{RunnerOpenCode, RunnerCodex} {
		cmd, err := reg.BuildCommand(RunnerRunRequest{
			RunnerID: string(id),
			Task:     "test",
			Mode:     "safe_edit",
		})
		if err != nil {
			t.Errorf("%s BuildCommand: %v", id, err)
			continue
		}
		if cmd.Binary == "" {
			t.Errorf("%s BuildCommand returned empty Binary", id)
		}
		if len(cmd.Args) == 0 {
			t.Errorf("%s BuildCommand returned empty Args", id)
		}
	}
}

func TestNewOpenCodeAdapter_WiredWithSpec(t *testing.T) {
	adapter := NewOpenCodeAdapter()
	if adapter.Spec().Binary != "opencode" {
		t.Errorf("expected binary=opencode, got %q", adapter.Spec().Binary)
	}
	cmd, err := adapter.BuildCommand(RunnerRunRequest{Task: "test"})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if cmd.Binary != "opencode" {
		t.Errorf("expected command binary=opencode, got %q", cmd.Binary)
	}
}

func TestAllRunners_IncludesAllAdapterIDs(t *testing.T) {
	all := AllRunners()
	ids := make(map[RunnerID]bool)
	for _, s := range all {
		ids[s.ID] = true
	}
	expected := []RunnerID{RunnerOpenCode, RunnerCodex}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("AllRunners missing %q", id)
		}
		var capabilities RunnerChatCapabilities
		for _, spec := range all {
			if spec.ID == id {
				capabilities = spec.Supports.Chat
				break
			}
		}
		if !capabilities.Cancel || !capabilities.ApprovalDecisions || !capabilities.CustomCwd {
			t.Errorf("AllRunners %q missing conservative chat action capabilities: %#v", id, capabilities)
		}
	}
	for _, id := range []RunnerID{RunnerClaude, RunnerGemini} {
		if ids[id] {
			t.Errorf("AllRunners should not include removed runner %q", id)
		}
	}
}

func TestRegistry_DetectAll(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fakecli", `echo "fakecli v1.0.0"; exit 0`)

	origPath := os.Getenv("PATH")
	var newPath string
	if origPath == "" {
		newPath = dir
	} else {
		newPath = dir + string(os.PathListSeparator) + origPath
	}
	t.Setenv("PATH", newPath)

	specs := []RunnerSpec{
		{ID: "test-runner", DisplayName: "Test", Binary: "fakecli", VersionArgs: []string{"--version"}},
	}
	reg := NewRunnerRegistry(specs, nil)
	results := reg.DetectAll(context.Background(), DetectOptions{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != RunnerStatusAvailable {
		t.Errorf("expected available, got %q", results[0].Status)
	}
}

func TestRegistry_DetectAllPopulatesCache(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "cachedcli", `echo "cachedcli v1.0.0"; exit 0`)

	origPath := os.Getenv("PATH")
	if origPath == "" {
		t.Setenv("PATH", dir)
	} else {
		t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)
	}

	reg := NewRunnerRegistry([]RunnerSpec{{
		ID:          "cached-runner",
		DisplayName: "Cached",
		Binary:      "cachedcli",
		VersionArgs: []string{"--version"},
	}}, nil)

	reg.DetectAll(context.Background(), DetectOptions{})

	info, ok := reg.DetectCached("cached-runner", time.Minute)
	if !ok {
		t.Fatal("expected cached runner detection result")
	}
	if info.Status != RunnerStatusAvailable {
		t.Fatalf("expected available cached status, got %q", info.Status)
	}
}

func assertArgsEqual(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("args length mismatch: want %d, got %d\nwant: %v\ngot:  %v", len(want), len(got), want, got)
		return
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("arg[%d]: want %q, got %q\nwant: %v\ngot:  %v", i, want[i], got[i], want, got)
			return
		}
	}
}

func containsArg(args []string, keyValue ...string) bool {
	for i := 0; i <= len(args)-len(keyValue); i++ {
		match := true
		for j, v := range keyValue {
			if args[i+j] != v {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func filepathSeparator() byte {
	return '/'
}
