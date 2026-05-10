package agentcli

import (
	"context"
	"strings"
	"testing"
)

func TestProcessManagerBinaryNotFoundAndStderrOnly(t *testing.T) {
	pm := NewProcessManager(64, 256)
	var events []AgentRunEvent
	out := pm.Run(context.Background(), CommandSpec{Binary: "missing-runner", Env: []string{"PATH=" + t.TempDir()}}, func(e AgentRunEvent) {
		events = append(events, e)
	})
	if out.ExitCode != -1 {
		t.Fatalf("expected missing binary exit code -1, got %d", out.ExitCode)
	}
	if len(events) == 0 || events[0].Type != "error" || !strings.Contains(events[0].Message, "resolve executable") {
		t.Fatalf("expected resolve error event, got %#v", events)
	}

	dir := t.TempDir()
	writeFakeBinary(t, dir, "stderr-only", `echo "stderr only" >&2`)
	out = pm.Run(context.Background(), CommandSpec{Binary: "stderr-only", Env: []string{"PATH=" + dir}}, nil)
	if out.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", out.ExitCode)
	}
	if out.StdoutPreview != "" || !strings.Contains(out.StderrPreview, "stderr only") || out.FinalTextPreview != "" {
		t.Fatalf("unexpected stderr-only output: %#v", out)
	}
}

func TestProcessManagerDefaultsNilCallbackAndCancelledProcess(t *testing.T) {
	pm := NewProcessManager(-1, -1)
	if pm.ChunkMaxBytes != DefaultEventChunkMaxBytes || pm.PreviewMaxBytes != DefaultPreviewMaxBytes {
		t.Fatalf("unexpected defaults: %#v", pm)
	}
	dir := t.TempDir()
	writeFakeBinary(t, dir, "stdout-only", `echo "hello"`)
	out := pm.Run(context.Background(), CommandSpec{Binary: "stdout-only", Env: []string{"PATH=" + dir}}, nil)
	if out.ExitCode != 0 || !strings.Contains(out.StdoutPreview, "hello") {
		t.Fatalf("unexpected nil-callback output: %#v", out)
	}

	writeFakeBinary(t, dir, "cancel-me", `sleep 1`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var events []AgentRunEvent
	out = pm.Run(ctx, CommandSpec{Binary: "cancel-me", Env: []string{"PATH=" + dir}}, func(e AgentRunEvent) {
		events = append(events, e)
	})
	if out.ExitCode != -1 {
		t.Fatalf("expected canceled start to return exit code -1, got %#v", out)
	}
	if len(events) == 0 || events[0].Type != "error" || !strings.Contains(events[0].Message, "process start") {
		t.Fatalf("expected process start error event, got %#v", events)
	}
}

func TestRingBufferOverflowAndScannerOverflow(t *testing.T) {
	rb := newRingBuffer(4)
	if _, err := rb.Write([]byte("abcdef")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := rb.String(); got != "cdef" {
		t.Fatalf("expected wrapped tail, got %q", got)
	}

	collector := newOutputCollector(64, RunnerCodex)
	var events []AgentRunEvent
	var seq int64
	readStream(strings.NewReader(strings.Repeat("x", 32)+"\n"), "stdout", 8, &seq, collector, func(e AgentRunEvent) {
		events = append(events, e)
	}, OutputPlain)
	if len(events) == 0 {
		t.Fatal("expected scanner overflow event")
	}
	last := events[len(events)-1]
	if last.Type != "error" || !strings.Contains(last.Message, "token too long") {
		t.Fatalf("unexpected scanner error event: %#v", last)
	}
}
