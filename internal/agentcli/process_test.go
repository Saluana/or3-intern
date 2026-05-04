package agentcli

import (
	"context"
	"strings"
	"testing"
)

func TestProcessManager_ResolvesBinaryFromCommandEnv(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fake-runner", `echo "ran from env path"`)
	t.Setenv("PATH", t.TempDir())

	pm := NewProcessManager(1024, 1024)
	out := pm.Run(context.Background(), CommandSpec{
		Binary: "fake-runner",
		Env:    []string{"PATH=" + dir},
	}, nil)

	if out.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%q", out.ExitCode, out.StderrPreview)
	}
	if !strings.Contains(out.StdoutPreview, "ran from env path") {
		t.Fatalf("expected stdout from fake runner, got %q", out.StdoutPreview)
	}
}

func TestProcessManager_ExtractsGeminiFinalText(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fake-gemini", `printf '%s\n' '{"response":"## Done\n\nShip it.","stats":{"totalTokens":12}}'`)

	pm := NewProcessManager(1024, 4096)
	out := pm.Run(context.Background(), CommandSpec{
		RunnerID:   RunnerGemini,
		Binary:     "fake-gemini",
		Env:        []string{"PATH=" + dir},
		OutputMode: OutputJSON,
	}, nil)

	if out.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", out.ExitCode)
	}
	if out.FinalTextPreview != "## Done\n\nShip it." {
		t.Fatalf("expected normalized gemini text, got %q", out.FinalTextPreview)
	}
}

func TestProcessManager_ExtractsClaudeFinalText(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fake-claude", `printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"partial"}]}}' && printf '%s\n' '{"type":"result","subtype":"success","result":"Final markdown answer"}'`)

	pm := NewProcessManager(1024, 4096)
	out := pm.Run(context.Background(), CommandSpec{
		RunnerID:   RunnerClaude,
		Binary:     "fake-claude",
		Env:        []string{"PATH=" + dir},
		OutputMode: OutputJSONL,
	}, nil)

	if out.FinalTextPreview != "Final markdown answer" {
		t.Fatalf("expected claude result text, got %q", out.FinalTextPreview)
	}
}

func TestProcessManager_ExtractsCodexFinalText(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fake-codex", `printf '%s\n' '{"type":"thread.started","thread_id":"t1"}' && printf '%s\n' '{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Repo contains docs and examples."}}'`)

	pm := NewProcessManager(1024, 4096)
	out := pm.Run(context.Background(), CommandSpec{
		RunnerID:   RunnerCodex,
		Binary:     "fake-codex",
		Env:        []string{"PATH=" + dir},
		OutputMode: OutputJSONL,
	}, nil)

	if out.FinalTextPreview != "Repo contains docs and examples." {
		t.Fatalf("expected codex final text, got %q", out.FinalTextPreview)
	}
}

func TestProcessManager_ExtractsOpenCodeFinalText(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, dir, "fake-opencode", `printf '%s\n' '{"type":"assistant_message","message":"Applied the requested fix."}'`)

	pm := NewProcessManager(1024, 4096)
	out := pm.Run(context.Background(), CommandSpec{
		RunnerID:   RunnerOpenCode,
		Binary:     "fake-opencode",
		Env:        []string{"PATH=" + dir},
		OutputMode: OutputJSON,
	}, nil)

	if out.FinalTextPreview != "Applied the requested fix." {
		t.Fatalf("expected opencode final text, got %q", out.FinalTextPreview)
	}
}
