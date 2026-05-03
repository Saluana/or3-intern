package agentcli

import (
	"context"
	"os"
	"testing"
)

func TestOpenCodeAdapter_DefaultArgs(t *testing.T) {
	adapter := &OpenCodeAdapter{spec: RunnerSpec{Binary: "opencode"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "fix tests",
		Cwd:  "/workspace",
		Mode: "safe_edit",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	args := cmd.Args
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
	if !containsArg(args, "--cd", "/workspace") {
		t.Errorf("expected --cd /workspace, got %v", args)
	}
}

func TestCodexAdapter_ReviewMode(t *testing.T) {
	adapter := &CodexAdapter{spec: RunnerSpec{Binary: "codex"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "do danger",
		Mode: "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !contains(cmd.Args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("expected --dangerously-bypass-approvals-and-sandbox in sandbox_auto, got %v", cmd.Args)
	}
}

func TestClaudeAdapter_SafeEdit(t *testing.T) {
	adapter := &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "fix bug",
		Mode: "safe_edit",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !contains(cmd.Args, "--bare") {
		t.Errorf("expected --bare, got %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--permission-mode", "acceptEdits") {
		t.Errorf("expected --permission-mode acceptEdits, got %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--output-format", "stream-json") {
		t.Errorf("expected --output-format stream-json, got %v", cmd.Args)
	}
}

func TestClaudeAdapter_ReviewMode(t *testing.T) {
	adapter := &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "review code",
		Mode: "review",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--permission-mode", "plan") {
		t.Errorf("expected plan for review, got %v", cmd.Args)
	}
}

func TestClaudeAdapter_SandboxAuto(t *testing.T) {
	adapter := &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "do it",
		Mode: "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--permission-mode", "bypassPermissions") {
		t.Errorf("expected bypassPermissions for sandbox_auto, got %v", cmd.Args)
	}
}

func TestClaudeAdapter_MaxTurns(t *testing.T) {
	adapter := &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task:     "do it",
		Mode:     "safe_edit",
		MaxTurns: 5,
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--max-turns", "5") {
		t.Errorf("expected --max-turns 5, got %v", cmd.Args)
	}
}

func TestClaudeAdapter_NoMaxTurns(t *testing.T) {
	adapter := &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "do it",
		Mode: "safe_edit",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	for _, a := range cmd.Args {
		if a == "--max-turns" {
			t.Errorf("unexpected --max-turns when MaxTurns=0")
		}
	}
}

func TestGeminiAdapter_DefaultArgs(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "hello",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--prompt", "hello") {
		t.Errorf("expected --prompt hello, got %v", cmd.Args)
	}
	if !containsArg(cmd.Args, "--output-format", "json") {
		t.Errorf("expected --output-format json, got %v", cmd.Args)
	}
}

func TestGeminiAdapter_SafeEdit(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "fix bug",
		Mode: "safe_edit",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--approval-mode", "auto_edit") {
		t.Errorf("expected --approval-mode auto_edit, got %v", cmd.Args)
	}
}

func TestGeminiAdapter_ReviewMode(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "review",
		Mode: "review",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--approval-mode", "default") {
		t.Errorf("expected --approval-mode default, got %v", cmd.Args)
	}
}

func TestGeminiAdapter_SandboxAuto(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "do it",
		Mode: "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	if !containsArg(cmd.Args, "--approval-mode", "yolo") {
		t.Errorf("expected --approval-mode yolo, got %v", cmd.Args)
	}
}

func TestGeminiAdapter_NoDuplicateYolo(t *testing.T) {
	adapter := &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}
	cmd, err := adapter.BuildCommand(AgentRunRequest{
		Task: "do it",
		Mode: "sandbox_auto",
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	count := 0
	for _, a := range cmd.Args {
		if a == "--yolo" {
			count++
		}
	}
	if count > 0 {
		t.Errorf("adapter emitted --yolo %d times (should be 0)", count)
	}
	count = 0
	for _, a := range cmd.Args {
		if a == "--approval-mode" || a == "yolo" {
			count++
		}
	}
	if count > 2 {
		t.Errorf("expected exactly one --approval-mode yolo pair, got: %v", cmd.Args)
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
		{"claude dollar", &ClaudeAdapter{spec: RunnerSpec{Binary: "claude"}}, "ls $(cat /etc/passwd)"},
		{"gemini newlines", &GeminiAdapter{spec: RunnerSpec{Binary: "gemini"}}, "first\nrm -rf /"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := tt.adapter.BuildCommand(AgentRunRequest{
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
	for _, id := range []RunnerID{RunnerOpenCode, RunnerCodex, RunnerClaude, RunnerGemini} {
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
	for _, id := range []RunnerID{RunnerOpenCode, RunnerCodex, RunnerClaude, RunnerGemini} {
		cmd, err := reg.BuildCommand(AgentRunRequest{
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
	cmd, err := adapter.BuildCommand(AgentRunRequest{Task: "test"})
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
	expected := []RunnerID{RunnerOpenCode, RunnerCodex, RunnerClaude, RunnerGemini, RunnerOR3}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("AllRunners missing %q", id)
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
