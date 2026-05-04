package agentcli

import (
	"strings"
	"testing"
)

func TestReadStream_AllowsExactChunkSizedLine(t *testing.T) {
	chunkMaxBytes := 16
	line := strings.Repeat("x", chunkMaxBytes)
	collector := newOutputCollector(64, "")
	var seq int64
	var events []AgentRunEvent

	readStream(strings.NewReader(line+"\n"), "stdout", chunkMaxBytes, &seq, collector, func(event AgentRunEvent) {
		events = append(events, event)
	}, OutputPlain)

	if len(events) != 1 {
		t.Fatalf("expected 1 output event, got %d", len(events))
	}
	if events[0].Type != "output" {
		t.Fatalf("expected output event, got %q", events[0].Type)
	}
	if events[0].Chunk != line {
		t.Fatalf("expected chunk length %d, got %d", len(line), len(events[0].Chunk))
	}
	if got := collector.stdout.String(); got != line+"\n" {
		t.Fatalf("expected collector stdout %q, got %q", line+"\n", got)
	}
}
