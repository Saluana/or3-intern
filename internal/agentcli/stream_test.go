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

func TestReadStream_LargeStructuredLineDoesNotEmitRawChunks(t *testing.T) {
	chunkMaxBytes := 16
	line := `{"type":"tool_use","part":{"state":{"output":"` + strings.Repeat("x", 64) + `"}}}`
	collector := newOutputCollector(256, RunnerOpenCode)
	var seq int64
	var events []AgentRunEvent

	readStream(strings.NewReader(line+"\n"), "stdout", chunkMaxBytes, &seq, collector, func(event AgentRunEvent) {
		events = append(events, event)
	}, OutputJSONL)

	if len(events) != 1 {
		t.Fatalf("expected only structured event for JSON stdout, got %#v", events)
	}
	if events[0].Type != "structured" {
		t.Fatalf("expected structured event, got %#v", events[0])
	}
	if got := collector.stdout.String(); !strings.Contains(got, `"tool_use"`) {
		t.Fatalf("expected collector to retain raw structured stdout, got %q", got)
	}
}

func TestReadStream_MultilineStructuredJSONDoesNotLeakRawChunks(t *testing.T) {
	chunkMaxBytes := 32
	line := "{\n  \"session_id\": \"session_123\",\n  \"response\": \"hello\",\n  \"stats\": {\n    \"models\": {}\n  }\n}"
	collector := newOutputCollector(512, RunnerGemini)
	var seq int64
	var events []AgentRunEvent

	readStream(strings.NewReader(line+"\n"), "stdout", chunkMaxBytes, &seq, collector, func(event AgentRunEvent) {
		events = append(events, event)
	}, OutputJSON)

	if len(events) != 1 {
		t.Fatalf("expected only structured event for multiline JSON stdout, got %#v", events)
	}
	if events[0].Type != "structured" {
		t.Fatalf("expected structured event, got %#v", events[0])
	}
	if got := collector.stdout.String(); !strings.Contains(got, `"session_id": "session_123"`) {
		t.Fatalf("expected collector to retain multiline structured stdout, got %q", got)
	}
}

func TestReadStream_NewlineFreeOutputFlushesBeforeUnboundedGrowth(t *testing.T) {
	chunkMaxBytes := 8
	line := strings.Repeat("x", DefaultEventChunkMaxBytes+1)
	collector := newOutputCollector(DefaultEventChunkMaxBytes*2, RunnerOpenCode)
	var seq int64
	var events []AgentRunEvent

	readStream(strings.NewReader(line), "stdout", chunkMaxBytes, &seq, collector, func(event AgentRunEvent) {
		events = append(events, event)
	}, OutputPlain)

	if len(events) == 0 {
		t.Fatal("expected newline-free output to be emitted in chunks")
	}
	for _, event := range events {
		if event.Type != "output" || len(event.Chunk) > chunkMaxBytes {
			t.Fatalf("unexpected bounded output event: %#v", event)
		}
	}
}
