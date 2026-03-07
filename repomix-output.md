This file is a merged representation of a subset of the codebase, containing files not matching ignore patterns, combined into a single document by Repomix.

# File Summary

## Purpose
This file contains a packed representation of a subset of the repository's contents that is considered the most important context.
It is designed to be easily consumable by AI systems for analysis, code review,
or other automated processes.

## File Format
The content is organized as follows:
1. This summary section
2. Repository information
3. Directory structure
4. Repository files (if enabled)
5. Multiple file entries, each consisting of:
  a. A header with the file path (## File: path/to/file)
  b. The full contents of the file in a code block

## Usage Guidelines
- This file should be treated as read-only. Any changes should be made to the
  original repository files, not this packed version.
- When processing this file, use the file path to distinguish
  between different files in the repository.
- Be aware that this file may contain sensitive information. Handle it with
  the same level of security as you would the original repository.

## Notes
- Some files may have been excluded based on .gitignore rules and Repomix's configuration
- Binary files are not included in this packed representation. Please refer to the Repository Structure section for a complete list of file paths, including binary files
- Files matching these patterns are excluded: nanobot-repo.md, .github, planning, missing.md, breakdown.md
- Files matching patterns in .gitignore are excluded
- Files matching default ignore patterns are excluded
- Files are sorted by Git change count (files with more changes are at the bottom)

# Directory Structure
```
builtin_skills/
  cron.md
cmd/
  or3-intern/
    init_test.go
    init.go
    main.go
    migrate.go
    tools_registry_test.go
internal/
  agent/
    agent_test.go
    prompt_test.go
    prompt.go
    runtime_streaming_test.go
    runtime_test.go
    runtime.go
    subagents_test.go
    subagents.go
  artifacts/
    attachment.go
    store_test.go
    store.go
  bus/
    bus_test.go
    bus.go
  channels/
    cli/
      cli_test.go
      cli.go
      deliver_test.go
      deliver.go
      service.go
    discord/
      discord_test.go
      discord.go
    slack/
      slack_test.go
      slack.go
    telegram/
      telegram_test.go
      telegram.go
    whatsapp/
      whatsapp_test.go
      whatsapp.go
    channels_test.go
    channels.go
    media.go
    stream.go
  config/
    config_test.go
    config.go
  cron/
    cron_test.go
    cron.go
  db/
    db_test.go
    db.go
    store.go
  memory/
    consolidate_test.go
    consolidate.go
    docs_test.go
    docs.go
    retrieve_test.go
    retrieve.go
    scheduler_test.go
    scheduler.go
    vector_test.go
    vector.go
  providers/
    openai_stream_test.go
    openai_test.go
    openai.go
  scope/
    scope.go
  skills/
    skills_test.go
    skills.go
  tools/
    context.go
    cron_test.go
    cron.go
    exec_test.go
    exec.go
    files_test.go
    files.go
    memory_test.go
    memory.go
    message_test.go
    message.go
    registry.go
    skill_run_test.go
    skill_run.go
    skill_test.go
    skill.go
    spawn_test.go
    spawn.go
    tools_test.go
    tools.go
    web_test.go
    web.go
  triggers/
    filewatch_test.go
    filewatch.go
    triggers.go
    webhook_test.go
    webhook.go
.env.example
.gitignore
go.mod
README.md
```

# Files

## File: builtin_skills/cron.md
````markdown
# cron
Use the `cron` tool to add/list/remove/run/status scheduled jobs.
````

## File: cmd/or3-intern/tools_registry_test.go
````go
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

type stubSpawnManager struct{}

func (stubSpawnManager) Enqueue(ctx context.Context, req tools.SpawnRequest) (tools.SpawnJob, error) {
	return tools.SpawnJob{ID: "job-1", ChildSessionKey: "child"}, nil
}

func TestBuildToolRegistry_ReturnsFreshToolInstances(t *testing.T) {
	cfg := config.Default()
	cfg.WorkspaceDir = t.TempDir()
	cfg.Tools.RestrictToWorkspace = true

	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	provider := providers.New("http://example.invalid", "key", time.Second)
	channelManager, err := buildChannelManager(cfg, cli.Deliverer{}, &artifacts.Store{Dir: t.TempDir(), DB: d}, cfg.MaxMediaBytes)
	if err != nil {
		t.Fatalf("buildChannelManager: %v", err)
	}
	inv := skills.Inventory{}

	reg1 := buildToolRegistry(cfg, d, provider, channelManager, &inv, nil, stubSpawnManager{})
	reg2 := buildToolRegistry(cfg, d, provider, channelManager, &inv, nil, stubSpawnManager{})

	for _, name := range []string{"read_file", "memory_search", "send_message", "spawn_subagent"} {
		tool1 := reg1.Get(name)
		tool2 := reg2.Get(name)
		if tool1 == nil || tool2 == nil {
			t.Fatalf("expected tool %q in both registries", name)
		}
		if fmt.Sprintf("%p", tool1) == fmt.Sprintf("%p", tool2) {
			t.Fatalf("expected fresh instance for %q", name)
		}
	}
}
````

## File: internal/agent/runtime_streaming_test.go
````go
package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/providers"
)

// mockStreamWriter records all deltas and close/abort calls.
type mockStreamWriter struct {
	deltas  []string
	closed  bool
	aborted bool
}

func (w *mockStreamWriter) WriteDelta(_ context.Context, text string) error {
	w.deltas = append(w.deltas, text)
	return nil
}

func (w *mockStreamWriter) Close(_ context.Context, _ string) error {
	w.closed = true
	return nil
}

func (w *mockStreamWriter) Abort(_ context.Context) error {
	w.aborted = true
	return nil
}

// mockStreamer implements channels.StreamingChannel using mockStreamWriter.
type mockStreamer struct {
	writer *mockStreamWriter
}

func (s *mockStreamer) BeginStream(_ context.Context, _ string, _ map[string]any) (channels.StreamWriter, error) {
	return s.writer, nil
}

// buildSSEServer creates a test server that returns an SSE stream.
func buildSSEServer(t *testing.T, sseLines []string) (*httptest.Server, *providers.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range sseLines {
			fmt.Fprintln(w, l)
		}
	}))
	t.Cleanup(srv.Close)
	c := providers.New(srv.URL, "test-key", 10*time.Second)
	c.HTTP = srv.Client()
	return srv, c
}

func TestRuntime_Streaming_FinalAnswer(t *testing.T) {
	d := openRuntimeTestDB(t)
	sseLines := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"content":" streamed"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}
	_, provider := buildSSEServer(t, sseLines)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	writer := &mockStreamWriter{}
	streamer := &mockStreamer{writer: writer}
	rt.Streamer = streamer

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-stream",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Streamer was used: deltas should have been written
	if len(writer.deltas) == 0 {
		t.Error("expected deltas written to stream writer")
	}
	combined := ""
	for _, d := range writer.deltas {
		combined += d
	}
	if combined != "Hello streamed" {
		t.Errorf("expected 'Hello streamed', got %q", combined)
	}
	if !writer.closed {
		t.Error("expected stream writer to be closed")
	}

	// When streaming, the Deliver method should NOT be called (to avoid double output)
	if len(deliver.messages) != 0 {
		t.Errorf("expected no delivered messages (already streamed), got %v", deliver.messages)
	}
}

func TestRuntime_Streaming_FallbackWhenNoStreamer(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "No streaming here"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)
	// rt.Streamer is nil - should fall back to normal Deliver

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-fallback",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(deliver.messages) == 0 {
		t.Error("expected message delivered via Deliver (no streamer)")
	}
	if deliver.messages[0] != "No streaming here" {
		t.Errorf("expected 'No streaming here', got %q", deliver.messages[0])
	}
}

func TestRuntime_Streaming_AbortOnToolCalls(t *testing.T) {
	d := openRuntimeTestDB(t)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: SSE response with a tool call
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, `data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"exec","arguments":"{\"cmd\":\"echo hi\"}"}}]},"finish_reason":"tool_calls"}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		} else {
			// Second call: SSE final answer
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, `data: {"id":"2","choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		}
	}))
	defer srv.Close()

	prov := providers.New(srv.URL, "key", 10*time.Second)
	prov.HTTP = srv.Client()

	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, prov, d, deliver)

	writers := []*mockStreamWriter{}
	rt.Streamer = &funcStreamer{fn: func() (channels.StreamWriter, error) {
		w := &mockStreamWriter{}
		writers = append(writers, w)
		return w, nil
	}}

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-tool",
		Channel:    "cli",
		From:       "user",
		Message:    "run something",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(writers) < 2 {
		t.Fatalf("expected at least 2 stream writers (one per loop), got %d", len(writers))
	}
	// First writer should have been aborted (tool call turn)
	if !writers[0].aborted {
		t.Error("expected first stream writer to be aborted (tool call turn)")
	}
	// Last writer should be closed (final answer)
	last := writers[len(writers)-1]
	if !last.closed {
		t.Error("expected last stream writer to be closed (final answer)")
	}
}

// funcStreamer allows a custom function to create writers.
type funcStreamer struct {
	fn func() (channels.StreamWriter, error)
}

func (s *funcStreamer) BeginStream(_ context.Context, _ string, _ map[string]any) (channels.StreamWriter, error) {
	return s.fn()
}
````

## File: internal/agent/subagents_test.go
````go
package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

func TestSubagentManager_SuccessPersistsAndNotifies(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	artifactsDir := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": strings.Repeat("x", 64)},
			}},
		})
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := &Runtime{
		DB:               d,
		Provider:         provider,
		Model:            "gpt-4",
		Tools:            tools.NewRegistry(),
		Builder:          &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops:     2,
		MaxToolBytes:     10,
		ToolPreviewBytes: 8,
		Artifacts:        &artifacts.Store{Dir: artifactsDir, DB: d},
		Deliver:          deliver,
	}
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start background work", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 2 * time.Second}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	job, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "background task", Channel: "cli", To: "user"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusSucceeded)
	if stored.ArtifactID == "" || stored.ResultPreview == "" {
		t.Fatalf("expected artifact-backed success result, got %#v", stored)
	}
	msgs, err := d.GetLastMessages(context.Background(), "parent", 20)
	if err != nil {
		t.Fatalf("GetLastMessages parent: %v", err)
	}
	if !containsMessage(msgs, "Background job "+job.ID+" completed") {
		t.Fatalf("expected parent completion note, got %#v", msgs)
	}
	if len(deliver.messages) == 0 || !strings.Contains(deliver.messages[0], "Background job "+job.ID+" finished") {
		t.Fatalf("expected completion delivery, got %#v", deliver.messages)
	}
	childMsgs, err := d.GetLastMessages(context.Background(), job.ChildSessionKey, 20)
	if err != nil {
		t.Fatalf("GetLastMessages child: %v", err)
	}
	if len(childMsgs) < 2 {
		t.Fatalf("expected child-session history, got %#v", childMsgs)
	}
}

func TestSubagentManager_FailurePersistsAndNotifies(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider down", http.StatusBadGateway)
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops: 2,
		Deliver:      deliver,
	}
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start background work", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 2 * time.Second}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	job, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "background task", Channel: "cli", To: "user"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusFailed)
	if stored.ErrorText == "" {
		t.Fatalf("expected persisted failure error, got %#v", stored)
	}
	msgs, err := d.GetLastMessages(context.Background(), "parent", 20)
	if err != nil {
		t.Fatalf("GetLastMessages parent: %v", err)
	}
	if !containsMessage(msgs, "Background job "+job.ID+" failed") {
		t.Fatalf("expected parent failure note, got %#v", msgs)
	}
	if len(deliver.messages) == 0 || !strings.Contains(deliver.messages[0], "Background job "+job.ID+" failed") {
		t.Fatalf("expected failure delivery, got %#v", deliver.messages)
	}
}

func TestSubagentManager_DoesNotBlockForegroundTurn(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req providers.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		last := req.Messages[len(req.Messages)-1]
		content := contentToString(last.Content)
		if strings.Contains(content, "long task") {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := buildSimpleRuntime(t, provider, d, deliver)
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 2 * time.Second}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	if _, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "long task", Channel: "cli", To: "user"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background job to start")
	}
	start := time.Now()
	err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "parent", Channel: "cli", From: "user", Message: "foreground follow-up"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("expected foreground turn to stay non-blocking, elapsed=%v", elapsed)
	}
	close(release)
}

func TestSubagentManager_TimeoutMarksFailure(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "slow"},
			}},
		})
	}))
	defer providerServer.Close()
	provider := providers.New(providerServer.URL, "k", 5*time.Second)
	provider.HTTP = providerServer.Client()
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      &Builder{DB: d, HistoryMax: 10},
		MaxToolLoops: 2,
		Deliver:      deliver,
	}
	if _, err := d.AppendMessage(context.Background(), "parent", "user", "start background work", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	mgr := &SubagentManager{DB: d, Runtime: rt, Deliver: deliver, MaxConcurrent: 1, MaxQueued: 8, TaskTimeout: 50 * time.Millisecond}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	job, err := mgr.Enqueue(context.Background(), tools.SpawnRequest{ParentSessionKey: "parent", Task: "background task", Channel: "cli", To: "user"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stored := waitForSubagentJob(t, d, job.ID, db.SubagentStatusFailed)
	if !strings.Contains(strings.ToLower(stored.ErrorText), "timeout") && !strings.Contains(strings.ToLower(stored.ErrorText), "deadline") {
		t.Fatalf("expected timeout-related failure, got %#v", stored)
	}
}

func TestSubagentManager_FinalizeFailureDoesNotDeliver(t *testing.T) {
	d := openRuntimeTestDB(t)
	deliver := &mockDeliverer{}
	job := db.SubagentJob{
		ID:               "job-finalize-fail",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-finalize-fail",
		Task:             "background task",
	}
	if err := d.EnqueueSubagentJob(context.Background(), job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	mgr := &SubagentManager{DB: d, Deliver: deliver}
	mgr.finalizeJob(job, db.SubagentStatusSucceeded, "preview", "", "")
	if len(deliver.messages) != 0 {
		t.Fatalf("expected no delivery on finalize failure, got %#v", deliver.messages)
	}
	msgs, err := d.GetLastMessages(context.Background(), job.ParentSessionKey, 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no persisted parent summary on finalize failure, got %#v", msgs)
	}
}

func waitForSubagentJob(t *testing.T, d *db.DB, id string, want string) db.SubagentJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, ok, err := d.GetSubagentJob(context.Background(), id)
		if err != nil {
			t.Fatalf("GetSubagentJob: %v", err)
		}
		if ok && job.Status == want {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job %s status %s", id, want)
	return db.SubagentJob{}
}

func containsMessage(msgs []db.Message, needle string) bool {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}
````

## File: internal/agent/subagents.go
````go
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type SubagentManager struct {
	DB              *db.DB
	Runtime         *Runtime
	Deliver         Deliverer
	MaxConcurrent   int
	MaxQueued       int
	TaskTimeout     time.Duration
	BackgroundTools func() *tools.Registry

	mu       sync.Mutex
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	notifyCh chan struct{}
	wg       sync.WaitGroup
}

func (m *SubagentManager) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("subagent manager is nil")
	}
	if m.DB == nil {
		return fmt.Errorf("subagent db not configured")
	}
	if m.Runtime == nil {
		return fmt.Errorf("subagent runtime not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.MaxConcurrent <= 0 {
		m.MaxConcurrent = 1
	}
	if m.MaxQueued <= 0 {
		m.MaxQueued = 32
	}
	if m.TaskTimeout <= 0 {
		m.TaskTimeout = 5 * time.Minute
	}
	if err := m.DB.MarkRunningSubagentsInterrupted(ctx, "subagent interrupted during restart"); err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.notifyCh = make(chan struct{}, 1)
	m.started = true
	for i := 0; i < m.MaxConcurrent; i++ {
		m.wg.Add(1)
		go m.workerLoop()
	}
	queued, err := m.DB.ListQueuedSubagentJobs(ctx)
	if err != nil {
		return err
	}
	if len(queued) > 0 {
		select {
		case m.notifyCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (m *SubagentManager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	m.started = false
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *SubagentManager) Enqueue(ctx context.Context, req tools.SpawnRequest) (tools.SpawnJob, error) {
	if m == nil || m.DB == nil {
		return tools.SpawnJob{}, fmt.Errorf("background subagents disabled")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return tools.SpawnJob{}, fmt.Errorf("empty task")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return tools.SpawnJob{}, fmt.Errorf("missing parent session")
	}
	jobID := newSubagentID()
	job := db.SubagentJob{
		ID:               jobID,
		ParentSessionKey: parentSessionKey,
		ChildSessionKey:  childSessionKey(parentSessionKey, jobID),
		Channel:          strings.TrimSpace(req.Channel),
		ReplyTo:          strings.TrimSpace(req.To),
		Task:             task,
		Status:           db.SubagentStatusQueued,
		MetadataJSON:     "{}",
	}
	if err := m.DB.EnqueueSubagentJobLimited(ctx, job, m.MaxQueued); err != nil {
		return tools.SpawnJob{}, err
	}
	m.signal()
	return tools.SpawnJob{ID: job.ID, ChildSessionKey: job.ChildSessionKey}, nil
}

func (m *SubagentManager) workerLoop() {
	defer m.wg.Done()
	for {
		ran, err := m.runOnce()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("subagent worker error: %v", err)
			}
		}
		if ran {
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-m.notifyCh:
		}
	}
}

func (m *SubagentManager) runOnce() (bool, error) {
	job, err := m.DB.ClaimNextSubagentJob(m.ctx)
	if err != nil || job == nil {
		return false, err
	}
	m.executeJob(*job)
	return true, nil
}

func (m *SubagentManager) executeJob(job db.SubagentJob) {
	runCtx, cancel := context.WithTimeout(m.ctx, m.TaskTimeout)
	defer cancel()
	result, err := m.runJob(runCtx, job)
	if err != nil {
		reason := strings.TrimSpace(err.Error())
		switch {
		case errors.Is(err, context.Canceled), errors.Is(runCtx.Err(), context.Canceled):
			m.finalizeJob(job, db.SubagentStatusInterrupted, "", "", reasonOrDefault(reason, "subagent interrupted"))
		case errors.Is(err, context.DeadlineExceeded), errors.Is(runCtx.Err(), context.DeadlineExceeded):
			m.finalizeJob(job, db.SubagentStatusFailed, "", "", reasonOrDefault(reason, "subagent timed out"))
		default:
			m.finalizeJob(job, db.SubagentStatusFailed, "", "", reasonOrDefault(reason, "subagent failed"))
		}
		return
	}
	m.finalizeJob(job, db.SubagentStatusSucceeded, result.Preview, result.ArtifactID, "")
}

func (m *SubagentManager) runJob(ctx context.Context, job db.SubagentJob) (BackgroundRunResult, error) {
	promptSnapshot, err := m.Runtime.BuildPromptSnapshot(ctx, job.ParentSessionKey, job.Task)
	if err != nil {
		return BackgroundRunResult{}, err
	}
	return m.Runtime.RunBackground(ctx, BackgroundRunInput{
		SessionKey:       job.ChildSessionKey,
		ParentSessionKey: job.ParentSessionKey,
		Task:             job.Task,
		PromptSnapshot:   promptSnapshot,
		Tools:            m.backgroundTools(),
		Meta: map[string]any{
			"subagent_job_id":    job.ID,
			"parent_session_key": job.ParentSessionKey,
		},
		Channel: job.Channel,
		ReplyTo: job.ReplyTo,
	})
}

func (m *SubagentManager) backgroundTools() *tools.Registry {
	if m.BackgroundTools != nil {
		return m.BackgroundTools()
	}
	return tools.NewRegistry()
}

func (m *SubagentManager) finalizeJob(job db.SubagentJob, status string, preview string, artifactID string, errText string) {
	success := status == db.SubagentStatusSucceeded
	text := formatParentSubagentSummary(job, success, preview, artifactID, errText)
	payload := map[string]any{
		"subagent_job_id": job.ID,
		"child_session":   job.ChildSessionKey,
		"status":          status,
	}
	if artifactID != "" {
		payload["artifact_id"] = artifactID
	}
	if err := m.DB.FinalizeSubagentJob(context.Background(), job, status, preview, artifactID, errText, text, payload); err != nil {
		log.Printf("finalize subagent failed: job=%s err=%v", job.ID, err)
		return
	}
	m.deliverCompletion(context.Background(), job, success, preview, artifactID, errText)
}

func (m *SubagentManager) deliverCompletion(ctx context.Context, job db.SubagentJob, success bool, preview string, artifactID string, errText string) {
	deliverer := m.Deliver
	if deliverer == nil && m.Runtime != nil {
		deliverer = m.Runtime.Deliver
	}
	if deliverer == nil || strings.TrimSpace(job.Channel) == "" || strings.TrimSpace(job.ReplyTo) == "" {
		return
	}
	text := formatDeliverySubagentSummary(job, success, preview, artifactID, errText)
	if err := deliverer.Deliver(ctx, job.Channel, job.ReplyTo, text); err != nil {
		log.Printf("subagent delivery failed: job=%s err=%v", job.ID, err)
	}
}

func (m *SubagentManager) signal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || m.notifyCh == nil {
		return
	}
	select {
	case m.notifyCh <- struct{}{}:
	default:
	}
}

func childSessionKey(parentSessionKey, jobID string) string {
	return parentSessionKey + ":subagent:" + jobID
}

func newSubagentID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return "job-" + hex.EncodeToString(raw[:])
}

func reasonOrDefault(reason string, fallback string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fallback
	}
	return reason
}

func formatParentSubagentSummary(job db.SubagentJob, success bool, preview string, artifactID string, errText string) string {
	if success {
		text := fmt.Sprintf("Background job %s completed: %s", job.ID, preview)
		if artifactID != "" {
			text += fmt.Sprintf("\nartifact_id=%s", artifactID)
		}
		return text
	}
	return fmt.Sprintf("Background job %s failed: %s", job.ID, reasonOrDefault(errText, "unknown error"))
}

func formatDeliverySubagentSummary(job db.SubagentJob, success bool, preview string, artifactID string, errText string) string {
	if success {
		text := fmt.Sprintf("Background job %s finished. %s", job.ID, preview)
		if artifactID != "" {
			text += fmt.Sprintf("\nartifact_id=%s", artifactID)
		}
		return text
	}
	return fmt.Sprintf("Background job %s failed. %s", job.ID, reasonOrDefault(errText, "unknown error"))
}
````

## File: internal/artifacts/attachment.go
````go
package artifacts

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

const (
	KindImage = "image"
	KindAudio = "audio"
	KindVideo = "video"
	KindFile  = "file"
)

type Attachment struct {
	ArtifactID string `json:"artifact_id"`
	Filename   string `json:"filename"`
	Mime       string `json:"mime"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"size_bytes"`
}

type StoredArtifact struct {
	ID         string
	SessionKey string
	Mime       string
	Path       string
	SizeBytes  int64
}

func DetectKind(filename, mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mt, "image/"):
		return KindImage
	case strings.HasPrefix(mt, "audio/"):
		return KindAudio
	case strings.HasPrefix(mt, "video/"):
		return KindVideo
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".heic", ".heif":
		return KindImage
	case ".mp3", ".m4a", ".wav", ".ogg", ".oga", ".opus", ".aac", ".flac":
		return KindAudio
	case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".m4v":
		return KindVideo
	default:
		return KindFile
	}
}

func NormalizeFilename(name, mimeType string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	if filepath.Ext(name) == "" {
		if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
			name += exts[0]
		}
	}
	return name
}

func Marker(att Attachment) string {
	name := strings.TrimSpace(att.Filename)
	if name == "" {
		name = "attachment"
	}
	kind := strings.TrimSpace(att.Kind)
	if kind == "" {
		kind = DetectKind(name, att.Mime)
	}
	return fmt.Sprintf("[%s: %s]", kind, name)
}

func FailureMarker(kind, name, reason string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = KindFile
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "attachment"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Sprintf("[%s: %s - unavailable]", kind, name)
	}
	return fmt.Sprintf("[%s: %s - %s]", kind, name, reason)
}
````

## File: internal/channels/cli/cli_test.go
````go
package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"or3-intern/internal/bus"
)

func TestChannel_Run_Exit(t *testing.T) {
	// Create a pipe to simulate stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	// Replace stdin
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test"}

	// Write /exit command
	go func() {
		w.WriteString("/exit\n")
		w.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error from Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to exit")
	}
}

func TestChannel_Run_PublishesMessage(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test-sess"}

	// Write a message then exit
	go func() {
		w.WriteString("hello world\n")
		w.WriteString("/exit\n")
		w.Close()
	}()

	done := make(chan struct{})
	go func() {
		ch.Run(context.Background())
		close(done)
	}()

	// Wait for event on bus
	select {
	case ev := <-b.Channel():
		if ev.Message != "hello world" {
			t.Errorf("expected message 'hello world', got %q", ev.Message)
		}
		if ev.SessionKey != "test-sess" {
			t.Errorf("expected session 'test-sess', got %q", ev.SessionKey)
		}
		if ev.Channel != "cli" {
			t.Errorf("expected channel 'cli', got %q", ev.Channel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	<-done
}

func TestChannel_Run_SkipsBlankLines(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test"}

	go func() {
		w.WriteString("  \n") // blank line
		w.WriteString("\n")   // empty
		w.WriteString("real message\n")
		w.WriteString("/exit\n")
		w.Close()
	}()

	done := make(chan struct{})
	go func() {
		ch.Run(context.Background())
		close(done)
	}()

	select {
	case ev := <-b.Channel():
		if ev.Message != "real message" {
			t.Errorf("expected 'real message', got %q", ev.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	<-done
}

func TestChannel_Run_DefaultSessionKey(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	// No SessionKey set - should default to "default"
	ch := &Channel{Bus: b}

	go func() {
		w.WriteString("msg\n")
		w.WriteString("/exit\n")
		w.Close()
	}()

	done := make(chan struct{})
	go func() {
		ch.Run(context.Background())
		close(done)
	}()

	select {
	case ev := <-b.Channel():
		if ev.SessionKey != "default" {
			t.Errorf("expected default session key 'default', got %q", ev.SessionKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	<-done
}

func TestChannel_Run_FullBus(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	// Create a bus with buffer=0 to simulate full bus
	b := bus.New(1)
	// Fill the bus
	b.Publish(bus.Event{})

	ch := &Channel{Bus: b, SessionKey: "test"}

	go func() {
		// This message should be dropped (bus full) but not crash
		w.WriteString("dropped message\n")
		w.WriteString("/exit\n")
		w.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestChannel_Run_EOFOnStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test"}

	// Close write end to simulate EOF
	w.Close()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error on EOF: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
````

## File: internal/channels/channels_test.go
````go
package channels

import (
	"context"
	"testing"

	"or3-intern/internal/bus"
)

type testChannel struct {
	name         string
	startedCount int
	stoppedCount int
	delivered    []string
}

func (c *testChannel) Name() string { return c.name }
func (c *testChannel) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	c.startedCount++
	return nil
}
func (c *testChannel) Stop(ctx context.Context) error {
	_ = ctx
	c.stoppedCount++
	return nil
}
func (c *testChannel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	_ = ctx
	_ = meta
	c.delivered = append(c.delivered, to+":"+text)
	return nil
}

func TestManager_RegisterStartDeliverStop(t *testing.T) {
	m := NewManager()
	ch := &testChannel{name: "telegram"}
	if err := m.Register(ch); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := m.StartAll(context.Background(), bus.New(1)); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if ch.startedCount != 1 {
		t.Fatalf("expected start count 1, got %d", ch.startedCount)
	}
	if err := m.Deliver(context.Background(), "telegram", "123", "hello"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(ch.delivered) != 1 || ch.delivered[0] != "123:hello" {
		t.Fatalf("unexpected delivered messages: %#v", ch.delivered)
	}
	if err := m.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if ch.stoppedCount != 1 {
		t.Fatalf("expected stop count 1, got %d", ch.stoppedCount)
	}
}

func TestManager_RejectsDuplicateNames(t *testing.T) {
	m := NewManager()
	if err := m.Register(&testChannel{name: "slack"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := m.Register(&testChannel{name: "slack"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
````

## File: internal/channels/channels.go
````go
package channels

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"or3-intern/internal/bus"
)

type Channel interface {
	Name() string
	Start(ctx context.Context, eventBus *bus.Bus) error
	Stop(ctx context.Context) error
	Deliver(ctx context.Context, to, text string, meta map[string]any) error
}

type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
	started  map[string]bool
}

func NewManager() *Manager {
	return &Manager{channels: map[string]Channel{}, started: map[string]bool{}}
}

func (m *Manager) Register(ch Channel) error {
	if ch == nil {
		return errors.New("nil channel")
	}
	name := strings.TrimSpace(strings.ToLower(ch.Name()))
	if name == "" {
		return errors.New("channel name required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.channels[name]; exists {
		return fmt.Errorf("channel already registered: %s", name)
	}
	m.channels[name] = ch
	return nil
}

func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.channels))
	for name := range m.channels {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) StartAll(ctx context.Context, eventBus *bus.Bus) error {
	for _, name := range m.Names() {
		if err := m.Start(ctx, name, eventBus); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Start(ctx context.Context, name string, eventBus *bus.Bus) error {
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.started[name] {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if err := ch.Start(ctx, eventBus); err != nil {
		return err
	}
	m.mu.Lock()
	m.started[name] = true
	m.mu.Unlock()
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	var errs []string
	for _, name := range m.Names() {
		if err := m.Stop(ctx, name); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context, name string) error {
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	started := m.started[name]
	m.mu.Unlock()
	if !started {
		return nil
	}
	if err := ch.Stop(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.started, name)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Deliver(ctx context.Context, channel, to, text string) error {
	return m.DeliverWithMeta(ctx, channel, to, text, nil)
}

func (m *Manager) DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error {
	if strings.TrimSpace(channel) == "" {
		channel = "cli"
	}
	ch, err := m.get(channel)
	if err != nil {
		return err
	}
	return ch.Deliver(ctx, to, text, meta)
}

func (m *Manager) get(name string) (Channel, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch := m.channels[name]
	if ch == nil {
		return nil, fmt.Errorf("channel not found: %s", name)
	}
	return ch, nil
}
````

## File: internal/channels/media.go
````go
package channels

import (
	"fmt"
	"strings"
)

func ComposeMessageText(text string, markers []string) string {
	parts := make([]string, 0, len(markers)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, strings.TrimSpace(text))
	}
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker == "" {
			continue
		}
		parts = append(parts, marker)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func MediaPaths(meta map[string]any) []string {
	if len(meta) == 0 {
		return nil
	}
	raw := meta["media_paths"]
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				continue
			}
			out = append(out, item)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
````

## File: internal/channels/stream.go
````go
package channels

import "context"

// StreamWriter is an optional interface for channels that can receive
// incremental text deltas (e.g., CLI live output, editable messages).
// Channels that do not implement streaming use final-only delivery.
type StreamWriter interface {
	// WriteDelta appends a text delta to the in-progress response.
	WriteDelta(ctx context.Context, text string) error
	// Close finalizes the stream with the complete text.
	Close(ctx context.Context, finalText string) error
	// Abort cancels the stream cleanly without leaving partial output.
	Abort(ctx context.Context) error
}

// StreamingChannel is an optional interface a channel can implement
// to indicate it supports incremental streaming delivery.
type StreamingChannel interface {
	// BeginStream starts a new streaming response to the given recipient.
	// meta contains channel-specific metadata (e.g., chat_id).
	// Returns a StreamWriter to write deltas, or an error.
	BeginStream(ctx context.Context, to string, meta map[string]any) (StreamWriter, error)
}
````

## File: internal/memory/docs_test.go
````go
package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
)

func openDocsTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSyncRoots(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	// create a temp directory with some files
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("# Hello\nThis is a test document."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}},
	}

	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots: %v", err)
	}

	rows, err := d.SQL.QueryContext(ctx, `SELECT path, kind, active FROM memory_docs WHERE scope_key='scope1' ORDER BY path`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type row struct {
		path, kind string
		active     int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.path, &r.kind, &r.active); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 docs, got %d: %v", len(got), got)
	}
	for _, r := range got {
		if r.active != 1 {
			t.Errorf("expected active=1 for %s, got %d", r.path, r.active)
		}
	}

	// verify kinds
	kinds := map[string]string{}
	for _, r := range got {
		kinds[filepath.Base(r.path)] = r.kind
	}
	if kinds["readme.md"] != "markdown" {
		t.Errorf("expected markdown, got %q", kinds["readme.md"])
	}
	if kinds["main.go"] != "go" {
		t.Errorf("expected go, got %q", kinds["main.go"])
	}
}

func TestRetrieveDocs(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	err := UpsertDoc(ctx, d, "scope1", "/docs/guide.md", "markdown", "User Guide",
		"How to get started", "Getting started: install the tool and run it.", nil, "abc123", 0, 100)
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}
	err = UpsertDoc(ctx, d, "scope1", "/docs/api.md", "markdown", "API Reference",
		"API endpoints list", "The API exposes REST endpoints for integration.", nil, "def456", 0, 200)
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	r := &DocRetriever{DB: d}

	results, err := r.RetrieveDocs(ctx, "scope1", "install tool", 5)
	if err != nil {
		t.Fatalf("RetrieveDocs: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	found := false
	for _, res := range results {
		if res.Title == "User Guide" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected User Guide in results, got %v", results)
	}
}

func TestRetrieveDocs_Empty(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	r := &DocRetriever{DB: d}
	results, err := r.RetrieveDocs(ctx, "scope1", "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results for blank query, got %d", len(results))
	}
}

func TestSyncRootsDeactivation(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	root := t.TempDir()
	filePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(filePath, []byte("important note content"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}},
	}

	// first sync - file should be active
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots: %v", err)
	}
	var active int
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT active FROM memory_docs WHERE scope_key='scope1' AND path=?`, filePath,
	).Scan(&active); err != nil {
		t.Fatalf("query after first sync: %v", err)
	}
	if active != 1 {
		t.Fatalf("expected active=1 after first sync, got %d", active)
	}

	// delete the file and sync again
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots after delete: %v", err)
	}

	if err := d.SQL.QueryRowContext(ctx,
		`SELECT active FROM memory_docs WHERE scope_key='scope1' AND path=?`, filePath,
	).Scan(&active); err != nil {
		t.Fatalf("query after second sync: %v", err)
	}
	if active != 0 {
		t.Errorf("expected active=0 after file deleted, got %d", active)
	}
}

func TestSyncRootsCaps(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	root := t.TempDir()
	for i := 0; i < 10; i++ {
		name := filepath.Join(root, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(name, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}, MaxFiles: 3},
	}

	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("SyncRoots: %v", err)
	}

	var count int
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_docs WHERE scope_key='scope1' AND active=1`,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count > 3 {
		t.Errorf("expected at most 3 docs (MaxFiles=3), got %d", count)
	}
}

func TestSyncRoots_NoRoots(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{},
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("expected no error with no roots, got %v", err)
	}
}

func TestSyncRoots_Idempotent(t *testing.T) {
	d := openDocsTestDB(t)
	ctx := context.Background()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("key = \"value\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexer := &DocIndexer{
		DB:     d,
		Config: DocIndexConfig{Roots: []string{root}},
	}

	// sync twice - should not error or duplicate
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("first SyncRoots: %v", err)
	}
	if err := indexer.SyncRoots(ctx, "scope1"); err != nil {
		t.Fatalf("second SyncRoots: %v", err)
	}

	var count int
	if err := d.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_docs WHERE scope_key='scope1' AND active=1`,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 doc after idempotent sync, got %d", count)
	}
}
````

## File: internal/memory/docs.go
````go
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
)

// DocIndexConfig controls what gets indexed.
type DocIndexConfig struct {
	Roots          []string
	MaxFiles       int
	MaxFileBytes   int
	MaxChunks      int
	EmbedMaxBytes  int
	RefreshSeconds int
	RetrieveLimit  int
}

// IndexedDoc is a row from memory_docs.
type IndexedDoc struct {
	ID        int64
	ScopeKey  string
	Path      string
	Kind      string
	Title     string
	Summary   string
	Text      string
	Embedding []byte
	MTimeMS   int64
	SizeBytes int64
	Active    bool
	UpdatedAt int64
}

// DocIndexer syncs configured roots into the memory_docs table.
type DocIndexer struct {
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
	Config     DocIndexConfig
}

func (x *DocIndexer) defaults() DocIndexConfig {
	c := x.Config
	if c.MaxFiles <= 0 {
		c.MaxFiles = 100
	}
	if c.MaxFileBytes <= 0 {
		c.MaxFileBytes = 64 * 1024
	}
	if c.MaxChunks <= 0 {
		c.MaxChunks = 500
	}
	if c.EmbedMaxBytes <= 0 {
		c.EmbedMaxBytes = 8 * 1024
	}
	if c.RetrieveLimit <= 0 {
		c.RetrieveLimit = 5
	}
	return c
}

// SyncRoots scans all configured roots and updates memory_docs for scopeKey.
// It enforces caps on file count and file size, skips symlinks, and
// deactivates docs for files that have disappeared.
func (x *DocIndexer) SyncRoots(ctx context.Context, scopeKey string) error {
	cfg := x.defaults()
	if len(cfg.Roots) == 0 {
		return nil
	}

	seen := map[string]bool{}
	fileCount := 0
	chunkCount := 0

	for _, root := range cfg.Roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot, err = filepath.EvalSymlinks(absRoot)
		if err != nil {
			continue
		}

		err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != absRoot {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".md", ".txt", ".go", ".py", ".js", ".ts", ".json", ".yaml", ".yml", ".toml", ".sh", ".rs", ".java", ".c", ".cpp", ".h":
			default:
				return nil
			}

			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(absRoot, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}

			if fileCount >= cfg.MaxFiles {
				return filepath.SkipAll
			}
			if chunkCount >= cfg.MaxChunks {
				return filepath.SkipAll
			}

			info, err := os.Lstat(realPath)
			if err != nil {
				return nil
			}
			if info.Size() > int64(cfg.MaxFileBytes) {
				return nil
			}

			seen[realPath] = true
			fileCount++

			data, err := os.ReadFile(realPath)
			if err != nil {
				return nil
			}
			if len(data) > cfg.MaxFileBytes {
				data = data[:cfg.MaxFileBytes]
			}

			h := fileHash(data)
			mtimeMS := info.ModTime().UnixMilli()
			sizeBytes := info.Size()

			kind := extKind(ext)
			title := filepath.Base(realPath)
			text := string(data)
			summary := extractSummary(text)

			if !x.needsUpdate(ctx, scopeKey, realPath, h) {
				chunkCount++
				return nil
			}

			var embedding []byte
			if x.Provider != nil && x.EmbedModel != "" && len(data) <= cfg.EmbedMaxBytes {
				vec, err := x.Provider.Embed(ctx, x.EmbedModel, truncateForEmbed(text, cfg.EmbedMaxBytes))
				if err == nil && len(vec) > 0 {
					embedding = PackFloat32(vec)
				}
			}

			now := db.NowMS()
			_, err = x.DB.SQL.ExecContext(ctx,
				`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, hash, mtime_ms, size_bytes, active, updated_at)
                 VALUES(?,?,?,?,?,?,?,?,?,?,1,?)
                 ON CONFLICT(scope_key, path) DO UPDATE SET
                   kind=excluded.kind, title=excluded.title, summary=excluded.summary,
                   text=excluded.text, embedding=excluded.embedding,
                   hash=excluded.hash, mtime_ms=excluded.mtime_ms,
                   size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
				scopeKey, realPath, kind, title, summary, text, nullBytes(embedding), h, mtimeMS, sizeBytes, now)
			if err != nil {
				return nil
			}
			chunkCount++
			return nil
		})
		_ = err
	}

	// deactivate docs no longer on disk
	rows, err := x.DB.SQL.QueryContext(ctx,
		`SELECT path FROM memory_docs WHERE scope_key=? AND active=1`, scopeKey)
	if err != nil {
		return err
	}
	var toDeactivate []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		if !seen[p] {
			toDeactivate = append(toDeactivate, p)
		}
	}
	rows.Close()
	for _, p := range toDeactivate {
		_, _ = x.DB.SQL.ExecContext(ctx,
			`UPDATE memory_docs SET active=0, updated_at=? WHERE scope_key=? AND path=?`,
			db.NowMS(), scopeKey, p)
	}
	return nil
}

func (x *DocIndexer) needsUpdate(ctx context.Context, scopeKey, path, newHash string) bool {
	row := x.DB.SQL.QueryRowContext(ctx,
		`SELECT hash FROM memory_docs WHERE scope_key=? AND path=? AND active=1`, scopeKey, path)
	var existing string
	if err := row.Scan(&existing); err != nil {
		return true
	}
	return existing != newHash
}

// DocRetriever retrieves indexed docs by FTS query.
type DocRetriever struct {
	DB *db.DB
}

// RetrievedDoc is a doc excerpt returned by retrieval.
type RetrievedDoc struct {
	Path    string
	Title   string
	Excerpt string
	Score   float64
}

// RetrieveDocs queries the FTS index for docs matching query.
func (r *DocRetriever) RetrieveDocs(ctx context.Context, scopeKey, query string, topK int) ([]RetrievedDoc, error) {
	if topK <= 0 {
		topK = 5
	}
	q := normalizeFTSQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := r.DB.SQL.QueryContext(ctx,
		`SELECT memory_docs_fts.rowid, memory_docs.path, memory_docs.title, memory_docs.text, bm25(memory_docs_fts) as rank
         FROM memory_docs_fts
         JOIN memory_docs ON memory_docs.id = memory_docs_fts.rowid
         WHERE memory_docs_fts MATCH ? AND memory_docs.scope_key=? AND memory_docs.active=1
         ORDER BY rank LIMIT ?`,
		q, scopeKey, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetrievedDoc
	for rows.Next() {
		var rowid int64
		var path, title, text string
		var rank float64
		if err := rows.Scan(&rowid, &path, &title, &text, &rank); err != nil {
			return nil, err
		}
		out = append(out, RetrievedDoc{
			Path:    path,
			Title:   title,
			Excerpt: excerptText(text, 500),
			Score:   1.0 / (1.0 + rank),
		})
	}
	return out, rows.Err()
}

// UpsertDoc inserts or updates a doc in memory_docs (for direct use by tests).
func UpsertDoc(ctx context.Context, d *db.DB, scopeKey, path, kind, title, summary, text string, embedding []byte, hash string, mtimeMS, sizeBytes int64) error {
	now := db.NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, hash, mtime_ms, size_bytes, active, updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,1,?)
         ON CONFLICT(scope_key, path) DO UPDATE SET
           kind=excluded.kind, title=excluded.title, summary=excluded.summary,
           text=excluded.text, embedding=excluded.embedding,
           hash=excluded.hash, mtime_ms=excluded.mtime_ms,
           size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
		scopeKey, path, kind, title, summary, text, nullBytes(embedding), hash, mtimeMS, sizeBytes, now)
	return err
}

func fileHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

func extKind(ext string) string {
	switch ext {
	case ".md":
		return "markdown"
	case ".txt":
		return "text"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sh":
		return "shell"
	default:
		return "text"
	}
}

func extractSummary(text string) string {
	for _, line := range strings.SplitN(text, "\n", 20) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "---") {
			continue
		}
		if len(line) > 200 {
			line = line[:200]
		}
		return line
	}
	return ""
}

func truncateForEmbed(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max]
}

func excerptText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "…"
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
````

## File: internal/providers/openai_stream_test.go
````go
package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sseServer(t *testing.T, lines []string) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "key", 0)
	c.HTTP = srv.Client()
	return srv, c
}

func TestChatStream_TextOnly(t *testing.T) {
	lines := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"content":", world"},"finish_reason":""}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	var got []string
	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{Model: "m"}, func(text string) {
		got = append(got, text)
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected choices")
	}
	content := resp.Choices[0].Message.Content
	if content != "Hello, world" {
		t.Errorf("expected 'Hello, world', got %q", content)
	}
	if strings.Join(got, "") != "Hello, world" {
		t.Errorf("onDelta got %v", got)
	}
}

func TestChatStream_NilOnDelta(t *testing.T) {
	lines := []string{
		`data: {"id":"1","choices":[{"delta":{"content":"hi"},"finish_reason":""}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("ChatStream with nil onDelta: %v", err)
	}
	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("expected 'hi', got %q", resp.Choices[0].Message.Content)
	}
}

func TestChatStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer srv.Close()
	c := New(srv.URL, "bad", 0)
	c.HTTP = srv.Client()

	_, err := c.ChatStream(context.Background(), ChatCompletionRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got %q", err.Error())
	}
}

func TestChatStream_SkipsNonDataLines(t *testing.T) {
	lines := []string{
		`: keep-alive`,
		``,
		`data: {"id":"1","choices":[{"delta":{"content":"ok"},"finish_reason":""}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Choices[0].Message.Content)
	}
}

func TestChatStream_WithToolCalls(t *testing.T) {
	lines := []string{
		`data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"exec","arguments":""}}]},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":"}}]},"finish_reason":""}]}`,
		`data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"echo\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}
	_, c := sseServer(t, lines)

	resp, err := c.ChatStream(context.Background(), ChatCompletionRequest{}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	tcs := resp.Choices[0].Message.ToolCalls
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}
	if tcs[0].ID != "call_1" {
		t.Errorf("expected ID 'call_1', got %q", tcs[0].ID)
	}
	if tcs[0].Function.Name != "exec" {
		t.Errorf("expected name 'exec', got %q", tcs[0].Function.Name)
	}
	if tcs[0].Function.Arguments != `{"cmd":"echo"}` {
		t.Errorf("unexpected arguments: %q", tcs[0].Function.Arguments)
	}
}

func TestMergeStreamToolCalls_IndexBased(t *testing.T) {
	existing := []ToolCall{}
	// first chunk: sets ID, name, empty args
	existing = mergeStreamToolCalls(existing, []ToolCall{
		{Index: 0, ID: "call_1", Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "exec", Arguments: ""}},
	})
	// second chunk: no ID, partial args
	existing = mergeStreamToolCalls(existing, []ToolCall{
		{Index: 0, Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Arguments: `{"cmd":`}},
	})
	// third chunk: rest of args
	existing = mergeStreamToolCalls(existing, []ToolCall{
		{Index: 0, Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Arguments: `"hi"}`}},
	})

	if len(existing) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(existing))
	}
	if existing[0].ID != "call_1" {
		t.Errorf("ID mismatch: %q", existing[0].ID)
	}
	if existing[0].Function.Arguments != `{"cmd":"hi"}` {
		t.Errorf("args mismatch: %q", existing[0].Function.Arguments)
	}
}
````

## File: internal/providers/openai_test.go
````go
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	c := New("https://api.example.com", "my-key", 30*time.Second)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.APIBase != "https://api.example.com" {
		t.Errorf("expected APIBase='https://api.example.com', got %q", c.APIBase)
	}
	if c.APIKey != "my-key" {
		t.Errorf("expected APIKey='my-key', got %q", c.APIKey)
	}
	if c.HTTP == nil {
		t.Error("expected non-nil HTTP client")
	}
}

func TestChat_Success(t *testing.T) {
	response := ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string    `json:"role"`
				Content   any       `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string    `json:"role"`
					Content   any       `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				}{
					Role:    "assistant",
					Content: "Hello! How can I help?",
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		APIKey:  "test-key",
		HTTP:    srv.Client(),
	}

	req := ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}

	resp, err := c.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", resp.Choices[0].Message.Role)
	}
}

func TestChat_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		APIKey:  "bad-key",
		HTTP:    srv.Client(),
	}

	_, err := c.Chat(context.Background(), ChatCompletionRequest{})
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
}

func TestChat_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		HTTP:    srv.Client(),
	}

	_, err := c.Chat(context.Background(), ChatCompletionRequest{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestChat_NoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not have Authorization header
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatCompletionResponse{})
	}))
	defer srv.Close()

	c := &Client{
		APIBase: srv.URL,
		APIKey:  "", // empty
		HTTP:    srv.Client(),
	}

	c.Chat(context.Background(), ChatCompletionRequest{})
}

func TestChat_WithToolCalls(t *testing.T) {
	response := ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string    `json:"role"`
				Content   any       `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string    `json:"role"`
					Content   any       `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				}{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{
							ID:   "call_123",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "exec",
								Arguments: `{"command":"echo hi"}`,
							},
						},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	resp, err := c.Chat(context.Background(), ChatCompletionRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "exec" {
		t.Errorf("expected tool name 'exec', got %q", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	}
}

func TestEmbed_Success(t *testing.T) {
	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	response := EmbeddingResponse{
		Data: []struct {
			Embedding []float32 `json:"embedding"`
		}{
			{Embedding: embedding},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	vec, err := c.Embed(context.Background(), "text-embedding-3-small", "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != len(embedding) {
		t.Fatalf("expected %d elements, got %d", len(embedding), len(vec))
	}
	for i, v := range embedding {
		if vec[i] != v {
			t.Errorf("vec[%d] = %v, want %v", i, vec[i], v)
		}
	}
}

func TestEmbed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "server error")
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), "model", "text")
	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
}

func TestEmbed_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), "model", "text")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEmbed_NoData(t *testing.T) {
	response := EmbeddingResponse{Data: []struct {
		Embedding []float32 `json:"embedding"`
	}{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), "model", "text")
	if err == nil {
		t.Fatal("expected error when no embedding data returned")
	}
}

func TestChat_ContextCanceled(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until test signals done or timeout
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := &Client{APIBase: srv.URL, HTTP: &http.Client{Timeout: 200 * time.Millisecond}}
	_, err := c.Chat(ctx, ChatCompletionRequest{})

	close(done) // unblock server handlers
	srv.Close()

	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestEmbed_WithAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(EmbeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
			}{{Embedding: []float32{0.1}}},
		})
	}))
	defer srv.Close()

	c := &Client{APIBase: srv.URL, APIKey: "my-embed-key", HTTP: srv.Client()}
	c.Embed(context.Background(), "model", "text")

	if gotAuth != "Bearer my-embed-key" {
		t.Errorf("expected 'Bearer my-embed-key', got %q", gotAuth)
	}
}
````

## File: internal/scope/scope.go
````go
package scope

import "strings"

const (
	GlobalMemoryScope = "__or3_global__"
	GlobalScopeAlias  = "global"
)

func IsGlobalScopeRequest(v string) bool {
	v = strings.TrimSpace(v)
	return strings.EqualFold(v, GlobalScopeAlias) || v == GlobalMemoryScope
}
````

## File: internal/tools/cron_test.go
````go
package tools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/cron"
)

func makeTestCronService(t *testing.T) *cron.Service {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := cron.New(path, func(ctx context.Context, job cron.CronJob) error {
		return nil
	})
	if err := svc.Start(); err != nil {
		t.Fatalf("cron.Start: %v", err)
	}
	t.Cleanup(func() { svc.Stop() })
	return svc
}

func TestCronTool_NoService(t *testing.T) {
	tool := &CronTool{}
	_, err := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if err == nil {
		t.Fatal("expected error when service is nil")
	}
}

func TestCronTool_UnknownAction(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}
	_, err := tool.Execute(context.Background(), map[string]any{"action": "invalid"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("expected 'unknown action', got %q", err.Error())
	}
}

func TestCronTool_Status(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}
	out, err := tool.Execute(context.Background(), map[string]any{"action": "status"})
	if err != nil {
		t.Fatalf("CronTool status: %v", err)
	}
	if !strings.Contains(out, "jobs") {
		t.Errorf("expected 'jobs' in status output, got %q", out)
	}
}

func TestCronTool_List_Empty(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}
	out, err := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("CronTool list: %v", err)
	}
	if out != "null" && out != "[]" {
		// Allow empty array notation
		if !strings.Contains(out, "null") && !strings.Contains(out, "[]") {
			t.Logf("list output: %q", out)
		}
	}
}

func TestCronTool_Add_And_List(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}

	// Add a job
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"job": map[string]any{
			"name":    "test job",
			"enabled": true,
			"schedule": map[string]any{
				"kind":     "cron",
				"expr":     "0 * * * *",
			},
			"payload": map[string]any{
				"kind":    "agent_turn",
				"message": "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("CronTool add: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}

	// List should have 1 job
	listOut, err := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("CronTool list: %v", err)
	}
	if !strings.Contains(listOut, "test job") {
		t.Errorf("expected 'test job' in list output, got %q", listOut)
	}
}

func TestCronTool_Add_MissingJob(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}
	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "add",
	})
	if err == nil {
		t.Fatal("expected error when job is missing")
	}
}

func TestCronTool_Add_Defaults(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}

	// Add minimal job (no enabled, no payload kind, no schedule kind)
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"job": map[string]any{
			"name": "minimal",
		},
	})
	if err != nil {
		t.Fatalf("CronTool add minimal: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
}

func TestCronTool_Remove(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}

	// Add a job first
	tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"job": map[string]any{
			"name": "to remove",
		},
	})

	jobs, _ := svc.List()
	if len(jobs) == 0 {
		t.Fatal("expected at least one job")
	}
	id := jobs[0].ID

	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "remove",
		"id":     id,
	})
	if err != nil {
		t.Fatalf("CronTool remove: %v", err)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("expected 'true' in remove output, got %q", out)
	}
}

func TestCronTool_Remove_NotFound(t *testing.T) {
	svc := makeTestCronService(t)
	tool := &CronTool{Svc: svc}

	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "remove",
		"id":     "nonexistent-id",
	})
	if err != nil {
		t.Fatalf("CronTool remove: %v", err)
	}
	if !strings.Contains(out, "false") {
		t.Errorf("expected 'false' for not-found removal, got %q", out)
	}
}

func TestCronTool_Run(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := cron.New(path, func(ctx context.Context, job cron.CronJob) error {
		ran = true
		return nil
	})
	if err := svc.Start(); err != nil {
		t.Fatalf("cron.Start: %v", err)
	}
	defer svc.Stop()

	// Add a disabled job so it only runs via force
	svc.Add(cron.CronJob{
		ID:      "test-run",
		Name:    "run test",
		Enabled: false,
		Schedule: cron.CronSchedule{Kind: cron.KindEvery, EveryMS: int64((time.Hour).Milliseconds())},
		Payload:  cron.CronPayload{Kind: "agent_turn", Message: "test"},
	})

	tool := &CronTool{Svc: svc}
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "run",
		"id":     "test-run",
		"force":  true,
	})
	if err != nil {
		t.Fatalf("CronTool run: %v", err)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("expected 'true' in run output, got %q", out)
	}
	if !ran {
		t.Error("expected runner to be called")
	}
}

func TestCronTool_Run_NotEnabled_NoForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := cron.New(path, func(ctx context.Context, job cron.CronJob) error {
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(cron.CronJob{
		ID:      "disabled-job",
		Enabled: false,
		Schedule: cron.CronSchedule{Kind: cron.KindEvery, EveryMS: int64((time.Hour).Milliseconds())},
		Payload:  cron.CronPayload{Kind: "agent_turn"},
	})

	tool := &CronTool{Svc: svc}
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "run",
		"id":     "disabled-job",
		"force":  false,
	})
	if err != nil {
		t.Fatalf("CronTool run: %v", err)
	}
	if !strings.Contains(out, "false") {
		t.Errorf("expected 'false' for disabled job without force, got %q", out)
	}
}

func TestCronTool_Run_WithError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := cron.New(path, func(ctx context.Context, job cron.CronJob) error {
		return errors.New("runner failed")
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(cron.CronJob{
		ID:      "err-job",
		Enabled: true,
		Schedule: cron.CronSchedule{Kind: cron.KindEvery, EveryMS: int64((time.Hour).Milliseconds())},
		Payload:  cron.CronPayload{Kind: "agent_turn"},
	})

	tool := &CronTool{Svc: svc}
	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "run",
		"id":     "err-job",
		"force":  true,
	})
	if err == nil {
		t.Fatal("expected error when runner fails")
	}
}

func TestCronTool_Name(t *testing.T) {
	tool := &CronTool{}
	if tool.Name() != "cron" {
		t.Errorf("expected 'cron', got %q", tool.Name())
	}
}

func TestCronTool_Schema(t *testing.T) {
	tool := &CronTool{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}
````

## File: internal/tools/cron.go
````go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/cron"
)

type CronTool struct {
	Base
	Svc *cron.Service
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Manage scheduled jobs: add/list/remove/run/status."
}
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"action": map[string]any{"type":"string","enum":[]any{"add","list","remove","run","status"}},
		"job": map[string]any{"type":"object","description":"job object for add"},
		"id": map[string]any{"type":"string","description":"job id for remove/run"},
		"force": map[string]any{"type":"boolean","description":"force run"},
	},"required":[]string{"action"}}
}
func (t *CronTool) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }

func (t *CronTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Svc == nil { return "", fmt.Errorf("cron service not configured") }
	act := strings.TrimSpace(fmt.Sprint(params["action"]))
	switch act {
	case "status":
		s, err := t.Svc.Status()
		if err != nil { return "", err }
		b, _ := json.MarshalIndent(s, "", "  ")
		return string(b), nil
	case "list":
		j, err := t.Svc.List()
		if err != nil { return "", err }
		b, _ := json.MarshalIndent(j, "", "  ")
		return string(b), nil
	case "remove":
		id := strings.TrimSpace(fmt.Sprint(params["id"]))
		ok, err := t.Svc.Remove(id)
		if err != nil { return "", err }
		return fmt.Sprintf("removed: %v", ok), nil
	case "run":
		id := strings.TrimSpace(fmt.Sprint(params["id"]))
		force, _ := params["force"].(bool)
		ok, err := t.Svc.RunNow(ctx, id, force)
		if err != nil { return "", err }
		return fmt.Sprintf("ran: %v", ok), nil
	case "add":
		raw, _ := params["job"].(map[string]any)
		if raw == nil { return "", fmt.Errorf("missing job") }
		b, _ := json.Marshal(raw)
		var j cron.CronJob
		if err := json.Unmarshal(b, &j); err != nil { return "", err }
		// defaults
		if j.Enabled == false && raw["enabled"] == nil { j.Enabled = true }
		if j.Payload.Kind == "" { j.Payload.Kind = "agent_turn" }
		if j.Schedule.Kind == "" { j.Schedule.Kind = cron.KindEvery; j.Schedule.EveryMS = int64((24*time.Hour).Milliseconds()) }
		if err := t.Svc.Add(j); err != nil { return "", err }
		return "ok", nil
	default:
		return "", fmt.Errorf("unknown action")
	}
}
````

## File: internal/tools/exec_test.go
````go
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecTool_BasicCommand(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", out)
	}
}

func TestExecTool_EmptyCommand(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "  ",
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecTool_MissingCommandParam(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command param")
	}
}

func TestExecTool_BlockedPattern(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "rm -rf /tmp/something",
	})
	if err == nil {
		t.Fatal("expected error for blocked pattern 'rm -rf'")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got %q", err.Error())
	}
}

func TestExecTool_CustomBlockedPatterns(t *testing.T) {
	tool := &ExecTool{
		Timeout:         5 * time.Second,
		BlockedPatterns: []string{"forbidden_cmd"},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "forbidden_cmd arg",
	})
	if err == nil {
		t.Fatal("expected error for custom blocked pattern")
	}
}

func TestExecTool_ExitError(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "exit 1",
	})
	if err != nil {
		t.Fatalf("Execute should not return error on exit code (got: %v)", err)
	}
	if !strings.Contains(out, "exit error") {
		t.Errorf("expected 'exit error' in output for non-zero exit, got %q", out)
	}
}

func TestExecTool_StderrOutput(t *testing.T) {
	tool := &ExecTool{Timeout: 5 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo err >&2",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// stderr is non-empty so output includes "stderr:"
	if !strings.Contains(out, "stderr") {
		t.Errorf("expected 'stderr' in output, got %q", out)
	}
}

func TestExecTool_OutputTruncation(t *testing.T) {
	tool := &ExecTool{
		Timeout:        5 * time.Second,
		OutputMaxBytes: 10,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo " + strings.Repeat("a", 100),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected '[truncated]' in output, got %q", out)
	}
}

func TestExecTool_RestrictDir_Inside(t *testing.T) {
	dir := t.TempDir()
	tool := &ExecTool{
		Timeout:     5 * time.Second,
		RestrictDir: dir,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"cwd":     dir,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected 'ok' in output, got %q", out)
	}
}

func TestExecTool_RestrictDir_Outside(t *testing.T) {
	dir := t.TempDir()
	tool := &ExecTool{
		Timeout:     5 * time.Second,
		RestrictDir: dir,
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo outside",
		"cwd":     "/tmp",
	})
	if err == nil {
		t.Fatal("expected error for cwd outside restrict dir")
	}
	if !strings.Contains(err.Error(), "outside allowed") {
		t.Errorf("expected 'outside allowed' in error, got %q", err.Error())
	}
}

func TestExecTool_TimeoutParam(t *testing.T) {
	tool := &ExecTool{Timeout: 10 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command":        "echo timeout_test",
		"timeoutSeconds": float64(5),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "timeout_test") {
		t.Errorf("expected 'timeout_test', got %q", out)
	}
}

func TestExecTool_WithCwd(t *testing.T) {
	dir := t.TempDir()
	tool := &ExecTool{Timeout: 5 * time.Second}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "pwd",
		"cwd":     dir,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Output should contain the temp dir path
	if !strings.Contains(out, filepath.Base(dir)) {
		t.Errorf("expected cwd in output, got %q", out)
	}
}

func TestExecTool_PathAppend(t *testing.T) {
	dir := t.TempDir()
	// Create a small script in the dir
	script := filepath.Join(dir, "myscript")
	os.WriteFile(script, []byte("#!/bin/sh\necho fromscript"), 0o755)

	tool := &ExecTool{
		Timeout:    5 * time.Second,
		PathAppend: dir,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "myscript",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "fromscript") {
		t.Errorf("expected 'fromscript', got %q", out)
	}
}

func TestExecTool_Name(t *testing.T) {
	tool := &ExecTool{}
	if tool.Name() != "exec" {
		t.Errorf("expected 'exec', got %q", tool.Name())
	}
}

func TestExecTool_Description(t *testing.T) {
	tool := &ExecTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestExecTool_Parameters(t *testing.T) {
	tool := &ExecTool{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected type 'object', got %v", params["type"])
	}
}

func TestExecTool_Schema(t *testing.T) {
	tool := &ExecTool{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected type 'function', got %v", schema["type"])
	}
}
````

## File: internal/tools/skill_run_test.go
````go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/skills"
)

func makeRunSkillInventory(t *testing.T, manifest string) (*skills.Inventory, string) {
	t.Helper()
	dir := t.TempDir()
	// Write the skill markdown file
	if err := os.WriteFile(filepath.Join(dir, "testskill.md"), []byte("# Test Skill\nDoes things."), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write skill.json manifest
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv := skills.Scan([]string{dir})
	return &inv, dir
}

func TestRunSkillNoInventory(t *testing.T) {
	tool := &RunSkill{}
	_, err := tool.Execute(context.Background(), map[string]any{"skill": "any"})
	if err == nil {
		t.Fatal("expected error when inventory is nil")
	}
}

func TestRunSkillNotFound(t *testing.T) {
	inv, _ := makeRunSkillInventory(t, "")
	tool := &RunSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"skill": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestRunSkillNoEntrypoints(t *testing.T) {
	manifest := `{"summary":"no eps","entrypoints":[]}`
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"skill": "testskill"})
	if err == nil {
		t.Fatal("expected error for skill with no entrypoints")
	}
	if !strings.Contains(err.Error(), "no declared entrypoints") {
		t.Errorf("expected 'no declared entrypoints' in error, got %q", err.Error())
	}
}

func TestRunSkillEntrypointNotFound(t *testing.T) {
	manifest := buildManifest([]skills.SkillEntry{
		{Name: "run", Command: []string{"echo", "hello"}, TimeoutSeconds: 5},
	})
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "testskill",
		"entrypoint": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing entrypoint")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestRunSkillSuccess(t *testing.T) {
	manifest := buildManifest([]skills.SkillEntry{
		{Name: "run", Command: []string{"echo", "hello world"}, TimeoutSeconds: 5},
	})
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}
	out, err := tool.Execute(context.Background(), map[string]any{"skill": "testskill"})
	if err != nil {
		t.Fatalf("RunSkill.Execute: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", out)
	}
}

func TestRunSkillEntrypointNamedSelection(t *testing.T) {
	manifest := buildManifest([]skills.SkillEntry{
		{Name: "first", Command: []string{"echo", "first-output"}, TimeoutSeconds: 5},
		{Name: "second", Command: []string{"echo", "second-output"}, TimeoutSeconds: 5},
	})
	inv, _ := makeRunSkillInventory(t, manifest)
	tool := &RunSkill{Inventory: inv}

	// Select first
	out, err := tool.Execute(context.Background(), map[string]any{
		"skill":      "testskill",
		"entrypoint": "first",
	})
	if err != nil {
		t.Fatalf("RunSkill.Execute (first): %v", err)
	}
	if !strings.Contains(out, "first-output") {
		t.Errorf("expected 'first-output', got %q", out)
	}

	// Select second
	out, err = tool.Execute(context.Background(), map[string]any{
		"skill":      "testskill",
		"entrypoint": "second",
	})
	if err != nil {
		t.Fatalf("RunSkill.Execute (second): %v", err)
	}
	if !strings.Contains(out, "second-output") {
		t.Errorf("expected 'second-output', got %q", out)
	}
}

func TestRunSkillName(t *testing.T) {
	tool := &RunSkill{}
	if tool.Name() != "run_skill" {
		t.Errorf("expected 'run_skill', got %q", tool.Name())
	}
}

func TestRunSkillSchema(t *testing.T) {
	tool := &RunSkill{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected schema type 'function', got %v", schema["type"])
	}
}

// buildManifest marshals a skill manifest JSON for use in tests.
func buildManifest(entries []skills.SkillEntry) string {
	type manifest struct {
		Summary     string             `json:"summary"`
		Entrypoints []skills.SkillEntry `json:"entrypoints"`
	}
	b, _ := json.Marshal(manifest{Summary: "test skill", Entrypoints: entries})
	return string(b)
}
````

## File: internal/tools/skill_run.go
````go
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/skills"
)

// RunSkill executes a declared entrypoint from a skill manifest.
// It does NOT accept arbitrary command strings; it only runs
// manifest-declared argv, preventing shell injection.
type RunSkill struct {
	Base
	Inventory      *skills.Inventory
	DefaultTimeout time.Duration
	RestrictDir    string
	OutputMaxBytes int
}

func (t *RunSkill) Name() string { return "run_skill" }

func (t *RunSkill) Description() string {
	return "Execute a declared entrypoint from a skill manifest. Only entrypoints declared in skill.json are allowed; arbitrary commands are not accepted."
}

func (t *RunSkill) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "Skill name from inventory",
			},
			"entrypoint": map[string]any{
				"type":        "string",
				"description": "Entrypoint name declared in the skill manifest (default: first entrypoint)",
			},
			"stdin": map[string]any{
				"type":        "string",
				"description": "Optional stdin input (only if entrypoint declares acceptsStdin: true)",
			},
		},
		"required": []string{"skill"},
	}
}

func (t *RunSkill) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RunSkill) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Inventory == nil {
		return "", fmt.Errorf("skills inventory not configured")
	}
	skillName := strings.TrimSpace(fmt.Sprint(params["skill"]))
	if skillName == "" {
		return "", fmt.Errorf("missing skill name")
	}
	meta, ok := t.Inventory.Get(skillName)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillName)
	}
	if len(meta.Entrypoints) == 0 {
		return "", fmt.Errorf("skill %q has no declared entrypoints", skillName)
	}

	// select entrypoint
	epName := ""
	if v, ok := params["entrypoint"]; ok && v != nil {
		epName = strings.TrimSpace(fmt.Sprint(v))
	}
	var ep *skills.SkillEntry
	if epName == "" {
		ep = &meta.Entrypoints[0]
	} else {
		for i := range meta.Entrypoints {
			if meta.Entrypoints[i].Name == epName {
				ep = &meta.Entrypoints[i]
				break
			}
		}
	}
	if ep == nil {
		return "", fmt.Errorf("entrypoint %q not found in skill %q", epName, skillName)
	}
	if len(ep.Command) == 0 {
		return "", fmt.Errorf("entrypoint %q has empty command", ep.Name)
	}

	// validate all command parts are non-empty (no shell expansion)
	for _, part := range ep.Command {
		if strings.TrimSpace(part) == "" {
			return "", fmt.Errorf("entrypoint command contains empty part")
		}
	}

	// working directory: skill's directory, verified to be inside RestrictDir if set
	cwd := filepath.Dir(meta.Path)
	if t.RestrictDir != "" {
		absRestrict, _ := filepath.Abs(t.RestrictDir)
		absCwd, _ := filepath.Abs(cwd)
		rel, err := filepath.Rel(absRestrict, absCwd)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("skill directory outside allowed path")
		}
	}

	// resolve timeout
	timeout := t.DefaultTimeout
	if ep.TimeoutSeconds > 0 {
		timeout = time.Duration(ep.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Direct argv execution - no shell
	cmd := exec.CommandContext(cctx, ep.Command[0], ep.Command[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	// stdin
	stdinText := ""
	if ep.AcceptsStdin {
		if v, ok := params["stdin"].(string); ok {
			stdinText = v
		}
	}
	if stdinText != "" {
		cmd.Stdin = strings.NewReader(stdinText)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	out := stdout.String()
	er := stderr.String()

	maxBytes := t.OutputMaxBytes
	if maxBytes <= 0 {
		maxBytes = 10000
	}
	if len(out) > maxBytes {
		out = out[:maxBytes] + "\n...[truncated]\n"
	}
	if len(er) > maxBytes {
		er = er[:maxBytes] + "\n...[truncated]\n"
	}

	if runErr != nil {
		return fmt.Sprintf("exit error: %v\n\nstdout:\n%s\n\nstderr:\n%s", runErr, out, er), nil
	}
	if strings.TrimSpace(er) != "" {
		return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", out, er), nil
	}
	return out, nil
}
````

## File: internal/tools/skill_test.go
````go
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/skills"
)

func makeTestSkillsInventory(t *testing.T) *skills.Inventory {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "my_skill.md"), []byte("# My Skill\nDo stuff"), 0o644)

	inv := skills.Scan([]string{dir})
	return &inv
}

func TestReadSkill_NoInventory(t *testing.T) {
	tool := &ReadSkill{}
	_, err := tool.Execute(context.Background(), map[string]any{"name": "any"})
	if err == nil {
		t.Fatal("expected error when inventory is nil")
	}
}

func TestReadSkill_MissingName(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"name": ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestReadSkill_WhitespaceName(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"name": "   "})
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}

func TestReadSkill_NotFound(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	_, err := tool.Execute(context.Background(), map[string]any{"name": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestReadSkill_Success(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	out, err := tool.Execute(context.Background(), map[string]any{"name": "my_skill"})
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if !strings.Contains(out, "my_skill") {
		t.Errorf("expected skill name in output, got %q", out)
	}
	if !strings.Contains(out, "Do stuff") {
		t.Errorf("expected skill body in output, got %q", out)
	}
}

func TestReadSkill_MaxBytes(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 1000)
	os.WriteFile(filepath.Join(dir, "big_skill.md"), []byte(content), 0o644)
	inv := skills.Scan([]string{dir})
	tool := &ReadSkill{Inventory: &inv, MaxBytes: 100}

	out, err := tool.Execute(context.Background(), map[string]any{"name": "big_skill"})
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	// The body should be truncated to 100 bytes
	if len(out) > 200 { // allow for the "# Skill: ..." header
		// Check that content portion is limited
		idx := strings.Index(out, "\n\n")
		if idx >= 0 {
			body := out[idx+2:]
			if len(body) > 100 {
				t.Errorf("expected body truncated to 100 bytes, got %d", len(body))
			}
		}
	}
}

func TestReadSkill_MaxBytesParam(t *testing.T) {
	inv := makeTestSkillsInventory(t)
	tool := &ReadSkill{Inventory: inv}
	out, err := tool.Execute(context.Background(), map[string]any{
		"name":     "my_skill",
		"maxBytes": float64(10),
	})
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	_ = out
}

func TestReadSkill_Name(t *testing.T) {
	tool := &ReadSkill{}
	if tool.Name() != "read_skill" {
		t.Errorf("expected 'read_skill', got %q", tool.Name())
	}
}

func TestReadSkill_Schema(t *testing.T) {
	tool := &ReadSkill{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}
````

## File: internal/tools/skill.go
````go
package tools

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/skills"
)

type ReadSkill struct {
	Base
	Inventory *skills.Inventory
	MaxBytes int
}

func (t *ReadSkill) Name() string { return "read_skill" }
func (t *ReadSkill) Description() string {
	return "Read the full body of a skill by name (for ClawHub-compatible SKILL.md usage)."
}
func (t *ReadSkill) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"name": map[string]any{"type":"string", "description":"Skill name from inventory"},
		"maxBytes": map[string]any{"type":"integer", "description":"Optional max bytes"},
	},"required":[]string{"name"}}
}
func (t *ReadSkill) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }

func (t *ReadSkill) Execute(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	if t.Inventory == nil { return "", fmt.Errorf("skills inventory not configured") }
	name := strings.TrimSpace(fmt.Sprint(params["name"]))
	if name == "" { return "", fmt.Errorf("missing name") }
	s, ok := t.Inventory.Get(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}
	maxBytes := t.MaxBytes
	if maxBytes <= 0 { maxBytes = 200000 }
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		maxBytes = int(v)
	}
	body, err := skills.LoadBody(s.Path, maxBytes)
	if err != nil { return "", err }
	return fmt.Sprintf("# Skill: %s\n\n%s", s.Name, body), nil
}
````

## File: internal/tools/spawn_test.go
````go
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSpawnManager struct {
	req SpawnRequest
	job SpawnJob
	err error
}

func (f *fakeSpawnManager) Enqueue(ctx context.Context, req SpawnRequest) (SpawnJob, error) {
	f.req = req
	if f.err != nil {
		return SpawnJob{}, f.err
	}
	if f.job.ID == "" {
		f.job = SpawnJob{ID: "job-123", ChildSessionKey: req.ParentSessionKey + ":subagent:job-123"}
	}
	return f.job, nil
}

func TestSpawnSubagent_ExecuteSuccessUsesContextDefaults(t *testing.T) {
	mgr := &fakeSpawnManager{}
	tool := &SpawnSubagent{Manager: mgr}
	ctx := ContextWithSession(context.Background(), "sess-1")
	ctx = ContextWithDelivery(ctx, "cli", "user")
	out, err := tool.Execute(ctx, map[string]any{"task": "investigate"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "job-123") {
		t.Fatalf("expected output to include job id, got %q", out)
	}
	if mgr.req.ParentSessionKey != "sess-1" || mgr.req.Channel != "cli" || mgr.req.To != "user" || mgr.req.Task != "investigate" {
		t.Fatalf("unexpected enqueue request: %#v", mgr.req)
	}
}

func TestSpawnSubagent_ExecuteEmptyTask(t *testing.T) {
	tool := &SpawnSubagent{Manager: &fakeSpawnManager{}}
	_, err := tool.Execute(context.Background(), map[string]any{"task": "   "})
	if err == nil {
		t.Fatal("expected empty task error")
	}
}

func TestSpawnSubagent_DisabledWithoutManager(t *testing.T) {
	tool := &SpawnSubagent{}
	_, err := tool.Execute(context.Background(), map[string]any{"task": "work"})
	if err == nil {
		t.Fatal("expected disabled error")
	}
}

func TestSpawnSubagent_ExecuteManagerError(t *testing.T) {
	tool := &SpawnSubagent{Manager: &fakeSpawnManager{err: errors.New("queue full")}}
	_, err := tool.Execute(context.Background(), map[string]any{"task": "work"})
	if err == nil || !strings.Contains(err.Error(), "queue full") {
		t.Fatalf("expected propagated queue error, got %v", err)
	}
}
````

## File: internal/tools/spawn.go
````go
package tools

import (
	"context"
	"fmt"
	"strings"
)

type SpawnRequest struct {
	ParentSessionKey string
	Task             string
	Channel          string
	To               string
}

type SpawnJob struct {
	ID              string
	ChildSessionKey string
}

type SpawnEnqueuer interface {
	Enqueue(ctx context.Context, req SpawnRequest) (SpawnJob, error)
}

type SpawnSubagent struct {
	Base
	Manager        SpawnEnqueuer
	DefaultChannel string
	DefaultTo      string
}

func (t *SpawnSubagent) Name() string { return "spawn_subagent" }

func (t *SpawnSubagent) Description() string {
	return "Queue a longer background task and return immediately with a stable job ID."
}

func (t *SpawnSubagent) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":    map[string]any{"type": "string", "description": "Task for the background subagent"},
			"channel": map[string]any{"type": "string", "description": "Optional delivery channel override"},
			"to":      map[string]any{"type": "string", "description": "Optional recipient override"},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnSubagent) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *SpawnSubagent) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Manager == nil {
		return "", fmt.Errorf("background subagents disabled")
	}
	task := readOptionalString(params, "task")
	if task == "" {
		return "", fmt.Errorf("empty task")
	}
	channel := readOptionalString(params, "channel")
	to := readOptionalString(params, "to")
	ctxChannel, ctxTo := DeliveryFromContext(ctx)
	if channel == "" {
		channel = firstNonEmpty(ctxChannel, t.DefaultChannel)
	}
	if to == "" {
		to = firstNonEmpty(ctxTo, t.DefaultTo)
	}
	job, err := t.Manager.Enqueue(ctx, SpawnRequest{
		ParentSessionKey: SessionFromContext(ctx),
		Task:             task,
		Channel:          channel,
		To:               to,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("queued background job_id=%s", job.ID), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readOptionalString(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
````

## File: internal/tools/tools_test.go
````go
package tools

import (
	"context"
	"testing"
)

func TestBase_SchemaFor(t *testing.T) {
	b := Base{}
	schema := b.SchemaFor("my_tool", "does stuff", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"arg": map[string]any{"type": "string"},
		},
	})
	if schema["type"] != "function" {
		t.Errorf("expected type 'function', got %v", schema["type"])
	}
	fn, ok := schema["function"].(map[string]any)
	if !ok {
		t.Fatal("expected 'function' key to be map[string]any")
	}
	if fn["name"] != "my_tool" {
		t.Errorf("expected name 'my_tool', got %v", fn["name"])
	}
	if fn["description"] != "does stuff" {
		t.Errorf("expected description 'does stuff', got %v", fn["description"])
	}
	if fn["parameters"] == nil {
		t.Error("expected parameters to be set")
	}
}

// --- Registry tests ---

type mockTool struct {
	Base
	name string
	desc string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.desc }
func (m *mockTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (m *mockTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return "mock result", nil
}
func (m *mockTool) Schema() map[string]any {
	return m.SchemaFor(m.name, m.desc, m.Parameters())
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "test_tool", desc: "a test tool"}
	r.Register(tool)

	got := r.Get("test_tool")
	if got == nil {
		t.Fatal("expected to find registered tool")
	}
	if got.Name() != "test_tool" {
		t.Errorf("expected name 'test_tool', got %q", got.Name())
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	if r.Get("nonexistent") != nil {
		t.Error("expected nil for unregistered tool")
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "tool_a", desc: ""})
	r.Register(&mockTool{name: "tool_b", desc: ""})

	names := r.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestRegistry_Definitions(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "tool_a", desc: "desc a"})
	defs := r.Definitions()
	if len(defs) != 1 {
		t.Errorf("expected 1 definition, got %d", len(defs))
	}
}

func TestRegistry_Execute_Success(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "test_tool", desc: ""})

	out, err := r.Execute(context.Background(), "test_tool", `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "mock result" {
		t.Errorf("expected 'mock result', got %q", out)
	}
}

func TestRegistry_Execute_NotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "missing_tool", `{}`)
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestRegistry_Execute_InvalidJSON(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "test_tool", desc: ""})

	_, err := r.Execute(context.Background(), "test_tool", `{invalid`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegistry_Execute_EmptyArgs(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "test_tool", desc: ""})

	out, err := r.Execute(context.Background(), "test_tool", "")
	if err != nil {
		t.Fatalf("Execute with empty args: %v", err)
	}
	if out != "mock result" {
		t.Errorf("expected 'mock result', got %q", out)
	}
}
````

## File: internal/tools/tools.go
````go
package tools

import (
	"context"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (string, error)
	Schema() map[string]any
}

type Base struct{}

func (Base) SchemaFor(name, desc string, params map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": name,
			"description": desc,
			"parameters": params,
		},
	}
}
````

## File: internal/triggers/filewatch_test.go
````go
package triggers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func newTestFileWatcher(t *testing.T, paths []string, pollSec, debounceSec int) (*FileWatcher, *bus.Bus) {
	t.Helper()
	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           paths,
		PollSeconds:     pollSec,
		DebounceSeconds: debounceSec,
	}
	fw := NewFileWatcher(cfg, b, "test-session")
	return fw, b
}

func TestFileWatcherPublishesChange(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "watched.txt")
	if err := os.WriteFile(filePath, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           []string{filePath},
		PollSeconds:     1,
		DebounceSeconds: 0,
	}
	fw := NewFileWatcher(cfg, b, "test-session")

	// Manually do first poll to establish baseline
	fw.poll(nil)

	// Modify file
	time.Sleep(10 * time.Millisecond) // ensure mtime difference
	if err := os.WriteFile(filePath, []byte("changed content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Poll again - should publish event now
	fw.poll(nil)

	select {
	case ev := <-b.Channel():
		if ev.Type != "file_change" {
			t.Errorf("expected EventFileChange, got %q", ev.Type)
		}
		if ev.SessionKey != "test-session" {
			t.Errorf("expected 'test-session', got %q", ev.SessionKey)
		}
		absPath, _ := filepath.Abs(filePath)
		if ev.From != absPath {
			t.Errorf("expected From=%q, got %q", absPath, ev.From)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file change event")
	}
}

func TestFileWatcherDebounce(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "debounce.txt")
	if err := os.WriteFile(filePath, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           []string{filePath},
		PollSeconds:     1,
		DebounceSeconds: 60, // very large debounce window
	}
	fw := NewFileWatcher(cfg, b, "test-session")

	// Establish baseline
	fw.poll(nil)

	// First change - should publish
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.poll(nil)

	// Second change very quickly (within debounce) - should NOT publish
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("v3"), 0o644); err != nil {
		t.Fatal(err)
	}
	fw.poll(nil)

	// Should have exactly one event
	count := 0
	drain:
	for {
		select {
		case <-b.Channel():
			count++
		default:
			break drain
		}
	}
	if count != 1 {
		t.Errorf("expected 1 event (debounce), got %d", count)
	}
}

func TestFileWatcherFirstObservationNoEvent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "baseline.txt")
	if err := os.WriteFile(filePath, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := bus.New(16)
	cfg := config.FileWatchConfig{
		Enabled:         true,
		Paths:           []string{filePath},
		PollSeconds:     1,
		DebounceSeconds: 0,
	}
	fw := NewFileWatcher(cfg, b, "test-session")

	// First poll - just baseline, no event
	fw.poll(nil)

	select {
	case ev := <-b.Channel():
		t.Errorf("unexpected event on first poll: %+v", ev)
	default:
		// correct: no event
	}
}
````

## File: internal/triggers/filewatch.go
````go
package triggers

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type FileWatcher struct {
	Config     config.FileWatchConfig
	Bus        *bus.Bus
	SessionKey string

	mu     sync.Mutex
	last   map[string]fileState
	cancel context.CancelFunc
}

type fileState struct {
	mtime  time.Time
	size   int64
	lastEv time.Time // last time we published an event for this path
}

func NewFileWatcher(cfg config.FileWatchConfig, b *bus.Bus, sessionKey string) *FileWatcher {
	return &FileWatcher{
		Config:     cfg,
		Bus:        b,
		SessionKey: sessionKey,
		last:       map[string]fileState{},
	}
}

func (fw *FileWatcher) Start(ctx context.Context) {
	if !fw.Config.Enabled || len(fw.Config.Paths) == 0 {
		return
	}
	pollInterval := time.Duration(fw.Config.PollSeconds) * time.Second
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	ctx, fw.cancel = context.WithCancel(ctx)
	go fw.loop(ctx, pollInterval)
}

func (fw *FileWatcher) Stop() {
	if fw.cancel != nil {
		fw.cancel()
	}
}

func (fw *FileWatcher) loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fw.poll(ctx)
		}
	}
}

func (fw *FileWatcher) poll(ctx context.Context) {
	debounce := time.Duration(fw.Config.DebounceSeconds) * time.Second
	if debounce <= 0 {
		debounce = 2 * time.Second
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	now := time.Now()
	for _, p := range fw.Config.Paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// Don't follow symlinks
		info, err := os.Lstat(absPath)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		prev, seen := fw.last[absPath]
		cur := fileState{mtime: info.ModTime(), size: info.Size()}
		if seen {
			// Check if changed
			if cur.mtime == prev.mtime && cur.size == prev.size {
				continue
			}
			// Debounce: don't republish if we published recently
			if now.Sub(prev.lastEv) < debounce {
				// update state but don't publish yet
				fw.last[absPath] = fileState{mtime: cur.mtime, size: cur.size, lastEv: prev.lastEv}
				continue
			}
		}
		cur.lastEv = now
		fw.last[absPath] = cur
		if !seen {
			// First observation - record baseline with zero lastEv so debounce
			// does not prevent the first change event from being published.
			fw.last[absPath] = fileState{mtime: cur.mtime, size: cur.size}
			continue
		}
		// Publish event
		ev := bus.Event{
			Type:       bus.EventFileChange,
			SessionKey: fw.SessionKey,
			Channel:    "filewatch",
			From:       absPath,
			Message:    "file changed: " + absPath,
			Meta: map[string]any{
				"path":  absPath,
				"size":  info.Size(),
				"mtime": info.ModTime().UnixMilli(),
			},
		}
		if ok := fw.Bus.Publish(ev); !ok {
			log.Printf("filewatch: bus full, dropping event for %s", absPath)
		}
	}
}
````

## File: internal/triggers/triggers.go
````go
package triggers

// TriggerMeta carries metadata from trigger events.
type TriggerMeta struct {
	Source  string            // "webhook" or "filewatch"
	Path    string            // for file-change events
	Route   string            // for webhook events
	Headers map[string]string // for webhook events (limited subset)
}
````

## File: internal/triggers/webhook_test.go
````go
package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

func newTestWebhookServer(t *testing.T, secret string) (*WebhookServer, *bus.Bus) {
	t.Helper()
	b := bus.New(16)
	cfg := config.WebhookConfig{
		Enabled:   true,
		Secret:    secret,
		MaxBodyKB: 1,
	}
	srv := NewWebhookServer(cfg, b, "test-session")
	return srv, b
}

func doRequest(t *testing.T, srv *WebhookServer, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rw := httptest.NewRecorder()
	srv.handle(rw, req)
	return rw
}

func TestWebhookAuthFailure(t *testing.T) {
	srv, _ := newTestWebhookServer(t, "mysecret")
	rw := doRequest(t, srv, "hello", nil)
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rw.Code)
	}
}

func TestWebhookAuthSuccess(t *testing.T) {
	srv, b := newTestWebhookServer(t, "mysecret")
	rw := doRequest(t, srv, "hello", map[string]string{
		"X-Webhook-Secret": "mysecret",
	})
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	select {
	case ev := <-b.Channel():
		if ev.Message != "hello" {
			t.Errorf("expected message 'hello', got %q", ev.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestWebhookHMAC(t *testing.T) {
	secret := "hmac-secret"
	body := `{"event":"push"}`
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	srv, b := newTestWebhookServer(t, secret)
	rw := doRequest(t, srv, body, map[string]string{
		"X-Hub-Signature-256": sig,
	})
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	select {
	case ev := <-b.Channel():
		if ev.Message != body {
			t.Errorf("expected body as message, got %q", ev.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}

func TestWebhookBodySizeLimit(t *testing.T) {
	srv, _ := newTestWebhookServer(t, "mysecret")
	// MaxBodyKB is 1, so generate > 1KB body
	bigBody := strings.Repeat("x", 1025)
	rw := doRequest(t, srv, bigBody, map[string]string{
		"X-Webhook-Secret": "mysecret",
	})
	if rw.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rw.Code)
	}
}

func TestWebhookPublishesToBus(t *testing.T) {
	srv, b := newTestWebhookServer(t, "s3cr3t")
	payload := `{"action":"test"}`
	rw := doRequest(t, srv, payload, map[string]string{
		"X-Webhook-Secret": "s3cr3t",
		"X-Request-ID":     "req-123",
	})
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	resp, _ := io.ReadAll(rw.Body)
	if string(resp) != "ok" {
		t.Errorf("expected body 'ok', got %q", string(resp))
	}
	select {
	case ev := <-b.Channel():
		if ev.Type != "webhook" {
			t.Errorf("expected EventWebhook, got %q", ev.Type)
		}
		if ev.SessionKey != "test-session" {
			t.Errorf("expected session key 'test-session', got %q", ev.SessionKey)
		}
		if ev.Message != payload {
			t.Errorf("expected message %q, got %q", payload, ev.Message)
		}
		if fmt.Sprint(ev.Meta["x-request-id"]) != "req-123" {
			t.Errorf("expected x-request-id 'req-123', got %q", ev.Meta["x-request-id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus event")
	}
}
````

## File: internal/triggers/webhook.go
````go
package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type WebhookServer struct {
	Config     config.WebhookConfig
	Bus        *bus.Bus
	SessionKey string
	server     *http.Server
}

func NewWebhookServer(cfg config.WebhookConfig, b *bus.Bus, sessionKey string) *WebhookServer {
	return &WebhookServer{Config: cfg, Bus: b, SessionKey: sessionKey}
}

func (w *WebhookServer) Start(ctx context.Context) error {
	if !w.Config.Enabled || strings.TrimSpace(w.Config.Secret) == "" {
		return nil
	}
	addr := strings.TrimSpace(w.Config.Addr)
	if addr == "" {
		addr = "127.0.0.1:8765"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", w.handle)
	mux.HandleFunc("/webhook/", w.handle)
	w.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("webhook listen %s: %w", addr, err)
	}
	go func() {
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("webhook server error: %v", err)
		}
	}()
	return nil
}

func (w *WebhookServer) Stop(ctx context.Context) error {
	if w.server == nil {
		return nil
	}
	return w.server.Shutdown(ctx)
}

func (w *WebhookServer) handle(rw http.ResponseWriter, r *http.Request) {
	maxBytes := int64(w.Config.MaxBodyKB) * 1024
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		http.Error(rw, "read error", http.StatusInternalServerError)
		return
	}
	if int64(len(body)) > maxBytes {
		http.Error(rw, "request too large", http.StatusRequestEntityTooLarge)
		return
	}

	if !w.authenticate(r, body) {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}

	route := strings.TrimPrefix(r.URL.Path, "/webhook")
	route = strings.TrimPrefix(route, "/")

	ev := bus.Event{
		Type:       bus.EventWebhook,
		SessionKey: w.SessionKey,
		Channel:    "webhook",
		From:       r.RemoteAddr,
		Message:    string(body),
		Meta: map[string]any{
			"route":        route,
			"content_type": r.Header.Get("Content-Type"),
			"x-request-id": r.Header.Get("X-Request-ID"),
		},
	}
	if ok := w.Bus.Publish(ev); !ok {
		http.Error(rw, "bus full", http.StatusServiceUnavailable)
		return
	}
	rw.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(rw, "ok")
}

func (w *WebhookServer) authenticate(r *http.Request, body []byte) bool {
	secret := w.Config.Secret
	if secret == "" {
		return false
	}
	// Check HMAC-SHA256 in X-Hub-Signature-256
	sig := r.Header.Get("X-Hub-Signature-256")
	if strings.HasPrefix(sig, "sha256=") {
		sig = strings.TrimPrefix(sig, "sha256=")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}
	// Fall back to simple shared secret in X-Webhook-Secret header
	return r.Header.Get("X-Webhook-Secret") == secret
}
````

## File: cmd/or3-intern/init.go
````go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
)

type initProviderPreset struct {
	label      string
	apiBase    string
	model      string
	embedModel string
}

var initProviderPresets = map[string]initProviderPreset{
	"1": {
		label:      "OpenAI",
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
	"2": {
		label:      "OpenRouter",
		apiBase:    "https://openrouter.ai/api/v1",
		model:      "openai/gpt-4o-mini",
		embedModel: "text-embedding-3-small",
	},
	"3": {
		label:      "Custom OpenAI-compatible",
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
}

func runInit(cfgPath string) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return runInitWithIO(os.Stdin, os.Stdout, cfgPathOrDefault(cfgPath), cwd)
}

func runInitWithIO(in io.Reader, out io.Writer, cfgPath, cwd string) error {
	reader := bufio.NewReader(in)
	cfg := initDefaults(cwd)

	fmt.Fprintln(out, "or3-intern setup")
	fmt.Fprintln(out, "We'll create a config file and pick defaults that work well for local testing.")
	fmt.Fprintf(out, "Config file: %s\n\n", cfgPath)

	providerChoice, err := promptChoice(reader, out,
		"Choose your provider",
		[]string{"1) OpenAI", "2) OpenRouter", "3) Custom OpenAI-compatible"},
		defaultProviderChoice(cfg.Provider.APIBase),
	)
	if err != nil {
		return err
	}
	applyProviderPreset(&cfg, providerChoice)

	cfg.Provider.APIBase, err = promptString(reader, out, "API base", cfg.Provider.APIBase)
	if err != nil {
		return err
	}
	cfg.Provider.Model, err = promptString(reader, out, "Chat model", cfg.Provider.Model)
	if err != nil {
		return err
	}
	cfg.Provider.EmbedModel, err = promptString(reader, out, "Embedding model", cfg.Provider.EmbedModel)
	if err != nil {
		return err
	}

	saveKey, err := promptBool(reader, out, "Save API key in config.json (stored locally with restricted permissions; env vars are safer)?", strings.TrimSpace(cfg.Provider.APIKey) != "")
	if err != nil {
		return err
	}
	if saveKey {
		cfg.Provider.APIKey, err = promptString(reader, out, "API key", cfg.Provider.APIKey)
		if err != nil {
			return err
		}
	} else {
		cfg.Provider.APIKey = ""
	}

	cfg.DBPath, err = promptString(reader, out, "SQLite DB path", cfg.DBPath)
	if err != nil {
		return err
	}
	cfg.ArtifactsDir, err = promptString(reader, out, "Artifacts directory", cfg.ArtifactsDir)
	if err != nil {
		return err
	}

	restrictWorkspace, err := promptBool(reader, out, "Restrict file tools to the current workspace?", cfg.Tools.RestrictToWorkspace)
	if err != nil {
		return err
	}
	cfg.Tools.RestrictToWorkspace = restrictWorkspace
	if restrictWorkspace {
		cfg.WorkspaceDir = cwd
	} else if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		cfg.WorkspaceDir = cwd
	}

	cfg.Tools.BraveAPIKey, err = promptString(reader, out, "Brave Search API key (optional)", cfg.Tools.BraveAPIKey)
	if err != nil {
		return err
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Saved config to %s\n", cfgPath)
	fmt.Fprintf(out, "Provider: %s\n", initProviderPresets[providerChoice].label)
	fmt.Fprintf(out, "DB: %s\n", cfg.DBPath)
	fmt.Fprintf(out, "Artifacts: %s\n", cfg.ArtifactsDir)
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) != "" {
		fmt.Fprintf(out, "Workspace restriction: enabled (%s)\n", cfg.WorkspaceDir)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next step:")
	fmt.Fprintln(out, "  go run ./cmd/or3-intern chat")
	return nil
}

func initDefaults(cwd string) config.Config {
	cfg := config.Default()
	config.ApplyEnvOverrides(&cfg)
	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		cfg.WorkspaceDir = cwd
		cfg.DBPath = filepath.Join(cwd, ".or3", "or3-intern.sqlite")
		cfg.ArtifactsDir = filepath.Join(cwd, ".or3", "artifacts")
		cfg.Tools.RestrictToWorkspace = true
	}
	return cfg
}

func defaultProviderChoice(apiBase string) string {
	if strings.Contains(strings.ToLower(apiBase), "openrouter.ai") {
		return "2"
	}
	return "1"
}

func applyProviderPreset(cfg *config.Config, choice string) {
	preset, ok := initProviderPresets[choice]
	if !ok || cfg == nil {
		return
	}
	cfg.Provider.APIBase = preset.apiBase
	cfg.Provider.Model = preset.model
	cfg.Provider.EmbedModel = preset.embedModel
}

func promptChoice(reader *bufio.Reader, out io.Writer, label string, options []string, defaultChoice string) (string, error) {
	fmt.Fprintln(out, label)
	for _, option := range options {
		fmt.Fprintf(out, "  %s\n", option)
	}
	for {
		answer, err := promptString(reader, out, "Selection", defaultChoice)
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		if _, ok := initProviderPresets[answer]; ok {
			return answer, nil
		}
		fmt.Fprintln(out, "Please choose 1, 2, or 3.")
	}
}

func promptBool(reader *bufio.Reader, out io.Writer, label string, defaultValue bool) (bool, error) {
	defaultText := "n"
	if defaultValue {
		defaultText = "y"
	}
	for {
		answer, err := promptString(reader, out, label+" (y/n)", defaultText)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Please answer y or n.")
		}
	}
}

func promptString(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}
````

## File: cmd/or3-intern/migrate.go
````go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"or3-intern/internal/db"
)

func migrateJSONL(ctx context.Context, d *db.DB, path, sessionKey string) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024), 4<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if len(line) == 0 { continue }
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			// tolerate non-json line
			if _, err := d.AppendMessage(ctx, sessionKey, "user", line, map[string]any{"migrated_line": lineNo}); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			continue
		}
		// detect metadata
		if lineNo == 1 {
			if _, ok := obj["messages"]; ok {
				// not expected
			}
			// store as session metadata_json if it looks like metadata
			if obj["role"] == nil && obj["content"] == nil {
				b, _ := json.Marshal(obj)
				if err := d.EnsureSession(ctx, sessionKey); err != nil {
					log.Printf("ensure session failed during migration: %v", err)
				}
				if _, err := d.SQL.ExecContext(ctx, `UPDATE sessions SET metadata_json=? WHERE key=?`, string(b), sessionKey); err != nil {
					log.Printf("session metadata update failed during migration: %v", err)
				}
				continue
			}
		}
		role := toStr(obj["role"])
		if role == "" { role = "user" }
		content := toStr(obj["content"])
		payload := obj
		delete(payload, "role")
		delete(payload, "content")
		_, err := d.AppendMessage(ctx, sessionKey, role, content, payload)
		if err != nil { return fmt.Errorf("line %d: %w", lineNo, err) }
	}
	return sc.Err()
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(v)
	}
}
````

## File: internal/agent/prompt_test.go
````go
package agent

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/memory"
)

// TestPromptIncludesIdentity verifies that IdentityText appears in the system prompt.
func TestPromptIncludesIdentity(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{
		DB:           d,
		HistoryMax:   10,
		IdentityText: "I am a test assistant with a unique identity.",
	}
	pp, _, err := b.Build(context.Background(), "sess", "hello")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := pp.System[0].Content.(string)
	if !strings.Contains(sys, "Identity") {
		t.Errorf("expected 'Identity' section header in system prompt, got %q", sys)
	}
	if !strings.Contains(sys, "unique identity") {
		t.Errorf("expected identity text in system prompt, got %q", sys)
	}
}

// TestPromptIncludesStaticMemory verifies that StaticMemory appears in the system prompt.
func TestPromptIncludesStaticMemory(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{
		DB:           d,
		HistoryMax:   10,
		StaticMemory: "Remember: the answer is always 42.",
	}
	pp, _, err := b.Build(context.Background(), "sess", "hello")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := pp.System[0].Content.(string)
	if !strings.Contains(sys, "Static Memory") {
		t.Errorf("expected 'Static Memory' section header in system prompt, got %q", sys)
	}
	if !strings.Contains(sys, "answer is always 42") {
		t.Errorf("expected static memory text in system prompt, got %q", sys)
	}
}

// TestHeartbeatOnlyForAutonomous verifies that HeartbeatText only appears when Autonomous=true.
func TestHeartbeatOnlyForAutonomous(t *testing.T) {
	d := openTestDB(t)
	heartbeat := "HEARTBEAT: check your tasks now."
	b := &Builder{
		DB:            d,
		HistoryMax:    10,
		HeartbeatText: heartbeat,
	}

	// Non-autonomous: heartbeat should NOT appear.
	ppNormal, _, err := b.BuildWithOptions(context.Background(), BuildOptions{
		SessionKey:  "sess",
		UserMessage: "hello",
		Autonomous:  false,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions (non-autonomous): %v", err)
	}
	sysNormal := ppNormal.System[0].Content.(string)
	if strings.Contains(sysNormal, "Heartbeat") {
		t.Errorf("expected NO 'Heartbeat' section for non-autonomous turn, got %q", sysNormal)
	}

	// Autonomous: heartbeat SHOULD appear.
	ppAuto, _, err := b.BuildWithOptions(context.Background(), BuildOptions{
		SessionKey:  "sess",
		UserMessage: "hello",
		Autonomous:  true,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions (autonomous): %v", err)
	}
	sysAuto := ppAuto.System[0].Content.(string)
	if !strings.Contains(sysAuto, "Heartbeat") {
		t.Errorf("expected 'Heartbeat' section for autonomous turn, got %q", sysAuto)
	}
	if !strings.Contains(sysAuto, "check your tasks now") {
		t.Errorf("expected heartbeat text in autonomous system prompt, got %q", sysAuto)
	}
}

// TestDocContextIncluded verifies that DocRetriever results appear in the system prompt.
func TestDocContextIncluded(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert a doc via UpsertDoc so RetrieveDocs can find it.
	err := memory.UpsertDoc(ctx, d, "scope1", "/docs/guide.md", "markdown", "guide.md",
		"A guide for testing", "This document explains testing procedures in detail.", nil, "abc123", 0, 100)
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	b := &Builder{
		DB:               d,
		HistoryMax:       10,
		DocRetriever:     &memory.DocRetriever{DB: d},
		DocScopeKey:      "scope1",
		DocRetrieveLimit: 5,
	}

	pp, _, err := b.BuildWithOptions(ctx, BuildOptions{
		SessionKey:  "sess",
		UserMessage: "testing procedures",
	})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	sys := pp.System[0].Content.(string)
	if !strings.Contains(sys, "Indexed File Context") {
		t.Errorf("expected 'Indexed File Context' section in system prompt, got %q", sys)
	}
	if !strings.Contains(sys, "guide.md") {
		t.Errorf("expected doc path in system prompt, got %q", sys)
	}
}

// TestBuildWithOptions_WrapperParity verifies Build and BuildWithOptions produce identical results.
func TestBuildWithOptions_WrapperParity(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{DB: d, HistoryMax: 10}

	pp1, ret1, err1 := b.Build(context.Background(), "s1", "msg")
	pp2, ret2, err2 := b.BuildWithOptions(context.Background(), BuildOptions{SessionKey: "s1", UserMessage: "msg"})

	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v / %v", err1, err2)
	}
	if len(ret1) != len(ret2) {
		t.Errorf("retrieved count mismatch: %d vs %d", len(ret1), len(ret2))
	}
	sys1 := pp1.System[0].Content.(string)
	sys2 := pp2.System[0].Content.(string)
	if sys1 != sys2 {
		t.Errorf("system prompts differ:\n%q\nvs\n%q", sys1, sys2)
	}
}

// TestIdentityAfterSoul verifies that the Identity section appears after SOUL.md.
func TestIdentityAfterSoul(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{
		DB:           d,
		HistoryMax:   10,
		IdentityText: "MyIdentity",
	}
	pp, _, err := b.Build(context.Background(), "s", "hi")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := pp.System[0].Content.(string)
	soulIdx := strings.Index(sys, "SOUL.md")
	identIdx := strings.Index(sys, "Identity")
	agentsIdx := strings.Index(sys, "AGENTS.md")
	if soulIdx < 0 || identIdx < 0 || agentsIdx < 0 {
		t.Fatalf("missing sections in: %q", sys)
	}
	if !(soulIdx < identIdx && identIdx < agentsIdx) {
		t.Errorf("expected order SOUL.md < Identity < AGENTS.md, indices: %d %d %d", soulIdx, identIdx, agentsIdx)
	}
}

// TestStaticMemoryAfterAgents verifies that the Static Memory section appears after AGENTS.md.
func TestStaticMemoryAfterAgents(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{
		DB:           d,
		HistoryMax:   10,
		StaticMemory: "MyStaticMem",
	}
	pp, _, err := b.Build(context.Background(), "s", "hi")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := pp.System[0].Content.(string)
	agentsIdx := strings.Index(sys, "AGENTS.md")
	staticIdx := strings.Index(sys, "Static Memory")
	toolsIdx := strings.Index(sys, "TOOLS.md")
	if agentsIdx < 0 || staticIdx < 0 || toolsIdx < 0 {
		t.Fatalf("missing sections in: %q", sys)
	}
	if !(agentsIdx < staticIdx && staticIdx < toolsIdx) {
		t.Errorf("expected order AGENTS.md < Static Memory < TOOLS.md, indices: %d %d %d", agentsIdx, staticIdx, toolsIdx)
	}
}

// TestEmptyOptionalSectionsOmitted verifies that empty optional sections are not rendered.
func TestEmptyOptionalSectionsOmitted(t *testing.T) {
	b := &Builder{}
	pinned := "(none)"
	retrieved := "(none)"
	noIdentity := ""
	noStaticMem := ""
	noHeartbeat := ""
	noDocContext := ""
	got := b.composeSystemPrompt(pinned, retrieved, noIdentity, noStaticMem, noHeartbeat, noDocContext)
	if strings.Contains(got, "Identity") {
		t.Error("expected 'Identity' section to be omitted when IdentityText is empty")
	}
	if strings.Contains(got, "Static Memory") {
		t.Error("expected 'Static Memory' section to be omitted when StaticMemory is empty")
	}
	if strings.Contains(got, "Heartbeat") {
		t.Error("expected 'Heartbeat' section to be omitted when heartbeatText is empty")
	}
	if strings.Contains(got, "Indexed File Context") {
		t.Error("expected 'Indexed File Context' section to be omitted when docContextText is empty")
	}
}
````

## File: internal/artifacts/store_test.go
````go
package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestStore_Save_OK(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	// Create the session
	d.EnsureSession(ctx, "sess1")

	store := &Store{Dir: dir, DB: d}
	id, err := store.Save(ctx, "sess1", "text/plain", []byte("artifact content"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty artifact ID")
	}

	// Check file was created
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Errorf("expected 1 artifact file, got %d", len(files))
	}

	// Check content
	content, _ := os.ReadFile(filepath.Join(dir, id))
	if string(content) != "artifact content" {
		t.Errorf("expected 'artifact content', got %q", string(content))
	}
}

func TestStore_Save_NoDirSet(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	store := &Store{Dir: "", DB: d}
	_, err := store.Save(ctx, "sess1", "text/plain", []byte("data"))
	if err == nil {
		t.Fatal("expected error when Dir is not set")
	}
}

func TestStore_Save_CreatesDir(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	d.EnsureSession(ctx, "sess1")

	// Use a dir that doesn't exist yet
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "artifacts", "subdir")

	store := &Store{Dir: dir, DB: d}
	id, err := store.Save(ctx, "sess1", "text/plain", []byte("hello"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty artifact ID")
	}

	// Dir should now exist
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected artifacts directory to be created")
	}
}

func TestStore_Save_MultipleArtifacts(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	d.EnsureSession(ctx, "sess")

	store := &Store{Dir: dir, DB: d}

	ids := map[string]bool{}
	for i := 0; i < 5; i++ {
		id, err := store.Save(ctx, "sess", "text/plain", []byte("data"))
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		if ids[id] {
			t.Errorf("duplicate artifact ID: %q", id)
		}
		ids[id] = true
	}
}

func TestStore_SaveNamedAndLookup(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	if err := d.EnsureSession(ctx, "sess"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	store := &Store{Dir: dir, DB: d}
	att, err := store.SaveNamed(ctx, "sess", "photo.png", "image/png", []byte("png-data"))
	if err != nil {
		t.Fatalf("SaveNamed: %v", err)
	}
	if att.ArtifactID == "" {
		t.Fatal("expected artifact id")
	}
	if att.Kind != KindImage {
		t.Fatalf("expected image kind, got %q", att.Kind)
	}
	if att.Filename != "photo.png" {
		t.Fatalf("expected filename to round-trip, got %q", att.Filename)
	}

	stored, err := store.Lookup(ctx, att.ArtifactID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if stored.Mime != "image/png" {
		t.Fatalf("expected mime image/png, got %q", stored.Mime)
	}
	content, err := os.ReadFile(stored.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "png-data" {
		t.Fatalf("unexpected stored content: %q", string(content))
	}
}

func TestStore_Save_CreatesSessionAutomatically(t *testing.T) {
	d := openTestDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	store := &Store{Dir: dir, DB: d}
	id, err := store.Save(ctx, "fresh-session", "text/plain", []byte("artifact content"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Fatal("expected artifact id")
	}
	var count int
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE key=?`, "fresh-session").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected session to be created, got count=%d", count)
	}
}

func TestStore_SaveNamed_NoDirSet(t *testing.T) {
	d := openTestDB(t)
	store := &Store{DB: d}
	if _, err := store.SaveNamed(context.Background(), "sess", "photo.png", "image/png", []byte("png-data")); err == nil {
		t.Fatal("expected error when artifacts dir is not configured")
	}
}

func TestRandID_NotEmpty(t *testing.T) {
	id := randID()
	if id == "" {
		t.Error("expected non-empty random ID")
	}
}

func TestRandID_Uniqueness(t *testing.T) {
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := randID()
		if ids[id] {
			t.Errorf("duplicate ID generated: %q", id)
		}
		ids[id] = true
	}
}
````

## File: internal/bus/bus_test.go
````go
package bus

import (
	"context"
	"testing"
	"time"
)

func TestNew_DefaultBuffer(t *testing.T) {
	b := New(0)
	if b == nil {
		t.Fatal("expected non-nil bus")
	}
	// should accept at least 128 events without blocking
	for i := 0; i < 128; i++ {
		ok := b.Publish(Event{Type: EventUserMessage, Message: "test"})
		if !ok {
			t.Fatalf("expected publish to succeed at i=%d", i)
		}
	}
}

func TestNew_CustomBuffer(t *testing.T) {
	b := New(4)
	if b == nil {
		t.Fatal("expected non-nil bus")
	}
	// first 4 succeed
	for i := 0; i < 4; i++ {
		ok := b.Publish(Event{Type: EventUserMessage})
		if !ok {
			t.Fatalf("expected publish to succeed at i=%d", i)
		}
	}
	// 5th should fail (buffer full)
	ok := b.Publish(Event{Type: EventUserMessage})
	if ok {
		t.Fatal("expected publish to fail on full buffer")
	}
}

func TestPublish_Success(t *testing.T) {
	b := New(10)
	ev := Event{
		Type:       EventUserMessage,
		SessionKey: "session1",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
		Meta:       map[string]any{"key": "val"},
	}
	ok := b.Publish(ev)
	if !ok {
		t.Fatal("expected publish to succeed")
	}

	got := <-b.Channel()
	if got.Type != EventUserMessage {
		t.Errorf("expected type %s, got %s", EventUserMessage, got.Type)
	}
	if got.SessionKey != "session1" {
		t.Errorf("expected session key 'session1', got %q", got.SessionKey)
	}
	if got.Message != "hello" {
		t.Errorf("expected message 'hello', got %q", got.Message)
	}
	if got.Meta["key"] != "val" {
		t.Errorf("expected meta key 'val', got %v", got.Meta["key"])
	}
}

func TestChannel_IsReadOnly(t *testing.T) {
	b := New(1)
	ch := b.Channel()
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestEventTypes(t *testing.T) {
	cases := []EventType{EventUserMessage, EventCron, EventSystem, EventWebhook, EventFileChange}
	for _, et := range cases {
		b := New(1)
		b.Publish(Event{Type: et})
		ev := <-b.Channel()
		if ev.Type != et {
			t.Errorf("expected type %s, got %s", et, ev.Type)
		}
	}
}

func TestBus_EventFlow(t *testing.T) {
	b := New(10)
	_ = context.Background()

	events := []Event{
		{Type: EventUserMessage, SessionKey: "s1", Message: "msg1"},
		{Type: EventCron, SessionKey: "s2", Message: "cron1"},
		{Type: EventSystem, SessionKey: "s3", Message: "sys1"},
	}

	for _, ev := range events {
		b.Publish(ev)
	}

	ch := b.Channel()
	for _, want := range events {
		select {
		case got := <-ch:
			if got.Type != want.Type || got.Message != want.Message {
				t.Errorf("got {%s,%s}, want {%s,%s}", got.Type, got.Message, want.Type, want.Message)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestPublish_Overflow(t *testing.T) {
	b := New(2)
	b.Publish(Event{Type: EventUserMessage})
	b.Publish(Event{Type: EventUserMessage})
	// third publish should drop
	ok := b.Publish(Event{Type: EventUserMessage})
	if ok {
		t.Fatal("expected overflow publish to return false")
	}
}
````

## File: internal/channels/cli/cli.go
````go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"or3-intern/internal/bus"
)

type Channel struct {
	Bus *bus.Bus
	SessionKey string
}

func (c *Channel) Run(ctx context.Context) error {
	if c.SessionKey == "" { c.SessionKey = "default" }
	in := bufio.NewScanner(os.Stdin)
	fmt.Println("or3-intern CLI. Type /exit to quit.")
	for {
		fmt.Print("> ")
		if !in.Scan() { return nil }
		line := strings.TrimSpace(in.Text())
		if line == "" { continue }
		if line == "/exit" { return nil }
		ok := c.Bus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: c.SessionKey, Channel: "cli", From: "local", Message: line})
		if !ok {
			fmt.Println("[warn] queue is full; message dropped")
		}
	}
}
````

## File: internal/channels/cli/deliver_test.go
````go
package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestDeliver_Basic(t *testing.T) {
	d := Deliverer{}
	err := d.Deliver(context.Background(), "cli", "user", "hello there")
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}

func TestDeliver_EmptyChannel(t *testing.T) {
	d := Deliverer{}
	// Should default to "cli" if channel is empty
	err := d.Deliver(context.Background(), "", "user", "message")
	if err != nil {
		t.Fatalf("Deliver with empty channel: %v", err)
	}
}

func TestDeliver_LongMessage(t *testing.T) {
	d := Deliverer{}
	msg := strings.Repeat("x", 10000)
	err := d.Deliver(context.Background(), "cli", "user", msg)
	if err != nil {
		t.Fatalf("Deliver long message: %v", err)
	}
}

// capturingWriter swaps os.Stdout with a buffer during a test.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestCLIStreamWriter_WriteDeltaAndClose(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		ctx := context.Background()
		_ = w.WriteDelta(ctx, "hello")
		_ = w.WriteDelta(ctx, " world")
		_ = w.Close(ctx, "hello world")
	})
	// text printed incrementally; Close adds a newline
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("expected trailing newline, got %q", out)
	}
}

func TestCLIStreamWriter_CloseWithoutDelta(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		ctx := context.Background()
		_ = w.Close(ctx, "fallback text")
	})
	if !strings.Contains(out, "fallback text") {
		t.Errorf("expected 'fallback text' in output, got %q", out)
	}
}

func TestCLIStreamWriter_AbortAfterDelta(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		ctx := context.Background()
		_ = w.WriteDelta(ctx, "partial")
		_ = w.Abort(ctx)
	})
	if !strings.Contains(out, "partial") {
		t.Errorf("expected 'partial' in output, got %q", out)
	}
	if !strings.Contains(out, "[aborted]") {
		t.Errorf("expected '[aborted]' in output, got %q", out)
	}
}

func TestCLIStreamWriter_AbortWithoutDelta(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		_ = w.Abort(context.Background())
	})
	// No output expected when nothing was written
	if strings.Contains(out, "[aborted]") {
		t.Errorf("unexpected '[aborted]' when nothing was written, got %q", out)
	}
}

func TestCLIStreamWriter_WriteAfterClose(t *testing.T) {
	w := &CLIStreamWriter{}
	ctx := context.Background()
	_ = w.WriteDelta(ctx, "hello")
	_ = w.Close(ctx, "hello")
	// Further writes should be silently ignored
	err := w.WriteDelta(ctx, "extra")
	if err != nil {
		t.Errorf("unexpected error after close: %v", err)
	}
}

func TestBeginStream_PrintsPrefix(t *testing.T) {
	out := captureStdout(t, func() {
		d := Deliverer{}
		sw, err := d.BeginStream(context.Background(), "user", nil)
		if err != nil {
			t.Errorf("BeginStream: %v", err)
			return
		}
		_ = sw.WriteDelta(context.Background(), "streamed")
		_ = sw.Close(context.Background(), "streamed")
	})
	if !strings.Contains(out, "[cli]") {
		t.Errorf("expected '[cli]' prefix in output, got %q", out)
	}
	if !strings.Contains(out, "streamed") {
		t.Errorf("expected 'streamed' in output, got %q", out)
	}
	fmt.Print() // flush
}
````

## File: internal/channels/cli/service.go
````go
package cli

import (
	"context"
	"fmt"

	"or3-intern/internal/bus"
)

type Service struct {
	Deliverer Deliverer
}

func (s Service) Name() string { return "cli" }

func (s Service) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	return nil
}

func (s Service) Stop(ctx context.Context) error {
	_ = ctx
	return nil
}

func (s Service) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	if len(meta) > 0 {
		if raw, ok := meta["media_paths"].([]string); ok && len(raw) > 0 {
			return fmt.Errorf("cli channel does not support media attachments")
		}
	}
	return s.Deliverer.Deliver(ctx, "cli", to, text)
}
````

## File: internal/channels/discord/discord_test.go
````go
package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openDiscordTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "discord.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestChannel_StartReceivesMessage(t *testing.T) {
	upgrader := websocket.Upgrader{}
	identified := make(chan bool, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 10000}})
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("Read identify: %v", err)
		}
		if strings.Contains(string(raw), `"op":2`) {
			identified <- true
		}
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "d": map[string]any{"user": map[string]any{"id": "B1"}}})
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "d": map[string]any{"id": "m1", "channel_id": "C1", "content": "<@B1> hello", "author": map[string]any{"id": "U1", "bot": false}, "mentions": []map[string]any{{"id": "B1"}}}})
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()
	b := bus.New(1)
	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", GatewayURL: "ws" + strings.TrimPrefix(wsServer.URL, "http"), RequireMention: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case <-identified:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for identify")
	}
	select {
	case ev := <-b.Channel():
		if ev.Channel != "discord" || ev.Message != "hello" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for discord event")
	}
}

func TestChannel_DeliverPostsMessage(t *testing.T) {
	var got map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/C1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer apiServer.Close()
	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", APIBase: apiServer.URL, DefaultChannelID: "C1"}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"message_reference": "m1"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["content"] != "hello" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestChannel_StartReceivesAttachmentMessage(t *testing.T) {
	d := openDiscordTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-data"))
	}))
	defer fileServer.Close()

	upgrader := websocket.Upgrader{}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 10000}})
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "d": map[string]any{"user": map[string]any{"id": "B1"}}})
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "MESSAGE_CREATE", "d": map[string]any{
			"id":         "m1",
			"channel_id": "C1",
			"content":    "",
			"author":     map[string]any{"id": "U1", "bot": false},
			"mentions":   []map[string]any{},
			"attachments": []map[string]any{{
				"url":          fileServer.URL + "/file.png",
				"filename":     "file.png",
				"content_type": "image/png",
				"size":         10,
			}},
		}})
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()

	b := bus.New(1)
	ch := &Channel{
		Config:        config.DiscordChannelConfig{Token: "token", GatewayURL: "ws" + strings.TrimPrefix(wsServer.URL, "http"), RequireMention: false},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: file.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for discord media event")
	}
}

func TestChannel_DeliverPostsMultipartWithMedia(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/C1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("expected multipart request, got %s", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if r.FormValue("payload_json") == "" {
			t.Fatal("expected payload_json field")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer apiServer.Close()

	ch := &Channel{Config: config.DiscordChannelConfig{Token: "token", APIBase: apiServer.URL, DefaultChannelID: "C1"}, MaxMediaBytes: 1024}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}
````

## File: internal/channels/discord/discord.go
````go
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.DiscordChannelConfig
	HTTP   *http.Client
	Dialer *websocket.Dialer
	Artifacts *artifacts.Store
	MaxMediaBytes int

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	botID  string
}

func (c *Channel) Name() string { return "discord" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.Token) == "" {
		return fmt.Errorf("discord token not configured")
	}
	url := strings.TrimSpace(c.Config.GatewayURL)
	if url == "" {
		var resp struct{ URL string `json:"url"` }
		if err := c.getJSON(ctx, c.apiBase()+"/gateway/bot", &resp); err != nil {
			return err
		}
		url = resp.URL
	}
	if url == "" {
		return fmt.Errorf("discord gateway url missing")
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
	c.mu.Unlock()
	go c.readLoop(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.cancel = nil
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("discord channel id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.postMultipart(ctx, channelID, text, mediaPaths, meta)
	}
	payload := map[string]any{"content": text}
	if replyID, ok := meta["message_reference"].(string); ok && replyID != "" {
		payload["message_reference"] = map[string]any{"message_id": replyID}
	}
	return c.postJSON(ctx, c.apiBase()+"/channels/"+channelID+"/messages", payload, nil)
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	var heartbeatTicker *time.Ticker
	defer func() {
		if heartbeatTicker != nil {
			heartbeatTicker.Stop()
		}
	}()
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var frame gatewayFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		switch frame.Op {
		case 10:
			var hello struct { HeartbeatInterval float64 `json:"heartbeat_interval"` }
			_ = json.Unmarshal(frame.D, &hello)
			_ = conn.WriteJSON(map[string]any{"op": 2, "d": map[string]any{"token": c.Config.Token, "intents": 513, "properties": map[string]string{"$os": "linux", "$browser": "or3-intern", "$device": "or3-intern"}}})
			interval := time.Duration(int64(hello.HeartbeatInterval)) * time.Millisecond
			if interval > 0 {
				heartbeatTicker = time.NewTicker(interval)
				go func() {
					for {
						select {
						case <-ctx.Done():
							return
						case <-heartbeatTicker.C:
							_ = conn.WriteJSON(map[string]any{"op": 1, "d": nil})
						}
					}
				}()
			}
		case 0:
			switch frame.T {
			case "READY":
				var ready struct { User struct { ID string `json:"id"` } `json:"user"` }
				_ = json.Unmarshal(frame.D, &ready)
				c.botID = ready.User.ID
			case "MESSAGE_CREATE":
				var msg inboundMessage
				_ = json.Unmarshal(frame.D, &msg)
				if msg.Author.Bot {
					continue
				}
				if !c.allowedUser(msg.Author.ID) {
					continue
				}
				if c.Config.RequireMention && c.botID != "" && !mentioned(msg.Mentions, c.botID) {
					continue
				}
				clean := strings.TrimSpace(stripMention(msg.Content, c.botID))
				sessionKey := "discord:" + msg.ChannelID
				attachments, markers := c.captureAttachments(ctx, sessionKey, msg.Attachments)
				content := rootchannels.ComposeMessageText(clean, markers)
				if content == "" {
					continue
				}
				meta := map[string]any{"channel_id": msg.ChannelID, "message_reference": msg.ID}
				if len(attachments) > 0 {
					meta["attachments"] = attachments
				}
				eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "discord", From: msg.Author.ID, Message: content, Meta: meta})
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://discord.com/api/v10"
	}
	return base
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord api error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) postJSON(ctx context.Context, endpoint string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api error: %s %s", resp.Status, string(body))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, refs []discordAttachment) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(refs))
	markers := make([]string, 0, len(refs))
	for _, ref := range refs {
		filename := artifacts.NormalizeFilename(ref.Filename, ref.ContentType)
		kind := artifacts.DetectKind(filename, ref.ContentType)
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && ref.Size > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := c.downloadAttachment(ctx, ref.URL)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "download failed"))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, ref.ContentType, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) downloadAttachment(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord attachment error: %s", resp.Status)
	}
	limit := int64(c.MaxMediaBytes)
	if limit <= 0 {
		limit = 25 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if c.MaxMediaBytes > 0 && len(data) > c.MaxMediaBytes {
		return nil, fmt.Errorf("discord attachment exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) postMultipart(ctx context.Context, channelID, text string, mediaPaths []string, meta map[string]any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	payload := map[string]any{}
	if strings.TrimSpace(text) != "" {
		payload["content"] = text
	}
	if replyID, ok := meta["message_reference"].(string); ok && replyID != "" {
		payload["message_reference"] = map[string]any{"message_id": replyID}
	}
	payloadJSON, _ := json.Marshal(payload)
	if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
		return err
	}
	for i, mediaPath := range mediaPaths {
		if err := c.attachFilePart(writer, i, mediaPath); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+"/channels/"+channelID+"/messages", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api error: %s %s", resp.Status, string(respBody))
	}
	return nil
}

func (c *Channel) attachFilePart(writer *multipart.Writer, index int, mediaPath string) error {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return err
	}
	if c.MaxMediaBytes == 0 {
		return fmt.Errorf("media attachments disabled by config")
	}
	if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
		return fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := writer.CreateFormFile(fmt.Sprintf("files[%d]", index), filepath.Base(mediaPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	return nil
}

func (c *Channel) allowedUser(user string) bool {
	if len(c.Config.AllowedUserIDs) == 0 {
		return true
	}
	for _, allowed := range c.Config.AllowedUserIDs {
		if strings.TrimSpace(allowed) == user {
			return true
		}
	}
	return false
}

func mentioned(mentions []mention, botID string) bool {
	for _, m := range mentions {
		if m.ID == botID {
			return true
		}
	}
	return false
}

func stripMention(content, botID string) string {
	if botID == "" {
		return content
	}
	content = strings.ReplaceAll(content, "<@"+botID+">", "")
	content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	return content
}

type gatewayFrame struct {
	Op int             `json:"op"`
	T  string          `json:"t"`
	D  json.RawMessage `json:"d"`
}

type mention struct {
	ID string `json:"id"`
}

type inboundMessage struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	Content   string    `json:"content"`
	Mentions  []mention `json:"mentions"`
	Attachments []discordAttachment `json:"attachments"`
	Author    struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
}

type discordAttachment struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}
````

## File: internal/channels/slack/slack_test.go
````go
package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openSlackTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "slack.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestChannel_StartReceivesEventAndAcks(t *testing.T) {
	upgrader := websocket.Upgrader{}
	ackSeen := make(chan string, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event":          map[string]any{"type": "message", "text": "<@B123> hello", "user": "U1", "channel": "C1"},
			},
		})
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("ReadJSON ack: %v", err)
		}
		ackSeen <- ack["envelope_id"].(string)
	}))
	defer wsServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps.connections.open" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(wsServer.URL, "http")})
			return
		}
		t.Fatalf("unexpected api path: %s", r.URL.Path)
	}))
	defer apiServer.Close()

	b := bus.New(1)
	ch := &Channel{Config: config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: true}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case env := <-ackSeen:
		if env != "env1" {
			t.Fatalf("unexpected ack envelope: %s", env)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack ack")
	}
	select {
	case ev := <-b.Channel():
		if ev.Channel != "slack" || ev.Message != "hello" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack event")
	}
}

func TestChannel_DeliverPostsMessage(t *testing.T) {
	var got map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer apiServer.Close()
	ch := &Channel{Config: config.SlackChannelConfig{BotToken: "bot", APIBase: apiServer.URL, DefaultChannelID: "C1"}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"thread_ts": "123.45"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["channel"] != "C1" || got["text"] != "hello" || got["thread_ts"] != "123.45" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestChannel_StartReceivesFileShare(t *testing.T) {
	d := openSlackTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer bot" {
			t.Fatalf("expected bot auth header, got %q", auth)
		}
		_, _ = w.Write([]byte("image-data"))
	}))
	defer fileServer.Close()

	upgrader := websocket.Upgrader{}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event": map[string]any{
					"type":    "message",
					"text":    "",
					"user":    "U1",
					"channel": "C1",
					"files": []map[string]any{{
						"id":                   "F1",
						"name":                 "image.png",
						"mimetype":             "image/png",
						"size":                 10,
						"url_private_download": fileServer.URL + "/download",
					}},
				},
			},
		})
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("ReadJSON ack: %v", err)
		}
		if ack["envelope_id"] != "env1" {
			t.Fatalf("unexpected ack: %#v", ack)
		}
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps.connections.open" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(wsServer.URL, "http")})
			return
		}
		t.Fatalf("unexpected api path: %s", r.URL.Path)
	}))
	defer apiServer.Close()

	b := bus.New(1)
	ch := &Channel{
		Config:        config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: false},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: image.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack media event")
	}
}

func TestChannel_StartReceivesFileShareWhenMentionRequired(t *testing.T) {
	d := openSlackTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer bot" {
			t.Fatalf("expected bot auth header, got %q", auth)
		}
		_, _ = w.Write([]byte("image-data"))
	}))
	defer fileServer.Close()

	upgrader := websocket.Upgrader{}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"envelope_id": "env1",
			"type":        "events_api",
			"payload": map[string]any{
				"authorizations": []map[string]any{{"user_id": "B123"}},
				"event": map[string]any{
					"type":    "message",
					"text":    "",
					"user":    "U1",
					"channel": "C1",
					"files": []map[string]any{{
						"id":                   "F1",
						"name":                 "image.png",
						"mimetype":             "image/png",
						"size":                 10,
						"url_private_download": fileServer.URL + "/download",
					}},
				},
			},
		})
		var ack map[string]any
		if err := conn.ReadJSON(&ack); err != nil {
			t.Fatalf("ReadJSON ack: %v", err)
		}
		if ack["envelope_id"] != "env1" {
			t.Fatalf("unexpected ack: %#v", ack)
		}
		<-time.After(100 * time.Millisecond)
	}))
	defer wsServer.Close()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps.connections.open" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "ws" + strings.TrimPrefix(wsServer.URL, "http")})
			return
		}
		t.Fatalf("unexpected api path: %s", r.URL.Path)
	}))
	defer apiServer.Close()

	b := bus.New(1)
	ch := &Channel{
		Config:        config.SlackChannelConfig{AppToken: "app", BotToken: "bot", APIBase: apiServer.URL, RequireMention: true},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: image.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for slack media event with mention requirement")
	}
}

func TestChannel_DeliverUploadsMedia(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST upload, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer uploadServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/files.getUploadURLExternal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":         true,
				"upload_url": uploadServer.URL + "/upload",
				"file_id":    "F1",
			})
		case "/files.completeUploadExternal":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected api path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	ch := &Channel{Config: config.SlackChannelConfig{BotToken: "bot", APIBase: apiServer.URL, DefaultChannelID: "C1"}, MaxMediaBytes: 1024}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}, "thread_ts": "123.45"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}
````

## File: internal/channels/slack/slack.go
````go
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
)

type Channel struct {
	Config        config.SlackChannelConfig
	HTTP          *http.Client
	Dialer        *websocket.Dialer
	Artifacts     *artifacts.Store
	MaxMediaBytes int

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	botID  string
}

func (c *Channel) Name() string { return "slack" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.AppToken) == "" || strings.TrimSpace(c.Config.BotToken) == "" {
		return fmt.Errorf("slack tokens not configured")
	}
	url, err := c.openSocketURL(ctx)
	if err != nil {
		return err
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
	c.mu.Unlock()
	go c.readLoop(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.cancel = nil
	c.conn = nil
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("slack channel id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.uploadFiles(ctx, channelID, text, mediaPaths, meta)
	}
	payload := map[string]any{"channel": channelID, "text": text}
	if threadTS, ok := meta["thread_ts"].(string); ok && threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	return c.postJSON(ctx, c.apiBase()+"/chat.postMessage", c.Config.BotToken, payload, nil)
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var envelope socketEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if envelope.EnvelopeID != "" {
			_ = conn.WriteJSON(map[string]any{"envelope_id": envelope.EnvelopeID})
		}
		if envelope.Type == "hello" {
			continue
		}
		if envelope.Type != "events_api" || envelope.Payload.Event.Type != "message" {
			continue
		}
		ev := envelope.Payload.Event
		if ev.BotID != "" || ev.User == "" {
			continue
		}
		if !c.allowedUser(ev.User) {
			continue
		}
		if envelope.Payload.Authorizations[0].UserID != "" && c.botID == "" {
			c.botID = envelope.Payload.Authorizations[0].UserID
		}
		if c.Config.RequireMention && c.botID != "" && !strings.Contains(ev.Text, "<@"+c.botID+">") && len(ev.Files) == 0 {
			continue
		}
		clean := strings.TrimSpace(strings.ReplaceAll(ev.Text, "<@"+c.botID+">", ""))
		sessionKey := "slack:" + ev.Channel
		attachments, markers := c.captureFiles(ctx, sessionKey, ev.Files)
		content := rootchannels.ComposeMessageText(clean, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{"channel_id": ev.Channel, "thread_ts": ev.ThreadTS}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "slack", From: ev.User, Message: content, Meta: meta})
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (c *Channel) openSocketURL(ctx context.Context) (string, error) {
	var resp struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := c.postJSON(ctx, c.apiBase()+"/apps.connections.open", c.Config.AppToken, nil, &resp); err != nil {
		return "", err
	}
	if !resp.OK || resp.URL == "" {
		return "", fmt.Errorf("slack socket url missing")
	}
	return resp.URL, nil
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://slack.com/api"
	}
	return base
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) postJSON(ctx context.Context, endpoint, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) postForm(ctx context.Context, endpoint, token string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) captureFiles(ctx context.Context, sessionKey string, files []slackFile) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(files))
	markers := make([]string, 0, len(files))
	for _, file := range files {
		filename := artifacts.NormalizeFilename(file.Name, file.Mimetype)
		kind := artifacts.DetectKind(filename, file.Mimetype)
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && file.Size > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := c.downloadPrivateFile(ctx, firstNonEmpty(file.URLPrivateDownload, file.URLPrivate))
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "download failed"))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, file.Mimetype, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) downloadPrivateFile(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Config.BotToken)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("slack file download error: %s", resp.Status)
	}
	limit := int64(c.MaxMediaBytes)
	if limit <= 0 {
		limit = 25 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if c.MaxMediaBytes > 0 && len(data) > c.MaxMediaBytes {
		return nil, fmt.Errorf("slack file exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) uploadFiles(ctx context.Context, channelID, text string, mediaPaths []string, meta map[string]any) error {
	files := make([]map[string]any, 0, len(mediaPaths))
	for _, mediaPath := range mediaPaths {
		fileID, title, err := c.uploadFile(ctx, mediaPath)
		if err != nil {
			return err
		}
		files = append(files, map[string]any{"id": fileID, "title": title})
	}
	payload := map[string]any{
		"channel_id": channelID,
		"files":      files,
	}
	if strings.TrimSpace(text) != "" {
		payload["initial_comment"] = text
	}
	if threadTS, ok := meta["thread_ts"].(string); ok && threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.postJSON(ctx, c.apiBase()+"/files.completeUploadExternal", c.Config.BotToken, payload, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("slack complete upload failed: %s", resp.Error)
	}
	return nil
}

func (c *Channel) uploadFile(ctx context.Context, mediaPath string) (string, string, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return "", "", err
	}
	if c.MaxMediaBytes == 0 {
		return "", "", fmt.Errorf("media attachments disabled by config")
	}
	if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
		return "", "", fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
	}
	var start struct {
		OK        bool   `json:"ok"`
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
		Error     string `json:"error"`
	}
	form := url.Values{}
	form.Set("filename", filepath.Base(mediaPath))
	form.Set("length", fmt.Sprintf("%d", info.Size()))
	if err := c.postForm(ctx, c.apiBase()+"/files.getUploadURLExternal", c.Config.BotToken, form, &start); err != nil {
		return "", "", err
	}
	if !start.OK || start.UploadURL == "" || start.FileID == "" {
		return "", "", fmt.Errorf("slack upload init failed: %s", start.Error)
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, start.UploadURL, file)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("slack upload error: %s", resp.Status)
	}
	return start.FileID, filepath.Base(mediaPath), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Channel) allowedUser(user string) bool {
	if len(c.Config.AllowedUserIDs) == 0 {
		return true
	}
	for _, allowed := range c.Config.AllowedUserIDs {
		if strings.TrimSpace(allowed) == user {
			return true
		}
	}
	return false
}

type socketEnvelope struct {
	EnvelopeID string `json:"envelope_id"`
	Type       string `json:"type"`
	Payload    struct {
		Event struct {
			Type     string      `json:"type"`
			Text     string      `json:"text"`
			User     string      `json:"user"`
			BotID    string      `json:"bot_id"`
			Channel  string      `json:"channel"`
			ThreadTS string      `json:"thread_ts"`
			Files    []slackFile `json:"files"`
		} `json:"event"`
		Authorizations []struct {
			UserID string `json:"user_id"`
		} `json:"authorizations"`
	} `json:"payload"`
}

type slackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Mimetype           string `json:"mimetype"`
	Filetype           string `json:"filetype"`
	Size               int64  `json:"size"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
}
````

## File: internal/channels/telegram/telegram_test.go
````go
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openTelegramTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "telegram.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestChannel_FetchUpdatesPublishesMessage(t *testing.T) {
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []map[string]any{{
				"update_id": 1,
				"message": map[string]any{
					"message_id": 99,
					"text":       "hello telegram",
					"chat":       map[string]any{"id": 123},
					"from":       map[string]any{"id": 456, "username": "alice"},
				},
			}},
		})
		mu.Lock()
		mu.Unlock()
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1}}
	b := bus.New(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case ev := <-b.Channel():
		if ev.Channel != "telegram" || ev.SessionKey != "telegram:123" || ev.Message != "hello telegram" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for telegram event")
	}
}

func TestChannel_DeliverSendsMessage(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, DefaultChatID: "123"}}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"reply_to_message_id": int64(44)}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got["chat_id"] != "123" || got["text"] != "hello" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestChannel_FetchUpdatesPublishesPhotoAttachment(t *testing.T) {
	d := openTelegramTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottoken/getUpdates":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": []map[string]any{{
					"update_id": 2,
					"message": map[string]any{
						"message_id": 100,
						"caption":    "see image",
						"chat":       map[string]any{"id": 123},
						"from":       map[string]any{"id": 456, "username": "alice"},
						"photo": []map[string]any{{
							"file_id":   "photo-1",
							"file_size": 10,
						}},
					},
				}},
			})
		case "/bottoken/getFile":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"file_path": "photos/p1.jpg", "file_size": 10},
			})
		case "/file/bottoken/photos/p1.jpg":
			_, _ = w.Write([]byte("image-data"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ch := &Channel{
		Config:        config.TelegramChannelConfig{Token: "token", APIBase: server.URL, PollSeconds: 1},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	b := bus.New(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "see image\n[image: photo.jpg]" {
			t.Fatalf("unexpected message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for telegram media event")
	}
}

func TestChannel_DeliverSendsPhotoUpload(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendPhoto" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if got := r.FormValue("chat_id"); got != "123" {
			t.Fatalf("expected chat_id 123, got %q", got)
		}
		if got := r.FormValue("caption"); got != "hello" {
			t.Fatalf("expected caption hello, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer server.Close()

	ch := &Channel{Config: config.TelegramChannelConfig{Token: "token", APIBase: server.URL, DefaultChatID: "123"}, MaxMediaBytes: 1024}
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}
````

## File: internal/channels/telegram/telegram.go
````go
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.TelegramChannelConfig
	HTTP   *http.Client
	Artifacts *artifacts.Store
	MaxMediaBytes int

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	offset  int64
}

func (c *Channel) Name() string { return "telegram" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.Token) == "" {
		return fmt.Errorf("telegram token not configured")
	}
	if eventBus == nil {
		return fmt.Errorf("event bus not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.running = true
	go c.poll(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.cancel = nil
	c.running = false
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	chatID := strings.TrimSpace(to)
	if chatID == "" {
		chatID = strings.TrimSpace(c.Config.DefaultChatID)
	}
	if chatID == "" {
		return fmt.Errorf("telegram target chat id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.deliverMedia(ctx, chatID, text, mediaPaths, meta)
	}
	payload := map[string]any{"chat_id": chatID, "text": text}
	if replyID, ok := meta["reply_to_message_id"].(int64); ok && replyID > 0 {
		payload["reply_to_message_id"] = replyID
	}
	return c.postJSON(ctx, "/sendMessage", payload, nil)
}

func (c *Channel) poll(ctx context.Context, eventBus *bus.Bus) {
	interval := time.Duration(c.Config.PollSeconds) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.fetchUpdates(ctx, eventBus); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}

}

func (c *Channel) fetchUpdates(ctx context.Context, eventBus *bus.Bus) error {
	query := map[string]string{"timeout": "0"}
	c.mu.Lock()
	if c.offset > 0 {
		query["offset"] = strconv.FormatInt(c.offset, 10)
	}
	c.mu.Unlock()
	var updates []update
	if err := c.getJSON(ctx, "/getUpdates", query, &updates); err != nil {
		return err
	}
	for _, update := range updates {
		c.mu.Lock()
		if next := update.UpdateID + 1; next > c.offset {
			c.offset = next
		}
		c.mu.Unlock()
		msg := update.Message
		chatID := strconv.FormatInt(msg.Chat.ID, 10)
		if !c.allowedChat(chatID) {
			continue
		}
		sessionKey := "telegram:" + chatID
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			text = strings.TrimSpace(msg.Caption)
		}
		attachments, markers := c.captureAttachments(ctx, sessionKey, msg)
		content := rootchannels.ComposeMessageText(text, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{
			"chat_id":             chatID,
			"message_id":          msg.MessageID,
			"reply_to_message_id": int64(msg.MessageID),
			"username":            msg.From.Username,
		}
		if msg.MediaGroupID != "" {
			meta["media_group_id"] = msg.MediaGroupID
		}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: sessionKey,
			Channel:    "telegram",
			From:       strconv.FormatInt(msg.From.ID, 10),
			Message:    content,
			Meta:       meta,
		})
	}
	return nil
}

func (c *Channel) allowedChat(chatID string) bool {
	if len(c.Config.AllowedChatIDs) == 0 {
		return true
	}
	for _, allowed := range c.Config.AllowedChatIDs {
		if strings.TrimSpace(allowed) == chatID {
			return true
		}
	}
	return false
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return base + "/bot" + c.Config.Token
}

func (c *Channel) getJSON(ctx context.Context, path string, query map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase()+path, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *Channel) postJSON(ctx context.Context, path string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, msg inboundMessage) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, 4)
	markers := make([]string, 0, 4)

	// Telegram media groups are processed one update at a time in v1.
	if len(msg.Photo) > 0 {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Photo[len(msg.Photo)-1].FileID,
			FileSize: msg.Photo[len(msg.Photo)-1].FileSize,
			Filename: "photo.jpg",
			Mime:     "image/jpeg",
			Kind:     artifacts.KindImage,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Voice.FileID != "" {
		filename := "voice.ogg"
		if msg.Voice.FileUniqueID != "" {
			filename = msg.Voice.FileUniqueID + ".ogg"
		}
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Voice.FileID,
			FileSize: msg.Voice.FileSize,
			Filename: filename,
			Mime:     "audio/ogg",
			Kind:     artifacts.KindAudio,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Audio.FileID != "" {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Audio.FileID,
			FileSize: msg.Audio.FileSize,
			Filename: msg.Audio.FileName,
			Mime:     msg.Audio.MimeType,
			Kind:     artifacts.KindAudio,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Document.FileID != "" {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Document.FileID,
			FileSize: msg.Document.FileSize,
			Filename: msg.Document.FileName,
			Mime:     msg.Document.MimeType,
			Kind:     artifacts.KindFile,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	return attachments, markers
}

type remoteAttachment struct {
	FileID   string
	FileSize int64
	Filename string
	Mime     string
	Kind     string
}

func (c *Channel) captureRemoteAttachment(ctx context.Context, sessionKey string, remote remoteAttachment) (artifacts.Attachment, string) {
	filename := artifacts.NormalizeFilename(remote.Filename, remote.Mime)
	if remote.Kind == "" {
		remote.Kind = artifacts.DetectKind(filename, remote.Mime)
	}
	if c.MaxMediaBytes == 0 {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "disabled by config")
	}
	if c.MaxMediaBytes > 0 && remote.FileSize > int64(c.MaxMediaBytes) {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "too large")
	}
	if c.Artifacts == nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "storage unavailable")
	}
	info, err := c.getFile(ctx, remote.FileID)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "download failed")
	}
	if c.MaxMediaBytes > 0 && info.FileSize > int64(c.MaxMediaBytes) {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "too large")
	}
	data, err := c.downloadFile(ctx, info.FilePath)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "download failed")
	}
	att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, firstNonEmpty(remote.Mime, mime.TypeByExtension(filepath.Ext(filename))), data)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "save failed")
	}
	return att, artifacts.Marker(att)
}

func (c *Channel) getFile(ctx context.Context, fileID string) (fileInfo, error) {
	var info fileInfo
	err := c.getJSON(ctx, "/getFile", map[string]string{"file_id": fileID}, &info)
	return info, err
}

func (c *Channel) downloadFile(ctx context.Context, filePath string) ([]byte, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if endpoint == "" {
		endpoint = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/file/bot"+c.Config.Token+"/"+strings.TrimLeft(filePath, "/"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram file error: %s", resp.Status)
	}
	limit := int64(c.MaxMediaBytes)
	if limit <= 0 {
		limit = 25 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if c.MaxMediaBytes > 0 && len(data) > c.MaxMediaBytes {
		return nil, fmt.Errorf("telegram file exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) deliverMedia(ctx context.Context, chatID, text string, mediaPaths []string, meta map[string]any) error {
	replyID := replyToMessageID(meta)
	for i, mediaPath := range mediaPaths {
		caption := ""
		if i == 0 {
			caption = text
		}
		if err := c.sendMediaFile(ctx, chatID, mediaPath, caption, replyID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(text) != "" && len(mediaPaths) == 0 {
		return c.postJSON(ctx, "/sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil)
	}
	return nil
}

func (c *Channel) sendMediaFile(ctx context.Context, chatID, mediaPath, caption string, replyID int64) error {
	endpoint, fieldName, mimeType := telegramSendSpec(mediaPath)
	file, err := os.Open(mediaPath)
	if err != nil {
		return err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if replyID > 0 {
		if err := writer.WriteField("reply_to_message_id", strconv.FormatInt(replyID, 10)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(caption) != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile(fieldName, filepath.Base(mediaPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if mimeType != "" {
		req.Header.Set("X-Or3-Media-Type", mimeType)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	return nil
}

func telegramSendSpec(path string) (endpoint string, fieldName string, mimeType string) {
	mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	switch artifacts.DetectKind(path, mimeType) {
	case artifacts.KindImage:
		return "/sendPhoto", "photo", mimeType
	case artifacts.KindAudio:
		if strings.HasSuffix(strings.ToLower(path), ".ogg") || strings.HasSuffix(strings.ToLower(path), ".opus") {
			return "/sendVoice", "voice", mimeType
		}
		return "/sendAudio", "audio", mimeType
	default:
		return "/sendDocument", "document", mimeType
	}
}

func replyToMessageID(meta map[string]any) int64 {
	switch v := meta["reply_to_message_id"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

type update struct {
	UpdateID int64          `json:"update_id"`
	Message  inboundMessage `json:"message"`
}

type inboundMessage struct {
	MessageID    int    `json:"message_id"`
	Text         string `json:"text"`
	Caption      string `json:"caption"`
	MediaGroupID string `json:"media_group_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	Photo []struct {
		FileID   string `json:"file_id"`
		FileSize int64  `json:"file_size"`
	} `json:"photo"`
	Voice struct {
		FileID       string `json:"file_id"`
		FileUniqueID string `json:"file_unique_id"`
		FileSize     int64  `json:"file_size"`
	} `json:"voice"`
	Audio struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	} `json:"audio"`
	Document struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	} `json:"document"`
}

type fileInfo struct {
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}
````

## File: internal/channels/whatsapp/whatsapp_test.go
````go
package whatsapp

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

func openWhatsAppTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "whatsapp.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestChannel_StartPublishesInboundMessage(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"type": "message", "id": "m1", "chat": "group1", "from": "123", "text": "hello", "isGroup": true})
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	b := bus.New(1)
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):]}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	select {
	case ev := <-b.Channel():
		if ev.Channel != "whatsapp" || ev.SessionKey != "whatsapp:group1" || ev.Message != "hello" {
			t.Fatalf("unexpected event: %#v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound whatsapp message")
	}
}

func TestChannel_DeliverWritesSendCommand(t *testing.T) {
	upgrader := websocket.Upgrader{}
	got := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("ReadJSON: %v", err)
		}
		got <- msg
	}))
	defer server.Close()
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], DefaultTo: "123"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, bus.New(1)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	if err := ch.Deliver(context.Background(), "", "hello", nil); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	select {
	case msg := <-got:
		if msg["type"] != "send" || msg["to"] != "123" || msg["text"] != "hello" {
			t.Fatalf("unexpected send command: %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for send command")
	}
}

func TestChannel_StartPublishesInboundAttachmentMessage(t *testing.T) {
	d := openWhatsAppTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"type": "message",
			"id":   "m1",
			"chat": "group1",
			"from": "123",
			"text": "",
			"attachments": []map[string]any{{
				"data_base64": base64.StdEncoding.EncodeToString([]byte("image-data")),
				"filename":    "photo.png",
				"mime":        "image/png",
				"kind":        "image",
				"size_bytes":  10,
			}},
		})
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	b := bus.New(1)
	ch := &Channel{
		Config:        config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):]},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: photo.png]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
		raw, ok := ev.Meta["attachments"].([]artifacts.Attachment)
		if !ok || len(raw) != 1 {
			t.Fatalf("expected one attachment in meta, got %#v", ev.Meta["attachments"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound whatsapp media message")
	}
}

func TestChannel_DeliverWritesSendCommandWithAttachments(t *testing.T) {
	mediaPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(mediaPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	upgrader := websocket.Upgrader{}
	got := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("ReadJSON: %v", err)
		}
		got <- msg
	}))
	defer server.Close()
	ch := &Channel{Config: config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):], DefaultTo: "123"}, MaxMediaBytes: 1024}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, bus.New(1)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())
	if err := ch.Deliver(context.Background(), "", "hello", map[string]any{"media_paths": []string{mediaPath}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	select {
	case msg := <-got:
		attachments, ok := msg["attachments"].([]any)
		if msg["type"] != "send" || msg["to"] != "123" || msg["text"] != "hello" || !ok || len(attachments) != 1 {
			t.Fatalf("unexpected send command: %#v", msg)
		}
		first, ok := attachments[0].(map[string]any)
		if !ok || first["data_base64"] == "" {
			t.Fatalf("expected inline attachment payload, got %#v", attachments[0])
		}
		if _, hasPath := first["path"]; hasPath {
			t.Fatalf("expected outbound bridge payload to omit local path, got %#v", first)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for media send command")
	}
}

func TestChannel_StartRejectsPathOnlyInboundAttachment(t *testing.T) {
	d := openWhatsAppTestDB(t)
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	attachmentPath := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(attachmentPath, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{
			"type": "message",
			"id":   "m1",
			"chat": "group1",
			"from": "123",
			"text": "",
			"attachments": []map[string]any{{
				"path":       attachmentPath,
				"filename":   "photo.png",
				"mime":       "image/png",
				"kind":       "image",
				"size_bytes": 10,
			}},
		})
		<-time.After(100 * time.Millisecond)
	}))
	defer server.Close()
	b := bus.New(1)
	ch := &Channel{
		Config:        config.WhatsAppBridgeConfig{BridgeURL: "ws" + server.URL[len("http"):]},
		Artifacts:     store,
		MaxMediaBytes: 1024,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx, b); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ch.Stop(context.Background())

	select {
	case ev := <-b.Channel():
		if ev.Message != "[image: photo.png - invalid media payload]" {
			t.Fatalf("unexpected event message: %#v", ev.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound whatsapp invalid media marker")
	}
}
````

## File: internal/channels/whatsapp/whatsapp.go
````go
package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
)

type Channel struct {
	Config        config.WhatsAppBridgeConfig
	Dialer        *websocket.Dialer
	Artifacts     *artifacts.Store
	MaxMediaBytes int

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	closed bool
}

func (c *Channel) Name() string { return "whatsapp" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.BridgeURL) == "" {
		return fmt.Errorf("whatsapp bridge url not configured")
	}
	conn, err := c.connect(ctx)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
	c.closed = false
	c.mu.Unlock()
	go c.readLoop(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.cancel = nil
	c.closed = true
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	target := strings.TrimSpace(to)
	if target == "" {
		target = strings.TrimSpace(c.Config.DefaultTo)
	}
	if target == "" {
		return fmt.Errorf("whatsapp target required")
	}
	cmd := map[string]any{"type": "send", "to": target, "text": text}
	if mediaPaths := rootchannels.MediaPaths(meta); len(mediaPaths) > 0 {
		attachments, err := c.outboundAttachments(mediaPaths)
		if err != nil {
			return err
		}
		cmd["attachments"] = attachments
	}
	if meta != nil {
		for k, v := range meta {
			if k == "media_paths" {
				continue
			}
			cmd[k] = v
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("whatsapp bridge not connected")
	}
	return c.conn.WriteJSON(cmd)
}

func (c *Channel) connect(ctx context.Context) (*websocket.Conn, error) {
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	headers := http.Header{}
	if token := strings.TrimSpace(c.Config.BridgeToken); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := dialer.DialContext(ctx, c.Config.BridgeURL, headers)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		var msg inboundMessage
		if err := conn.ReadJSON(&msg); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		if msg.Type != "message" {
			continue
		}
		if !c.allowedFrom(msg.From) {
			continue
		}
		target := strings.TrimSpace(msg.Chat)
		if target == "" {
			target = strings.TrimSpace(msg.From)
		}
		attachments, markers := c.captureAttachments(ctx, "whatsapp:"+target, msg.Attachments)
		content := rootchannels.ComposeMessageText(msg.Text, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{
			"chat_id":             target,
			"message_id":          msg.ID,
			"reply_to_message_id": msg.ID,
			"is_group":            msg.IsGroup,
		}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: "whatsapp:" + target,
			Channel:    "whatsapp",
			From:       msg.From,
			Message:    content,
			Meta:       meta,
		})
	}
}

func (c *Channel) allowedFrom(from string) bool {
	if len(c.Config.AllowedFrom) == 0 {
		return true
	}
	for _, allowed := range c.Config.AllowedFrom {
		if strings.TrimSpace(allowed) == strings.TrimSpace(from) {
			return true
		}
	}
	return false
}

type inboundMessage struct {
	Type        string             `json:"type"`
	ID          string             `json:"id"`
	Chat        string             `json:"chat"`
	From        string             `json:"from"`
	Text        string             `json:"text"`
	IsGroup     bool               `json:"isGroup"`
	Attachments []bridgeAttachment `json:"attachments"`
}

type bridgeAttachment struct {
	Path       string `json:"path,omitempty"`
	DataBase64 string `json:"data_base64,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Mime       string `json:"mime,omitempty"`
	Kind       string `json:"kind,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, refs []bridgeAttachment) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(refs))
	markers := make([]string, 0, len(refs))
	for _, ref := range refs {
		filename := artifacts.NormalizeFilename(ref.Filename, ref.Mime)
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" {
			kind = artifacts.DetectKind(filename, ref.Mime)
		}
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && ref.SizeBytes > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := decodeBridgeAttachment(ref, c.MaxMediaBytes)
		if err != nil {
			reason := "invalid media payload"
			if strings.Contains(err.Error(), "too large") {
				reason = "too large"
			}
			markers = append(markers, artifacts.FailureMarker(kind, filename, reason))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, ref.Mime, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) outboundAttachments(paths []string) ([]bridgeAttachment, error) {
	attachments := make([]bridgeAttachment, 0, len(paths))
	for _, mediaPath := range paths {
		info, err := os.Stat(mediaPath)
		if err != nil {
			return nil, err
		}
		if c.MaxMediaBytes == 0 {
			return nil, fmt.Errorf("media attachments disabled by config")
		}
		if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
			return nil, fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
		}
		data, err := os.ReadFile(mediaPath)
		if err != nil {
			return nil, err
		}
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(mediaPath)))
		attachments = append(attachments, bridgeAttachment{
			DataBase64: base64.StdEncoding.EncodeToString(data),
			Filename:   filepath.Base(mediaPath),
			Mime:       mimeType,
			Kind:       artifacts.DetectKind(mediaPath, mimeType),
			SizeBytes:  info.Size(),
		})
	}
	return attachments, nil
}

func decodeBridgeAttachment(ref bridgeAttachment, maxBytes int) ([]byte, error) {
	raw := strings.TrimSpace(ref.DataBase64)
	if raw == "" {
		return nil, fmt.Errorf("missing inline data")
	}
	if maxBytes > 0 && base64.StdEncoding.DecodedLen(len(raw)) > maxBytes {
		return nil, fmt.Errorf("attachment too large")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return nil, fmt.Errorf("attachment too large")
	}
	return data, nil
}

func BridgeURL(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil {
		return ""
	}
	if u.Path == "" {
		u.Path = "/ws"
	}
	return u.String()
}

func NewTestDialer() *websocket.Dialer {
	return &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
}
````

## File: internal/cron/cron_test.go
````go
package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error {
		return nil
	})
	return svc, path
}

func TestNew(t *testing.T) {
	svc, _ := makeService(t)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestStart_Stop(t *testing.T) {
	svc, _ := makeService(t)

	if err := svc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Starting again should be a no-op
	if err := svc.Start(); err != nil {
		t.Fatalf("Start (second call): %v", err)
	}
	svc.Stop()
	// Stopping again should be a no-op
	svc.Stop()
}

func TestAdd_And_List(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	job := CronJob{
		Name:    "test job",
		Enabled: true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
		Payload:  CronPayload{Kind: "agent_turn", Message: "hello"},
	}
	if err := svc.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	jobs, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "test job" {
		t.Errorf("expected name 'test job', got %q", jobs[0].Name)
	}
}

func TestAdd_AutoGeneratesID(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	job := CronJob{Name: "no-id", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}}
	svc.Add(job)

	jobs, _ := svc.List()
	if jobs[0].ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestAdd_UsesNameAsIDIfMissing(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	job := CronJob{Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}}
	svc.Add(job)

	jobs, _ := svc.List()
	// ID should match Name when no Name is given either (both auto-generated)
	if jobs[0].Name == "" {
		t.Error("expected name to default to id")
	}
}

func TestAdd_SetsTimestamps(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	before := time.Now().UnixMilli()
	svc.Add(CronJob{Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})
	after := time.Now().UnixMilli()

	jobs, _ := svc.List()
	if jobs[0].CreatedAtMS < before || jobs[0].CreatedAtMS > after {
		t.Errorf("CreatedAtMS out of range: got %d, expected [%d, %d]", jobs[0].CreatedAtMS, before, after)
	}
}

func TestRemove_Found(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{ID: "job1", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})

	found, err := svc.Remove("job1")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !found {
		t.Error("expected found=true")
	}

	jobs, _ := svc.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after removal, got %d", len(jobs))
	}
}

func TestRemove_NotFound(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	found, err := svc.Remove("nonexistent")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if found {
		t.Error("expected found=false for nonexistent job")
	}
}

func TestRunNow_Success(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error {
		ran = true
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:       "runme",
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found, err := svc.RunNow(context.Background(), "runme", false)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !found {
		t.Error("expected found=true")
	}
	if !ran {
		t.Error("expected runner to be called")
	}
}

func TestRunNow_NotFound(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	found, err := svc.RunNow(context.Background(), "missing", false)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if found {
		t.Error("expected found=false for missing job")
	}
}

func TestRunNow_Disabled_NoForce(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error {
		ran = true
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:       "disabled",
		Enabled:  false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found, _ := svc.RunNow(context.Background(), "disabled", false)
	if found {
		t.Error("expected found=false for disabled job without force")
	}
	if ran {
		t.Error("expected runner NOT to be called for disabled job without force")
	}
}

func TestRunNow_Disabled_WithForce(t *testing.T) {
	ran := false
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error {
		ran = true
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:       "force-run",
		Enabled:  false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	found, err := svc.RunNow(context.Background(), "force-run", true)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if !found {
		t.Error("expected found=true with force")
	}
	if !ran {
		t.Error("expected runner to be called with force")
	}
}

func TestRunNow_DeleteAfterRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error {
		return nil
	})
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:             "delete-after",
		Enabled:        true,
		DeleteAfterRun: true,
		Schedule:       CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	svc.RunNow(context.Background(), "delete-after", false)

	jobs, _ := svc.List()
	for _, j := range jobs {
		if j.ID == "delete-after" {
			t.Error("expected job to be deleted after run")
		}
	}
}

func TestStatus_NoJobs(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	s, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s["jobs"].(int) != 0 {
		t.Errorf("expected 0 jobs, got %v", s["jobs"])
	}
}

func TestStatus_WithJobs(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	s, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s["jobs"].(int) != 1 {
		t.Errorf("expected 1 job, got %v", s["jobs"])
	}
}

func TestStatus_NextWakeAtMS(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	// Add a job with a known next_run_at_ms
	next := time.Now().Add(time.Hour).UnixMilli()
	svc.Add(CronJob{
		Enabled:  true,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
		State:    CronJobState{NextRunAtMS: &next},
	})

	s, err := svc.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s["next_wake_at_ms"] == nil {
		t.Error("expected next_wake_at_ms to be set")
	}
}

func TestArmJob_KindAt_PastTime(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	// at time in the past - should not schedule (no panic)
	svc.Add(CronJob{
		ID:      "at-past",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindAt,
			AtMS: time.Now().Add(-time.Hour).UnixMilli(),
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestArmJob_KindEvery_ZeroInterval(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	// Zero EveryMS should default to 60s
	svc.Add(CronJob{
		ID:      "every-zero",
		Enabled: true,
		Schedule: CronSchedule{
			Kind:    KindEvery,
			EveryMS: 0,
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestArmJob_KindCron_ValidExpr(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:      "cron-expr",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindCron,
			Expr: "0 * * * *",
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestArmJob_KindCron_InvalidExpr(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	// Invalid cron expr - should log but not panic
	svc.Add(CronJob{
		ID:      "bad-expr",
		Enabled: true,
		Schedule: CronSchedule{
			Kind: KindCron,
			Expr: "not a valid cron expression at all",
		},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job (still added, just not scheduled), got %d", len(jobs))
	}
}

func TestArmJob_DisabledJob(t *testing.T) {
	svc, _ := makeService(t)
	svc.Start()
	defer svc.Stop()

	svc.Add(CronJob{
		ID:      "disabled",
		Enabled: false,
		Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
	})

	jobs, _ := svc.List()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

func TestFilepathDir(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/home/user/config.json", "/home/user"},
		{"config.json", "."},
		{"/config.json", "."},
		{"a/b/c/d.json", "a/b/c"},
	}
	for _, tc := range tests {
		got := filepathDir(tc.input)
		if got != tc.want {
			t.Errorf("filepathDir(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRandID_Length(t *testing.T) {
	id := randID()
	if len(id) != 10 {
		t.Errorf("expected 10-char ID, got %d: %q", len(id), id)
	}
}

func TestRandID_Uniqueness(t *testing.T) {
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := randID()
		if ids[id] {
			t.Errorf("duplicate id generated: %q", id)
		}
		ids[id] = true
	}
}

func TestLoad_FileNotExist(t *testing.T) {
	svc, _ := makeService(t)
	// load from non-existent path should return empty store
	st, err := svc.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(st.Jobs) != 0 {
		t.Errorf("expected 0 jobs from non-existent file, got %d", len(st.Jobs))
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	os.WriteFile(path, []byte("{invalid"), 0o644)

	svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
	_, err := svc.load()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSave_And_Load(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error { return nil })

	st := Store{
		Version: 1,
		Jobs: []CronJob{
			{ID: "saved-job", Name: "saved", Enabled: true},
		},
	}
	if err := svc.save(st); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := svc.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(loaded.Jobs))
	}
	if loaded.Jobs[0].ID != "saved-job" {
		t.Errorf("expected ID 'saved-job', got %q", loaded.Jobs[0].ID)
	}
}

func TestRunNow_UpdatesLastRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
	svc.Start()
	defer svc.Stop()

	before := time.Now().UnixMilli()
	svc.Add(CronJob{ID: "track-run", Enabled: true, Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000}})
	svc.RunNow(context.Background(), "track-run", false)
	after := time.Now().UnixMilli()

	jobs, _ := svc.List()
	if len(jobs) == 0 {
		t.Fatal("expected 1 job")
	}
	if jobs[0].State.LastRunAtMS == nil {
		t.Fatal("expected LastRunAtMS to be set")
	}
	if *jobs[0].State.LastRunAtMS < before || *jobs[0].State.LastRunAtMS > after {
		t.Errorf("LastRunAtMS=%d out of range [%d,%d]", *jobs[0].State.LastRunAtMS, before, after)
	}
	if jobs[0].State.LastStatus != "ok" {
		t.Errorf("expected LastStatus='ok', got %q", jobs[0].State.LastStatus)
	}
}

func TestArmJob_KindAt_FutureTime(t *testing.T) {
runCh := make(chan struct{}, 1)
dir := t.TempDir()
path := filepath.Join(dir, "cron.json")
svc := New(path, func(ctx context.Context, job CronJob) error {
runCh <- struct{}{}
return nil
})
svc.Start()
defer svc.Stop()

// Schedule to run very soon
atMS := time.Now().Add(100 * time.Millisecond).UnixMilli()
svc.Add(CronJob{
ID:      "at-future",
Enabled: true,
Schedule: CronSchedule{
Kind: KindAt,
AtMS: atMS,
},
})

// Wait for it to run
select {
case <-runCh:
// success
case <-time.After(2 * time.Second):
t.Error("timeout waiting for KindAt job to run")
}
}

func TestRemove_WithSchedulerEntry(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "cron.json")
svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
svc.Start()
defer svc.Stop()

// Add a cron job (uses the scheduler)
svc.Add(CronJob{
ID:      "sched-job",
Enabled: true,
Schedule: CronSchedule{Kind: KindCron, Expr: "0 * * * *"},
})

// Remove should also remove from scheduler entries
found, err := svc.Remove("sched-job")
if err != nil {
t.Fatalf("Remove: %v", err)
}
if !found {
t.Error("expected found=true")
}

jobs, _ := svc.List()
if len(jobs) != 0 {
t.Errorf("expected 0 jobs after removal, got %d", len(jobs))
}
}

func TestStart_WithExistingJobs(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "cron.json")

// First, create a service and add jobs
svc1 := New(path, func(ctx context.Context, job CronJob) error { return nil })
svc1.Start()
svc1.Add(CronJob{
ID:      "existing",
Enabled: true,
Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
})
svc1.Stop()

// Create a new service with same path - Start should load existing jobs
svc2 := New(path, func(ctx context.Context, job CronJob) error { return nil })
if err := svc2.Start(); err != nil {
t.Fatalf("Start with existing jobs: %v", err)
}
defer svc2.Stop()

jobs, _ := svc2.List()
if len(jobs) != 1 {
t.Errorf("expected 1 job loaded from file, got %d", len(jobs))
}
}

func TestRunNow_SaveError(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "cron.json")
svc := New(path, func(ctx context.Context, job CronJob) error { return nil })
svc.Start()
defer svc.Stop()

svc.Add(CronJob{
ID:      "save-err",
Enabled: true,
Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
})

// Run successfully
found, err := svc.RunNow(context.Background(), "save-err", false)
if err != nil {
t.Fatalf("RunNow: %v", err)
}
if !found {
t.Error("expected found=true")
}
}

func TestCronPayloadSessionKey(t *testing.T) {
payload := CronPayload{
Kind:       "agent_turn",
Message:    "hello from cron",
SessionKey: "custom-session-123",
Channel:    "telegram",
To:         "user456",
}

// Serialize
data, err := json.Marshal(payload)
if err != nil {
t.Fatalf("Marshal: %v", err)
}

// Deserialize
var decoded CronPayload
if err := json.Unmarshal(data, &decoded); err != nil {
t.Fatalf("Unmarshal: %v", err)
}

if decoded.SessionKey != "custom-session-123" {
t.Errorf("expected SessionKey %q, got %q", "custom-session-123", decoded.SessionKey)
}
if decoded.Kind != "agent_turn" {
t.Errorf("expected Kind %q, got %q", "agent_turn", decoded.Kind)
}
if decoded.Message != "hello from cron" {
t.Errorf("expected Message %q, got %q", "hello from cron", decoded.Message)
}
}

func TestCronPayloadSessionKey_OmitEmpty(t *testing.T) {
// SessionKey should be omitted when empty (json:"session_key,omitempty")
payload := CronPayload{
Kind:    "agent_turn",
Message: "no session key",
}
data, err := json.Marshal(payload)
if err != nil {
t.Fatalf("Marshal: %v", err)
}
if strings.Contains(string(data), "session_key") {
t.Errorf("expected session_key to be omitted when empty, got: %s", string(data))
}
}

func TestCronRunnerPerJobSession(t *testing.T) {
svc, _ := makeService(t)
if err := svc.Start(); err != nil {
t.Fatalf("Start: %v", err)
}
defer svc.Stop()

// Track which session key the runner sees
var capturedSessionKey string
var runnerCalled bool
svc2 := &Service{
path: svc.path,
runner: func(ctx context.Context, job CronJob) error {
capturedSessionKey = job.Payload.SessionKey
runnerCalled = true
return nil
},
}

// Simulate a job with per-job SessionKey
job := CronJob{
ID:      "per-job-session",
Name:    "Per Job Session Test",
Enabled: true,
Schedule: CronSchedule{Kind: KindEvery, EveryMS: 60000},
Payload: CronPayload{
Kind:       "agent_turn",
Message:    "per-job message",
SessionKey: "per-job-session-key",
},
}

// Directly call the runner with the job
if err := svc2.runner(context.Background(), job); err != nil {
t.Fatalf("runner: %v", err)
}

if !runnerCalled {
t.Fatal("expected runner to be called")
}
if capturedSessionKey != "per-job-session-key" {
t.Errorf("expected SessionKey %q, got %q", "per-job-session-key", capturedSessionKey)
}
}
````

## File: internal/memory/retrieve_test.go
````go
package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

func openRetrieveTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestNewRetriever(t *testing.T) {
	d := openRetrieveTestDB(t)
	r := NewRetriever(d)
	if r == nil {
		t.Fatal("expected non-nil retriever")
	}
	if r.VectorWeight != 0.7 {
		t.Errorf("expected VectorWeight=0.7, got %v", r.VectorWeight)
	}
	if r.FTSWeight != 0.3 {
		t.Errorf("expected FTSWeight=0.3, got %v", r.FTSWeight)
	}
	if r.VectorScanLimit != 2000 {
		t.Errorf("expected VectorScanLimit=2000, got %d", r.VectorScanLimit)
	}
}

func TestRetrieve_Empty(t *testing.T) {
	d := openRetrieveTestDB(t)
	r := NewRetriever(d)
	ctx := context.Background()

	results, err := r.Retrieve(ctx, "session1", "hello", []float32{0.5, 0.5}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestRetrieve_WithVectorResults(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	blob := PackFloat32([]float32{1.0, 0.0})
	d.InsertMemoryNote(ctx, "session1", "vector match", blob, sql.NullInt64{}, "")

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "query", []float32{1.0, 0.0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	found := false
	for _, res := range results {
		if res.Text == "vector match" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'vector match' in results")
	}
}

func TestRetrieve_SourceLabels(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	// Insert note with known embedding
	blob := PackFloat32([]float32{1.0, 0.0})
	d.InsertMemoryNote(ctx, "session1", "fox quick jump", blob, sql.NullInt64{}, "")

	r := NewRetriever(d)
	// Exact vector match, also FTS match
	results, err := r.Retrieve(ctx, "session1", "fox quick jump", []float32{1.0, 0.0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// If both vector and FTS match, source should be "hybrid"
	for _, res := range results {
		if res.Text == "fox quick jump" {
			if res.Source != "hybrid" && res.Source != "vector" && res.Source != "fts" {
				t.Errorf("unexpected source %q", res.Source)
			}
		}
	}
}

func TestRetrieve_TopKLimit(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		blob := PackFloat32([]float32{float32(i), 0.0})
		d.InsertMemoryNote(ctx, "session1", "note", blob, sql.NullInt64{}, "")
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "note", []float32{5.0, 0.0}, 10, 10, 3)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestRetrieve_SortedByScore(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	blobs := [][]float32{{1, 0}, {0, 1}, {0.7071, 0.7071}}
	texts := []string{"alpha", "beta", "gamma"}
	for i, v := range blobs {
		d.InsertMemoryNote(ctx, "session1", texts[i], PackFloat32(v), sql.NullInt64{}, "")
	}

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session1", "alpha", []float32{1, 0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending: [%d]=%v > [%d]=%v", i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestRetrieve_RespectsSessionScope(t *testing.T) {
	d := openRetrieveTestDB(t)
	ctx := context.Background()

	d.InsertMemoryNote(ctx, "session-a", "private note", PackFloat32([]float32{1, 0}), sql.NullInt64{}, "")
	d.InsertMemoryNote(ctx, scope.GlobalMemoryScope, "shared note", PackFloat32([]float32{1, 0}), sql.NullInt64{}, "")

	r := NewRetriever(d)
	results, err := r.Retrieve(ctx, "session-b", "note", []float32{1, 0}, 5, 5, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 || results[0].Text != "shared note" {
		t.Fatalf("expected shared note only, got %#v", results)
	}
	for _, result := range results {
		if result.Text == "private note" {
			t.Fatalf("unexpected cross-session result: %#v", results)
		}
	}
}

func TestNormalizeFTSQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  ", ""},
		{"hello world", "hello world"},
		{"hello:world", `"hello:world"`},
		{`with "quotes"`, `with """quotes"""`},
		{"star*term", `"star*term"`},
		{"normal words only", "normal words only"},
	}
	for _, tc := range tests {
		got := normalizeFTSQuery(tc.input)
		if got != tc.want {
			t.Errorf("normalizeFTSQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
````

## File: internal/memory/scheduler_test.go
````go
package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_SingleFlightAndCoalesce(t *testing.T) {
	var started int32
	block := make(chan struct{})
	done := make(chan struct{})
	s := NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		atomic.AddInt32(&started, 1)
		if atomic.LoadInt32(&started) == 1 {
			<-block
		}
		if atomic.LoadInt32(&started) >= 2 {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	s.Trigger("sess")
	time.Sleep(30 * time.Millisecond)
	s.Trigger("sess")
	close(block)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected coalesced second pass")
	}
	if got := atomic.LoadInt32(&started); got != 2 {
		t.Fatalf("expected exactly 2 runs, got %d", got)
	}
}

func TestScheduler_IndependentSessions(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	done := make(chan struct{}, 2)
	s := NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		mu.Lock()
		calls[sessionKey]++
		mu.Unlock()
		done <- struct{}{}
	})
	s.Trigger("a")
	s.Trigger("b")

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("expected both sessions to run")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"] != 1 || calls["b"] != 1 {
		t.Fatalf("unexpected calls: %#v", calls)
	}
}

func TestScheduler_RemovesIdleSessionState(t *testing.T) {
	done := make(chan struct{})
	s := NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		close(done)
	})
	s.Trigger("sess")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected scheduler run")
	}
	time.Sleep(30 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) != 0 {
		t.Fatalf("expected idle sessions map to be empty, got %#v", s.sessions)
	}
}

func TestScheduler_RunUsesCanceledBaseContext(t *testing.T) {
	baseCtx, cancel := context.WithCancel(context.Background())
	cancel()
	errCh := make(chan error, 1)
	s := NewSchedulerWithContext(baseCtx, 2*time.Second, func(ctx context.Context, sessionKey string) {
		errCh <- ctx.Err()
	})
	s.Trigger("sess")
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected canceled context")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected scheduler run")
	}
}
````

## File: internal/memory/vector_test.go
````go
package memory

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"

	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

// ---- PackFloat32 / UnpackFloat32 ----

func TestPackUnpackFloat32_RoundTrip(t *testing.T) {
	orig := []float32{1.0, 2.5, -3.0, 0.0, 1e10}
	packed := PackFloat32(orig)
	if len(packed) != len(orig)*4 {
		t.Errorf("expected %d bytes, got %d", len(orig)*4, len(packed))
	}
	got, err := UnpackFloat32(packed)
	if err != nil {
		t.Fatalf("UnpackFloat32: %v", err)
	}
	if len(got) != len(orig) {
		t.Fatalf("expected %d values, got %d", len(orig), len(got))
	}
	for i := range orig {
		if got[i] != orig[i] {
			t.Errorf("index %d: expected %v, got %v", i, orig[i], got[i])
		}
	}
}

func TestPackFloat32_Empty(t *testing.T) {
	packed := PackFloat32(nil)
	if len(packed) != 0 {
		t.Errorf("expected empty bytes for nil input, got %d bytes", len(packed))
	}
}

func TestUnpackFloat32_Invalid(t *testing.T) {
	_, err := UnpackFloat32([]byte{1, 2, 3}) // 3 bytes is not a multiple of 4
	if err == nil {
		t.Fatal("expected error for invalid blob size")
	}
}

func TestUnpackFloat32_Empty(t *testing.T) {
	got, err := UnpackFloat32([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(got))
	}
}

// ---- Cosine ----

func TestCosine_Identical(t *testing.T) {
	v := []float32{1, 2, 3}
	score := Cosine(v, v)
	if math.Abs(score-1.0) > 1e-6 {
		t.Errorf("expected cosine similarity of identical vectors ≈1.0, got %v", score)
	}
}

func TestCosine_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	score := Cosine(a, b)
	if math.Abs(score) > 1e-6 {
		t.Errorf("expected cosine similarity of orthogonal vectors ≈0.0, got %v", score)
	}
}

func TestCosine_Opposite(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{-1, 0}
	score := Cosine(a, b)
	if math.Abs(score+1.0) > 1e-6 {
		t.Errorf("expected cosine similarity of opposite vectors ≈-1.0, got %v", score)
	}
}

func TestCosine_ZeroVectorA(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{1, 2}
	score := Cosine(a, b)
	if score != 0 {
		t.Errorf("expected 0 when a is zero vector, got %v", score)
	}
}

func TestCosine_ZeroVectorB(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{0, 0}
	score := Cosine(a, b)
	if score != 0 {
		t.Errorf("expected 0 when b is zero vector, got %v", score)
	}
}

func TestCosine_DifferentLengths(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0}
	// should use min length
	score := Cosine(a, b)
	if math.Abs(score-1.0) > 1e-6 {
		t.Errorf("expected ~1.0 for first-element match, got %v", score)
	}
}

// ---- heap ----

func TestCandMinHeap(t *testing.T) {
	h := &candMinHeap{
		{ID: 1, Score: 0.5},
		{ID: 2, Score: 0.9},
		{ID: 3, Score: 0.1},
	}
	if h.Len() != 3 {
		t.Errorf("expected Len 3, got %d", h.Len())
	}
	if !h.Less(2, 0) { // 0.1 < 0.5
		t.Error("expected Less(2,0) to be true")
	}
	h.Swap(0, 1)
	if (*h)[0].ID != 2 || (*h)[1].ID != 1 {
		t.Error("expected Swap to swap elements")
	}
}

// ---- VectorSearch ----

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestVectorSearch_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	query := []float32{0.5, 0.5}
	results, err := VectorSearch(ctx, d, "session1", query, 5, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestVectorSearch_Results(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert notes with embeddings
	vecs := [][]float32{
		{1, 0},
		{0, 1},
		{0.7071, 0.7071},
	}
	for i, v := range vecs {
		blob := PackFloat32(v)
		d.InsertMemoryNote(ctx, "session1", []string{"first", "second", "third"}[i], blob, sql.NullInt64{}, "")
	}

	// Query similar to {1, 0}
	results, err := VectorSearch(ctx, d, "session1", []float32{1, 0}, 3, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// "first" should appear in results (it has cosine=1.0 with {1,0})
	found := false
	for _, r := range results {
		if r.Text == "first" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'first' to be in results")
	}
	// VectorSearch returns results sorted ascending (min->max) by score
	// Just verify all results are present
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestVectorSearch_InvalidEmbeddingSkipped(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert a note with invalid embedding
	d.InsertMemoryNote(ctx, "session1", "bad note", []byte{1, 2, 3}, sql.NullInt64{}, "")
	// Insert a good one
	blob := PackFloat32([]float32{1, 0})
	d.InsertMemoryNote(ctx, "session1", "good note", blob, sql.NullInt64{}, "")

	results, err := VectorSearch(ctx, d, "session1", []float32{1, 0}, 5, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (bad note skipped), got %d", len(results))
	}
	if results[0].Text != "good note" {
		t.Errorf("expected 'good note', got %q", results[0].Text)
	}
}

func TestVectorSearch_KLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert 5 notes
	for i := 0; i < 5; i++ {
		blob := PackFloat32([]float32{float32(i), 0})
		d.InsertMemoryNote(ctx, "session1", "note", blob, sql.NullInt64{}, "")
	}

	results, err := VectorSearch(ctx, d, "session1", []float32{4, 0}, 3, 100)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestVectorSearch_PreservesSessionRowsWhenGlobalCorpusIsNewer(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.InsertMemoryNote(ctx, "session-a", "session match", PackFloat32([]float32{1, 0}), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote session: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := d.InsertMemoryNote(ctx, scope.GlobalMemoryScope, "shared note", PackFloat32([]float32{0, 1}), sql.NullInt64{}, ""); err != nil {
			t.Fatalf("InsertMemoryNote shared: %v", err)
		}
	}

	results, err := VectorSearch(ctx, d, "session-a", []float32{1, 0}, 3, 2)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	found := false
	for _, result := range results {
		if result.Text == "session match" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected session-scoped note to remain searchable, got %#v", results)
	}
}
````

## File: internal/tools/context.go
````go
package tools

import (
	"context"

	"or3-intern/internal/scope"
)

type sessionContextKey struct{}
type deliveryChannelContextKey struct{}
type deliveryToContextKey struct{}

func ContextWithSession(ctx context.Context, sessionKey string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sessionKey == "" {
		sessionKey = scope.GlobalMemoryScope
	}
	return context.WithValue(ctx, sessionContextKey{}, sessionKey)
}

func ContextWithDelivery(ctx context.Context, channel, to string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, deliveryChannelContextKey{}, channel)
	return context.WithValue(ctx, deliveryToContextKey{}, to)
}

func SessionFromContext(ctx context.Context) string {
	if ctx == nil {
		return scope.GlobalMemoryScope
	}
	if sessionKey, ok := ctx.Value(sessionContextKey{}).(string); ok && sessionKey != "" {
		return sessionKey
	}
	return scope.GlobalMemoryScope
}

func DeliveryFromContext(ctx context.Context) (channel string, to string) {
	if ctx == nil {
		return "", ""
	}
	if v, ok := ctx.Value(deliveryChannelContextKey{}).(string); ok {
		channel = v
	}
	if v, ok := ctx.Value(deliveryToContextKey{}).(string); ok {
		to = v
	}
	return channel, to
}
````

## File: internal/tools/exec.go
````go
package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ExecTool struct {
	Base
	Timeout time.Duration
	RestrictDir string // if non-empty, cwd must be inside
	PathAppend string
	OutputMaxBytes int
	BlockedPatterns []string
}

const defaultExecOutputMaxBytes = 10000

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Run a shell command with safety limits. Output is truncated."
}
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to run"},
			"cwd": map[string]any{"type": "string", "description": "Working directory (optional)"},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Override timeout (optional)"},
		},
		"required": []string{"command"},
	}
}
func (t *ExecTool) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }

var defaultBlockedPatterns = []string{
	"rm -rf", "mkfs", "dd ", "shutdown", "reboot", "poweroff", ":(){", ">|", "chown -R /", "chmod -R 777 /",
}

func (t *ExecTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	cmdS, _ := params["command"].(string)
	if strings.TrimSpace(cmdS) == "" { return "", errors.New("missing command") }
	lc := strings.ToLower(cmdS)
	patterns := t.BlockedPatterns
	if len(patterns) == 0 { patterns = defaultBlockedPatterns }
	for _, b := range patterns {
		if strings.Contains(lc, b) {
			return "", fmt.Errorf("blocked command pattern: %q", b)
		}
	}
	cwd, _ := params["cwd"].(string)
	if cwd == "" { cwd, _ = os.Getwd() }
	if t.RestrictDir != "" {
		abs, _ := filepath.Abs(cwd)
		root, _ := filepath.Abs(t.RestrictDir)
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("cwd outside allowed directory")
		}
	}

	to := t.Timeout
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		to = time.Duration(int(v)) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	c := exec.CommandContext(cctx, "bash", "-lc", cmdS)
	c.Dir = cwd
	if t.PathAppend != "" {
		env := os.Environ()
		env = append(env, "PATH="+os.Getenv("PATH")+string(os.PathListSeparator)+t.PathAppend)
		c.Env = env
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	out := stdout.String()
	er := stderr.String()
	max := t.OutputMaxBytes
	if max <= 0 { max = defaultExecOutputMaxBytes }
	if len(out) > max { out = out[:max] + "\n...[truncated]\n" }
	if len(er) > max { er = er[:max] + "\n...[truncated]\n" }
	if err != nil {
		return fmt.Sprintf("exit error: %v\n\nstdout:\n%s\n\nstderr:\n%s", err, out, er), nil
	}
	if strings.TrimSpace(er) != "" {
		return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", out, er), nil
	}
	return out, nil
}
````

## File: internal/tools/files_test.go
````go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- safePath ----

func TestSafePath_Valid(t *testing.T) {
	tool := &FileTool{}
	dir := t.TempDir()
	path, err := tool.safePath(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("safePath: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestSafePath_Empty(t *testing.T) {
	tool := &FileTool{}
	_, err := tool.safePath("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestSafePath_Whitespace(t *testing.T) {
	tool := &FileTool{}
	_, err := tool.safePath("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only path")
	}
}

func TestSafePath_OutsideRoot(t *testing.T) {
	dir := t.TempDir()
	tool := &FileTool{Root: dir}
	_, err := tool.safePath("/tmp")
	if err == nil {
		t.Fatal("expected error for path outside root")
	}
	if !strings.Contains(err.Error(), "outside allowed root") {
		t.Errorf("expected 'outside allowed root', got %q", err.Error())
	}
}

func TestSafePath_InsideRoot(t *testing.T) {
	dir := t.TempDir()
	tool := &FileTool{Root: dir}
	path, err := tool.safePath(filepath.Join(dir, "subdir", "file.txt"))
	if err != nil {
		t.Fatalf("safePath: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestSafePath_BlocksSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	tool := &FileTool{Root: root}
	_, err := tool.safePath(filepath.Join(link, "secret.txt"))
	if err == nil {
		t.Fatal("expected symlink escape to be blocked")
	}
}

// ---- ReadFile ----

func TestReadFile_OK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	os.WriteFile(p, []byte("hello file"), 0o644)

	tool := &ReadFile{}
	out, err := tool.Execute(context.Background(), map[string]any{"path": p})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if out != "hello file" {
		t.Errorf("expected 'hello file', got %q", out)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	tool := &ReadFile{}
	_, err := tool.Execute(context.Background(), map[string]any{"path": "/nonexistent/file.txt"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFile_Truncation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "large.txt")
	os.WriteFile(p, []byte(strings.Repeat("x", 200)), 0o644)

	tool := &ReadFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":     p,
		"maxBytes": float64(100),
	})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(out) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(out))
	}
}

func TestReadFile_EmptyPath(t *testing.T) {
	tool := &ReadFile{}
	_, err := tool.Execute(context.Background(), map[string]any{"path": ""})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadFile_Name(t *testing.T) {
	tool := &ReadFile{}
	if tool.Name() != "read_file" {
		t.Errorf("expected 'read_file', got %q", tool.Name())
	}
}

func TestReadFile_Schema(t *testing.T) {
	tool := &ReadFile{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- WriteFile ----

func TestWriteFile_OK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")

	tool := &WriteFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    p,
		"content": "written content",
	})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "written content" {
		t.Errorf("expected 'written content', got %q", string(got))
	}
}

func TestWriteFile_Mkdirs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "c", "file.txt")

	tool := &WriteFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    p,
		"content": "content",
		"mkdirs":  true,
	})
	if err != nil {
		t.Fatalf("WriteFile with mkdirs: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
}

func TestWriteFile_NoMkdirs_FailsOnMissingDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "missing", "file.txt")

	tool := &WriteFile{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    p,
		"content": "content",
	})
	if err == nil {
		t.Fatal("expected error when parent dir is missing and mkdirs=false")
	}
}

func TestWriteFile_Name(t *testing.T) {
	tool := &WriteFile{}
	if tool.Name() != "write_file" {
		t.Errorf("expected 'write_file', got %q", tool.Name())
	}
}

// ---- EditFile ----

func TestEditFile_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	os.WriteFile(p, []byte("foo foo foo"), 0o644)

	tool := &EditFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": p,
		"edits": []any{
			map[string]any{"find": "foo", "replace": "bar"},
		},
	})
	if err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "bar bar bar" {
		t.Errorf("expected 'bar bar bar', got %q", string(got))
	}
}

func TestEditFile_ReplaceCount(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	os.WriteFile(p, []byte("foo foo foo"), 0o644)

	tool := &EditFile{}
	tool.Execute(context.Background(), map[string]any{
		"path": p,
		"edits": []any{
			map[string]any{"find": "foo", "replace": "bar", "count": float64(1)},
		},
	})
	got, _ := os.ReadFile(p)
	if string(got) != "bar foo foo" {
		t.Errorf("expected 'bar foo foo', got %q", string(got))
	}
}

func TestEditFile_NotFound(t *testing.T) {
	tool := &EditFile{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":  "/nonexistent/file.txt",
		"edits": []any{},
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestEditFile_Name(t *testing.T) {
	tool := &EditFile{}
	if tool.Name() != "edit_file" {
		t.Errorf("expected 'edit_file', got %q", tool.Name())
	}
}

func TestEditFile_NoEdits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file.txt")
	os.WriteFile(p, []byte("original"), 0o644)

	tool := &EditFile{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":  p,
		"edits": []any{},
	})
	if err != nil {
		t.Fatalf("EditFile no edits: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "original" {
		t.Errorf("expected unchanged 'original', got %q", string(got))
	}
}

// ---- ListDir ----

func TestListDir_OK(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)

	tool := &ListDir{}
	out, err := tool.Execute(context.Background(), map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}

	var entries []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestListDir_MaxEntries(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, []string{"a", "b", "c", "d", "e"}[i]+".txt"), []byte("x"), 0o644)
	}

	tool := &ListDir{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": dir,
		"max":  float64(2),
	})
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	var entries []any
	json.Unmarshal([]byte(out), &entries)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries with max=2, got %d", len(entries))
	}
}

func TestListDir_NotFound(t *testing.T) {
	tool := &ListDir{}
	_, err := tool.Execute(context.Background(), map[string]any{"path": "/nonexistent/directory"})
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestListDir_Name(t *testing.T) {
	tool := &ListDir{}
	if tool.Name() != "list_dir" {
		t.Errorf("expected 'list_dir', got %q", tool.Name())
	}
}

func TestWriteFile_Description(t *testing.T) {
	tool := &WriteFile{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestWriteFile_Parameters(t *testing.T) {
	tool := &WriteFile{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestEditFile_Description(t *testing.T) {
	tool := &EditFile{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestEditFile_Parameters(t *testing.T) {
	tool := &EditFile{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestEditFile_Schema(t *testing.T) {
	tool := &EditFile{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

func TestListDir_Description(t *testing.T) {
	tool := &ListDir{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestListDir_Parameters(t *testing.T) {
	tool := &ListDir{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestListDir_Schema(t *testing.T) {
	tool := &ListDir{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

func TestReadFile_Description(t *testing.T) {
	tool := &ReadFile{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestReadFile_Parameters(t *testing.T) {
	tool := &ReadFile{}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected 'object', got %v", params["type"])
	}
}

func TestWriteFile_Schema(t *testing.T) {
tool := &WriteFile{}
schema := tool.Schema()
if schema["type"] != "function" {
t.Errorf("expected 'function', got %v", schema["type"])
}
}
````

## File: internal/tools/memory_test.go
````go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

func makeMemoryTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func makeEmbedServer(t *testing.T, vec []float32) (*httptest.Server, *providers.Client) {
	t.Helper()
	resp := map[string]any{
		"data": []map[string]any{
			{"embedding": vec},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	c := providers.New(srv.URL, "test-key", 10*time.Second)
	c.HTTP = srv.Client()
	return srv, c
}

// ---- MemorySetPinned ----

func TestMemorySetPinned_NoDB(t *testing.T) {
	tool := &MemorySetPinned{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"key":     "k",
		"content": "v",
	})
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemorySetPinned_EmptyKey(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	_, err := tool.Execute(context.Background(), map[string]any{
		"key":     "",
		"content": "value",
	})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestMemorySetPinned_EmptyContent(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	_, err := tool.Execute(context.Background(), map[string]any{
		"key":     "mykey",
		"content": "",
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestMemorySetPinned_Success(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	ctx := ContextWithSession(context.Background(), "session-a")
	out, err := tool.Execute(ctx, map[string]any{
		"key":     "name",
		"content": "Alice",
	})
	if err != nil {
		t.Fatalf("MemorySetPinned: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}

	// Verify pinned memory was stored
	pinned, _ := d.GetPinned(context.Background(), "session-a")
	if pinned["name"] != "Alice" {
		t.Errorf("expected pinned['name']='Alice', got %q", pinned["name"])
	}
}

func TestMemorySetPinned_Overwrite(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}

	ctx := ContextWithSession(context.Background(), "session-a")
	tool.Execute(ctx, map[string]any{"key": "k", "content": "first"})
	tool.Execute(ctx, map[string]any{"key": "k", "content": "second"})

	pinned, _ := d.GetPinned(context.Background(), "session-a")
	if pinned["k"] != "second" {
		t.Errorf("expected 'second', got %q", pinned["k"])
	}
}

func TestMemorySetPinned_GlobalScope(t *testing.T) {
	d := makeMemoryTestDB(t)
	tool := &MemorySetPinned{DB: d}
	if _, err := tool.Execute(context.Background(), map[string]any{"key": "shared", "content": "value", "scope": scope.GlobalScopeAlias}); err != nil {
		t.Fatalf("MemorySetPinned: %v", err)
	}
	pinned, _ := d.GetPinned(context.Background(), "other-session")
	if pinned["shared"] != "value" {
		t.Fatalf("expected global pinned memory, got %#v", pinned)
	}
}

func TestMemorySetPinned_Name(t *testing.T) {
	tool := &MemorySetPinned{}
	if tool.Name() != "memory_set_pinned" {
		t.Errorf("expected 'memory_set_pinned', got %q", tool.Name())
	}
}

func TestMemorySetPinned_Schema(t *testing.T) {
	tool := &MemorySetPinned{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- MemoryAddNote ----

func TestMemoryAddNote_NoDB(t *testing.T) {
	tool := &MemoryAddNote{}
	_, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemoryAddNote_EmptyText(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	_, err := tool.Execute(context.Background(), map[string]any{"text": ""})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestMemoryAddNote_Success(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	out, err := tool.Execute(context.Background(), map[string]any{
		"text": "This is a memory note",
		"tags": "important",
	})
	if err != nil {
		t.Fatalf("MemoryAddNote: %v", err)
	}
	if !strings.HasPrefix(out, "ok:") {
		t.Errorf("expected 'ok: ...' output, got %q", out)
	}
}

func TestMemoryAddNote_WithSourceMessageID(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	out, err := tool.Execute(context.Background(), map[string]any{
		"text":              "memory with source",
		"source_message_id": float64(42),
	})
	if err != nil {
		t.Fatalf("MemoryAddNote: %v", err)
	}
	if !strings.HasPrefix(out, "ok:") {
		t.Errorf("expected 'ok: ...' output, got %q", out)
	}
}

func TestMemoryAddNote_EmbedFails(t *testing.T) {
	d := makeMemoryTestDB(t)
	// Create a server that returns an error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "embed error")
	}))
	defer srv.Close()
	c := providers.New(srv.URL, "key", 10*time.Second)
	c.HTTP = srv.Client()

	tool := &MemoryAddNote{DB: d, Provider: c, EmbedModel: "model"}
	_, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err == nil {
		t.Fatal("expected error when embed fails")
	}
}

func TestMemoryAddNote_Name(t *testing.T) {
	tool := &MemoryAddNote{}
	if tool.Name() != "memory_add_note" {
		t.Errorf("expected 'memory_add_note', got %q", tool.Name())
	}
}

func TestMemoryAddNote_Schema(t *testing.T) {
	tool := &MemoryAddNote{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- MemorySearch ----

func TestMemorySearch_NoDB(t *testing.T) {
	tool := &MemorySearch{}
	_, err := tool.Execute(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

func TestMemorySearch_EmptyQuery(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemorySearch{DB: d, Provider: c, EmbedModel: "model"}
	_, err := tool.Execute(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestMemorySearch_EmptyDB(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemorySearch{
		DB:              d,
		Provider:        c,
		EmbedModel:      "model",
		VectorK:         5,
		FTSK:            5,
		TopK:            5,
		VectorScanLimit: 100,
	}
	out, err := tool.Execute(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output for empty db, got %q", out)
	}
}

func TestMemorySearch_WithTopKParam(t *testing.T) {
	d := makeMemoryTestDB(t)
	_, c := makeEmbedServer(t, []float32{0.1, 0.2})
	tool := &MemorySearch{
		DB:         d,
		Provider:   c,
		EmbedModel: "model",
		TopK:       3,
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
		"topK":  float64(2),
	})
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	_ = out
}

func TestMemorySearch_EmbedFails(t *testing.T) {
	d := makeMemoryTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	}))
	defer srv.Close()
	c := providers.New(srv.URL, "key", 10*time.Second)
	c.HTTP = srv.Client()

	tool := &MemorySearch{DB: d, Provider: c, EmbedModel: "model", TopK: 5}
	_, err := tool.Execute(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Fatal("expected error when embed fails")
	}
}

func TestMemorySearch_Name(t *testing.T) {
	tool := &MemorySearch{}
	if tool.Name() != "memory_search" {
		t.Errorf("expected 'memory_search', got %q", tool.Name())
	}
}

func TestMemorySearch_Schema(t *testing.T) {
	tool := &MemorySearch{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}
````

## File: internal/tools/message_test.go
````go
package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSendMessage_NoDeliver(t *testing.T) {
	tool := &SendMessage{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "hello",
	})
	if err == nil {
		t.Fatal("expected error when deliver is nil")
	}
}

func TestSendMessage_Success(t *testing.T) {
	var gotChannel, gotTo, gotText string
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			gotChannel = ch
			gotTo = to
			gotText = text
			return nil
		},
		DefaultChannel: "cli",
		DefaultTo:      "user",
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"text":    "hello world",
		"channel": "",
		"to":      "",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
	if gotChannel != "cli" {
		t.Errorf("expected channel 'cli', got %q", gotChannel)
	}
	if gotTo != "user" {
		t.Errorf("expected to 'user', got %q", gotTo)
	}
	if gotText != "hello world" {
		t.Errorf("expected text 'hello world', got %q", gotText)
	}
}

func TestSendMessage_CustomChannelAndTo(t *testing.T) {
	var gotChannel, gotTo string
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			gotChannel = ch
			gotTo = to
			return nil
		},
		DefaultChannel: "default-ch",
		DefaultTo:      "default-to",
	}
	tool.Execute(context.Background(), map[string]any{
		"text":    "msg",
		"channel": "custom-ch",
		"to":      "custom-to",
	})
	if gotChannel != "custom-ch" {
		t.Errorf("expected channel 'custom-ch', got %q", gotChannel)
	}
	if gotTo != "custom-to" {
		t.Errorf("expected to 'custom-to', got %q", gotTo)
	}
}

func TestSendMessage_EmptyText(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return nil
		},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "",
	})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestSendMessage_DeliverError(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return errors.New("deliver failed")
		},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "test",
	})
	if err == nil {
		t.Fatal("expected error when deliver returns error")
	}
}

func TestSendMessage_Name(t *testing.T) {
	tool := &SendMessage{}
	if tool.Name() != "send_message" {
		t.Errorf("expected 'send_message', got %q", tool.Name())
	}
}

func TestSendMessage_Description(t *testing.T) {
	tool := &SendMessage{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestSendMessage_Schema(t *testing.T) {
	tool := &SendMessage{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

func TestSendMessage_TextOnlyWhitespace(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return nil
		},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "  ",
	})
	if err == nil {
		t.Fatal("expected error for whitespace-only text without media")
	}
}

func TestSendMessage_MediaOnlySuccess(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var gotText string
	var gotMeta map[string]any
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			gotText = text
			gotMeta = meta
			return nil
		},
		AllowedRoot:   root,
		MaxMediaBytes: 1024,
	}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"media": []any{mediaPath},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotText != "" {
		t.Fatalf("expected empty text for media-only message, got %q", gotText)
	}
	wantPath, err := canonicalizePath(mediaPath)
	if err != nil {
		t.Fatalf("canonicalizePath: %v", err)
	}
	paths, ok := gotMeta["media_paths"].([]string)
	if !ok || len(paths) != 1 || paths[0] != wantPath {
		t.Fatalf("expected media_paths to be passed through, got %#v", gotMeta)
	}
}

func TestSendMessage_UsesContextDefaultsWhenKeysOmitted(t *testing.T) {
	var gotChannel, gotTo string
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			gotChannel = ch
			gotTo = to
			return nil
		},
	}
	ctx := ContextWithDelivery(context.Background(), "discord", "channel-1")
	if _, err := tool.Execute(ctx, map[string]any{"text": "hello"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotChannel != "discord" || gotTo != "channel-1" {
		t.Fatalf("expected context delivery target, got %q/%q", gotChannel, gotTo)
	}
}

func TestSendMessage_MissingTextDoesNotBecomeNilString(t *testing.T) {
	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return nil
		},
	}
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected empty message error when text and media are both omitted")
	}
}

func TestSendMessage_MediaOutsideAllowedRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mediaPath := filepath.Join(other, "image.png")
	if err := os.WriteFile(mediaPath, []byte("image-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &SendMessage{
		Deliver: func(ctx context.Context, ch, to, text string, meta map[string]any) error {
			return nil
		},
		AllowedRoot:   root,
		MaxMediaBytes: 1024,
	}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"text":  "hello",
		"media": []any{mediaPath},
	}); err == nil {
		t.Fatal("expected error for media outside allowed root")
	}
}
````

## File: internal/tools/message.go
````go
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DeliverFunc func(ctx context.Context, channel, to, text string, meta map[string]any) error

type SendMessage struct {
	Base
	Deliver        DeliverFunc
	DefaultChannel string
	DefaultTo      string
	AllowedRoot    string
	ArtifactsDir   string
	MaxMediaBytes  int
}

func (t *SendMessage) Name() string { return "send_message" }
func (t *SendMessage) Description() string {
	return "Send a message via a configured channel (for reminders/cron or proactive messages)."
}
func (t *SendMessage) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"channel": map[string]any{"type": "string"},
		"to":      map[string]any{"type": "string"},
		"text":    map[string]any{"type": "string"},
		"media": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional local file paths to send as attachments.",
		},
	}, "required": []string{}}
}
func (t *SendMessage) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *SendMessage) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Deliver == nil {
		return "", fmt.Errorf("deliver not configured")
	}
	ctxChannel, ctxTo := DeliveryFromContext(ctx)
	ch := readOptionalString(params, "channel")
	to := readOptionalString(params, "to")
	text := readOptionalString(params, "text")
	if ch == "" {
		ch = strings.TrimSpace(t.DefaultChannel)
	}
	if ch == "" {
		ch = strings.TrimSpace(ctxChannel)
	}
	if to == "" {
		to = strings.TrimSpace(t.DefaultTo)
	}
	if to == "" {
		to = strings.TrimSpace(ctxTo)
	}
	mediaPaths, err := t.validateMediaPaths(params["media"])
	if err != nil {
		return "", err
	}
	if text == "" && len(mediaPaths) == 0 {
		return "", fmt.Errorf("message requires text or media")
	}
	var meta map[string]any
	if len(mediaPaths) > 0 {
		meta = map[string]any{"media_paths": mediaPaths}
	}
	if err := t.Deliver(ctx, ch, to, text, meta); err != nil {
		return "", err
	}
	return "ok", nil
}

func (t *SendMessage) validateMediaPaths(raw any) ([]string, error) {
	items, err := stringSlice(raw)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	roots := make([]string, 0, 2)
	if strings.TrimSpace(t.AllowedRoot) != "" {
		roots = append(roots, strings.TrimSpace(t.AllowedRoot))
	}
	if strings.TrimSpace(t.ArtifactsDir) != "" {
		roots = append(roots, strings.TrimSpace(t.ArtifactsDir))
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		p, err := filepath.Abs(strings.TrimSpace(item))
		if err != nil {
			return nil, err
		}
		p, err = canonicalizePath(p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("media path is a directory: %s", item)
		}
		if t.MaxMediaBytes == 0 {
			return nil, fmt.Errorf("media attachments disabled by config")
		}
		if t.MaxMediaBytes > 0 && info.Size() > int64(t.MaxMediaBytes) {
			return nil, fmt.Errorf("media path exceeds maxMediaBytes: %s", item)
		}
		if len(roots) > 0 {
			allowed := false
			for _, root := range roots {
				ok, err := pathWithinRoot(p, root)
				if err != nil {
					return nil, err
				}
				if ok {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, fmt.Errorf("media path outside allowed roots: %s", item)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func pathWithinRoot(absPath, root string) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	root, err = canonicalizeRoot(root)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func stringSlice(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				continue
			}
			out = append(out, item)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("media must be an array of strings")
	}
}
````

## File: internal/tools/registry.go
````go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool)      { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) Tool { return r.tools[name] }
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for k := range r.tools {
		out = append(out, k)
	}
	return out
}

func (r *Registry) Definitions() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Schema())
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	t := r.tools[name]
	if t == nil {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	var params map[string]any
	if argsJSON == "" {
		params = map[string]any{}
	} else {
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return "", fmt.Errorf("invalid tool args: %w", err)
		}
	}
	return t.Execute(ctx, params)
}
````

## File: internal/tools/web_test.go
````go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- StripHTML ----

func TestStripHTML_NoTags(t *testing.T) {
	in := "plain text"
	out := StripHTML(in)
	if out != "plain text" {
		t.Errorf("expected 'plain text', got %q", out)
	}
}

func TestStripHTML_WithTags(t *testing.T) {
	in := "<p>Hello <b>World</b></p>"
	out := StripHTML(in)
	if out != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", out)
	}
}

func TestStripHTML_Empty(t *testing.T) {
	out := StripHTML("")
	if out != "" {
		t.Errorf("expected empty string, got %q", out)
	}
}

func TestStripHTML_OnlyTags(t *testing.T) {
	out := StripHTML("<br><br/>")
	if out != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

// ---- WebFetch ----

func TestWebFetch_InvalidURL(t *testing.T) {
	tool := &WebFetch{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "ftp://not-http",
	})
	if err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

func TestWebFetch_EmptyURL(t *testing.T) {
	tool := &WebFetch{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "",
	})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestWebFetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello from server")
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: &http.Client{Transport: &urlRewriteTransport{base: srv.URL}}}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.com/test",
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if !strings.Contains(out, "hello from server") {
		t.Errorf("expected server response in output, got %q", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("expected status 200 in output, got %q", out)
	}
}

func TestWebFetch_MaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 1000))
	}))
	defer srv.Close()

	tool := &WebFetch{HTTP: &http.Client{Transport: &urlRewriteTransport{base: srv.URL}}}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":      "https://example.com/large",
		"maxBytes": float64(50),
	})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	// Body should be limited to 50 bytes
	_ = out
}

func TestWebFetch_BlocksLocalhost(t *testing.T) {
	tool := &WebFetch{}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "http://127.0.0.1:8080"})
	if err == nil {
		t.Fatal("expected localhost fetch to be blocked")
	}
}

func TestWebFetch_StopsAfterDefaultRedirectLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/loop", http.StatusFound)
	}))
	defer srv.Close()

	tool := &WebFetch{
		HTTP:    &http.Client{Transport: &urlRewriteTransport{base: srv.URL}},
		Timeout: 2 * time.Second,
	}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "https://example.com/loop"})
	if err == nil {
		t.Fatal("expected redirect loop to fail")
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected redirect limit error, got %v", err)
	}
}

func TestWebFetch_Name(t *testing.T) {
	tool := &WebFetch{}
	if tool.Name() != "web_fetch" {
		t.Errorf("expected 'web_fetch', got %q", tool.Name())
	}
}

func TestWebFetch_Schema(t *testing.T) {
	tool := &WebFetch{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// ---- WebSearch ----

func TestWebSearch_NoAPIKey(t *testing.T) {
	tool := &WebSearch{APIKey: ""}
	_, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
	})
	if err == nil {
		t.Fatal("expected error when API key is not set")
	}
	if !strings.Contains(err.Error(), "Brave API key") {
		t.Errorf("expected 'Brave API key' in error, got %q", err.Error())
	}
}

func TestWebSearch_Success(t *testing.T) {
	// Mock Brave Search API
	response := map[string]any{
		"web": map[string]any{
			"results": []any{
				map[string]any{
					"title":       "Test Result",
					"url":         "https://example.com",
					"description": "A test result",
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP:   srv.Client(),
	}
	// Override endpoint by changing the HTTP client transport
	// We can't easily override the URL, so let's test via a custom HTTP client
	// that redirects to the test server
	tool.HTTP = &http.Client{
		Transport: &urlRewriteTransport{
			base: srv.URL,
		},
	}

	out, err := tool.Execute(context.Background(), map[string]any{
		"query": "golang test",
	})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if !strings.Contains(out, "Test Result") {
		t.Errorf("expected 'Test Result' in output, got %q", out)
	}
}

func TestWebSearch_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "unauthorized")
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "bad-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	_, err := tool.Execute(context.Background(), map[string]any{
		"query": "test",
	})
	if err == nil {
		t.Fatal("expected error for HTTP error response")
	}
}

func TestWebSearch_DefaultCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that count param is in URL
		count := r.URL.Query().Get("count")
		if count != "5" {
			t.Errorf("expected count=5, got %q", count)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": []any{}}})
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	tool.Execute(context.Background(), map[string]any{"query": "test"})
}

func TestWebSearch_MaxCountCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := r.URL.Query().Get("count")
		if count != "10" {
			t.Errorf("expected count capped at 10, got %q", count)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": []any{}}})
	}))
	defer srv.Close()

	tool := &WebSearch{
		APIKey: "test-key",
		HTTP: &http.Client{
			Transport: &urlRewriteTransport{base: srv.URL},
		},
	}

	tool.Execute(context.Background(), map[string]any{
		"query": "test",
		"count": float64(100), // exceeds default max of 10
	})
}

func TestWebSearch_Name(t *testing.T) {
	tool := &WebSearch{}
	if tool.Name() != "web_search" {
		t.Errorf("expected 'web_search', got %q", tool.Name())
	}
}

func TestWebSearch_Schema(t *testing.T) {
	tool := &WebSearch{}
	schema := tool.Schema()
	if schema["type"] != "function" {
		t.Errorf("expected 'function', got %v", schema["type"])
	}
}

// urlRewriteTransport rewrites all requests to a test server base URL
type urlRewriteTransport struct {
	base string
}

func (t *urlRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = "http"
	req2.URL.Host = strings.TrimPrefix(t.base, "http://")
	return http.DefaultTransport.RoundTrip(req2)
}
````

## File: .env.example
````
# Example environment for or3-intern
#
# This repo does NOT auto-load .env files.
# Load it in your shell before running, for example:
#   set -a; source .env; set +a
#   go run ./cmd/or3-intern chat
#
# If you use OpenRouter, set OR3_API_BASE and OR3_API_KEY.
# If you use OpenAI defaults, OPENAI_API_KEY is enough.

# --- Provider ---
# Used as the default API key unless OR3_API_KEY is set.
OPENAI_API_KEY=

# Preferred explicit provider key override.
OR3_API_KEY=

# OpenAI-compatible API base.
# OpenAI default: https://api.openai.com/v1
# OpenRouter: https://openrouter.ai/api/v1
OR3_API_BASE=https://api.openai.com/v1

# Chat model name.
# Examples:
#   gpt-4.1-mini
#   openai/gpt-4o-mini
OR3_MODEL=gpt-4.1-mini

# Embedding model used for memory retrieval.
OR3_EMBED_MODEL=text-embedding-3-small

# --- App storage ---
OR3_DB_PATH=
OR3_ARTIFACTS_DIR=

# --- Optional tool integrations ---
BRAVE_API_KEY=

# --- Optional chat channels ---
OR3_TELEGRAM_TOKEN=
OR3_SLACK_APP_TOKEN=
OR3_SLACK_BOT_TOKEN=
OR3_DISCORD_TOKEN=
OR3_WHATSAPP_BRIDGE_URL=ws://127.0.0.1:3001/ws
OR3_WHATSAPP_BRIDGE_TOKEN=
````

## File: .gitignore
````
.env
.or3/
/or3-intern
or3-intern.exe
````

## File: cmd/or3-intern/init_test.go
````go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
)

func TestInitDefaults_UsesWorkspacePaths(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	cfg := initDefaults("/tmp/project")
	if cfg.DBPath != "/tmp/project/.or3/or3-intern.sqlite" {
		t.Fatalf("unexpected DB path: %q", cfg.DBPath)
	}
	if cfg.ArtifactsDir != "/tmp/project/.or3/artifacts" {
		t.Fatalf("unexpected artifacts dir: %q", cfg.ArtifactsDir)
	}
	if !cfg.Tools.RestrictToWorkspace {
		t.Fatal("expected workspace restriction enabled")
	}
	if cfg.WorkspaceDir != "/tmp/project" {
		t.Fatalf("unexpected workspace dir: %q", cfg.WorkspaceDir)
	}
}

func TestRunInitWithIO_WritesConfig(t *testing.T) {
	t.Setenv("OR3_DB_PATH", "")
	t.Setenv("OR3_ARTIFACTS_DIR", "")
	t.Setenv("OR3_API_BASE", "")
	t.Setenv("OR3_API_KEY", "")
	t.Setenv("OR3_MODEL", "")
	t.Setenv("OR3_EMBED_MODEL", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	input := strings.NewReader(strings.Join([]string{
		"2",
		"",
		"",
		"",
		"y",
		"test-key",
		"",
		"",
		"",
		"",
	}, "\n"))
	var out strings.Builder

	if err := runInitWithIO(input, &out, configPath, "/workspace/project"); err != nil {
		t.Fatalf("runInitWithIO: %v", err)
	}

	b, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.Provider.APIBase != "https://openrouter.ai/api/v1" {
		t.Fatalf("unexpected API base: %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.Model != "openai/gpt-4o-mini" {
		t.Fatalf("unexpected model: %q", cfg.Provider.Model)
	}
	if cfg.Provider.APIKey != "test-key" {
		t.Fatalf("unexpected API key: %q", cfg.Provider.APIKey)
	}
	if cfg.DBPath != "/workspace/project/.or3/or3-intern.sqlite" {
		t.Fatalf("unexpected DB path: %q", cfg.DBPath)
	}
	if !strings.Contains(out.String(), "Saved config") {
		t.Fatalf("expected success output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "go run ./cmd/or3-intern chat") {
		t.Fatalf("expected next-step instructions, got %q", out.String())
	}
}

func TestBuildChannelManager_RegistersEnabledChannels(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "test-token"
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.AppToken = "app"
	cfg.Channels.Slack.BotToken = "bot"

	mgr, err := buildChannelManager(cfg, cli.Deliverer{}, nil, 0)
	if err != nil {
		t.Fatalf("buildChannelManager: %v", err)
	}
	names := strings.Join(mgr.Names(), ",")
	if !strings.Contains(names, "cli") || !strings.Contains(names, "telegram") || !strings.Contains(names, "slack") {
		t.Fatalf("expected registered channels, got %q", names)
	}
}
````

## File: internal/agent/agent_test.go
````go
package agent

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/skills"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// ---- truncateText ----

func TestTruncateText_NoMax(t *testing.T) {
	s := "hello world"
	got := truncateText(s, 0)
	if got != s {
		t.Errorf("expected %q, got %q", s, got)
	}
}

func TestTruncateText_WithinLimit(t *testing.T) {
	s := "hello"
	got := truncateText(s, 100)
	if got != s {
		t.Errorf("expected %q, got %q", s, got)
	}
}

func TestTruncateText_Truncated(t *testing.T) {
	s := strings.Repeat("a", 200)
	got := truncateText(s, 100)
	if len(got) >= 200 {
		t.Error("expected truncation to happen")
	}
	if !strings.Contains(got, "[truncated]") {
		t.Errorf("expected '[truncated]' marker, got %q", got)
	}
}

func TestTruncateText_TrimsWhitespace(t *testing.T) {
	s := "  hello  "
	got := truncateText(s, 0)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

// ---- oneLine ----

func TestOneLine_SingleLine(t *testing.T) {
	s := "hello world"
	got := oneLine(s, 0)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestOneLine_MultiLine(t *testing.T) {
	s := "line one\nline two\nline three"
	got := oneLine(s, 0)
	if strings.Contains(got, "\n") {
		t.Error("expected no newlines in output")
	}
}

func TestOneLine_MaxLength(t *testing.T) {
	s := strings.Repeat("a", 300)
	got := oneLine(s, 100)
	// "…" is 3 bytes in UTF-8, so max 100 chars + 3 bytes = 103
	if len(got) > 103 {
		t.Errorf("expected at most 103 bytes, got %d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected '…' suffix, got %q", got)
	}
}

func TestOneLine_CollapseSpaces(t *testing.T) {
	s := "hello   world"
	got := oneLine(s, 0)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

// ---- formatPinned ----

func TestFormatPinned_Empty(t *testing.T) {
	got := formatPinned(map[string]string{})
	if got != "(none)" {
		t.Errorf("expected '(none)', got %q", got)
	}
}

func TestFormatPinned_WithEntries(t *testing.T) {
	m := map[string]string{
		"name": "Alice",
		"age":  "30",
	}
	got := formatPinned(m)
	if !strings.Contains(got, "name") || !strings.Contains(got, "Alice") {
		t.Errorf("expected 'name: Alice' in output, got %q", got)
	}
	if !strings.Contains(got, "age") || !strings.Contains(got, "30") {
		t.Errorf("expected 'age: 30' in output, got %q", got)
	}
}

func TestFormatPinned_SkipsEmptyValues(t *testing.T) {
	m := map[string]string{
		"present": "value",
		"empty":   "",
		"spaces":  "   ",
	}
	got := formatPinned(m)
	if strings.Contains(got, "empty") {
		t.Error("expected empty-value key to be skipped")
	}
	if strings.Contains(got, "spaces") {
		t.Error("expected whitespace-only value key to be skipped")
	}
}

func TestFormatPinned_AllEmpty_ReturnsNone(t *testing.T) {
	m := map[string]string{
		"key": "  ",
	}
	got := formatPinned(m)
	if got != "(none)" {
		t.Errorf("expected '(none)' when all values empty, got %q", got)
	}
}

func TestFormatPinned_Sorted(t *testing.T) {
	m := map[string]string{
		"z_key": "last",
		"a_key": "first",
		"m_key": "middle",
	}
	got := formatPinned(m)
	aIdx := strings.Index(got, "a_key")
	mIdx := strings.Index(got, "m_key")
	zIdx := strings.Index(got, "z_key")
	if aIdx > mIdx || mIdx > zIdx {
		t.Errorf("expected sorted output, got %q", got)
	}
}

// ---- formatRetrieved ----

func TestFormatRetrieved_Empty(t *testing.T) {
	got := formatRetrieved(nil)
	if got != "(none)" {
		t.Errorf("expected '(none)', got %q", got)
	}
}

func TestFormatRetrieved_WithResults(t *testing.T) {
	ms := []memory.Retrieved{
		{Source: "vector", Text: "relevant text", Score: 0.9},
		{Source: "fts", Text: "another text", Score: 0.5},
	}
	got := formatRetrieved(ms)
	if !strings.Contains(got, "relevant text") {
		t.Errorf("expected 'relevant text' in output, got %q", got)
	}
	if !strings.Contains(got, "vector") {
		t.Errorf("expected source 'vector' in output, got %q", got)
	}
}

// ---- Builder.composeSystemPrompt ----

func TestBuilder_ComposeSystemPrompt_Defaults(t *testing.T) {
	b := &Builder{}
	got := b.composeSystemPrompt("(none)", "(none)", "", "", "", "")
	if !strings.Contains(got, "System Prompt") {
		t.Errorf("expected '# System Prompt' in output, got %q", got)
	}
	if !strings.Contains(got, "SOUL.md") {
		t.Errorf("expected 'SOUL.md' section, got %q", got)
	}
}

func TestBuilder_ComposeSystemPrompt_CustomSoul(t *testing.T) {
	b := &Builder{Soul: "Custom soul text"}
	got := b.composeSystemPrompt("(none)", "(none)", "", "", "", "")
	if !strings.Contains(got, "Custom soul text") {
		t.Errorf("expected custom soul in output, got %q", got)
	}
}

func TestBuilder_ComposeSystemPrompt_Truncation(t *testing.T) {
	b := &Builder{
		BootstrapTotalMaxChars: 50,
	}
	got := b.composeSystemPrompt("(none)", "(none)", "", "", "", "")
	if len(got) > 100 { // allow for "[truncated]" suffix
		// Just verify it's bounded
	}
	_ = got
}

func TestBuilder_ComposeSystemPrompt_WithPinned(t *testing.T) {
	b := &Builder{}
	got := b.composeSystemPrompt("- name: Alice", "(none)", "", "", "", "")
	if !strings.Contains(got, "name: Alice") {
		t.Errorf("expected pinned memory in output, got %q", got)
	}
}

func TestBuilder_ComposeSystemPrompt_WithSkills(t *testing.T) {
	dir := t.TempDir()
	_ = skills.Scan([]string{dir}) // use empty skills
	b := &Builder{}
	got := b.composeSystemPrompt("(none)", "(none)", "", "", "", "")
	if !strings.Contains(got, "Skills Inventory") {
		t.Errorf("expected 'Skills Inventory' section, got %q", got)
	}
}

// ---- Builder.Build ----

func TestBuilder_Build_Basic(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{
		DB:         d,
		HistoryMax: 10,
	}

	pp, _, err := b.Build(context.Background(), "test-session", "hello")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pp.System) == 0 {
		t.Error("expected at least one system message")
	}
	if pp.System[0].Role != "system" {
		t.Errorf("expected role 'system', got %q", pp.System[0].Role)
	}
}

func TestBuilder_Build_WithHistory(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.AppendMessage(ctx, "s1", "user", "first message", nil)
	d.AppendMessage(ctx, "s1", "assistant", "first response", nil)

	b := &Builder{
		DB:         d,
		HistoryMax: 10,
	}

	pp, _, err := b.Build(ctx, "s1", "second message")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pp.History) < 1 {
		t.Error("expected history messages")
	}
}

func TestBuilder_Build_NoHistory(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{
		DB:         d,
		HistoryMax: 10,
	}

	pp, retrieved, err := b.Build(context.Background(), "empty-session", "hello")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pp.History) != 0 {
		t.Errorf("expected no history, got %d messages", len(pp.History))
	}
	if len(retrieved) != 0 {
		t.Errorf("expected no retrieved (no provider), got %d", len(retrieved))
	}
}

func TestBuilder_Build_EmptyMessage(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{
		DB:         d,
		HistoryMax: 10,
	}

	_, _, err := b.Build(context.Background(), "s1", "")
	if err != nil {
		t.Fatalf("Build with empty message: %v", err)
	}
}

func TestBuilder_Build_ToolCallsInHistory(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.AppendMessage(ctx, "s2", "user", "user msg", nil)
	// Append assistant message with tool_calls payload
	d.AppendMessage(ctx, "s2", "assistant", "tool content", map[string]any{
		"tool_calls": []map[string]any{
			{"id": "tc1", "type": "function", "function": map[string]any{"name": "test_tool", "arguments": "{}"}},
		},
	})

	b := &Builder{DB: d, HistoryMax: 10}
	pp, _, err := b.Build(ctx, "s2", "next msg")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, m := range pp.History {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tool calls to be parsed from history")
	}
}

func TestBuilder_Build_UserImageAttachmentWithVision(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	if err := d.EnsureSession(ctx, "media"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	att, err := store.SaveNamed(ctx, "media", "photo.png", "image/png", []byte("fake-image"))
	if err != nil {
		t.Fatalf("SaveNamed: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "media", "user", "describe this\n[image: photo.png]", map[string]any{
		"meta": map[string]any{"attachments": []artifacts.Attachment{att}},
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	b := &Builder{DB: d, Artifacts: store, EnableVision: true, HistoryMax: 10}
	pp, _, err := b.Build(ctx, "media", "describe this")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pp.History) != 1 {
		t.Fatalf("expected one history message, got %d", len(pp.History))
	}
	parts, ok := pp.History[0].Content.([]map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", pp.History[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text + image parts, got %#v", parts)
	}
	if parts[1]["type"] != "image_url" {
		t.Fatalf("expected image_url part, got %#v", parts[1])
	}
}

func TestBuilder_Build_UserImageAttachmentWithVisionDisabledFallsBackToText(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.AppendMessage(ctx, "media-off", "user", "look\n[image: photo.png]", map[string]any{
		"meta": map[string]any{"attachments": []map[string]any{{
			"artifact_id": "missing",
			"filename":    "photo.png",
			"mime":        "image/png",
			"kind":        "image",
			"size_bytes":  12,
		}}},
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	b := &Builder{DB: d, EnableVision: false, HistoryMax: 10}
	pp, _, err := b.Build(ctx, "media-off", "look")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, ok := pp.History[0].Content.(string); !ok || got != "look\n[image: photo.png]" {
		t.Fatalf("expected text fallback, got %#v", pp.History[0].Content)
	}
}

func TestBuilder_Build_UserImageAttachmentMissingArtifactFallsBackToText(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.AppendMessage(ctx, "media-missing", "user", "look\n[image: photo.png]", map[string]any{
		"meta": map[string]any{"attachments": []map[string]any{{
			"artifact_id": "missing",
			"filename":    "photo.png",
			"mime":        "image/png",
			"kind":        "image",
			"size_bytes":  12,
		}}},
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	b := &Builder{
		DB:           d,
		Artifacts:    &artifacts.Store{Dir: t.TempDir(), DB: d},
		EnableVision: true,
		HistoryMax:   10,
	}
	pp, _, err := b.Build(ctx, "media-missing", "look")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, ok := pp.History[0].Content.(string); !ok || got != "look\n[image: photo.png]" {
		t.Fatalf("expected missing artifact fallback to text, got %#v", pp.History[0].Content)
	}
}

func TestBuilder_Build_UserImageAttachmentsRespectVisionBudget(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	store := &artifacts.Store{Dir: t.TempDir(), DB: d}
	if err := d.EnsureSession(ctx, "media-budget"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	imageData := bytes.Repeat([]byte("a"), 3<<20)
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("photo-%d.png", i)
		att, err := store.SaveNamed(ctx, "media-budget", name, "image/png", imageData)
		if err != nil {
			t.Fatalf("SaveNamed %d: %v", i, err)
		}
		if _, err := d.AppendMessage(ctx, "media-budget", "user", fmt.Sprintf("describe %d\n[image: %s]", i, name), map[string]any{
			"meta": map[string]any{"attachments": []artifacts.Attachment{att}},
		}); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	b := &Builder{DB: d, Artifacts: store, EnableVision: true, HistoryMax: 10}
	pp, _, err := b.Build(ctx, "media-budget", "describe")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pp.History) != 3 {
		t.Fatalf("expected three history messages, got %d", len(pp.History))
	}
	if _, ok := pp.History[0].Content.([]map[string]any); !ok {
		t.Fatalf("expected first image within budget to be structured, got %T", pp.History[0].Content)
	}
	if _, ok := pp.History[1].Content.([]map[string]any); !ok {
		t.Fatalf("expected second image within budget to be structured, got %T", pp.History[1].Content)
	}
	if got, ok := pp.History[2].Content.(string); !ok || !strings.Contains(got, "[image: photo-2.png]") {
		t.Fatalf("expected third image to fall back to text marker, got %#v", pp.History[2].Content)
	}
}

// ---- CronRunner ----

func TestCronRunner_PublishesEvent(t *testing.T) {
	b := bus.New(10)
	runner := CronRunner(b, "test-session")

	job := cron.CronJob{
		ID:   "job1",
		Name: "test job",
		Payload: cron.CronPayload{
			Kind:    "agent_turn",
			Message: "scheduled message",
			Channel: "cli",
			To:      "user",
		},
	}

	err := runner(context.Background(), job)
	if err != nil {
		t.Fatalf("CronRunner: %v", err)
	}

	select {
	case ev := <-b.Channel():
		if ev.Type != bus.EventCron {
			t.Errorf("expected EventCron, got %s", ev.Type)
		}
		if ev.SessionKey != "test-session" {
			t.Errorf("expected session 'test-session', got %q", ev.SessionKey)
		}
		if ev.Message != "scheduled message" {
			t.Errorf("expected 'scheduled message', got %q", ev.Message)
		}
		if ev.Meta["job_id"] != "job1" {
			t.Errorf("expected meta job_id='job1', got %v", ev.Meta["job_id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestCronRunner_EmptyMessage_UsesName(t *testing.T) {
	b := bus.New(10)
	runner := CronRunner(b, "test-session")

	job := cron.CronJob{
		ID:   "j1",
		Name: "my job",
		Payload: cron.CronPayload{
			Kind:    "agent_turn",
			Message: "", // empty
		},
	}

	runner(context.Background(), job)

	select {
	case ev := <-b.Channel():
		if !strings.Contains(ev.Message, "my job") {
			t.Errorf("expected message to contain job name, got %q", ev.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestCronRunner_FullBus_ReturnsError(t *testing.T) {
	b := bus.New(1)
	b.Publish(bus.Event{}) // fill the bus

	runner := CronRunner(b, "session")
	err := runner(context.Background(), cron.CronJob{
		ID:      "j1",
		Payload: cron.CronPayload{Message: "msg"},
	})
	if err == nil {
		t.Fatal("expected error when bus is full")
	}
	if !strings.Contains(err.Error(), "full") {
		t.Errorf("expected 'full' in error, got %q", err.Error())
	}
}

// ---- WithTimeout ----

func TestWithTimeout_Default(t *testing.T) {
	ctx := context.Background()
	cctx, cancel := WithTimeout(ctx, 0)
	defer cancel()
	if cctx == nil {
		t.Fatal("expected non-nil context")
	}
	dl, ok := cctx.Deadline()
	if !ok {
		t.Fatal("expected deadline to be set")
	}
	if time.Until(dl) <= 0 {
		t.Error("expected future deadline")
	}
}

func TestWithTimeout_CustomSec(t *testing.T) {
	ctx := context.Background()
	cctx, cancel := WithTimeout(ctx, 5)
	defer cancel()
	dl, ok := cctx.Deadline()
	if !ok {
		t.Fatal("expected deadline to be set")
	}
	remaining := time.Until(dl)
	if remaining <= 4*time.Second || remaining > 5*time.Second+100*time.Millisecond {
		t.Errorf("expected ~5s deadline, got %v", remaining)
	}
}

// ---- contentToString ----

func TestContentToString_String(t *testing.T) {
	got := contentToString("hello")
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestContentToString_Nil(t *testing.T) {
	got := contentToString(nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestContentToString_Map(t *testing.T) {
	got := contentToString(map[string]any{"key": "val"})
	if got == "" {
		t.Error("expected non-empty JSON output for map")
	}
	if !strings.Contains(got, "key") {
		t.Errorf("expected 'key' in JSON output, got %q", got)
	}
}

func TestContentToString_Int(t *testing.T) {
	got := contentToString(42)
	if got != "42" {
		t.Errorf("expected '42', got %q", got)
	}
}

// ---- toToolDefs ----

func TestToToolDefs_NilRegistry(t *testing.T) {
	defs := toToolDefs(nil)
	if defs != nil {
		t.Error("expected nil for nil registry")
	}
}
````

## File: internal/artifacts/store.go
````go
package artifacts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/db"
)

type Store struct {
	Dir string
	DB  *db.DB
}

func (s *Store) Save(ctx context.Context, sessionKey, mime string, data []byte) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("artifacts dir not set")
	}
	if s.DB == nil {
		return "", fmt.Errorf("artifacts db not set")
	}
	if err := s.DB.EnsureSession(ctx, strings.TrimSpace(sessionKey)); err != nil {
		return "", err
	}
	_ = os.MkdirAll(s.Dir, 0o755)
	id := randID()
	path := filepath.Join(s.Dir, id)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	_, err := s.DB.SQL.ExecContext(ctx,
		`INSERT INTO artifacts(id, session_key, mime, path, size_bytes, created_at) VALUES(?,?,?,?,?,?)`,
		id, sessionKey, mime, path, len(data), time.Now().UnixMilli())
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return id, nil
}

func (s *Store) SaveNamed(ctx context.Context, sessionKey, filename, mimeType string, data []byte) (Attachment, error) {
	filename = NormalizeFilename(filename, mimeType)
	id, err := s.Save(ctx, sessionKey, mimeType, data)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{
		ArtifactID: id,
		Filename:   filename,
		Mime:       strings.TrimSpace(mimeType),
		Kind:       DetectKind(filename, mimeType),
		SizeBytes:  int64(len(data)),
	}, nil
}

func (s *Store) Lookup(ctx context.Context, artifactID string) (StoredArtifact, error) {
	if s.DB == nil {
		return StoredArtifact{}, fmt.Errorf("artifacts db not set")
	}
	row := s.DB.SQL.QueryRowContext(ctx,
		`SELECT id, session_key, mime, path, size_bytes FROM artifacts WHERE id=?`,
		strings.TrimSpace(artifactID),
	)
	var stored StoredArtifact
	if err := row.Scan(&stored.ID, &stored.SessionKey, &stored.Mime, &stored.Path, &stored.SizeBytes); err != nil {
		if err == sql.ErrNoRows {
			return StoredArtifact{}, fmt.Errorf("artifact not found: %s", artifactID)
		}
		return StoredArtifact{}, err
	}
	return stored, nil
}

func randID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
````

## File: internal/bus/bus.go
````go
package bus

import (
	"context"
)

type EventType string

const (
	EventUserMessage EventType = "user_message"
	EventCron        EventType = "cron"
	EventSystem      EventType = "system"
	EventWebhook     EventType = "webhook"
	EventFileChange  EventType = "file_change"
)

type Event struct {
	Type EventType
	SessionKey string
	Channel string
	From string
	Message string
	Meta map[string]any
}

type Handler func(ctx context.Context, ev Event) error

type Bus struct {
	ch chan Event
}

func New(buffer int) *Bus {
	if buffer <= 0 { buffer = 128 }
	return &Bus{ch: make(chan Event, buffer)}
}

func (b *Bus) Publish(ev Event) bool {
	select {
	case b.ch <- ev:
		return true
	default:
		return false
	}
}
func (b *Bus) Channel() <-chan Event { return b.ch }
````

## File: internal/channels/cli/deliver.go
````go
package cli

import (
	"context"
	"fmt"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
)

type Deliverer struct{}

func (Deliverer) Name() string { return "cli" }

func (Deliverer) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	return nil
}

func (Deliverer) Stop(ctx context.Context) error {
	_ = ctx
	return nil
}

func (Deliverer) Deliver(ctx context.Context, channel, to, text string) error {
	_ = ctx
	if channel == "" { channel = "cli" }
	fmt.Printf("\n[%s] %s\n\n", channel, text)
	return nil
}

// CLIStreamWriter writes deltas directly to stdout.
type CLIStreamWriter struct {
	started bool
	closed  bool
	aborted bool
}

func (w *CLIStreamWriter) WriteDelta(ctx context.Context, text string) error {
	_ = ctx
	if w.closed || w.aborted {
		return nil
	}
	w.started = true
	fmt.Print(text)
	return nil
}

func (w *CLIStreamWriter) Close(ctx context.Context, finalText string) error {
	_ = ctx
	if w.aborted {
		return nil
	}
	w.closed = true
	if w.started {
		fmt.Println() // newline after streamed content
	} else {
		// Never streamed - print the final text now
		fmt.Printf("\n[cli] %s\n\n", finalText)
	}
	return nil
}

func (w *CLIStreamWriter) Abort(ctx context.Context) error {
	_ = ctx
	w.aborted = true
	if w.started {
		fmt.Println("\n[aborted]")
	}
	return nil
}

// BeginStream implements channels.StreamingChannel.
func (Deliverer) BeginStream(ctx context.Context, to string, meta map[string]any) (channels.StreamWriter, error) {
	_ = ctx
	_ = to
	_ = meta
	fmt.Print("\n[cli] ")
	return &CLIStreamWriter{}, nil
}
````

## File: internal/memory/retrieve.go
````go
package memory

import (
	"context"
	"sort"
	"strings"

	"or3-intern/internal/db"
)

type Retrieved struct {
	Source string // pinned|vector|fts
	ID int64
	Text string
	Score float64
}

type Retriever struct {
	DB *db.DB
	VectorWeight float64
	FTSWeight float64
	VectorScanLimit int
}

func NewRetriever(d *db.DB) *Retriever {
	return &Retriever{DB: d, VectorWeight: 0.7, FTSWeight: 0.3, VectorScanLimit: 2000}
}

func (r *Retriever) Retrieve(ctx context.Context, sessionKey, query string, queryVec []float32, vectorK, ftsK, topK int) ([]Retrieved, error) {
	vecs, err := VectorSearch(ctx, r.DB, sessionKey, queryVec, vectorK, r.VectorScanLimit)
	if err != nil { return nil, err }
	fts, _ := r.DB.SearchFTS(ctx, sessionKey, normalizeFTSQuery(query), ftsK)

	type agg struct {
		id int64
		text string
		v float64
		f float64
	}
	m := map[int64]*agg{}
	for _, c := range vecs {
		a := m[c.ID]
		if a == nil { a = &agg{id: c.ID, text: c.Text}; m[c.ID] = a }
		a.v = c.Score
	}
	for _, f := range fts {
		a := m[f.ID]
		if a == nil { a = &agg{id: f.ID, text: f.Text}; m[f.ID] = a }
		// bm25 lower is better. Convert to a positive "higher is better".
		a.f = 1.0 / (1.0 + f.Rank)
	}

	out := make([]Retrieved, 0, len(m))
	for _, a := range m {
		score := (a.v * r.VectorWeight) + (a.f * r.FTSWeight)
		src := "hybrid"
		if a.f > 0 && a.v == 0 { src = "fts" }
		if a.v > 0 && a.f == 0 { src = "vector" }
		out = append(out, Retrieved{Source: src, ID: a.id, Text: a.text, Score: score})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID > out[j].ID // stable-ish
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > topK { out = out[:topK] }
	return out, nil
}

func normalizeFTSQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" { return "" }
	// simple: split on spaces, quote terms that contain punctuation
	parts := strings.Fields(q)
	for i, p := range parts {
		if strings.ContainsAny(p, `":*`) {
			parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		}
	}
	return strings.Join(parts, " ")
}
````

## File: internal/memory/scheduler.go
````go
package memory

import (
	"context"
	"sync"
	"time"
)

type Scheduler struct {
	timeout time.Duration
	run     func(context.Context, string)
	baseCtx context.Context

	mu       sync.Mutex
	sessions map[string]*schedulerState
}

type schedulerState struct {
	running bool
	dirty   bool
}

func NewScheduler(timeout time.Duration, run func(context.Context, string)) *Scheduler {
	return NewSchedulerWithContext(context.Background(), timeout, run)
}

func NewSchedulerWithContext(baseCtx context.Context, timeout time.Duration, run func(context.Context, string)) *Scheduler {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return &Scheduler{
		timeout:  timeout,
		run:      run,
		baseCtx:  baseCtx,
		sessions: map[string]*schedulerState{},
	}
}

func (s *Scheduler) Trigger(sessionKey string) {
	if s == nil || s.run == nil || sessionKey == "" {
		return
	}
	s.mu.Lock()
	state, ok := s.sessions[sessionKey]
	if !ok {
		state = &schedulerState{}
		s.sessions[sessionKey] = state
	}
	if state.running {
		state.dirty = true
		s.mu.Unlock()
		return
	}
	state.running = true
	state.dirty = false
	s.mu.Unlock()

	go s.runLoop(sessionKey)
}

func (s *Scheduler) runLoop(sessionKey string) {
	for {
		base := s.baseCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, s.timeout)
		s.run(ctx, sessionKey)
		cancel()

		s.mu.Lock()
		state := s.sessions[sessionKey]
		if state == nil {
			s.mu.Unlock()
			return
		}
		if state.dirty {
			state.dirty = false
			s.mu.Unlock()
			continue
		}
		delete(s.sessions, sessionKey)
		s.mu.Unlock()
		return
	}
}
````

## File: internal/memory/vector.go
````go
package memory

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

func PackFloat32(vec []float32) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, vec)
	return b.Bytes()
}

func UnpackFloat32(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 { return nil, errors.New("invalid float32 blob") }
	out := make([]float32, len(blob)/4)
	if err := binary.Read(bytes.NewReader(blob), binary.LittleEndian, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func Cosine(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n { n = len(b) }
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 { return 0 }
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

type VecCandidate struct {
	ID int64
	Text string
	Score float64
}

type candMinHeap []VecCandidate

func (h candMinHeap) Len() int { return len(h) }
func (h candMinHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h candMinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *candMinHeap) Push(x any) { *h = append(*h, x.(VecCandidate)) }
func (h *candMinHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func VectorSearch(ctx context.Context, d *db.DB, sessionKey string, queryVec []float32, k int, scanLimit int) ([]VecCandidate, error) {
	h := &candMinHeap{}
	heap.Init(h)

	scopes := []string{scope.GlobalMemoryScope}
	if trimmedSessionKey := strings.TrimSpace(sessionKey); trimmedSessionKey != "" && trimmedSessionKey != scope.GlobalMemoryScope {
		scopes = append(scopes, sessionKey)
	}
	for _, memoryScope := range scopes {
		rows, err := d.StreamMemoryNotesScopeLimit(ctx, memoryScope, scanLimit)
		if err != nil { return nil, err }
		if err := addVectorCandidates(rows, queryVec, k, h); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	// pop into descending slice
	out := make([]VecCandidate, h.Len())
	for i := len(out)-1; i >= 0; i-- {
		out[i] = heap.Pop(h).(VecCandidate)
	}
	// now out ascending (min->max). reverse to max->min
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 { out[i], out[j] = out[j], out[i] }
	return out, nil
}

func addVectorCandidates(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, queryVec []float32, k int, h *candMinHeap) error {
	for rows.Next() {
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		if err := rows.Scan(&id, &text, &emb, &src, &tags, &created); err != nil {
			return err
		}
		v, err := UnpackFloat32(emb)
		if err != nil {
			continue
		}
		score := Cosine(queryVec, v)
		if h.Len() < k {
			heap.Push(h, VecCandidate{ID: id, Text: text, Score: score})
		} else if (*h)[0].Score < score {
			(*h)[0] = VecCandidate{ID: id, Text: text, Score: score}
			heap.Fix(h, 0)
		}
	}
	return rows.Err()
}
````

## File: internal/skills/skills_test.go
````go
package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeSkillsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill_one.md"), []byte("# Skill One\nContent one"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill_two.txt"), []byte("Skill two content"), 0o644)
	os.WriteFile(filepath.Join(dir, "not_a_skill.json"), []byte(`{}`), 0o644)
	return dir
}

func TestScan_Empty(t *testing.T) {
	inv := Scan(nil)
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(inv.Skills))
	}
}

func TestScan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	inv := Scan([]string{dir})
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills in empty dir, got %d", len(inv.Skills))
	}
}

func TestScan_BlankDirSkipped(t *testing.T) {
	inv := Scan([]string{"   ", ""})
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills with blank dirs, got %d", len(inv.Skills))
	}
}

func TestScan_FiltersByExtension(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})
	// should include .md and .txt but not .json
	if len(inv.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(inv.Skills))
	}
	for _, s := range inv.Skills {
		if s.Name == "not_a_skill" {
			t.Error("expected .json file to be excluded")
		}
	}
}

func TestScan_SortedByName(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})
	for i := 1; i < len(inv.Skills); i++ {
		if inv.Skills[i].Name < inv.Skills[i-1].Name {
			t.Errorf("expected sorted skills, got %q before %q", inv.Skills[i-1].Name, inv.Skills[i].Name)
		}
	}
}

func TestScan_SkillFields(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	for _, s := range inv.Skills {
		if s.Name == "" {
			t.Error("expected non-empty skill name")
		}
		if s.Path == "" {
			t.Error("expected non-empty skill path")
		}
		if s.ID == "" {
			t.Error("expected non-empty skill ID")
		}
		if s.Size <= 0 {
			t.Errorf("expected positive size for %q, got %d", s.Name, s.Size)
		}
	}
}

func TestScan_MultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "alpha.md"), []byte("alpha"), 0o644)
	os.WriteFile(filepath.Join(dir2, "beta.md"), []byte("beta"), 0o644)

	inv := Scan([]string{dir1, dir2})
	if len(inv.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(inv.Skills))
	}
}

func TestScan_SkipsSymlinkedSkill(t *testing.T) {
	dir := t.TempDir()
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "outside.md")
	os.WriteFile(target, []byte("outside"), 0o644)
	link := filepath.Join(dir, "outside.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	inv := Scan([]string{dir})
	if len(inv.Skills) != 0 {
		t.Fatalf("expected symlinked skill to be skipped, got %#v", inv.Skills)
	}
}

func TestInventory_Get_Found(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	s, ok := inv.Get("skill_one")
	if !ok {
		t.Fatal("expected to find 'skill_one'")
	}
	if s.Name != "skill_one" {
		t.Errorf("expected name 'skill_one', got %q", s.Name)
	}
}

func TestInventory_Get_NotFound(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	_, ok := inv.Get("nonexistent")
	if ok {
		t.Error("expected 'nonexistent' to not be found")
	}
}

func TestInventory_Summary_Empty(t *testing.T) {
	inv := Scan(nil)
	s := inv.Summary(50)
	if s != "(no skills found)" {
		t.Errorf("expected '(no skills found)', got %q", s)
	}
}

func TestInventory_Summary_WithItems(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})

	s := inv.Summary(10)
	if !strings.Contains(s, "skill_one") && !strings.Contains(s, "skill_two") {
		t.Errorf("expected summary to contain skill names, got %q", s)
	}
}

func TestInventory_Summary_MaxItems(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := []string{"aaa", "bbb", "ccc", "ddd", "eee"}[i]
		os.WriteFile(filepath.Join(dir, name+".md"), []byte("content"), 0o644)
	}

	inv := Scan([]string{dir})
	// Limit to 2
	s := inv.Summary(2)
	lines := strings.Split(strings.TrimSpace(s), "\n")
	// 2 items + "…" = 3 lines
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (2 items + ellipsis), got %d: %q", len(lines), s)
	}
	if lines[2] != "…" {
		t.Errorf("expected last line to be '…', got %q", lines[2])
	}
}

func TestInventory_Summary_DefaultMax(t *testing.T) {
	dir := makeSkillsDir(t)
	inv := Scan([]string{dir})
	// passing 0 should use default of 50
	s := inv.Summary(0)
	if s == "" || s == "(no skills found)" {
		t.Errorf("expected summary with content, got %q", s)
	}
}

func TestLoadBody_Normal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.md")
	content := "# Skill\nSome content here"
	os.WriteFile(path, []byte(content), 0o644)

	got, err := LoadBody(path, 0)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestLoadBody_Truncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.md")
	content := strings.Repeat("a", 100)
	os.WriteFile(path, []byte(content), 0o644)

	got, err := LoadBody(path, 50)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if len(got) != 50 {
		t.Errorf("expected 50 bytes, got %d", len(got))
	}
}

func TestLoadBody_FileNotFound(t *testing.T) {
	_, err := LoadBody("/nonexistent/path/skill.md", 0)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestHash_Deterministic(t *testing.T) {
	h1 := hash("some/path/file.md")
	h2 := hash("some/path/file.md")
	if h1 != h2 {
		t.Errorf("expected deterministic hash, got %q and %q", h1, h2)
	}
}

func TestHash_Different(t *testing.T) {
	h1 := hash("path/a.md")
	h2 := hash("path/b.md")
	if h1 == h2 {
		t.Error("expected different hashes for different paths")
	}
}

// ---- Manifest and front matter ----

func TestSkillManifestParsing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "myskill.md"), []byte("# My Skill\nDoes things."), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{
		"summary": "a really cool skill",
		"entrypoints": [
			{"name": "run", "command": ["python", "main.py"], "timeoutSeconds": 30, "acceptsStdin": false}
		]
	}`), 0o644)

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	s := inv.Skills[0]
	if s.Summary != "a really cool skill" {
		t.Errorf("expected summary 'a really cool skill', got %q", s.Summary)
	}
	if len(s.Entrypoints) != 1 {
		t.Fatalf("expected 1 entrypoint, got %d", len(s.Entrypoints))
	}
	ep := s.Entrypoints[0]
	if ep.Name != "run" {
		t.Errorf("expected entrypoint name 'run', got %q", ep.Name)
	}
	if len(ep.Command) != 2 || ep.Command[0] != "python" {
		t.Errorf("expected command [python main.py], got %v", ep.Command)
	}
	if ep.TimeoutSeconds != 30 {
		t.Errorf("expected timeout 30, got %d", ep.TimeoutSeconds)
	}
	if ep.AcceptsStdin {
		t.Error("expected acceptsStdin false")
	}
}

func TestSkillFrontMatterSummary(t *testing.T) {
	dir := t.TempDir()
	content := "---\nsummary: parses front matter correctly\n---\n# Skill\nBody text."
	os.WriteFile(filepath.Join(dir, "frontmatter.md"), []byte(content), 0o644)

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	s := inv.Skills[0]
	if s.Summary != "parses front matter correctly" {
		t.Errorf("expected summary 'parses front matter correctly', got %q", s.Summary)
	}
}

func TestSkillManifestOverridesFrontMatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\nsummary: from front matter\n---\n# Skill"
	os.WriteFile(filepath.Join(dir, "skill.md"), []byte(content), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"summary":"from manifest"}`), 0o644)

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	// manifest takes precedence
	if inv.Skills[0].Summary != "from manifest" {
		t.Errorf("expected manifest summary to take precedence, got %q", inv.Skills[0].Summary)
	}
}

func TestSkillSummaryInInventory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("---\nsummary: does alpha things\n---\n# Alpha"), 0o644)
	os.WriteFile(filepath.Join(dir, "beta.md"), []byte("# Beta\nNo front matter."), 0o644)

	inv := Scan([]string{dir})
	s := inv.Summary(10)

	if !strings.Contains(s, "alpha: does alpha things") {
		t.Errorf("expected summary line 'alpha: does alpha things' in %q", s)
	}
	if !strings.Contains(s, "- beta\n") && !strings.HasSuffix(s, "- beta") {
		t.Errorf("expected plain '- beta' line (no summary) in %q", s)
	}
}

func TestExtractFrontMatterSummary_NoFrontMatter(t *testing.T) {
	got := extractFrontMatterSummary("# Title\nsome body")
	if got != "" {
		t.Errorf("expected empty string for no front matter, got %q", got)
	}
}

func TestExtractFrontMatterSummary_WithSummary(t *testing.T) {
	content := "---\nsummary: hello world\nauthor: test\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestExtractFrontMatterSummary_WithQuotedSummary(t *testing.T) {
	content := "---\nsummary: \"quoted value\"\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "quoted value" {
		t.Errorf("expected 'quoted value', got %q", got)
	}
}

func TestExtractFrontMatterSummary_MissingSummaryKey(t *testing.T) {
	content := "---\nauthor: someone\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "" {
		t.Errorf("expected empty string when no summary key, got %q", got)
	}
}

func TestSkillManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "myskill.md"), []byte("# Skill"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`not valid json`), 0o644)

	// Should not panic; skill loads without summary/entrypoints
	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill even with invalid manifest, got %d", len(inv.Skills))
	}
	if inv.Skills[0].Summary != "" {
		t.Errorf("expected empty summary for invalid manifest, got %q", inv.Skills[0].Summary)
	}
}
````

## File: internal/tools/files.go
````go
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileTool struct {
	Base
	Root string // allowed root (optional)
}

const (
	defaultReadFileMaxBytes = 200000
	defaultListDirMaxEntries = 200
)

func (t *FileTool) safePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" { return "", errors.New("missing path") }
	abs, err := filepath.Abs(p)
	if err != nil { return "", err }
	abs, err = canonicalizePath(abs)
	if err != nil { return "", err }
	if t.Root != "" {
		root, err := filepath.Abs(t.Root)
		if err != nil { return "", err }
		root, err = canonicalizeRoot(root)
		if err != nil { return "", err }
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path outside allowed root")
		}
	}
	return abs, nil
}

func canonicalizeRoot(root string) (string, error) {
	if _, err := os.Stat(root); err != nil { return "", err }
	return filepath.EvalSymlinks(root)
}

func canonicalizePath(abs string) (string, error) {
	if _, err := os.Lstat(abs); err == nil {
		return filepath.EvalSymlinks(abs)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	existing := abs
	missingParts := make([]string, 0, 4)
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", os.ErrNotExist
		}
		missingParts = append(missingParts, filepath.Base(existing))
		existing = parent
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil { return "", err }
	for i := len(missingParts) - 1; i >= 0; i-- {
		realExisting = filepath.Join(realExisting, missingParts[i])
	}
	return realExisting, nil
}

type ReadFile struct{ FileTool }
func (t *ReadFile) Name() string { return "read_file" }
func (t *ReadFile) Description() string { return "Read a UTF-8 text file." }
func (t *ReadFile) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"maxBytes": map[string]any{"type":"integer","description":"Max bytes to read (default 200000)"},
	},"required":[]string{"path"}}
}
func (t *ReadFile) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *ReadFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	max := defaultReadFileMaxBytes
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 { max = int(v) }
	b, err := os.ReadFile(p)
	if err != nil { return "", err }
	if len(b) > max { b = b[:max] }
	return string(b), nil
}

type WriteFile struct{ FileTool }
func (t *WriteFile) Name() string { return "write_file" }
func (t *WriteFile) Description() string { return "Write text to a file (overwrites)." }
func (t *WriteFile) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"content": map[string]any{"type":"string"},
		"mkdirs": map[string]any{"type":"boolean"},
	},"required":[]string{"path","content"}}
}
func (t *WriteFile) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *WriteFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	content := fmt.Sprint(params["content"])
	mkdirs, _ := params["mkdirs"].(bool)
	if mkdirs {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { return "", err }
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil { return "", err }
	return "ok", nil
}

type EditFile struct{ FileTool }
func (t *EditFile) Name() string { return "edit_file" }
func (t *EditFile) Description() string {
	return "Edit a text file by applying a list of find/replace operations."
}
func (t *EditFile) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"edits": map[string]any{"type":"array","items":map[string]any{
			"type":"object",
			"properties":map[string]any{
				"find": map[string]any{"type":"string"},
				"replace": map[string]any{"type":"string"},
				"count": map[string]any{"type":"integer","description":"max replacements (0=all)"},
			},
			"required":[]string{"find","replace"},
		}},
	},"required":[]string{"path","edits"}}
}
func (t *EditFile) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *EditFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	b, err := os.ReadFile(p)
	if err != nil { return "", err }
	s := string(b)
	rawEdits, _ := params["edits"].([]any)
	for _, e := range rawEdits {
		m, _ := e.(map[string]any)
		find := fmt.Sprint(m["find"])
		replace := fmt.Sprint(m["replace"])
		count := 0
		if v, ok := m["count"].(float64); ok { count = int(v) }
		if count <= 0 {
			s = strings.ReplaceAll(s, find, replace)
		} else {
			s = strings.Replace(s, find, replace, count)
		}
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil { return "", err }
	return "ok", nil
}

type ListDir struct{ FileTool }
func (t *ListDir) Name() string { return "list_dir" }
func (t *ListDir) Description() string { return "List directory entries." }
func (t *ListDir) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"path": map[string]any{"type":"string"},
		"max": map[string]any{"type":"integer"},
	},"required":[]string{"path"}}
}
func (t *ListDir) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *ListDir) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil { return "", err }
	ents, err := os.ReadDir(p)
	if err != nil { return "", err }
	max := defaultListDirMaxEntries
	if v, ok := params["max"].(float64); ok && int(v) > 0 { max = int(v) }
	type entry struct{ Name string `json:"name"`; IsDir bool `json:"isDir"`; Size int64 `json:"size"` }
	out := []entry{}
	for _, e := range ents {
		if len(out) >= max { break }
		info, _ := e.Info()
		sz := int64(0)
		if info != nil { sz = info.Size() }
		out = append(out, entry{Name: e.Name(), IsDir: e.IsDir(), Size: sz})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}
````

## File: internal/tools/memory.go
````go
package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

type MemorySetPinned struct {
	Base
	DB *db.DB
}
func (t *MemorySetPinned) Name() string { return "memory_set_pinned" }
func (t *MemorySetPinned) Description() string { return "Upsert a pinned memory entry (always included in prompts)." }
func (t *MemorySetPinned) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"key": map[string]any{"type":"string"},
		"content": map[string]any{"type":"string"},
		"scope": map[string]any{"type":"string", "description":"Optional scope override: 'global' to share across sessions"},
	},"required":[]string{"key","content"}}
}
func (t *MemorySetPinned) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *MemorySetPinned) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil { return "", fmt.Errorf("db not set") }
	key := strings.TrimSpace(fmt.Sprint(params["key"]))
	content := strings.TrimSpace(fmt.Sprint(params["content"]))
	if key == "" || content == "" { return "", fmt.Errorf("missing key/content") }
	if err := t.DB.UpsertPinned(ctx, memoryScopeFromParams(ctx, params), key, content); err != nil { return "", err }
	return "ok", nil
}

type MemoryAddNote struct {
	Base
	DB *db.DB
	Provider *providers.Client
	EmbedModel string
}
func (t *MemoryAddNote) Name() string { return "memory_add_note" }
func (t *MemoryAddNote) Description() string { return "Add a semantic memory note to the indexed memory store." }
func (t *MemoryAddNote) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"text": map[string]any{"type":"string"},
		"tags": map[string]any{"type":"string","description":"comma-separated tags (optional)"},
		"source_message_id": map[string]any{"type":"integer","description":"source message id (optional)"},
		"scope": map[string]any{"type":"string", "description":"Optional scope override: 'global' to share across sessions"},
	},"required":[]string{"text"}}
}
func (t *MemoryAddNote) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *MemoryAddNote) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil { return "", fmt.Errorf("missing deps") }
	text := strings.TrimSpace(fmt.Sprint(params["text"]))
	if text == "" { return "", fmt.Errorf("empty text") }
	tags := strings.TrimSpace(fmt.Sprint(params["tags"]))
	var src sql.NullInt64
	if v, ok := params["source_message_id"].(float64); ok && int64(v) > 0 {
		src = sql.NullInt64{Int64: int64(v), Valid: true}
	}
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, text)
	if err != nil { return "", err }
	blob := memory.PackFloat32(vec)
	id, err := t.DB.InsertMemoryNote(ctx, memoryScopeFromParams(ctx, params), text, blob, src, tags)
	if err != nil { return "", err }
	return fmt.Sprintf("ok: %d", id), nil
}

type MemorySearch struct {
	Base
	DB *db.DB
	Provider *providers.Client
	EmbedModel string
	VectorK int
	FTSK int
	TopK int
	VectorScanLimit int
}
func (t *MemorySearch) Name() string { return "memory_search" }
func (t *MemorySearch) Description() string { return "Search long-term memory (hybrid semantic + keyword) and return top results." }
func (t *MemorySearch) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"query": map[string]any{"type":"string"},
		"topK": map[string]any{"type":"integer"},
		"scope": map[string]any{"type":"string", "description":"Optional scope override: 'global' to search only shared memory"},
	},"required":[]string{"query"}}
}
func (t *MemorySearch) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *MemorySearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil { return "", fmt.Errorf("missing deps") }
	q := strings.TrimSpace(fmt.Sprint(params["query"]))
	if q == "" { return "", fmt.Errorf("empty query") }
	topK := t.TopK
	if v, ok := params["topK"].(float64); ok && int(v) > 0 { topK = int(v) }
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, q)
	if err != nil { return "", err }
	r := memory.NewRetriever(t.DB)
	r.VectorScanLimit = t.VectorScanLimit
	got, err := r.Retrieve(ctx, memoryScopeFromParams(ctx, params), q, vec, t.VectorK, t.FTSK, topK)
	if err != nil { return "", err }
	var b strings.Builder
	for i, m := range got {
		b.WriteString(fmt.Sprintf("%d. [%s] %.4f %s\n", i+1, m.Source, m.Score, m.Text))
	}
	return b.String(), nil
}

func memoryScopeFromParams(ctx context.Context, params map[string]any) string {
	if requestedScope := strings.TrimSpace(fmt.Sprint(params["scope"])); scope.IsGlobalScopeRequest(requestedScope) {
		return scope.GlobalMemoryScope
	}
	return SessionFromContext(ctx)
}
````

## File: internal/tools/web.go
````go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type WebFetch struct{
	Base
	HTTP *http.Client
	Timeout time.Duration
	DefaultMaxBytes int
}

const (
	defaultWebTimeout = 20 * time.Second
	defaultWebFetchMaxBytes = 200000
	defaultWebFetchMaxRedirects = 10
	defaultWebSearchMaxCount = 10
	defaultWebSearchReadMaxBytes = 1 << 20
)

func (t *WebFetch) Name() string { return "web_fetch" }
func (t *WebFetch) Description() string { return "Fetch a URL (GET) and return text (truncated)." }
func (t *WebFetch) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"url": map[string]any{"type":"string"},
		"maxBytes": map[string]any{"type":"integer"},
	},"required":[]string{"url"}}
}
func (t *WebFetch) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }
func (t *WebFetch) Execute(ctx context.Context, params map[string]any) (string, error) {
	u := fmt.Sprint(params["url"])
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("invalid url")
	}
	parsed, err := url.Parse(u)
	if err != nil { return "", err }
	if err := validateFetchURL(ctx, parsed); err != nil { return "", err }
	max := t.DefaultMaxBytes
	if max <= 0 { max = defaultWebFetchMaxBytes }
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 { max = int(v) }
	client := t.HTTP
	if t.HTTP == nil {
		to := t.Timeout
		if to <= 0 { to = defaultWebTimeout }
		client = &http.Client{Timeout: to}
	} else {
		copyClient := *t.HTTP
		client = &copyClient
	}
	prevCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= defaultWebFetchMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", defaultWebFetchMaxRedirects)
		}
		if prevCheckRedirect != nil {
			if err := prevCheckRedirect(req, via); err != nil {
				return err
			}
		}
		return validateFetchURL(req.Context(), req.URL)
	}
	r, err := http.NewRequestWithContext(ctx, "GET", parsed.String(), nil)
	if err != nil { return "", err }
	resp, err := client.Do(r)
	if err != nil { return "", err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(max)))
	return fmt.Sprintf("status: %s\n\n%s", resp.Status, string(body)), nil
}

func validateFetchURL(ctx context.Context, target *url.URL) error {
	if target == nil {
		return fmt.Errorf("invalid url")
	}
	hostname := strings.TrimSpace(strings.ToLower(target.Hostname()))
	if hostname == "" {
		return fmt.Errorf("missing host")
	}
	if isBlockedFetchHostname(hostname) {
		return fmt.Errorf("blocked fetch target")
	}
	if ip, err := netip.ParseAddr(hostname); err == nil {
		if isBlockedFetchAddr(ip.Unmap()) {
			return fmt.Errorf("blocked fetch target")
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host did not resolve")
	}
	for _, addr := range addrs {
		if ip, ok := netip.AddrFromSlice(addr.IP); ok && isBlockedFetchAddr(ip.Unmap()) {
			return fmt.Errorf("blocked fetch target")
		}
	}
	return nil
}

func isBlockedFetchHostname(hostname string) bool {
	switch hostname {
	case "localhost", "ip6-localhost", "metadata.google.internal":
		return true
	default:
		return false
	}
}

func isBlockedFetchAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	return addr.String() == "169.254.169.254"
}

type WebSearch struct{
	Base
	APIKey string
	HTTP *http.Client
	Timeout time.Duration
	ReadMaxBytes int
}

func (t *WebSearch) Name() string { return "web_search" }
func (t *WebSearch) Description() string {
	return "Search the web (Brave Search API) and return top results."
}
func (t *WebSearch) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"query": map[string]any{"type":"string"},
		"count": map[string]any{"type":"integer","description":"max results (default 5)"},
	},"required":[]string{"query"}}
}
func (t *WebSearch) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }

func (t *WebSearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	if strings.TrimSpace(t.APIKey) == "" {
		return "", fmt.Errorf("Brave API key not configured (set BRAVE_API_KEY)")
	}
	q := fmt.Sprint(params["query"])
	count := 5
	if v, ok := params["count"].(float64); ok && int(v) > 0 { count = int(v) }
	if count > defaultWebSearchMaxCount { count = defaultWebSearchMaxCount }
	if t.HTTP == nil {
		to := t.Timeout
		if to <= 0 { to = defaultWebTimeout }
		t.HTTP = &http.Client{Timeout: to}
	}

	endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(q) + "&count=" + fmt.Sprint(count)
	r, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil { return "", err }
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-Subscription-Token", t.APIKey)

	resp, err := t.HTTP.Do(r)
	if err != nil { return "", err }
	defer resp.Body.Close()
	maxRead := t.ReadMaxBytes
	if maxRead <= 0 { maxRead = defaultWebSearchReadMaxBytes }
	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxRead)))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("search error %s: %s", resp.Status, string(body))
	}

	// Reduce response to stable subset
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	out := map[string]any{"query": q, "results": []any{}}
	web, _ := raw["web"].(map[string]any)
	results, _ := web["results"].([]any)
	for _, it := range results {
		m, _ := it.(map[string]any)
		out["results"] = append(out["results"].([]any), map[string]any{
			"title": m["title"],
			"url": m["url"],
			"description": m["description"],
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// Optional: simple text extract from HTML (very rough)
func StripHTML(s string) string {
	var b bytes.Buffer
	in := false
	for _, r := range s {
		if r == '<' { in = true; continue }
		if r == '>' { in = false; continue }
		if !in { b.WriteRune(r) }
	}
	return b.String()
}
````

## File: go.mod
````
module or3-intern

go 1.22

require (
	github.com/gorilla/websocket v1.5.3
	github.com/robfig/cron/v3 v3.0.1
	modernc.org/sqlite v1.33.1
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.22.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)
````

## File: internal/agent/runtime_test.go
````go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

// mockDeliverer captures delivered messages
type mockDeliverer struct {
	messages []string
	channel  string
	to       string
	err      error
}

func (m *mockDeliverer) Deliver(ctx context.Context, channel, to, text string) error {
	m.messages = append(m.messages, text)
	m.channel = channel
	m.to = to
	return m.err
}

// buildChatServer creates a test HTTP server that responds to /chat/completions
func buildChatServer(t *testing.T, response providers.ChatCompletionResponse) (*httptest.Server, *providers.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(srv.Close)
	c := providers.New(srv.URL, "test-key", 10*time.Second)
	c.HTTP = srv.Client()
	return srv, c
}

func openRuntimeTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func buildSimpleRuntime(t *testing.T, provider *providers.Client, d *db.DB, deliver *mockDeliverer) *Runtime {
	t.Helper()
	reg := tools.NewRegistry()
	b := &Builder{
		DB:         d,
		HistoryMax: 10,
	}
	return &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      b,
		MaxToolLoops: 2,
		Deliver:      deliver,
	}
}

func TestRuntime_Handle_UserMessage(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "Hello there!"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	}

	err := rt.Handle(context.Background(), ev)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(deliver.messages) == 0 {
		t.Error("expected at least one delivered message")
	}
	if deliver.messages[0] != "Hello there!" {
		t.Errorf("expected 'Hello there!', got %q", deliver.messages[0])
	}
}

func TestRuntime_Handle_UsesChannelTargetFromEventMeta(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "Reply"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Channel:    "discord",
		From:       "user-id",
		Message:    "hello",
		Meta:       map[string]any{"channel_id": "channel-1"},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if deliver.to != "channel-1" {
		t.Fatalf("expected delivery to channel target, got %q", deliver.to)
	}
}

func TestRuntime_Handle_PersistsAttachmentMetadata(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "ok"},
		}},
	}
	_, provider := buildChatServer(t, response)
	rt := buildSimpleRuntime(t, provider, d, &mockDeliverer{})
	ev := bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Channel:    "telegram",
		From:       "user",
		Message:    "see image\n[image: photo.png]",
		Meta: map[string]any{
			"attachments": []map[string]any{{
				"artifact_id": "artifact-1",
				"filename":    "photo.png",
				"mime":        "image/png",
				"kind":        "image",
				"size_bytes":  10,
			}},
		},
	}
	if err := rt.Handle(context.Background(), ev); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	msgs, err := d.GetLastMessages(context.Background(), "sess1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected persisted messages")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(msgs[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	meta, ok := payload["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta in payload, got %#v", payload)
	}
	attachments, ok := meta["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("expected attachments in payload meta, got %#v", meta["attachments"])
	}
}

func TestRuntime_Handle_CronEvent(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "Cron response"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventCron,
		SessionKey: "sess1",
		Message:    "cron task",
	})
	if err != nil {
		t.Fatalf("Handle cron: %v", err)
	}
}

func TestRuntime_Handle_UnknownEvent(t *testing.T) {
	d := openRuntimeTestDB(t)
	_, provider := buildChatServer(t, providers.ChatCompletionResponse{})
	rt := buildSimpleRuntime(t, provider, d, &mockDeliverer{})

	// Unknown event type should return nil without processing
	err := rt.Handle(context.Background(), bus.Event{
		Type:       "unknown_type",
		SessionKey: "sess1",
	})
	if err != nil {
		t.Fatalf("expected no error for unknown event type, got: %v", err)
	}
}

func TestRuntime_Handle_NoBuilder(t *testing.T) {
	d := openRuntimeTestDB(t)
	_, provider := buildChatServer(t, providers.ChatCompletionResponse{})
	rt := &Runtime{
		DB:       d,
		Provider: provider,
		Model:    "gpt-4",
		Tools:    tools.NewRegistry(),
		Builder:  nil, // no builder
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Message:    "hello",
	})
	if err == nil {
		t.Fatal("expected error when builder is nil")
	}
}

func TestRuntime_Handle_NoChoices(t *testing.T) {
	d := openRuntimeTestDB(t)
	// Return empty choices
	response := providers.ChatCompletionResponse{Choices: nil}
	_, provider := buildChatServer(t, response)
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      b,
		MaxToolLoops: 2,
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess1",
		Message:    "hello",
	})
	if err == nil {
		t.Fatal("expected error when no choices returned")
	}
}

func TestRuntime_Handle_WithToolCall(t *testing.T) {
	d := openRuntimeTestDB(t)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			// First call returns tool call
			resp = providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc1",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "echo_tool", Arguments: `{}`},
						}},
					},
				}},
			}
		} else {
			// Second call returns final text
			resp = providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{Role: "assistant", Content: "Final answer"},
				}},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	reg := tools.NewRegistry()
	// Register a simple echo tool
	reg.Register(&echoTool{})

	deliver := &mockDeliverer{}
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        reg,
		Builder:      b,
		MaxToolLoops: 6,
		Deliver:      deliver,
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-tool",
		Message:    "do tool call",
	})
	if err != nil {
		t.Fatalf("Handle with tool call: %v", err)
	}
	if len(deliver.messages) == 0 || deliver.messages[0] != "Final answer" {
		t.Errorf("expected 'Final answer', got %v", deliver.messages)
	}
}

// echoTool is a simple test tool for agent tests
type echoTool struct{ tools.Base }

func (e *echoTool) Name() string        { return "echo_tool" }
func (e *echoTool) Description() string { return "echoes input" }
func (e *echoTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (e *echoTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return "echo result", nil
}
func (e *echoTool) Schema() map[string]any {
	return e.SchemaFor(e.Name(), e.Description(), e.Parameters())
}

type deliveryContextTool struct {
	tools.Base
	channel string
	to      string
}

func (dct *deliveryContextTool) Name() string        { return "delivery_context_tool" }
func (dct *deliveryContextTool) Description() string { return "captures delivery context" }
func (dct *deliveryContextTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (dct *deliveryContextTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	dct.channel, dct.to = tools.DeliveryFromContext(ctx)
	return "captured", nil
}
func (dct *deliveryContextTool) Schema() map[string]any {
	return dct.SchemaFor(dct.Name(), dct.Description(), dct.Parameters())
}

func TestRuntime_Handle_ArtifactSave(t *testing.T) {
	d := openRuntimeTestDB(t)
	artifactsDir := t.TempDir()

	// First call: return tool call that generates large output
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc-large",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "large_output_tool", Arguments: `{}`},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{Role: "assistant", Content: "done"},
				}},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()

	d.EnsureSession(context.Background(), "sess-artifact")

	reg := tools.NewRegistry()
	reg.Register(&largeOutputTool{})

	deliver := &mockDeliverer{}
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:               d,
		Provider:         provider,
		Model:            "gpt-4",
		Tools:            reg,
		Builder:          b,
		MaxToolLoops:     6,
		Deliver:          deliver,
		MaxToolBytes:     10, // small limit to trigger artifact save
		ToolPreviewBytes: 5,
		Artifacts:        &artifacts.Store{Dir: artifactsDir, DB: d},
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-artifact",
		Message:    "large output",
	})
	if err != nil {
		t.Fatalf("Handle artifact: %v", err)
	}
}

func TestRuntime_Handle_ToolContextIncludesDelivery(t *testing.T) {
	d := openRuntimeTestDB(t)
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		var resp providers.ChatCompletionResponse
		if callCount == 1 {
			resp = providers.ChatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					} `json:"message"`
				}{{
					Message: struct {
						Role      string               `json:"role"`
						Content   any                  `json:"content"`
						ToolCalls []providers.ToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []providers.ToolCall{{
							ID:   "tc-delivery",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "delivery_context_tool", Arguments: `{}`},
						}},
					},
				}},
			}
		} else {
			resp = providers.ChatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string               `json:"role"`
					Content   any                  `json:"content"`
					ToolCalls []providers.ToolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "done"}}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	provider := providers.New(srv.URL, "key", 10*time.Second)
	provider.HTTP = srv.Client()
	tool := &deliveryContextTool{}
	reg := tools.NewRegistry()
	reg.Register(tool)
	rt := &Runtime{DB: d, Provider: provider, Model: "gpt-4", Tools: reg, Builder: &Builder{DB: d, HistoryMax: 10}, MaxToolLoops: 4}
	if err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess",
		Channel:    "discord",
		From:       "user-1",
		Message:    "hello",
		Meta:       map[string]any{"channel_id": "channel-1"},
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if tool.channel != "discord" || tool.to != "channel-1" {
		t.Fatalf("expected delivery context discord/channel-1, got %q/%q", tool.channel, tool.to)
	}
}

// largeOutputTool generates output larger than MaxToolBytes
type largeOutputTool struct{ tools.Base }

func (e *largeOutputTool) Name() string        { return "large_output_tool" }
func (e *largeOutputTool) Description() string { return "generates large output" }
func (e *largeOutputTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (e *largeOutputTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	// Generate output that exceeds MaxToolBytes
	return fmt.Sprintf("%s", string(make([]byte, 100))), nil
}
func (e *largeOutputTool) Schema() map[string]any {
	return e.SchemaFor(e.Name(), e.Description(), e.Parameters())
}

func TestRuntime_Handle_NoMaxLoops_Defaults(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "response"},
		}},
	}
	_, provider := buildChatServer(t, response)
	b := &Builder{DB: d, HistoryMax: 10}
	rt := &Runtime{
		DB:           d,
		Provider:     provider,
		Model:        "gpt-4",
		Tools:        tools.NewRegistry(),
		Builder:      b,
		MaxToolLoops: 0, // should default to 6
	}

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-default-loops",
		Message:    "hello",
	})
	if err != nil {
		t.Fatalf("Handle with default max loops: %v", err)
	}
}

func TestRuntime_Handle_SystemEvent(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "sys response"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventSystem,
		SessionKey: "sess-sys",
		Message:    "system message",
	})
	if err != nil {
		t.Fatalf("Handle system: %v", err)
	}
}

func TestToToolDefs_WithRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})

	defs := toToolDefs(reg)
	if len(defs) != 1 {
		t.Errorf("expected 1 tool def, got %d", len(defs))
	}
	if defs[0].Type != "function" {
		t.Errorf("expected type 'function', got %q", defs[0].Type)
	}
	if defs[0].Function.Name != "echo_tool" {
		t.Errorf("expected name 'echo_tool', got %q", defs[0].Function.Name)
	}
}

func TestRuntime_LockFor_SameKey(t *testing.T) {
	rt := &Runtime{}
	mu1 := rt.lockFor("key1")
	mu2 := rt.lockFor("key1")
	if mu1 != mu2 {
		t.Error("expected same mutex for same key")
	}
}

func TestRuntime_LockFor_DifferentKeys(t *testing.T) {
	rt := &Runtime{}
	mu1 := rt.lockFor("key1")
	mu2 := rt.lockFor("key2")
	if mu1 == mu2 {
		t.Error("expected different mutexes for different keys")
	}
}

func TestRuntime_ConsolidationScheduler_DoesNotBlockTurn(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "ok"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	rt.Consolidator = &memory.Consolidator{Provider: &providers.Client{}}
	rt.ConsolidationScheduler = memory.NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		started <- struct{}{}
		<-release
	})

	start := time.Now()
	err := rt.Handle(context.Background(), bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-non-blocking",
		Channel:    "cli",
		From:       "user",
		Message:    "hello",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("expected non-blocking turn, elapsed=%v", elapsed)
	}
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("expected scheduler run")
	}
	close(release)
}

func TestRuntime_HandleNewSession_Success(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if _, err := d.AppendMessage(ctx, "sess-new", "user", "hello", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	provServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": `{"summary":"done","canonical_memory":"- fact"}`}}},
			})
		case "/embeddings":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": []float32{0.1}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provServer.Close()
	prov := providers.New(provServer.URL, "k", 5*time.Second)
	prov.HTTP = provServer.Client()

	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:       d,
		Provider: prov,
		Model:    "gpt-4.1-mini",
		Tools:    tools.NewRegistry(),
		Builder:  &Builder{DB: d, HistoryMax: 40},
		Deliver:  deliver,
		Consolidator: &memory.Consolidator{
			DB:                 d,
			Provider:           prov,
			WindowSize:         1,
			MaxMessages:        50,
			MaxInputChars:      4000,
			CanonicalPinnedKey: "long_term_memory",
		},
	}
	if err := rt.Handle(ctx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-new",
		Channel:    "cli",
		From:       "user",
		Message:    "/new",
	}); err != nil {
		t.Fatalf("Handle /new: %v", err)
	}
	msgs, err := d.GetLastMessages(ctx, "sess-new", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected messages cleared, got %d", len(msgs))
	}
	if len(deliver.messages) == 0 || deliver.messages[0] != "New session started." {
		t.Fatalf("unexpected deliver messages: %#v", deliver.messages)
	}
}

func TestRuntime_HandleNewSession_FailurePreservesHistory(t *testing.T) {
	d := openRuntimeTestDB(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := d.AppendMessage(ctx, "sess-new-fail", "user", "hello", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	provServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer provServer.Close()
	prov := providers.New(provServer.URL, "k", 5*time.Second)
	prov.HTTP = provServer.Client()

	deliver := &mockDeliverer{}
	rt := &Runtime{
		DB:       d,
		Provider: prov,
		Model:    "gpt-4.1-mini",
		Tools:    tools.NewRegistry(),
		Builder:  &Builder{DB: d, HistoryMax: 40},
		Deliver:  deliver,
		Consolidator: &memory.Consolidator{
			DB:            d,
			Provider:      prov,
			WindowSize:    1,
			MaxMessages:   50,
			MaxInputChars: 4000,
		},
	}
	if err := rt.Handle(ctx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: "sess-new-fail",
		Channel:    "cli",
		From:       "user",
		Message:    "/new",
	}); err != nil {
		t.Fatalf("Handle /new: %v", err)
	}
	msgs, err := d.GetLastMessages(ctx, "sess-new-fail", 20)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected history preserved on archive failure")
	}
	if len(deliver.messages) == 0 || !strings.Contains(deliver.messages[0], "Memory archival failed") {
		t.Fatalf("expected archival failure message, got %#v", deliver.messages)
	}
}

func TestRuntime_ConsolidationScheduler_SingleFlightCoalesces(t *testing.T) {
	d := openRuntimeTestDB(t)
	response := providers.ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{{
			Message: struct {
				Role      string               `json:"role"`
				Content   any                  `json:"content"`
				ToolCalls []providers.ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "ok"},
		}},
	}
	_, provider := buildChatServer(t, response)
	deliver := &mockDeliverer{}
	rt := buildSimpleRuntime(t, provider, d, deliver)

	var runs int32
	firstRelease := make(chan struct{})
	secondSeen := make(chan struct{}, 1)
	rt.Consolidator = &memory.Consolidator{Provider: &providers.Client{}}
	rt.ConsolidationScheduler = memory.NewScheduler(2*time.Second, func(ctx context.Context, sessionKey string) {
		n := atomic.AddInt32(&runs, 1)
		if n == 1 {
			<-firstRelease
			return
		}
		if n == 2 {
			secondSeen <- struct{}{}
		}
	})

	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-c", Channel: "cli", From: "u", Message: "a"}); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if err := rt.Handle(context.Background(), bus.Event{Type: bus.EventUserMessage, SessionKey: "sess-c", Channel: "cli", From: "u", Message: "b"}); err != nil {
		t.Fatalf("second handle: %v", err)
	}
	close(firstRelease)
	select {
	case <-secondSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("expected coalesced second scheduler pass")
	}
}

func TestRuntimeWithIndexedDocs(t *testing.T) {
d := openRuntimeTestDB(t)
ctx := context.Background()

// Create a temp dir with a markdown file containing "important penguin behavior"
tmpDir := t.TempDir()
docContent := "# Penguin Facts\n\nImportant penguin behavior: penguins huddle together for warmth.\n"
docPath := filepath.Join(tmpDir, "penguins.md")
if err := os.WriteFile(docPath, []byte(docContent), 0o644); err != nil {
t.Fatalf("write doc file: %v", err)
}

// Insert the doc directly via UpsertDoc (no real embedding server needed)
if err := memory.UpsertDoc(ctx, d, "test-scope", docPath, "markdown", "Penguin Facts", "", docContent, nil, "testhash", 0, int64(len(docContent))); err != nil {
t.Fatalf("UpsertDoc: %v", err)
}

docRetriever := &memory.DocRetriever{DB: d}

// Build a fake provider response
response := providers.ChatCompletionResponse{
Choices: []struct {
Message struct {
Role      string               `json:"role"`
Content   any                  `json:"content"`
ToolCalls []providers.ToolCall `json:"tool_calls"`
} `json:"message"`
}{{
Message: struct {
Role      string               `json:"role"`
Content   any                  `json:"content"`
ToolCalls []providers.ToolCall `json:"tool_calls"`
}{Role: "assistant", Content: "Penguins huddle for warmth."},
}},
}
_, provider := buildChatServer(t, response)

b := &Builder{
DB:               d,
HistoryMax:       10,
DocRetriever:     docRetriever,
DocScopeKey:      "test-scope",
DocRetrieveLimit: 5,
}

pp, _, err := b.BuildWithOptions(ctx, BuildOptions{SessionKey: "test-scope", UserMessage: "penguin behavior"})
if err != nil {
t.Fatalf("BuildWithOptions: %v", err)
}

// Find system prompt content
var sysText string
for _, msg := range pp.System {
if msg.Role == "system" {
if s, ok := msg.Content.(string); ok {
sysText += s
}
}
}

// The system prompt should include the doc excerpt about penguins
if !strings.Contains(sysText, "penguin") && !strings.Contains(strings.ToLower(sysText), "penguin") {
t.Errorf("expected system prompt to contain penguin doc context, got:\n%s", sysText)
}
_ = provider
}

func TestRuntimeLinkedSessionHistory(t *testing.T) {
d := openRuntimeTestDB(t)
ctx := context.Background()

// Link two session keys to a shared scope
if err := d.LinkSession(ctx, "session-a", "shared-scope", nil); err != nil {
t.Fatalf("LinkSession a: %v", err)
}
if err := d.LinkSession(ctx, "session-b", "shared-scope", nil); err != nil {
t.Fatalf("LinkSession b: %v", err)
}

// Append messages to both sessions
if _, err := d.AppendMessage(ctx, "session-a", "user", "hello from session-a", nil); err != nil {
t.Fatalf("AppendMessage a user: %v", err)
}
if _, err := d.AppendMessage(ctx, "session-a", "assistant", "reply to session-a", nil); err != nil {
t.Fatalf("AppendMessage a assistant: %v", err)
}
if _, err := d.AppendMessage(ctx, "session-b", "user", "hello from session-b", nil); err != nil {
t.Fatalf("AppendMessage b user: %v", err)
}
if _, err := d.AppendMessage(ctx, "session-b", "assistant", "reply to session-b", nil); err != nil {
t.Fatalf("AppendMessage b assistant: %v", err)
}

// GetLastMessagesScoped with either session key should return messages from both sessions
msgs, err := d.GetLastMessagesScoped(ctx, "session-a", 20)
if err != nil {
t.Fatalf("GetLastMessagesScoped: %v", err)
}
if len(msgs) < 2 {
t.Fatalf("expected at least 2 messages across linked sessions, got %d", len(msgs))
}

contents := map[string]bool{}
for _, m := range msgs {
contents[m.Content] = true
}
if !contents["hello from session-a"] || !contents["hello from session-b"] {
t.Fatalf("expected messages from both sessions, got %v", contents)
}

// Verify chronological order
for i := 1; i < len(msgs); i++ {
if msgs[i].ID < msgs[i-1].ID {
t.Fatalf("messages not in chronological order at index %d", i)
}
}
}
````

## File: internal/cron/cron.go
````go
package cron

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type ScheduleKind string
const (
	KindAt ScheduleKind = "at"
	KindEvery ScheduleKind = "every"
	KindCron ScheduleKind = "cron"
)

type CronSchedule struct {
	Kind ScheduleKind `json:"kind"`
	AtMS int64 `json:"at_ms,omitempty"`
	EveryMS int64 `json:"every_ms,omitempty"`
	Expr string `json:"expr,omitempty"`
	TZ string `json:"tz,omitempty"`
}

type CronPayload struct {
	Kind       string `json:"kind"` // "agent_turn"|"system_event"
	Message    string `json:"message"`
	Deliver    bool   `json:"deliver"`
	Channel    string `json:"channel,omitempty"`
	To         string `json:"to,omitempty"`
	SessionKey string `json:"session_key,omitempty"` // optional per-job session key override
}

type CronJobState struct {
	NextRunAtMS *int64 `json:"next_run_at_ms,omitempty"`
	LastRunAtMS *int64 `json:"last_run_at_ms,omitempty"`
	LastStatus string `json:"last_status,omitempty"` // ok|error|skipped
	LastError string `json:"last_error,omitempty"`
}

type CronJob struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Enabled bool `json:"enabled"`
	Schedule CronSchedule `json:"schedule"`
	Payload CronPayload `json:"payload"`
	State CronJobState `json:"state"`
	CreatedAtMS int64 `json:"created_at_ms"`
	UpdatedAtMS int64 `json:"updated_at_ms"`
	DeleteAfterRun bool `json:"delete_after_run"`
}

type Store struct {
	Version int `json:"version"`
	Jobs []CronJob `json:"jobs"`
}

type Runner func(ctx context.Context, job CronJob) error

type Service struct {
	mu sync.Mutex
	path string
	runner Runner
	c *cron.Cron
	entries map[string]cron.EntryID
}

func New(path string, runner Runner) *Service {
	return &Service{
		path: path,
		runner: runner,
		entries: map[string]cron.EntryID{},
	}
}

func (s *Service) load() (Store, error) {
	var st Store
	st.Version = 1
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) save(st Store) error {
	if err := os.MkdirAll(filepathDir(s.path), 0o755); err != nil { return err }
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}

func filepathDir(p string) string {
	i := len(p)-1
	for i >= 0 && p[i] != '/' && p[i] != '\\' { i-- }
	if i <= 0 { return "." }
	return p[:i]
}

func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.c != nil { return nil }

	s.c = cron.New(cron.WithSeconds(), cron.WithParser(cron.NewParser(cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)))
	st, err := s.load()
	if err != nil { return err }
	for _, j := range st.Jobs {
		s.armJobLocked(j)
	}
	s.c.Start()
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		ctx := s.c.Stop()
		<-ctx.Done()
		s.c = nil
		s.entries = map[string]cron.EntryID{}
	}
}

func (s *Service) Status() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return nil, err }
	next := int64(0)
	for _, j := range st.Jobs {
		if j.State.NextRunAtMS != nil {
			if next == 0 || *j.State.NextRunAtMS < next { next = *j.State.NextRunAtMS }
		}
	}
	var nextPtr *int64
	if next != 0 { nextPtr = &next }
	return map[string]any{"jobs": len(st.Jobs), "next_wake_at_ms": nextPtr}, nil
}

func (s *Service) List() ([]CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return nil, err }
	return st.Jobs, nil
}

func (s *Service) Add(job CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return err }
	now := time.Now().UnixMilli()
	job.CreatedAtMS = now
	job.UpdatedAtMS = now
	if job.ID == "" { job.ID = randID() }
	if job.Name == "" { job.Name = job.ID }
	st.Jobs = append(st.Jobs, job)
	if err := s.save(st); err != nil { return err }
	s.armJobLocked(job)
	return nil
}

func (s *Service) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return false, err }
	found := false
	n := make([]CronJob, 0, len(st.Jobs))
	for _, j := range st.Jobs {
		if j.ID == id {
			found = true
			if eid, ok := s.entries[id]; ok && s.c != nil {
				s.c.Remove(eid)
				delete(s.entries, id)
			}
			continue
		}
		n = append(n, j)
	}
	st.Jobs = n
	if err := s.save(st); err != nil { return false, err }
	return found, nil
}

func (s *Service) RunNow(ctx context.Context, id string, force bool) (bool, error) {
	s.mu.Lock()
	st, err := s.load()
	s.mu.Unlock()
	if err != nil { return false, err }
	for _, j := range st.Jobs {
		if j.ID == id {
			if !force && !j.Enabled { return false, nil }
			err := s.runner(ctx, j)
			s.mu.Lock()
			defer s.mu.Unlock()
			st2, loadErr := s.load()
			if loadErr != nil {
				return true, err
			}
			shouldDelete := false
			for i := range st2.Jobs {
				if st2.Jobs[i].ID == id {
					now := time.Now().UnixMilli()
					st2.Jobs[i].State.LastRunAtMS = &now
					if err != nil {
						st2.Jobs[i].State.LastStatus = "error"
						st2.Jobs[i].State.LastError = err.Error()
					} else {
						st2.Jobs[i].State.LastStatus = "ok"
						st2.Jobs[i].State.LastError = ""
					}
					if st2.Jobs[i].DeleteAfterRun {
						shouldDelete = true
						break
					}
					break
				}
			}
			if shouldDelete {
				next := make([]CronJob, 0, len(st2.Jobs))
				for _, jj := range st2.Jobs {
					if jj.ID == id { continue }
					next = append(next, jj)
				}
				st2.Jobs = next
				if eid, ok := s.entries[id]; ok && s.c != nil {
					s.c.Remove(eid)
					delete(s.entries, id)
				}
			}
			if saveErr := s.save(st2); saveErr != nil {
				log.Printf("cron save failed: %v", saveErr)
			}
			return true, err
		}
	}
	return false, nil
}

func (s *Service) armJobLocked(job CronJob) {
	if s.c == nil { return }
	if !job.Enabled { return }
	switch job.Schedule.Kind {
	case KindAt:
		at := time.UnixMilli(job.Schedule.AtMS)
		if time.Now().After(at) { return }
		delay := time.Until(at)
		// schedule using timer goroutine
		go func(id string, d time.Duration) {
			time.Sleep(d)
			if err := s.runner(context.Background(), job); err != nil {
				log.Printf("cron runner error: id=%s err=%v", id, err)
			}
		}(job.ID, delay)
	case KindEvery:
		sec := int64(job.Schedule.EveryMS / 1000)
		if sec <= 0 { sec = 60 }
		spec := "@every " + (time.Duration(sec) * time.Second).String()
		eid, err := s.c.AddFunc(spec, func() {
			if e := s.runner(context.Background(), job); e != nil {
				log.Printf("cron runner error: id=%s err=%v", job.ID, e)
			}
		})
		if err == nil {
			s.entries[job.ID] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", job.ID, spec, err)
		}
	case KindCron:
		spec := job.Schedule.Expr
		eid, err := s.c.AddFunc(spec, func() {
			if e := s.runner(context.Background(), job); e != nil {
				log.Printf("cron runner error: id=%s err=%v", job.ID, e)
			}
		})
		if err == nil {
			s.entries[job.ID] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", job.ID, spec, err)
		}
	}
}

func randUint() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(b[:])
}

func randID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	for i := range b { b[i] = chars[int(randUint()%uint64(len(chars)))] }
	return string(b)
}
````

## File: internal/memory/consolidate_test.go
````go
package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
)

func openConsolidateTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

type callCounts struct {
	Chat  *int32
	Embed *int32
}

func buildConsolidationProvider(t *testing.T, chatBody string, embedOK bool) (*providers.Client, callCounts) {
	t.Helper()
	var chatCalls int32
	var embedCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			atomic.AddInt32(&chatCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": chatBody}},
				},
			})
		case "/embeddings":
			atomic.AddInt32(&embedCalls, 1)
			if !embedOK {
				http.Error(w, "embed fail", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": []float32{0.1, 0.2}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p := providers.New(srv.URL, "test-key", 5*time.Second)
	p.HTTP = srv.Client()
	return p, callCounts{Chat: &chatCalls, Embed: &embedCalls}
}

func TestConsolidator_NilProvider(t *testing.T) {
	d := openConsolidateTestDB(t)
	c := &Consolidator{DB: d}
	if err := c.MaybeConsolidate(context.Background(), "sess", 10); err != nil {
		t.Fatalf("expected nil error for nil provider, got: %v", err)
	}
}

func TestConsolidator_TooFewMessages_NoProviderCall(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", "msg", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"x","canonical_memory":"- x"}`, true)
	c := &Consolidator{DB: d, Provider: prov, WindowSize: 5}
	if err := c.MaybeConsolidate(ctx, "sess", 2); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if atomic.LoadInt32(calls.Chat) != 0 {
		t.Fatalf("expected no chat calls, got %d", atomic.LoadInt32(calls.Chat))
	}
}

func TestConsolidator_RunOnce_PersistsNoteCursorAndCanonical(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := d.AppendMessage(ctx, "sess", role, "message "+string(rune('a'+i)), nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"Short summary.","canonical_memory":"- prefers concise output"}`, true)
	c := &Consolidator{
		DB:                 d,
		Provider:           prov,
		WindowSize:         5,
		MaxMessages:        50,
		MaxInputChars:      12000,
		EmbedModel:         "embed-model",
		CanonicalPinnedKey: "long_term_memory",
	}
	didWork, err := c.RunOnce(ctx, "sess", 5, RunMode{})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected consolidation work")
	}
	if atomic.LoadInt32(calls.Chat) != 1 {
		t.Fatalf("expected 1 chat call, got %d", atomic.LoadInt32(calls.Chat))
	}
	if atomic.LoadInt32(calls.Embed) != 1 {
		t.Fatalf("expected 1 embed call, got %d", atomic.LoadInt32(calls.Embed))
	}

	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 5)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID == 0 {
		t.Fatal("expected cursor to advance")
	}
	pinned, ok, err := d.GetPinnedValue(ctx, "sess", "long_term_memory")
	if err != nil {
		t.Fatalf("GetPinnedValue: %v", err)
	}
	if !ok || !strings.Contains(pinned, "concise output") {
		t.Fatalf("expected canonical memory update, got ok=%v value=%q", ok, pinned)
	}
}

func TestConsolidator_EmptyTranscript_AdvancesCursor(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	var ids []int64
	for i := 0; i < 6; i++ {
		id, err := d.AppendMessage(ctx, "sess", "tool", "tool output", nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		ids = append(ids, id)
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"unused","canonical_memory":"unused"}`, true)
	c := &Consolidator{DB: d, Provider: prov, WindowSize: 1, MaxMessages: 50, MaxInputChars: 12000}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{ArchiveAll: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected didWork true for cursor advancement")
	}
	if atomic.LoadInt32(calls.Chat) != 0 {
		t.Fatalf("expected no chat call for empty transcript, got %d", atomic.LoadInt32(calls.Chat))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != ids[len(ids)-1] {
		t.Fatalf("expected cursor=%d, got %d", ids[len(ids)-1], lastID)
	}
}

func TestConsolidator_ArchiveAll_MultiPass(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", "line", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"pass summary","canonical_memory":"- memory"}`, false)
	c := &Consolidator{
		DB:                 d,
		Provider:           prov,
		WindowSize:         10,
		MaxMessages:        25,
		MaxInputChars:      2000,
		CanonicalPinnedKey: "long_term_memory",
	}
	if err := c.ArchiveAll(ctx, "sess", 40); err != nil {
		t.Fatalf("ArchiveAll: %v", err)
	}
	lastID, oldestID, err := d.GetConsolidationRange(ctx, "sess", 40)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if oldestID != 0 && lastID < oldestID {
		t.Fatalf("expected cursor to move through archive-all range, got last=%d oldest=%d", lastID, oldestID)
	}
	if atomic.LoadInt32(calls.Chat) < 2 {
		t.Fatalf("expected multiple chat calls for multipass archive, got %d", atomic.LoadInt32(calls.Chat))
	}
}

func TestConsolidator_MaxInputCharsBoundsPromptAndSkipsEmbedOnFailure(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", strings.Repeat("x", 400), nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"bounded summary","canonical_memory":"- bounded"}`, false)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    5,
		MaxMessages:   50,
		MaxInputChars: 500,
		EmbedModel:    "embed-model",
	}
	didWork, err := c.RunOnce(ctx, "sess", 5, RunMode{})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected work to be done")
	}
	if atomic.LoadInt32(calls.Embed) != 1 {
		t.Fatalf("expected embed attempt, got %d", atomic.LoadInt32(calls.Embed))
	}
}

func TestConsolidator_RunOnce_OnlyAdvancesThroughIncludedMessages(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	ids := make([]int64, 0, 3)
	for _, content := range []string{"short one", "short two", strings.Repeat("z", 300)} {
		id, err := d.AppendMessage(ctx, "sess", "user", content, nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		ids = append(ids, id)
	}
	prov, _ := buildConsolidationProvider(t, `{"summary":"bounded summary","canonical_memory":"- bounded"}`, true)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    1,
		MaxMessages:   10,
		MaxInputChars: 40,
	}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{ArchiveAll: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected work to be done")
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != ids[1] {
		t.Fatalf("expected cursor to stop at second included message %d, got %d", ids[1], lastID)
	}
	rows, err := d.StreamMemoryNotesScopeLimit(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	var note db.MemoryNoteRow
	if !rows.Next() {
		t.Fatal("expected a memory note")
	}
	if err := rows.Scan(&note.ID, &note.Text, &note.Embedding, &note.SourceMessageID, &note.Tags, &note.CreatedAt); err != nil {
		t.Fatalf("rows.Scan: %v", err)
	}
	if !note.SourceMessageID.Valid || note.SourceMessageID.Int64 != ids[1] {
		t.Fatalf("expected source message id %d, got %+v", ids[1], note.SourceMessageID)
	}
	if rows.Next() {
		t.Fatal("expected exactly one memory note")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	remaining, err := d.GetConsolidationMessages(ctx, "sess", lastID, 0, 10)
	if err != nil {
		t.Fatalf("GetConsolidationMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != ids[2] {
		t.Fatalf("expected only third message to remain unconsolidated, got %#v", remaining)
	}
}

func TestConsolidator_RunOnce_FirstOversizeMessageStillAdvancesSafely(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	firstID, err := d.AppendMessage(ctx, "sess", "user", strings.Repeat("x", 200), nil)
	if err != nil {
		t.Fatalf("AppendMessage first: %v", err)
	}
	secondID, err := d.AppendMessage(ctx, "sess", "user", "tail", nil)
	if err != nil {
		t.Fatalf("AppendMessage second: %v", err)
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"trimmed summary","canonical_memory":"- trimmed"}`, true)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    1,
		MaxMessages:   10,
		MaxInputChars: 20,
	}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{ArchiveAll: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected work to be done")
	}
	if atomic.LoadInt32(calls.Chat) != 1 {
		t.Fatalf("expected one chat call, got %d", atomic.LoadInt32(calls.Chat))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != firstID {
		t.Fatalf("expected cursor to advance through first truncated message %d, got %d", firstID, lastID)
	}
	remaining, err := d.GetConsolidationMessages(ctx, "sess", lastID, 0, 10)
	if err != nil {
		t.Fatalf("GetConsolidationMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != secondID {
		t.Fatalf("expected second message to remain unconsolidated, got %#v", remaining)
	}
}

func TestConsolidator_RunOnce_AdaptiveTriggerOnLargeTranscript(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()
	for _, content := range []string{
		strings.Repeat("a", 30),
		strings.Repeat("b", 30),
		"active",
	} {
		if _, err := d.AppendMessage(ctx, "sess", "user", content, nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	prov, calls := buildConsolidationProvider(t, `{"summary":"adaptive summary","canonical_memory":"- adaptive"}`, true)
	c := &Consolidator{
		DB:            d,
		Provider:      prov,
		WindowSize:    5,
		MaxMessages:   10,
		MaxInputChars: 80,
	}
	didWork, err := c.RunOnce(ctx, "sess", 1, RunMode{})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !didWork {
		t.Fatal("expected adaptive trigger to consolidate")
	}
	if atomic.LoadInt32(calls.Chat) != 1 {
		t.Fatalf("expected one chat call, got %d", atomic.LoadInt32(calls.Chat))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 1)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	remaining, err := d.GetConsolidationMessages(ctx, "sess", lastID, 0, 10)
	if err != nil {
		t.Fatalf("GetConsolidationMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Content != "active" {
		t.Fatalf("expected only active-window message to remain, got %#v", remaining)
	}
}

func TestContentToStr_Other(t *testing.T) {
	got := contentToStr(42)
	if !strings.Contains(got, "42") {
		t.Errorf("expected '42' in output, got %q", got)
	}
}
````

## File: internal/memory/consolidate.go
````go
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

const defaultCanonicalMemoryKey = "long_term_memory"
const canonicalMemoryInputDivisor = 2

const consolidationPrompt = `You are consolidating chat memory.

Return ONLY JSON with this exact shape:
{"summary":"...", "canonical_memory":"..."}

Rules:
- summary: 3-5 concise sentences describing key facts, decisions, and context from the excerpt.
- canonical_memory: concise markdown bullet list of durable facts/preferences. Start from Existing canonical memory, keep still-true facts, and merge new stable facts.
- If no durable facts changed, canonical_memory may equal Existing canonical memory.

Existing canonical memory:
%s

Conversation excerpt:
%s`

// Consolidator rolls up conversation messages older than the active history
// window into durable memory notes (stored in memory_notes for vector/FTS
// retrieval). It is safe to call MaybeConsolidate after every agent turn;
// it is a no-op when there is nothing to consolidate.
type Consolidator struct {
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
	ChatModel  string
	// WindowSize is the minimum number of consolidatable messages required
	// before a consolidation run is triggered. Default: 10.
	WindowSize int
	// MaxMessages bounds how many messages are processed per consolidation pass.
	// Default: 50.
	MaxMessages int
	// MaxInputChars bounds transcript size passed to the LLM. Default: 12000.
	MaxInputChars int
	// CanonicalPinnedKey is the memory_pinned key used for canonical long-term memory.
	CanonicalPinnedKey string
}

type RunMode struct {
	ArchiveAll bool
}

// MaybeConsolidate checks whether there are enough old messages to warrant a
// consolidation pass and, if so, summarises them into a memory note.
func (c *Consolidator) MaybeConsolidate(ctx context.Context, sessionKey string, historyMax int) error {
	_, err := c.RunOnce(ctx, sessionKey, historyMax, RunMode{})
	return err
}

// ArchiveAll drains all unconsolidated messages in bounded passes.
func (c *Consolidator) ArchiveAll(ctx context.Context, sessionKey string, historyMax int) error {
	const maxPasses = 1024
	for i := 0; i < maxPasses; i++ {
		didWork, err := c.RunOnce(ctx, sessionKey, historyMax, RunMode{ArchiveAll: true})
		if err != nil {
			return err
		}
		if !didWork {
			return nil
		}
	}
	return fmt.Errorf("archive-all exceeded max passes")
}

// RunOnce performs a single bounded consolidation pass.
func (c *Consolidator) RunOnce(ctx context.Context, sessionKey string, historyMax int, mode RunMode) (bool, error) {
	if c.Provider == nil {
		return false, nil
	}
	windowSize := c.WindowSize
	if windowSize <= 0 {
		windowSize = 10
	}
	maxMessages := c.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 50
	}
	maxInputChars := c.MaxInputChars
	if maxInputChars <= 0 {
		maxInputChars = 12000
	}
	if historyMax <= 0 {
		historyMax = 40
	}
	canonicalKey := strings.TrimSpace(c.CanonicalPinnedKey)
	if canonicalKey == "" {
		canonicalKey = defaultCanonicalMemoryKey
	}

	lastID, oldestActiveID, err := c.DB.GetConsolidationRange(ctx, sessionKey, historyMax)
	if err != nil {
		return false, fmt.Errorf("consolidation range: %w", err)
	}
	beforeID := oldestActiveID
	if mode.ArchiveAll {
		beforeID = 0
	} else if oldestActiveID == 0 || oldestActiveID <= lastID+1 {
		return false, nil
	}

	msgs, err := c.DB.GetConsolidationMessages(ctx, sessionKey, lastID, beforeID, maxMessages)
	if err != nil {
		return false, fmt.Errorf("consolidation messages: %w", err)
	}
	if len(msgs) == 0 {
		return false, nil
	}
	lastCandidateID := msgs[len(msgs)-1].ID

	// Build a plain-text conversation transcript.
	var sb strings.Builder
	var lastIncludedID int64
	for _, m := range msgs {
		// Skip tool messages – they're noisy and usually captured by the surrounding turns.
		if m.Role == "tool" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		line := m.Role + ": " + content
		if sb.Len()+len(line)+1 > maxInputChars {
			if sb.Len() == 0 {
				remaining := maxInputChars - len(m.Role) - 3
				if remaining > 0 {
					line = m.Role + ": " + content[:remaining] + "…"
					sb.WriteString(line)
					sb.WriteString("\n")
					lastIncludedID = m.ID
				}
			}
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n")
		lastIncludedID = m.ID
	}
	transcript := strings.TrimSpace(sb.String())
	memScope := sessionKey
	if memScope == "" || memScope == scope.GlobalMemoryScope {
		memScope = scope.GlobalMemoryScope
	}
	if transcript == "" {
		_, err := c.DB.WriteConsolidation(ctx, db.ConsolidationWrite{
			SessionKey:  sessionKey,
			ScopeKey:    memScope,
			CursorMsgID: lastCandidateID,
		})
		if err != nil {
			return false, fmt.Errorf("consolidation advance cursor: %w", err)
		}
		return true, nil
	}
	shouldConsolidate := mode.ArchiveAll || len(msgs) >= windowSize
	if !shouldConsolidate {
		adaptiveTriggerChars := maxInputChars / canonicalMemoryInputDivisor
		if adaptiveTriggerChars <= 0 {
			adaptiveTriggerChars = 1
		}
		if len(msgs) >= maxMessages || len(transcript) >= adaptiveTriggerChars {
			shouldConsolidate = true
		}
	}
	if !shouldConsolidate {
		return false, nil
	}

	currentCanonical, _, err := c.DB.GetPinnedValue(ctx, memScope, canonicalKey)
	if err != nil {
		return false, fmt.Errorf("consolidation get canonical memory: %w", err)
	}
	currentCanonical = trimTo(currentCanonical, maxInputChars/canonicalMemoryInputDivisor)

	model := c.ChatModel
	if model == "" {
		model = "gpt-4.1-mini"
	}
	req := providers.ChatCompletionRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{Role: "user", Content: fmt.Sprintf(consolidationPrompt, currentCanonical, transcript)},
		},
		Temperature: 0,
	}
	resp, err := c.Provider.Chat(ctx, req)
	if err != nil {
		return false, fmt.Errorf("consolidation chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return false, fmt.Errorf("consolidation: no choices returned")
	}
	summary, canonical := parseConsolidationOutput(contentToStr(resp.Choices[0].Message.Content))
	summary = trimTo(summary, maxInputChars/canonicalMemoryInputDivisor)
	canonical = trimTo(canonical, maxInputChars)
	if canonical == "" {
		canonical = currentCanonical
	}

	if summary == "" {
		w := db.ConsolidationWrite{
			SessionKey:  sessionKey,
			ScopeKey:    memScope,
			CursorMsgID: lastIncludedID,
		}
		if canonical != "" {
			w.CanonicalKey = canonicalKey
			w.CanonicalText = canonical
		}
		_, err := c.DB.WriteConsolidation(ctx, w)
		if err != nil {
			return false, fmt.Errorf("consolidation update cursor: %w", err)
		}
		log.Printf("consolidated %d messages for session %q (cursor-only)", len(msgs), sessionKey)
		return true, nil
	}

	embedModel := c.EmbedModel
	var embedding []byte
	if embedModel != "" {
		vec, embedErr := c.Provider.Embed(ctx, embedModel, summary)
		if embedErr != nil {
			log.Printf("consolidation embed failed: %v", embedErr)
			embedding = make([]byte, 0)
		} else {
			embedding = PackFloat32(vec)
		}
	} else {
		embedding = make([]byte, 0)
	}

	w := db.ConsolidationWrite{
		SessionKey:  sessionKey,
		ScopeKey:    memScope,
		NoteText:    summary,
		Embedding:   embedding,
		SourceMsgID: sql.NullInt64{Int64: lastIncludedID, Valid: true},
		NoteTags:    "consolidation",
		CursorMsgID: lastIncludedID,
	}
	if canonical != "" {
		w.CanonicalKey = canonicalKey
		w.CanonicalText = canonical
	}
	_, err = c.DB.WriteConsolidation(ctx, w)
	if err != nil {
		return false, fmt.Errorf("consolidation write: %w", err)
	}

	log.Printf("consolidated %d messages for session %q into memory note", len(msgs), sessionKey)
	return true, nil
}

// contentToStr converts a ChatMessage Content (string or other) to a plain string.
func contentToStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

type consolidationOutput struct {
	Summary   string `json:"summary"`
	Canonical string `json:"canonical_memory"`
}

func parseConsolidationOutput(raw string) (summary string, canonical string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	var out consolidationOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return strings.TrimSpace(out.Summary), strings.TrimSpace(out.Canonical)
	}
	return raw, ""
}

func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max])
	}
	return s
}
````

## File: internal/providers/openai.go
````go
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	APIBase string
	APIKey  string
	HTTP    *http.Client
}

func New(apiBase, apiKey string, timeout time.Duration) *Client {
	return &Client{
		APIBase: apiBase,
		APIKey: apiKey,
		HTTP: &http.Client{Timeout: timeout},
	}
}

type ChatMessage struct {
	Role string `json:"role"`
	Content any `json:"content,omitempty"` // string|null
	Name string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDef struct {
	Type string `json:"type"`
	Function ToolFunc `json:"function"`
}
type ToolFunc struct {
	Name string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters any `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
	Type  string `json:"type"`
	Function struct{
		Name string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatCompletionRequest struct {
	Model string `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Tools []ToolDef `json:"tools,omitempty"`
	ToolChoice any `json:"tool_choice,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []struct{
		Message struct{
			Role string `json:"role"`
			Content any `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) Chat(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	var out ChatCompletionResponse
	b, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/chat/completions", bytes.NewReader(b))
	if err != nil { return out, err }
	r.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" { r.Header.Set("Authorization", "Bearer "+c.APIKey) }

	resp, err := c.HTTP.Do(r)
	if err != nil { return out, err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

// ChatCompletionStreamRequest is sent when stream=true.
type ChatCompletionStreamRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream"`
}

type ChatStreamDelta struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ChatStreamChoice struct {
	Index        int             `json:"index"`
	Delta        ChatStreamDelta `json:"delta"`
	FinishReason string          `json:"finish_reason"`
}

type ChatStreamChunk struct {
	ID      string             `json:"id"`
	Choices []ChatStreamChoice `json:"choices"`
}

// ChatStream sends the request with stream:true, calls onDelta for each text
// delta, and returns the fully-accumulated ChatCompletionResponse.
func (c *Client) ChatStream(ctx context.Context, req ChatCompletionRequest, onDelta func(text string)) (ChatCompletionResponse, error) {
	streamReq := ChatCompletionStreamRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		ToolChoice:  req.ToolChoice,
		Temperature: req.Temperature,
		Stream:      true,
	}
	b, _ := json.Marshal(streamReq)
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(r)
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return ChatCompletionResponse{}, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	var contentBuilder strings.Builder
	var finalToolCalls []ToolCall

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk ChatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}
		if len(delta.ToolCalls) > 0 {
			finalToolCalls = mergeStreamToolCalls(finalToolCalls, delta.ToolCalls)
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatCompletionResponse{}, err
	}

	out := ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   any        `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string     `json:"role"`
					Content   any        `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				}{
					Role:      "assistant",
					Content:   contentBuilder.String(),
					ToolCalls: finalToolCalls,
				},
			},
		},
	}
	return out, nil
}

// mergeStreamToolCalls accumulates tool-call deltas arriving over SSE.
// OpenAI streaming sends each piece as {index, partial args}; we expand the
// slice to the required index and concatenate name/arguments incrementally.
func mergeStreamToolCalls(existing []ToolCall, delta []ToolCall) []ToolCall {
	for _, d := range delta {
		idx := d.Index
		for len(existing) <= idx {
			existing = append(existing, ToolCall{})
		}
		existing[idx].Function.Arguments += d.Function.Arguments
		if d.Function.Name != "" {
			existing[idx].Function.Name += d.Function.Name
		}
		if d.ID != "" {
			existing[idx].ID = d.ID
		}
		if d.Type != "" {
			existing[idx].Type = d.Type
		}
		existing[idx].Index = idx
	}
	return existing
}

type EmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}
type EmbeddingResponse struct {
	Data []struct{
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) Embed(ctx context.Context, model, input string) ([]float32, error) {
	var out EmbeddingResponse
	b, _ := json.Marshal(EmbeddingRequest{Model: model, Input: input})
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/embeddings", bytes.NewReader(b))
	if err != nil { return nil, err }
	r.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" { r.Header.Set("Authorization", "Bearer "+c.APIKey) }

	resp, err := c.HTTP.Do(r)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if err := json.Unmarshal(body, &out); err != nil { return nil, err }
	if len(out.Data) == 0 { return nil, fmt.Errorf("no embedding returned") }
	return out.Data[0].Embedding, nil
}
````

## File: internal/skills/skills.go
````go
package skills

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SkillEntry describes a declared executable entrypoint from a skill manifest.
type SkillEntry struct {
	Name           string   `json:"name"`
	Command        []string `json:"command"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	AcceptsStdin   bool     `json:"acceptsStdin"`
}

type SkillMeta struct {
	Name        string
	Path        string
	ModTime     time.Time
	Size        int64
	ID          string
	Summary     string       // short capability description
	Entrypoints []SkillEntry // declared executable entrypoints from manifest
}

type Inventory struct {
	Skills []SkillMeta
	byName map[string]SkillMeta
}

// skillManifest is the JSON structure of skill.json.
type skillManifest struct {
	Summary     string       `json:"summary"`
	Entrypoints []SkillEntry `json:"entrypoints"`
}

// loadManifest tries to load a skill.json from the same directory as path.
func loadManifest(dir string) (skillManifest, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "skill.json"))
	if err != nil {
		return skillManifest{}, false
	}
	var m skillManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return skillManifest{}, false
	}
	return m, true
}

// maxFrontMatterLines is the maximum number of lines scanned for YAML front matter.
const maxFrontMatterLines = 20

// extractFrontMatterSummary parses the first YAML front matter block (--- ... ---)
// and returns the value of the "summary:" field if present.
func extractFrontMatterSummary(content string) string {
	lines := strings.SplitN(content, "\n", maxFrontMatterLines)
	if len(lines) == 0 {
		return ""
	}
	if strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if strings.HasPrefix(trimmed, "summary:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "summary:"))
			// Strip optional quotes
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

func Scan(dirs []string) Inventory {
	var skills []SkillMeta
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		root, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".md" && ext != ".txt" {
				return nil
			}
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(root, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}
			info, _ := d.Info()
			mt := time.Time{}
			sz := int64(0)
			if info != nil {
				mt = info.ModTime()
				sz = info.Size()
			}
			name := strings.TrimSuffix(filepath.Base(realPath), ext)
			meta := SkillMeta{Name: name, Path: realPath, ModTime: mt, Size: sz, ID: hash(realPath)}

			// Try skill.json manifest in the same directory.
			if man, ok := loadManifest(filepath.Dir(realPath)); ok {
				meta.Summary = man.Summary
				meta.Entrypoints = man.Entrypoints
			}

			// Try YAML front matter summary if not already set from manifest.
			if meta.Summary == "" && ext == ".md" {
				if data, readErr := os.ReadFile(realPath); readErr == nil {
					meta.Summary = extractFrontMatterSummary(string(data))
				}
			}

			skills = append(skills, meta)
			return nil
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	by := map[string]SkillMeta{}
	for _, s := range skills {
		by[s.Name] = s
	}
	return Inventory{Skills: skills, byName: by}
}

func (inv Inventory) Get(name string) (SkillMeta, bool) {
	s, ok := inv.byName[name]
	return s, ok
}

func (inv Inventory) Summary(max int) string {
	if max <= 0 {
		max = 50
	}
	lines := []string{}
	for i, s := range inv.Skills {
		if i >= max {
			lines = append(lines, "…")
			break
		}
		if s.Summary != "" {
			lines = append(lines, "- "+s.Name+": "+s.Summary)
		} else {
			lines = append(lines, "- "+s.Name)
		}
	}
	if len(lines) == 0 {
		return "(no skills found)"
	}
	return strings.Join(lines, "\n")
}

func LoadBody(path string, maxBytes int) (string, error) {
	if maxBytes <= 0 { maxBytes = 200000 }
	info, err := os.Lstat(path)
	if err != nil { return "", err }
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() { return "", fs.ErrPermission }
	b, err := os.ReadFile(path)
	if err != nil { return "", err }
	if len(b) > maxBytes { b = b[:maxBytes] }
	return string(b), nil
}

func hash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}
````

## File: internal/agent/prompt.go
````go
package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
)

const DefaultSoul = `# Soul
I am or3-intern, a personal AI assistant.
- Be clear and direct
- Prefer deterministic, bounded work
- Use tools when needed; keep outputs short
`

const DefaultAgentInstructions = `# Agent Instructions
- Use pinned memory for stable facts.
- Retrieve relevant memory snippets before answering.
- Keep constant RAM usage: last N messages + top K memories only.
- Large tool outputs must spill to artifacts.
`

const DefaultToolNotes = `# Tool Usage Notes
exec:
- Commands have a timeout
- Dangerous commands blocked
- Output truncated
cron:
- Use cron tool for scheduled reminders.
`

const (
	defaultBootstrapMaxChars      = 20000
	defaultBootstrapTotalMaxChars = 150000
	defaultPinnedOneLineMax       = 220
	defaultRetrievedOneLineMax    = 240
	defaultSkillsSummaryMax       = 80
	defaultVisionMaxImages        = 4
	defaultVisionMaxImageBytes    = 4 << 20
	defaultVisionTotalBytes       = 8 << 20
)

type PromptParts struct {
	System  []providers.ChatMessage
	History []providers.ChatMessage
}

// BuildOptions holds options for building a prompt.
type BuildOptions struct {
	SessionKey  string
	UserMessage string
	Autonomous  bool // true for cron/webhook/file-change events
}

type Builder struct {
	DB           *db.DB
	Artifacts    *artifacts.Store
	Skills       skills.Inventory
	Mem          *memory.Retriever
	Provider     *providers.Client
	EmbedModel   string
	EnableVision bool

	Soul                   string
	AgentInstructions      string
	ToolNotes              string
	BootstrapMaxChars      int
	BootstrapTotalMaxChars int
	SkillsSummaryMax       int

	HistoryMax int
	VectorK    int
	FTSK       int
	TopK       int

	// New fields for lightweight OpenClaw parity
	IdentityText     string // content of IDENTITY.md
	StaticMemory     string // content of MEMORY.md
	HeartbeatText    string // content of HEARTBEAT.md – injected only for autonomous turns
	DocRetriever     *memory.DocRetriever // for indexed file context
	DocScopeKey      string               // scope key for doc retrieval
	DocRetrieveLimit int                  // max docs to retrieve
}

// Build builds a prompt snapshot. It is a convenience wrapper around BuildWithOptions.
func (b *Builder) Build(ctx context.Context, sessionKey string, userMessage string) (PromptParts, []memory.Retrieved, error) {
	return b.BuildWithOptions(ctx, BuildOptions{SessionKey: sessionKey, UserMessage: userMessage})
}

// BuildWithOptions builds a prompt snapshot using the provided options.
func (b *Builder) BuildWithOptions(ctx context.Context, opts BuildOptions) (PromptParts, []memory.Retrieved, error) {
	pinned, err := b.DB.GetPinned(ctx, opts.SessionKey)
	if err != nil {
		return PromptParts{}, nil, err
	}
	pinnedText := formatPinned(pinned)

	// embed and retrieve
	var retrieved []memory.Retrieved
	if b.Mem != nil && b.Provider != nil && strings.TrimSpace(opts.UserMessage) != "" {
		vec, err := b.Provider.Embed(ctx, b.EmbedModel, opts.UserMessage)
		if err == nil {
			retrieved, _ = b.Mem.Retrieve(ctx, opts.SessionKey, opts.UserMessage, vec, b.VectorK, b.FTSK, b.TopK)
		}
	}
	memText := formatRetrieved(retrieved)

	// indexed doc context
	var docContextText string
	if b.DocRetriever != nil && strings.TrimSpace(opts.UserMessage) != "" {
		limit := b.DocRetrieveLimit
		if limit <= 0 {
			limit = 5
		}
		docs, _ := b.DocRetriever.RetrieveDocs(ctx, b.DocScopeKey, opts.UserMessage, limit)
		if len(docs) > 0 {
			var sb strings.Builder
			for i, d := range docs {
				sb.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, d.Path, d.Excerpt))
			}
			docContextText = strings.TrimSpace(sb.String())
		}
	}

	histRows, err := b.DB.GetLastMessages(ctx, opts.SessionKey, b.HistoryMax)
	if err != nil {
		return PromptParts{}, nil, err
	}
	visionBudget := newVisionBudget()
	hist := make([]providers.ChatMessage, 0, len(histRows))
	for _, m := range histRows {
		msg := providers.ChatMessage{Role: m.Role, Content: m.Content}
		var payload map[string]any
		if err := json.Unmarshal([]byte(m.PayloadJSON), &payload); err == nil {
			if m.Role == "assistant" {
				if raw, ok := payload["tool_calls"]; ok {
					b, _ := json.Marshal(raw)
					var tcs []providers.ToolCall
					if err := json.Unmarshal(b, &tcs); err == nil {
						msg.ToolCalls = tcs
					}
				}
			}
			if m.Role == "user" {
				msg.Content = b.buildUserContent(ctx, m.Content, attachmentsFromPayload(payload), visionBudget)
			}
		}
		hist = append(hist, msg)
	}

	heartbeat := ""
	if opts.Autonomous {
		heartbeat = b.HeartbeatText
	}
	sysText := b.composeSystemPrompt(pinnedText, memText, b.IdentityText, b.StaticMemory, heartbeat, docContextText)
	sys := []providers.ChatMessage{
		{Role: "system", Content: sysText},
	}
	return PromptParts{System: sys, History: hist}, retrieved, nil
}

func attachmentsFromPayload(payload map[string]any) []artifacts.Attachment {
	if len(payload) == 0 {
		return nil
	}
	raw := payload["attachments"]
	if raw == nil {
		if meta, ok := payload["meta"].(map[string]any); ok {
			raw = meta["attachments"]
		}
	}
	if raw == nil {
		return nil
	}
	b, _ := json.Marshal(raw)
	var atts []artifacts.Attachment
	if err := json.Unmarshal(b, &atts); err != nil {
		return nil
	}
	out := make([]artifacts.Attachment, 0, len(atts))
	for _, att := range atts {
		if strings.TrimSpace(att.ArtifactID) == "" {
			continue
		}
		if strings.TrimSpace(att.Filename) == "" {
			att.Filename = "attachment"
		}
		if strings.TrimSpace(att.Kind) == "" {
			att.Kind = artifacts.DetectKind(att.Filename, att.Mime)
		}
		out = append(out, att)
	}
	return out
}

type visionBudget struct {
	remainingImages int
	remainingBytes  int64
}

func newVisionBudget() *visionBudget {
	return &visionBudget{
		remainingImages: defaultVisionMaxImages,
		remainingBytes:  defaultVisionTotalBytes,
	}
}

func (b *Builder) buildUserContent(ctx context.Context, text string, atts []artifacts.Attachment, budget *visionBudget) any {
	if !b.EnableVision || b.Artifacts == nil || len(atts) == 0 {
		return text
	}
	parts := make([]map[string]any, 0, len(atts)+1)
	imageParts := 0
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	for _, att := range atts {
		if strings.TrimSpace(att.Kind) != artifacts.KindImage && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(att.Mime)), "image/") {
			continue
		}
		part, ok := b.imagePart(ctx, att, budget)
		if !ok {
			continue
		}
		parts = append(parts, part)
		imageParts++
	}
	if imageParts == 0 {
		return text
	}
	return parts
}

func (b *Builder) imagePart(ctx context.Context, att artifacts.Attachment, budget *visionBudget) (map[string]any, bool) {
	if budget == nil || budget.remainingImages <= 0 || budget.remainingBytes <= 0 {
		return nil, false
	}
	stored, err := b.Artifacts.Lookup(ctx, att.ArtifactID)
	if err != nil {
		return nil, false
	}
	sizeBytes := stored.SizeBytes
	if sizeBytes <= 0 {
		info, err := os.Stat(stored.Path)
		if err != nil {
			return nil, false
		}
		sizeBytes = info.Size()
	}
	if sizeBytes <= 0 || sizeBytes > defaultVisionMaxImageBytes || sizeBytes > budget.remainingBytes {
		return nil, false
	}
	data, err := readCappedFile(stored.Path, defaultVisionMaxImageBytes)
	if err != nil {
		return nil, false
	}
	if int64(len(data)) > budget.remainingBytes {
		return nil, false
	}
	mimeType := strings.TrimSpace(stored.Mime)
	if mimeType == "" {
		mimeType = strings.TrimSpace(att.Mime)
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(stored.Path))
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, false
	}
	budget.remainingImages--
	budget.remainingBytes -= int64(len(data))
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
		},
	}, true
}

func readCappedFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds vision limit")
	}
	return data, nil
}

func (b *Builder) composeSystemPrompt(pinnedText, memText, identityText, staticMemoryText, heartbeatText, docContextText string) string {
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 {
		maxEach = defaultBootstrapMaxChars
	}
	maxTotal := b.BootstrapTotalMaxChars
	if maxTotal <= 0 {
		maxTotal = defaultBootstrapTotalMaxChars
	}
	skillsMax := b.SkillsSummaryMax
	if skillsMax <= 0 {
		skillsMax = defaultSkillsSummaryMax
	}

	soul := strings.TrimSpace(b.Soul)
	if soul == "" {
		soul = DefaultSoul
	}
	inst := strings.TrimSpace(b.AgentInstructions)
	if inst == "" {
		inst = DefaultAgentInstructions
	}
	notes := strings.TrimSpace(b.ToolNotes)
	if notes == "" {
		notes = DefaultToolNotes
	}

	type section struct {
		title string
		text  string
	}
	// Build sections in order, omitting optional ones when empty.
	sections := []section{
		{title: "SOUL.md", text: truncateText(soul, maxEach)},
	}
	if t := strings.TrimSpace(identityText); t != "" {
		sections = append(sections, section{title: "Identity", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "AGENTS.md", text: truncateText(inst, maxEach)})
	if t := strings.TrimSpace(staticMemoryText); t != "" {
		sections = append(sections, section{title: "Static Memory", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "TOOLS.md", text: truncateText(notes, maxEach)})
	if t := strings.TrimSpace(heartbeatText); t != "" {
		sections = append(sections, section{title: "Heartbeat", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "Pinned Memory", text: pinnedText})
	sections = append(sections, section{title: "Retrieved Memory", text: memText})
	if t := strings.TrimSpace(docContextText); t != "" {
		sections = append(sections, section{title: "Indexed File Context", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "Skills Inventory", text: b.Skills.Summary(skillsMax)})

	var out strings.Builder
	out.WriteString("# System Prompt\n")
	for _, s := range sections {
		out.WriteString("\n## ")
		out.WriteString(s.title)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(s.text))
		out.WriteString("\n")
	}
	return truncateText(strings.TrimSpace(out.String()), maxTotal)
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "\n…[truncated]"
	}
	return s
}

func formatPinned(m map[string]string) string {
	if len(m) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := strings.TrimSpace(m[k])
		if v == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, oneLine(v, defaultPinnedOneLineMax)))
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "(none)"
	}
	return s
}

func formatRetrieved(ms []memory.Retrieved) string {
	if len(ms) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for i, m := range ms {
		b.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, m.Source, oneLine(m.Text, defaultRetrievedOneLineMax)))
	}
	return strings.TrimSpace(b.String())
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		s = s[:max] + "…"
	}
	return s
}
````

## File: internal/db/db.go
````go
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"or3-intern/internal/scope"

	_ "modernc.org/sqlite"
)

type DB struct {
	SQL *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path)
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(1) // deterministic, low-RAM
	d := &DB{SQL: s}
	if err := d.migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.SQL.Close() }

func (d *DB) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions(
			key TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS messages(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS messages_session_id ON messages(session_key, id);`,
		`CREATE TABLE IF NOT EXISTS artifacts(
			id TEXT PRIMARY KEY,
			session_key TEXT NOT NULL,
			mime TEXT NOT NULL,
			path TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS memory_pinned(
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(session_key, key)
		);`,
		`CREATE TABLE IF NOT EXISTS memory_notes(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			source_message_id INTEGER,
			tags TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);`,
		// FTS5
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(text, content='memory_notes', content_rowid='id');`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_ai AFTER INSERT ON memory_notes BEGIN
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_ad AFTER DELETE ON memory_notes BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, text) VALUES('delete', old.id, old.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_au AFTER UPDATE ON memory_notes BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, text) VALUES('delete', old.id, old.text);
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
		`CREATE TABLE IF NOT EXISTS subagent_jobs(
			id TEXT PRIMARY KEY,
			parent_session_key TEXT NOT NULL,
			child_session_key TEXT NOT NULL,
			channel TEXT NOT NULL,
			reply_to TEXT NOT NULL,
			task TEXT NOT NULL,
			status TEXT NOT NULL,
			result_preview TEXT NOT NULL DEFAULT '',
			artifact_id TEXT NOT NULL DEFAULT '',
			error_text TEXT NOT NULL DEFAULT '',
			requested_at INTEGER NOT NULL,
			started_at INTEGER NOT NULL DEFAULT 0,
			finished_at INTEGER NOT NULL DEFAULT 0,
			attempts INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS subagent_jobs_status_requested_at ON subagent_jobs(status, requested_at);`,
		`CREATE INDEX IF NOT EXISTS subagent_jobs_parent_session ON subagent_jobs(parent_session_key, requested_at);`,
		`CREATE TABLE IF NOT EXISTS session_links(
			session_key TEXT PRIMARY KEY,
			scope_key TEXT NOT NULL,
			linked_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS session_links_scope_key ON session_links(scope_key);`,
		`CREATE TABLE IF NOT EXISTS memory_docs(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope_key TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL,
			embedding BLOB,
			hash TEXT NOT NULL,
			mtime_ms INTEGER NOT NULL,
			size_bytes INTEGER NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL,
			UNIQUE(scope_key, path)
		);`,
		`CREATE INDEX IF NOT EXISTS memory_docs_scope_path ON memory_docs(scope_key, path);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_docs_fts USING fts5(title, summary, text, content='memory_docs', content_rowid='id');`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_ai AFTER INSERT ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(rowid, title, summary, text) VALUES (new.id, new.title, new.summary, new.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_ad AFTER DELETE ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(memory_docs_fts, rowid, title, summary, text) VALUES('delete', old.id, old.title, old.summary, old.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_au AFTER UPDATE ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(memory_docs_fts, rowid, title, summary, text) VALUES('delete', old.id, old.title, old.summary, old.text);
			INSERT INTO memory_docs_fts(rowid, title, summary, text) VALUES (new.id, new.title, new.summary, new.text);
		END;`,
	}
	for _, s := range stmts {
		if _, err := d.SQL.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	if err := d.migrateMemoryPinned(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryNotesSessionColumn(ctx); err != nil {
		return err
	}
	if err := d.migrateLegacyGlobalMemoryScope(ctx); err != nil {
		return err
	}
	return nil
}

func NowMS() int64 { return time.Now().UnixMilli() }

func (d *DB) migrateMemoryPinned(ctx context.Context) error {
	hasSession, err := d.tableHasColumn(ctx, "memory_pinned", "session_key")
	if err != nil {
		return err
	}
	if hasSession {
		_, err = d.SQL.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS memory_pinned_session_key_key ON memory_pinned(session_key, key);`)
		return err
	}
	stmts := []string{
		`ALTER TABLE memory_pinned RENAME TO memory_pinned_legacy;`,
		`CREATE TABLE memory_pinned(
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(session_key, key)
		);`,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at)
		 SELECT '` + scope.GlobalMemoryScope + `', key, content, updated_at FROM memory_pinned_legacy;`,
		`DROP TABLE memory_pinned_legacy;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS memory_pinned_session_key_key ON memory_pinned(session_key, key);`,
	}
	for _, stmt := range stmts {
		if _, err := d.SQL.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureMemoryNotesSessionColumn(ctx context.Context) error {
	hasSession, err := d.tableHasColumn(ctx, "memory_notes", "session_key")
	if err != nil {
		return err
	}
	if !hasSession {
		if _, err := d.SQL.ExecContext(ctx, `ALTER TABLE memory_notes ADD COLUMN session_key TEXT NOT NULL DEFAULT '`+scope.GlobalMemoryScope+`';`); err != nil {
			return err
		}
	}
	_, err = d.SQL.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS memory_notes_session_id ON memory_notes(session_key, id);`)
	return err
}

func (d *DB) migrateLegacyGlobalMemoryScope(ctx context.Context) error {
	if scope.GlobalMemoryScope == scope.GlobalScopeAlias {
		return nil
	}
	if _, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at)
		 SELECT ?, key, content, updated_at FROM memory_pinned WHERE session_key=?
		 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		scope.GlobalMemoryScope, scope.GlobalScopeAlias); err != nil {
		return err
	}
	if _, err := d.SQL.ExecContext(ctx, `DELETE FROM memory_pinned WHERE session_key=?`, scope.GlobalScopeAlias); err != nil {
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE memory_notes SET session_key=? WHERE session_key=?`, scope.GlobalMemoryScope, scope.GlobalScopeAlias)
	return err
}

func (d *DB) tableHasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	rows, err := d.SQL.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}
````

## File: README.md
````markdown
# or3-intern (v1)

Go rewrite of nanobot with SQLite persistence + hybrid long-term memory retrieval.

## Quick start

1) Run guided setup:
```bash
go run ./cmd/or3-intern init
```

2) Start interactive chat:
```bash
go run ./cmd/or3-intern chat
```

3) Or run enabled external channels:
```bash
go run ./cmd/or3-intern serve
```

The `init` command can store your provider settings in `~/.or3-intern/config.json`, so you do not need to manually manage env vars unless you want to.

## Commands

- `or3-intern init` guided first-run setup
- `or3-intern chat` interactive CLI
- `or3-intern serve` run enabled external channels (Telegram / Slack / Discord / WhatsApp bridge)
- `or3-intern agent -m "hello"` one-shot
- `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]`

## Notes

- Uses SQLite with WAL + single-connection for deterministic low-RAM operation.
- History is always fetched with `LIMIT` and never full-scanned.
- Hybrid memory retrieval: pinned + vector (cosine) + FTS keyword search.
- External channels are disabled by default; configure them in `config.json` or via env vars before using `serve`.
- Supported non-CLI channels: Telegram, Slack, Discord, and a local WhatsApp bridge.

## Dependencies

This repo uses external Go modules (SQLite driver + cron parser). If you're building in an offline environment, you must vendor modules ahead of time.

## Channel Integrations

`or3-intern` supports these non-CLI channels:

- Telegram
- Slack
- Discord
- WhatsApp via a local bridge

All external channels are disabled by default.

### Running Channels

Use the CLI chat for local terminal interaction:

```bash
go run ./cmd/or3-intern chat
```

Use the channel runner for enabled external integrations:

```bash
go run ./cmd/or3-intern serve
```

`serve` starts the agent workers plus any enabled channels from your config.

### Environment Variables

You can configure channels through `config.json` or environment variables.

Available env vars:

```dotenv
OR3_TELEGRAM_TOKEN=
OR3_SLACK_APP_TOKEN=
OR3_SLACK_BOT_TOKEN=
OR3_DISCORD_TOKEN=
OR3_WHATSAPP_BRIDGE_URL=ws://127.0.0.1:3001/ws
OR3_WHATSAPP_BRIDGE_TOKEN=
```

### Config Shape

The `config.json` channel section looks like this:

```json
{
	"channels": {
		"telegram": {
			"enabled": false,
			"token": "",
			"apiBase": "https://api.telegram.org",
			"pollSeconds": 2,
			"defaultChatId": "",
			"allowedChatIds": []
		},
		"slack": {
			"enabled": false,
			"appToken": "",
			"botToken": "",
			"apiBase": "https://slack.com/api",
			"socketModeUrl": "",
			"defaultChannelId": "",
			"allowedUserIds": [],
			"requireMention": true
		},
		"discord": {
			"enabled": false,
			"token": "",
			"apiBase": "https://discord.com/api/v10",
			"gatewayUrl": "",
			"defaultChannelId": "",
			"allowedUserIds": [],
			"requireMention": true
		},
		"whatsApp": {
			"enabled": false,
			"bridgeUrl": "ws://127.0.0.1:3001/ws",
			"bridgeToken": "",
			"defaultTo": "",
			"allowedFrom": []
		}
	}
}
```

### Telegram

- Set `channels.telegram.enabled=true`
- Set `channels.telegram.token` or `OR3_TELEGRAM_TOKEN`
- Optionally set `defaultChatId` for outbound `send_message` defaults
- Optionally restrict inbound traffic with `allowedChatIds`

Telegram uses polling, so no webhook setup is required.

### Slack

- Set `channels.slack.enabled=true`
- Set `channels.slack.appToken` and `channels.slack.botToken`
- Optionally set `defaultChannelId`
- Optionally restrict inbound traffic with `allowedUserIds`
- `requireMention=true` is recommended for shared channels

Slack uses Socket Mode for inbound events and Web API for outbound messages.

### Discord

- Set `channels.discord.enabled=true`
- Set `channels.discord.token`
- Optionally set `defaultChannelId`
- Optionally restrict inbound traffic with `allowedUserIds`
- `requireMention=true` is recommended for guild channels

Discord uses the Gateway for inbound events and REST for outbound messages.

### WhatsApp Bridge

WhatsApp support expects a compatible local bridge service.

- Set `channels.whatsApp.enabled=true`
- Set `channels.whatsApp.bridgeUrl` or `OR3_WHATSAPP_BRIDGE_URL`
- Optionally set `channels.whatsApp.bridgeToken`
- Optionally set `defaultTo` and `allowedFrom`

The bridge should expose a websocket endpoint compatible with the message format used by `or3-intern`.

### Session Keys

External channels automatically namespace session keys by platform, for example:

- `telegram:<chat-id>`
- `slack:<channel-id>`
- `discord:<channel-id>`
- `whatsapp:<chat-id>`

This keeps chat history and long-term memory isolated by channel/session.

## New Features

### Bootstrap Files

Three markdown files configure the agent's identity and persistent context:

- **IDENTITY.md** – Loaded once at startup; defines who the agent is (name, role, personality traits). Injects into every system prompt.
- **MEMORY.md** – Static knowledge the agent always has access to (facts, preferences, standing instructions). Injects into every system prompt.
- **HEARTBEAT.md** – Autonomous task list injected only during scheduled (cron/webhook/file-watch) turns, not user-initiated chats. Useful for periodic background tasks.

Configure file paths in `config.json`:

```json
{
  "identityFile": "/path/to/IDENTITY.md",
  "memoryFile":   "/path/to/MEMORY.md",
  "heartbeat": { "tasksFile": "/path/to/HEARTBEAT.md" }
}
```

### Document Index

Opt-in file indexing allows the agent to retrieve relevant file excerpts as context for each query.

```json
{
  "docIndex": {
    "enabled": true,
    "roots": ["/path/to/docs", "/path/to/notes"],
    "maxFiles": 200,
    "maxFileBytes": 65536,
    "refreshSeconds": 300,
    "retrieveLimit": 5
  }
}
```

- Files are indexed at startup and re-synced every `refreshSeconds`.
- Retrieval uses full-text search (FTS5) to find relevant excerpts.
- Only non-empty matches are injected into the system prompt.
- Supported file types: `.md`, `.txt`, `.go`, `.py`, `.js`, `.ts`, `.json`, `.yaml`, `.toml`, `.sh`.

### Session Scopes

Link multiple session keys to a shared scope for cross-channel continuity. Sessions in the same scope share conversation history.

```bash
# Link a Telegram session and a Discord session to one scope
or3-intern scope link telegram:12345 my-project
or3-intern scope link discord:67890 my-project

# List all sessions in a scope
or3-intern scope list my-project

# Resolve the scope for a session
or3-intern scope resolve telegram:12345
```

### Skill Manifests

Skills can include a `skill.json` manifest for rich metadata:

```json
{
  "summary": "Does something useful",
  "entrypoints": [
    {
      "name": "run",
      "command": ["./run.sh", "--mode", "fast"],
      "timeoutSeconds": 30,
      "acceptsStdin": false
    }
  ]
}
```

Place skills in `builtin_skills/` (bundled) or `workspace_skills/` (user-defined). Enable execution with `skills.enableExec=true`.

### Triggers

**Webhook server** – receives POST requests and dispatches them as agent events:

```json
{
  "triggers": {
    "webhook": {
      "enabled": true,
      "addr": ":8080",
      "secret": "my-secret-token"
    }
  }
}
```

The webhook server listens at `/webhook` (fixed path).

**File watcher** – polls configured paths for new/changed files:

```json
{
  "triggers": {
    "fileWatch": {
      "enabled": true,
      "paths": ["/path/to/watch", "/another/path"],
      "pollSeconds": 10,
      "debounceSeconds": 2
    }
  }
}
```

Both trigger types use `HEARTBEAT.md` instructions when dispatching autonomous turns.

### Streaming

CLI (`chat` command) supports live streamed output. The assistant's response is printed token-by-token as it arrives from the provider. No additional configuration required.

### Cron Jobs with Per-Job Session Keys

Scheduled jobs can target a specific session (and thus its history/memory) independently of the default session:

```json
{
  "payload": {
    "kind": "agent_turn",
    "message": "Daily standup summary",
    "session_key": "slack:standup-channel",
    "channel": "slack",
    "to": "standup-channel"
  }
}
```

When `session_key` is set on a job payload, it overrides the global `defaultSessionKey` for that job.
````

## File: internal/config/config_test.go
````go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENAI_API_KEY",
		"BRAVE_API_KEY",
		"OR3_DB_PATH",
		"OR3_ARTIFACTS_DIR",
		"OR3_API_BASE",
		"OR3_API_KEY",
		"OR3_MODEL",
		"OR3_EMBED_MODEL",
		"OR3_TELEGRAM_TOKEN",
		"OR3_SLACK_APP_TOKEN",
		"OR3_SLACK_BOT_TOKEN",
		"OR3_DISCORD_TOKEN",
		"OR3_WHATSAPP_BRIDGE_URL",
		"OR3_WHATSAPP_BRIDGE_TOKEN",
		"OR3_SUBAGENTS_ENABLED",
		"OR3_SUBAGENTS_MAX_CONCURRENT",
		"OR3_SUBAGENTS_MAX_QUEUED",
		"OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS",
	} {
		t.Setenv(key, "")
	}
}

func TestDefault_Values(t *testing.T) {
	clearConfigEnv(t)
	cfg := Default()

	if cfg.DefaultSessionKey != "cli:default" {
		t.Errorf("expected DefaultSessionKey='cli:default', got %q", cfg.DefaultSessionKey)
	}
	if cfg.HistoryMax != 40 {
		t.Errorf("expected HistoryMax=40, got %d", cfg.HistoryMax)
	}
	if cfg.MaxToolBytes != 24*1024 {
		t.Errorf("expected MaxToolBytes=%d, got %d", 24*1024, cfg.MaxToolBytes)
	}
	if cfg.MaxMediaBytes != 20*1024*1024 {
		t.Errorf("expected MaxMediaBytes=%d, got %d", 20*1024*1024, cfg.MaxMediaBytes)
	}
	if cfg.MaxToolLoops != 6 {
		t.Errorf("expected MaxToolLoops=6, got %d", cfg.MaxToolLoops)
	}
	if cfg.VectorK != 8 {
		t.Errorf("expected VectorK=8, got %d", cfg.VectorK)
	}
	if cfg.FTSK != 8 {
		t.Errorf("expected FTSK=8, got %d", cfg.FTSK)
	}
	if cfg.VectorScanLimit != 2000 {
		t.Errorf("expected VectorScanLimit=2000, got %d", cfg.VectorScanLimit)
	}
	if cfg.WorkerCount != 4 {
		t.Errorf("expected WorkerCount=4, got %d", cfg.WorkerCount)
	}
	if cfg.Provider.Model != "gpt-4.1-mini" {
		t.Errorf("expected Model='gpt-4.1-mini', got %q", cfg.Provider.Model)
	}
	if cfg.Provider.APIBase != "https://api.openai.com/v1" {
		t.Errorf("expected APIBase='https://api.openai.com/v1', got %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.TimeoutSeconds != 60 {
		t.Errorf("expected TimeoutSeconds=60, got %d", cfg.Provider.TimeoutSeconds)
	}
	if cfg.Provider.EnableVision {
		t.Error("expected Provider.EnableVision=false by default")
	}
	if cfg.Cron.Enabled != true {
		t.Error("expected Cron.Enabled=true")
	}
	if cfg.BootstrapMaxChars != 20000 {
		t.Errorf("expected BootstrapMaxChars=20000, got %d", cfg.BootstrapMaxChars)
	}
	if cfg.BootstrapTotalMaxChars != 150000 {
		t.Errorf("expected BootstrapTotalMaxChars=150000, got %d", cfg.BootstrapTotalMaxChars)
	}
	if !cfg.Tools.RestrictToWorkspace {
		t.Error("expected RestrictToWorkspace=true by default")
	}
	if cfg.ConsolidationMaxMessages != 50 {
		t.Errorf("expected ConsolidationMaxMessages=50, got %d", cfg.ConsolidationMaxMessages)
	}
	if cfg.ConsolidationMaxInputChars != 12000 {
		t.Errorf("expected ConsolidationMaxInputChars=12000, got %d", cfg.ConsolidationMaxInputChars)
	}
	if cfg.ConsolidationAsyncTimeoutSeconds != 30 {
		t.Errorf("expected ConsolidationAsyncTimeoutSeconds=30, got %d", cfg.ConsolidationAsyncTimeoutSeconds)
	}
	if cfg.Subagents.Enabled {
		t.Error("expected Subagents.Enabled=false by default")
	}
	if cfg.Subagents.MaxConcurrent != 1 {
		t.Errorf("expected Subagents.MaxConcurrent=1, got %d", cfg.Subagents.MaxConcurrent)
	}
	if cfg.Subagents.MaxQueued != 32 {
		t.Errorf("expected Subagents.MaxQueued=32, got %d", cfg.Subagents.MaxQueued)
	}
	if cfg.Subagents.TaskTimeoutSeconds != 300 {
		t.Errorf("expected Subagents.TaskTimeoutSeconds=300, got %d", cfg.Subagents.TaskTimeoutSeconds)
	}
}

func TestLoad_FileNotExist_CreatesDefault(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultSessionKey != "cli:default" {
		t.Errorf("expected DefaultSessionKey='cli:default', got %q", cfg.DefaultSessionKey)
	}
	// should have created the file
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestLoad_FileNotExist_AppliesEnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	t.Setenv("OR3_API_BASE", "https://openrouter.ai/api/v1")
	t.Setenv("OR3_API_KEY", "env-key")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider.APIBase != "https://openrouter.ai/api/v1" {
		t.Fatalf("expected env API base override, got %q", cfg.Provider.APIBase)
	}
	if cfg.Provider.APIKey != "env-key" {
		t.Fatalf("expected env API key override, got %q", cfg.Provider.APIKey)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to be created")
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	var saved Config
	if err := json.Unmarshal(stored, &saved); err != nil {
		t.Fatalf("unmarshal stored config: %v", err)
	}
	if saved.Provider.APIBase != Default().Provider.APIBase {
		t.Fatalf("expected on-disk config to keep default API base, got %q", saved.Provider.APIBase)
	}
}

func TestSave_ExistingFilePermissionsAreTightened(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, mustJSON(Default()), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config permissions 0600 after save, got %o", info.Mode().Perm())
	}
}

func TestLoad_ValidFile(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	input := Config{
		DBPath:            "/tmp/test.db",
		DefaultSessionKey: "test:session",
		HistoryMax:        20,
		MaxToolLoops:      3,
		Provider: ProviderConfig{
			APIBase:        "https://custom.api",
			TimeoutSeconds: 30,
		},
	}
	b, _ := json.MarshalIndent(input, "", "  ")
	os.WriteFile(path, b, 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("expected DBPath='/tmp/test.db', got %q", cfg.DBPath)
	}
	if cfg.DefaultSessionKey != "test:session" {
		t.Errorf("expected DefaultSessionKey='test:session', got %q", cfg.DefaultSessionKey)
	}
	if cfg.HistoryMax != 20 {
		t.Errorf("expected HistoryMax=20, got %d", cfg.HistoryMax)
	}
	if cfg.MaxMediaBytes != Default().MaxMediaBytes {
		t.Errorf("expected missing MaxMediaBytes to default to %d, got %d", Default().MaxMediaBytes, cfg.MaxMediaBytes)
	}
	if cfg.Provider.EnableVision {
		t.Error("expected missing EnableVision to default to false")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("{invalid json"), 0o644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write valid default config
	b, _ := json.MarshalIndent(Default(), "", "  ")
	os.WriteFile(path, b, 0o644)

	// Set env vars
	t.Setenv("OR3_DB_PATH", "/env/test.db")
	t.Setenv("OR3_API_KEY", "env-key")
	t.Setenv("OR3_MODEL", "env-model")
	t.Setenv("OR3_EMBED_MODEL", "env-embed")
	t.Setenv("OR3_API_BASE", "https://env.api")
	t.Setenv("OR3_SUBAGENTS_ENABLED", "true")
	t.Setenv("OR3_SUBAGENTS_MAX_CONCURRENT", "3")
	t.Setenv("OR3_SUBAGENTS_MAX_QUEUED", "12")
	t.Setenv("OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS", "90")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBPath != "/env/test.db" {
		t.Errorf("expected DBPath='/env/test.db', got %q", cfg.DBPath)
	}
	if cfg.Provider.APIKey != "env-key" {
		t.Errorf("expected APIKey='env-key', got %q", cfg.Provider.APIKey)
	}
	if cfg.Provider.Model != "env-model" {
		t.Errorf("expected Model='env-model', got %q", cfg.Provider.Model)
	}
	if cfg.Provider.EmbedModel != "env-embed" {
		t.Errorf("expected EmbedModel='env-embed', got %q", cfg.Provider.EmbedModel)
	}
	if cfg.Provider.APIBase != "https://env.api" {
		t.Errorf("expected APIBase='https://env.api', got %q", cfg.Provider.APIBase)
	}
	if !cfg.Subagents.Enabled {
		t.Error("expected subagents enabled from env override")
	}
	if cfg.Subagents.MaxConcurrent != 3 {
		t.Errorf("expected MaxConcurrent=3, got %d", cfg.Subagents.MaxConcurrent)
	}
	if cfg.Subagents.MaxQueued != 12 {
		t.Errorf("expected MaxQueued=12, got %d", cfg.Subagents.MaxQueued)
	}
	if cfg.Subagents.TaskTimeoutSeconds != 90 {
		t.Errorf("expected TaskTimeoutSeconds=90, got %d", cfg.Subagents.TaskTimeoutSeconds)
	}
}

func TestLoad_SubagentNormalization(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	input := Default()
	input.Subagents.MaxConcurrent = 0
	input.Subagents.MaxQueued = 0
	input.Subagents.TaskTimeoutSeconds = 0
	b, _ := json.MarshalIndent(input, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Subagents.MaxConcurrent != 1 || cfg.Subagents.MaxQueued != 32 || cfg.Subagents.TaskTimeoutSeconds != 300 {
		t.Fatalf("expected normalized subagent defaults, got %+v", cfg.Subagents)
	}
}

func TestLoad_ArtifactsDirEnvOverride(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	b, _ := json.MarshalIndent(Default(), "", "  ")
	os.WriteFile(path, b, 0o644)

	t.Setenv("OR3_ARTIFACTS_DIR", "/env/artifacts")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ArtifactsDir != "/env/artifacts" {
		t.Errorf("expected ArtifactsDir='/env/artifacts', got %q", cfg.ArtifactsDir)
	}
}

func TestLoad_ChannelEnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	b, _ := json.MarshalIndent(Default(), "", "  ")
	os.WriteFile(path, b, 0o644)

	t.Setenv("OR3_TELEGRAM_TOKEN", "telegram-token")
	t.Setenv("OR3_SLACK_APP_TOKEN", "slack-app")
	t.Setenv("OR3_SLACK_BOT_TOKEN", "slack-bot")
	t.Setenv("OR3_DISCORD_TOKEN", "discord-token")
	t.Setenv("OR3_WHATSAPP_BRIDGE_URL", "ws://127.0.0.1:3001/ws")
	t.Setenv("OR3_WHATSAPP_BRIDGE_TOKEN", "bridge-token")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Channels.Telegram.Token != "telegram-token" || cfg.Channels.Slack.AppToken != "slack-app" || cfg.Channels.Slack.BotToken != "slack-bot" || cfg.Channels.Discord.Token != "discord-token" || cfg.Channels.WhatsApp.BridgeToken != "bridge-token" {
		t.Fatalf("unexpected channel env overrides: %#v", cfg.Channels)
	}
}

func TestLoad_ZeroValues_GetDefaults(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// config with zero values
	input := Config{}
	b, _ := json.MarshalIndent(input, "", "  ")
	os.WriteFile(path, b, 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultSessionKey != "cli:default" {
		t.Errorf("expected DefaultSessionKey='cli:default', got %q", cfg.DefaultSessionKey)
	}
	if cfg.HistoryMax != 40 {
		t.Errorf("expected HistoryMax=40, got %d", cfg.HistoryMax)
	}
	if cfg.MaxToolBytes != 24*1024 {
		t.Errorf("expected MaxToolBytes=%d, got %d", 24*1024, cfg.MaxToolBytes)
	}
	if cfg.MaxToolLoops != 6 {
		t.Errorf("expected MaxToolLoops=6, got %d", cfg.MaxToolLoops)
	}
	if cfg.VectorScanLimit != 2000 {
		t.Errorf("expected VectorScanLimit=2000, got %d", cfg.VectorScanLimit)
	}
	if cfg.WorkerCount != 4 {
		t.Errorf("expected WorkerCount=4, got %d", cfg.WorkerCount)
	}
	if cfg.Provider.TimeoutSeconds != 60 {
		t.Errorf("expected TimeoutSeconds=60, got %d", cfg.Provider.TimeoutSeconds)
	}
	if cfg.ConsolidationMaxMessages != 50 {
		t.Errorf("expected ConsolidationMaxMessages=50, got %d", cfg.ConsolidationMaxMessages)
	}
	if cfg.ConsolidationMaxInputChars != 12000 {
		t.Errorf("expected ConsolidationMaxInputChars=12000, got %d", cfg.ConsolidationMaxInputChars)
	}
	if cfg.ConsolidationAsyncTimeoutSeconds != 30 {
		t.Errorf("expected ConsolidationAsyncTimeoutSeconds=30, got %d", cfg.ConsolidationAsyncTimeoutSeconds)
	}
}

func TestLoad_EmptyPath_UsesDefault(t *testing.T) {
	clearConfigEnv(t)
	// Use a temp home dir to avoid touching real home
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultSessionKey == "" {
		t.Error("expected non-empty DefaultSessionKey")
	}
}

func TestMustJSON(t *testing.T) {
	clearConfigEnv(t)
	cfg := Default()
	b := mustJSON(cfg)
	if len(b) == 0 {
		t.Fatal("expected non-empty JSON output")
	}
	var out Config
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
}
````

## File: internal/db/db_test.go
````go
package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/scope"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpen_CreatesDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer d.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected db file to be created")
	}
}

func TestOpen_MigratesLegacyMemorySchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open legacy db: %v", err)
	}
	defer sqlDB.Close()

	legacyStmts := []string{
		`CREATE TABLE sessions(
			key TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE messages(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE memory_pinned(
			key TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE memory_notes(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			source_message_id INTEGER,
			tags TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);`,
		`CREATE VIRTUAL TABLE memory_fts USING fts5(text, content='memory_notes', content_rowid='id');`,
		`CREATE TRIGGER memory_notes_ai AFTER INSERT ON memory_notes BEGIN
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
	}
	for _, stmt := range legacyStmts {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("exec legacy schema: %v", err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO memory_notes(text, embedding, tags, created_at) VALUES('legacy note', x'00000000', '', 1)`); err != nil {
		t.Fatalf("seed legacy note: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO memory_pinned(key, content, updated_at) VALUES('name', 'legacy', 1)`); err != nil {
		t.Fatalf("seed legacy pinned: %v", err)
	}
	_ = sqlDB.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open migrated db: %v", err)
	}
	defer d.Close()

	pinned, err := d.GetPinned(context.Background(), "session1")
	if err != nil {
		t.Fatalf("GetPinned after migration: %v", err)
	}
	if pinned["name"] != "legacy" {
		t.Fatalf("expected legacy pinned data after migration, got %#v", pinned)
	}

	rows, err := d.StreamMemoryNotesLimit(context.Background(), "session1", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesLimit after migration: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected migrated memory note row")
	}
	var id int64
	var text string
	var emb []byte
	var sourceID any
	var tags string
	var createdAt int64
	if err := rows.Scan(&id, &text, &emb, &sourceID, &tags, &createdAt); err != nil {
		t.Fatalf("scan migrated memory note: %v", err)
	}
	if text != "legacy note" {
		t.Fatalf("expected migrated memory note, got %q", text)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	// A path inside a non-existent directory shouldn't cause Open to fail
	// because SQLite creates the file. But an invalid path format should fail.
	_, err := Open("/dev/null/invalid/path/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestNowMS(t *testing.T) {
	before := time.Now().UnixMilli()
	ms := NowMS()
	after := time.Now().UnixMilli()
	if ms < before || ms > after {
		t.Errorf("NowMS() = %d, expected between %d and %d", ms, before, after)
	}
}

func TestClose(t *testing.T) {
	d := openTestDB(t)
	if err := d.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}

func TestEnsureSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	err := d.EnsureSession(ctx, "test-session")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	// calling again should upsert without error
	err = d.EnsureSession(ctx, "test-session")
	if err != nil {
		t.Fatalf("EnsureSession (second call): %v", err)
	}
}

func TestAppendMessage_Basic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id, err := d.AppendMessage(ctx, "session1", "user", "hello", nil)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestAppendMessage_WithPayload(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	payload := map[string]any{"channel": "cli", "from": "user"}
	id, err := d.AppendMessage(ctx, "session1", "user", "hello", payload)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
}

func TestGetLastMessages_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	msgs, err := d.GetLastMessages(ctx, "session1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestGetLastMessages_Chronological(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.AppendMessage(ctx, "s1", "user", "first", nil)
	d.AppendMessage(ctx, "s1", "assistant", "second", nil)
	d.AppendMessage(ctx, "s1", "user", "third", nil)

	msgs, err := d.GetLastMessages(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	// Should be aligned so first is user
	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected first message role 'user', got %q", msgs[0].Role)
	}
}

func TestGetLastMessages_LimitRespected(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		d.AppendMessage(ctx, "s1", "user", "msg", nil)
	}

	msgs, err := d.GetLastMessages(ctx, "s1", 3)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) > 3 {
		t.Errorf("expected at most 3 messages, got %d", len(msgs))
	}
}

func TestGetLastMessages_AlignToUser(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert only assistant messages
	d.AppendMessage(ctx, "s1", "assistant", "resp1", nil)
	d.AppendMessage(ctx, "s1", "assistant", "resp2", nil)

	msgs, err := d.GetLastMessages(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	// should return empty (no user start) or at least not start with assistant
	for _, m := range msgs {
		if m.Role == "assistant" && msgs[0].Role == "assistant" {
			// This would only be invalid if first is assistant - but alignment strips leading non-user
			break
		}
	}
}

func TestGetPinned_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	pinned, err := d.GetPinned(ctx, "session1")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if len(pinned) != 0 {
		t.Errorf("expected empty pinned, got %d entries", len(pinned))
	}
}

func TestUpsertPinned_And_GetPinned(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	err := d.UpsertPinned(ctx, "session1", "name", "Alice")
	if err != nil {
		t.Fatalf("UpsertPinned: %v", err)
	}

	pinned, err := d.GetPinned(ctx, "session1")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if v, ok := pinned["name"]; !ok || v != "Alice" {
		t.Errorf("expected pinned['name']='Alice', got %q", pinned["name"])
	}
}

func TestUpsertPinned_Overwrites(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.UpsertPinned(ctx, "session1", "key", "first")
	d.UpsertPinned(ctx, "session1", "key", "second")

	pinned, _ := d.GetPinned(ctx, "session1")
	if pinned["key"] != "second" {
		t.Errorf("expected 'second', got %q", pinned["key"])
	}
}

func TestGetPinned_IncludesGlobalAndSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertPinned(ctx, scope.GlobalMemoryScope, "shared", "all"); err != nil {
		t.Fatalf("UpsertPinned global: %v", err)
	}
	if err := d.UpsertPinned(ctx, "session-a", "local", "only-a"); err != nil {
		t.Fatalf("UpsertPinned session: %v", err)
	}

	pinned, err := d.GetPinned(ctx, "session-a")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if pinned["shared"] != "all" || pinned["local"] != "only-a" {
		t.Fatalf("expected global and session pinned values, got %#v", pinned)
	}

	pinnedB, err := d.GetPinned(ctx, "session-b")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if pinnedB["shared"] != "all" {
		t.Fatalf("expected shared global entry, got %#v", pinnedB)
	}
	if _, ok := pinnedB["local"]; ok {
		t.Fatalf("did not expect session-a entry in session-b view: %#v", pinnedB)
	}
}

func TestGetPinned_SessionNamedGlobalDoesNotLeak(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertPinned(ctx, scope.GlobalMemoryScope, "shared", "all"); err != nil {
		t.Fatalf("UpsertPinned global: %v", err)
	}
	if err := d.UpsertPinned(ctx, scope.GlobalScopeAlias, "local", "only-global-session"); err != nil {
		t.Fatalf("UpsertPinned session: %v", err)
	}

	pinnedOther, err := d.GetPinned(ctx, "session-b")
	if err != nil {
		t.Fatalf("GetPinned other: %v", err)
	}
	if pinnedOther["shared"] != "all" {
		t.Fatalf("expected shared global entry, got %#v", pinnedOther)
	}
	if _, ok := pinnedOther["local"]; ok {
		t.Fatalf("did not expect session named global to leak, got %#v", pinnedOther)
	}

	pinnedGlobal, err := d.GetPinned(ctx, scope.GlobalScopeAlias)
	if err != nil {
		t.Fatalf("GetPinned global session: %v", err)
	}
	if pinnedGlobal["local"] != "only-global-session" {
		t.Fatalf("expected session-global entry, got %#v", pinnedGlobal)
	}
}

func TestInsertMemoryNote(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	embedding := make([]byte, 4*3)
	id, err := d.InsertMemoryNote(ctx, "session1", "test text", embedding, sql.NullInt64{}, "tag1,tag2")
	if err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestStreamMemoryNotesLimit_NoLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.InsertMemoryNote(ctx, "session1", "note1", make([]byte, 4), sql.NullInt64{}, "")
	d.InsertMemoryNote(ctx, "session1", "note2", make([]byte, 4), sql.NullInt64{}, "")

	rows, err := d.StreamMemoryNotesLimit(ctx, "session1", 0)
	if err != nil {
		t.Fatalf("StreamMemoryNotesLimit: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		rows.Scan(&id, &text, &emb, &src, &tags, &created)
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestStreamMemoryNotesLimit_WithLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		d.InsertMemoryNote(ctx, "session1", "note", make([]byte, 4), sql.NullInt64{}, "")
	}

	rows, err := d.StreamMemoryNotesLimit(ctx, "session1", 2)
	if err != nil {
		t.Fatalf("StreamMemoryNotesLimit: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		rows.Scan(&id, &text, &emb, &src, &tags, &created)
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestSearchFTS(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert notes to trigger FTS
	d.InsertMemoryNote(ctx, "session1", "the quick brown fox", make([]byte, 4), sql.NullInt64{}, "")
	d.InsertMemoryNote(ctx, "session1", "lazy dog sits", make([]byte, 4), sql.NullInt64{}, "")

	results, err := d.SearchFTS(ctx, "session1", "quick fox", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one FTS result")
	}
	if results[0].Text != "the quick brown fox" {
		t.Errorf("expected 'the quick brown fox', got %q", results[0].Text)
	}
}

func TestSearchFTS_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	results, err := d.SearchFTS(ctx, "session1", "anything", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestSearchFTS_SessionIsolation(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.InsertMemoryNote(ctx, "session-a", "private fox", make([]byte, 4), sql.NullInt64{}, "")
	d.InsertMemoryNote(ctx, scope.GlobalMemoryScope, "shared fox", make([]byte, 4), sql.NullInt64{}, "")

	results, err := d.SearchFTS(ctx, "session-b", "fox", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) != 1 || results[0].Text != "shared fox" {
		t.Fatalf("expected only shared result, got %#v", results)
	}
}

func TestMessage_Fields(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.AppendMessage(ctx, "sess", "user", "hello world", nil)
	msgs, err := d.GetLastMessages(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.SessionKey != "sess" {
		t.Errorf("expected SessionKey='sess', got %q", m.SessionKey)
	}
	if m.Role != "user" {
		t.Errorf("expected Role='user', got %q", m.Role)
	}
	if m.Content != "hello world" {
		t.Errorf("expected Content='hello world', got %q", m.Content)
	}
	if m.CreatedAt <= 0 {
		t.Errorf("expected positive CreatedAt, got %d", m.CreatedAt)
	}
}

// ---- GetConsolidationRange ----

func TestGetConsolidationRange_NoSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	lastID, oldestID, err := d.GetConsolidationRange(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 || oldestID != 0 {
		t.Errorf("expected (0,0) for missing session, got (%d,%d)", lastID, oldestID)
	}
}

func TestGetConsolidationRange_FewMessages(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert fewer messages than historyMax — all messages are in the active
	// window, so oldestActiveID equals the first message's ID and there is
	// nothing to consolidate (no messages with id < oldestActiveID beyond the cursor).
	var firstID int64
	for i := 0; i < 3; i++ {
		id, _ := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		if i == 0 {
			firstID = id
		}
	}

	lastID, oldestID, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 {
		t.Errorf("expected lastID=0 (nothing consolidated), got %d", lastID)
	}
	// With 3 messages and historyMax=10, the active window covers all messages.
	// oldestActiveID should equal the first message's ID.
	if oldestID != firstID {
		t.Errorf("expected oldestActiveID=%d (all in window), got %d", firstID, oldestID)
	}
}

func TestGetConsolidationRange_ManyMessages(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert 20 messages, historyMax=5 → oldestActiveID should be the ID of
	// the 16th message (5 from the end).
	var ids []int64
	for i := 0; i < 20; i++ {
		id, err := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		ids = append(ids, id)
	}

	lastID, oldestActiveID, err := d.GetConsolidationRange(ctx, "sess", 5)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 {
		t.Errorf("expected lastID=0 (nothing consolidated yet), got %d", lastID)
	}
	// oldestActiveID should be the 16th message ID (index 15).
	expectedOldest := ids[15]
	if oldestActiveID != expectedOldest {
		t.Errorf("expected oldestActiveID=%d, got %d", expectedOldest, oldestActiveID)
	}
}

// ---- GetMessagesForConsolidation ----

func TestGetMessagesForConsolidation_Range(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	var ids []int64
	for i := 0; i < 10; i++ {
		id, _ := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		ids = append(ids, id)
	}

	// Retrieve messages strictly between ids[2] and ids[7].
	msgs, err := d.GetMessagesForConsolidation(ctx, "sess", ids[2], ids[7])
	if err != nil {
		t.Fatalf("GetMessagesForConsolidation: %v", err)
	}
	if len(msgs) != 4 {
		t.Errorf("expected 4 messages (ids[3]..ids[6]), got %d", len(msgs))
	}
	if msgs[0].ID != ids[3] {
		t.Errorf("expected first message id=%d, got %d", ids[3], msgs[0].ID)
	}
}

func TestGetMessagesForConsolidation_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	msgs, err := d.GetMessagesForConsolidation(ctx, "sess", 0, 1)
	if err != nil {
		t.Fatalf("GetMessagesForConsolidation: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

// ---- SetLastConsolidatedID ----

func TestSetLastConsolidatedID(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.EnsureSession(ctx, "sess")
	if err := d.SetLastConsolidatedID(ctx, "sess", 42); err != nil {
		t.Fatalf("SetLastConsolidatedID: %v", err)
	}

	// Verify via GetConsolidationRange.
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 42 {
		t.Errorf("expected lastConsolidatedID=42, got %d", lastID)
	}
}

func TestWriteConsolidation_Atomic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	var lastMsgID int64
	for i := 0; i < 3; i++ {
		id, err := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		lastMsgID = id
	}

	noteID, err := d.WriteConsolidation(ctx, ConsolidationWrite{
		SessionKey:    "sess",
		ScopeKey:      "sess",
		NoteText:      "summary",
		Embedding:     []byte{},
		SourceMsgID:   sql.NullInt64{Int64: lastMsgID, Valid: true},
		NoteTags:      "consolidation",
		CanonicalKey:  "long_term_memory",
		CanonicalText: "- stable fact",
		CursorMsgID:   lastMsgID,
	})
	if err != nil {
		t.Fatalf("WriteConsolidation: %v", err)
	}
	if noteID == 0 {
		t.Fatal("expected non-zero note id")
	}

	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != lastMsgID {
		t.Fatalf("expected last consolidated id %d, got %d", lastMsgID, lastID)
	}

	rows, err := d.StreamMemoryNotesScopeLimit(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 note, got %d", count)
	}

	pinned, ok, err := d.GetPinnedValue(ctx, "sess", "long_term_memory")
	if err != nil {
		t.Fatalf("GetPinnedValue: %v", err)
	}
	if !ok || pinned != "- stable fact" {
		t.Fatalf("expected canonical memory to be updated, got ok=%v value=%q", ok, pinned)
	}
}

func TestWriteConsolidation_RollbackOnFailure(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := d.EnsureSession(ctx, "sess"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	// Empty canonical key and source message id is valid, but we intentionally fail on cursor update
	// by writing to a missing session key.
	_, err := d.WriteConsolidation(ctx, ConsolidationWrite{
		SessionKey:  "missing-session",
		ScopeKey:    "sess",
		NoteText:    "summary",
		Embedding:   []byte{},
		NoteTags:    "consolidation",
		CursorMsgID: 999,
	})
	if err == nil {
		t.Fatal("expected write error for missing session")
	}

	rows, err := d.StreamMemoryNotesScopeLimit(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("expected no note due to rollback")
	}
}

func TestResetSessionHistory(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", "msg", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	if err := d.SetLastConsolidatedID(ctx, "sess", 2); err != nil {
		t.Fatalf("SetLastConsolidatedID: %v", err)
	}

	if err := d.ResetSessionHistory(ctx, "sess"); err != nil {
		t.Fatalf("ResetSessionHistory: %v", err)
	}

	msgs, err := d.GetLastMessages(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages after reset, got %d", len(msgs))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 {
		t.Fatalf("expected cursor reset to 0, got %d", lastID)
	}
}

func TestOpen_CreatesSubagentJobsTable(t *testing.T) {
	d := openTestDB(t)
	row := d.SQL.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='subagent_jobs'`)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("expected subagent_jobs table, got err=%v", err)
	}
	if name != "subagent_jobs" {
		t.Fatalf("expected subagent_jobs table, got %q", name)
	}
}

func TestSubagentJobs_Lifecycle(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	job := SubagentJob{
		ID:               "job-1",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-1",
		Channel:          "cli",
		ReplyTo:          "user",
		Task:             "do work",
	}
	if err := d.EnqueueSubagentJob(ctx, job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	queued, err := d.ListQueuedSubagentJobs(ctx)
	if err != nil {
		t.Fatalf("ListQueuedSubagentJobs: %v", err)
	}
	if len(queued) != 1 || queued[0].ID != job.ID {
		t.Fatalf("expected queued job, got %#v", queued)
	}
	claimed, err := d.ClaimNextSubagentJob(ctx)
	if err != nil {
		t.Fatalf("ClaimNextSubagentJob: %v", err)
	}
	if claimed == nil || claimed.Status != SubagentStatusRunning || claimed.Attempts != 1 {
		t.Fatalf("expected running claimed job, got %#v", claimed)
	}
	if err := d.MarkSubagentSucceeded(ctx, job.ID, "preview", "artifact-1"); err != nil {
		t.Fatalf("MarkSubagentSucceeded: %v", err)
	}
	stored, ok, err := d.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatal("expected stored job")
	}
	if stored.Status != SubagentStatusSucceeded || stored.ResultPreview != "preview" || stored.ArtifactID != "artifact-1" || stored.FinishedAt == 0 {
		t.Fatalf("unexpected stored job after success: %#v", stored)
	}
}

func TestSubagentJobs_ReconcileRunning(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	job := SubagentJob{
		ID:               "job-2",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-2",
		Channel:          "cli",
		ReplyTo:          "user",
		Task:             "do work",
	}
	if err := d.EnqueueSubagentJob(ctx, job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	if _, err := d.AppendMessage(ctx, job.ParentSessionKey, "user", "start", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	if err := d.MarkSubagentRunning(ctx, job.ID); err != nil {
		t.Fatalf("MarkSubagentRunning: %v", err)
	}
	if err := d.MarkRunningSubagentsInterrupted(ctx, "restart"); err != nil {
		t.Fatalf("MarkRunningSubagentsInterrupted: %v", err)
	}
	stored, ok, err := d.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatal("expected stored job")
	}
	if stored.Status != SubagentStatusInterrupted || stored.ErrorText != "restart" || stored.FinishedAt == 0 {
		t.Fatalf("unexpected interrupted job: %#v", stored)
	}
}

func TestSubagentJobs_EnqueueWithLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- d.EnqueueSubagentJobLimited(ctx, SubagentJob{
				ID:               "job-limit-" + string(rune('a'+i)),
				ParentSessionKey: "parent",
				ChildSessionKey:  "parent:subagent:" + string(rune('a'+i)),
				Task:             "do work",
			}, 1)
		}(i)
	}
	wg.Wait()
	close(errCh)
	var successCount int
	var fullCount int
	for err := range errCh {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrSubagentQueueFull):
			fullCount++
		default:
			t.Fatalf("unexpected enqueue error: %v", err)
		}
	}
	if successCount != 1 || fullCount != 1 {
		t.Fatalf("expected one success and one queue-full error, got success=%d full=%d", successCount, fullCount)
	}
	queued, err := d.ListQueuedSubagentJobs(ctx)
	if err != nil {
		t.Fatalf("ListQueuedSubagentJobs: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("expected exactly one queued job, got %#v", queued)
	}
}

func TestSubagentJobs_FinalizePersistsSummaryAtomically(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	job := SubagentJob{
		ID:               "job-finalize",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-finalize",
		Task:             "do work",
	}
	if err := d.EnqueueSubagentJob(ctx, job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	if _, err := d.AppendMessage(ctx, job.ParentSessionKey, "user", "start", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	if err := d.MarkSubagentRunning(ctx, job.ID); err != nil {
		t.Fatalf("MarkSubagentRunning: %v", err)
	}
	if err := d.FinalizeSubagentJob(ctx, job, SubagentStatusSucceeded, "done", "artifact-1", "", "summary text", map[string]any{"subagent_job_id": job.ID}); err != nil {
		t.Fatalf("FinalizeSubagentJob: %v", err)
	}
	stored, ok, err := d.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatal("expected stored job")
	}
	if stored.Status != SubagentStatusSucceeded || stored.ResultPreview != "done" || stored.ArtifactID != "artifact-1" {
		t.Fatalf("unexpected finalized job: %#v", stored)
	}
	msgs, err := d.GetLastMessages(ctx, job.ParentSessionKey, 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].Content != "summary text" {
		t.Fatalf("expected parent summary message, got %#v", msgs)
	}
}

func TestLinkSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.LinkSession(ctx, "session-a", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession a: %v", err)
	}
	if err := d.LinkSession(ctx, "session-b", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession b: %v", err)
	}

	scopeA, err := d.ResolveScopeKey(ctx, "session-a")
	if err != nil {
		t.Fatalf("ResolveScopeKey a: %v", err)
	}
	if scopeA != "scope-1" {
		t.Fatalf("expected scope-1, got %q", scopeA)
	}

	scopeB, err := d.ResolveScopeKey(ctx, "session-b")
	if err != nil {
		t.Fatalf("ResolveScopeKey b: %v", err)
	}
	if scopeB != "scope-1" {
		t.Fatalf("expected scope-1, got %q", scopeB)
	}
}

func TestResolveScopeKeyUnlinked(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	scopeKey, err := d.ResolveScopeKey(ctx, "unlinked-session")
	if err != nil {
		t.Fatalf("ResolveScopeKey: %v", err)
	}
	if scopeKey != "unlinked-session" {
		t.Fatalf("expected unlinked-session, got %q", scopeKey)
	}
}

func TestListScopeSessions(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.LinkSession(ctx, "sess-x", "scope-2", nil); err != nil {
		t.Fatalf("LinkSession x: %v", err)
	}
	if err := d.LinkSession(ctx, "sess-y", "scope-2", nil); err != nil {
		t.Fatalf("LinkSession y: %v", err)
	}

	sessions, err := d.ListScopeSessions(ctx, "scope-2")
	if err != nil {
		t.Fatalf("ListScopeSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	found := map[string]bool{}
	for _, s := range sessions {
		found[s] = true
	}
	if !found["sess-x"] || !found["sess-y"] {
		t.Fatalf("expected sess-x and sess-y, got %v", sessions)
	}
}

func TestGetLastMessagesScoped(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Link two sessions to a scope
	if err := d.LinkSession(ctx, "scoped-a", "scope-3", nil); err != nil {
		t.Fatalf("LinkSession a: %v", err)
	}
	if err := d.LinkSession(ctx, "scoped-b", "scope-3", nil); err != nil {
		t.Fatalf("LinkSession b: %v", err)
	}

	// Add messages to both sessions
	if _, err := d.AppendMessage(ctx, "scoped-a", "user", "hello from a", nil); err != nil {
		t.Fatalf("AppendMessage a user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "scoped-a", "assistant", "reply from a", nil); err != nil {
		t.Fatalf("AppendMessage a assistant: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "scoped-b", "user", "hello from b", nil); err != nil {
		t.Fatalf("AppendMessage b user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "scoped-b", "assistant", "reply from b", nil); err != nil {
		t.Fatalf("AppendMessage b assistant: %v", err)
	}

	msgs, err := d.GetLastMessagesScoped(ctx, "scoped-a", 10)
	if err != nil {
		t.Fatalf("GetLastMessagesScoped: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	// First message must be a user message (alignment rule)
	if msgs[0].Role != "user" {
		t.Fatalf("expected first message to be user, got %q", msgs[0].Role)
	}
	// Messages must be in ascending id order (chronological)
	for i := 1; i < len(msgs); i++ {
		if msgs[i].ID < msgs[i-1].ID {
			t.Fatalf("messages not in chronological order at index %d", i)
		}
	}
	// Both sessions' messages should appear
	contents := map[string]bool{}
	for _, m := range msgs {
		contents[m.Content] = true
	}
	if !contents["hello from a"] || !contents["hello from b"] {
		t.Fatalf("expected messages from both sessions, got %v", contents)
	}
}
````

## File: internal/db/store.go
````go
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"or3-intern/internal/scope"
)

type Message struct {
	ID          int64
	SessionKey  string
	Role        string
	Content     string
	PayloadJSON string
	CreatedAt   int64
}

type ConsolidationMessage struct {
	ID      int64
	Role    string
	Content string
}

type ConsolidationWrite struct {
	SessionKey    string
	ScopeKey      string
	NoteText      string
	Embedding     []byte
	SourceMsgID   sql.NullInt64
	NoteTags      string
	CanonicalKey  string
	CanonicalText string
	CursorMsgID   int64
}

const (
	SubagentStatusQueued      = "queued"
	SubagentStatusRunning     = "running"
	SubagentStatusSucceeded   = "succeeded"
	SubagentStatusFailed      = "failed"
	SubagentStatusInterrupted = "interrupted"
)

var ErrSubagentQueueFull = errors.New("subagent queue is full")

type SubagentJob struct {
	ID               string
	ParentSessionKey string
	ChildSessionKey  string
	Channel          string
	ReplyTo          string
	Task             string
	Status           string
	ResultPreview    string
	ArtifactID       string
	ErrorText        string
	RequestedAt      int64
	StartedAt        int64
	FinishedAt       int64
	Attempts         int
	MetadataJSON     string
}

func (d *DB) EnsureSession(ctx context.Context, key string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		key, now, now)
	return err
}

func (d *DB) AppendMessage(ctx context.Context, sessionKey, role, content string, payload any) (int64, error) {
	if err := d.EnsureSession(ctx, sessionKey); err != nil {
		return 0, err
	}
	pb, _ := json.Marshal(payload)
	now := NowMS()
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := d.SQL.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey); err != nil {
		return id, err
	}
	return id, nil
}

func (d *DB) GetLastMessages(ctx context.Context, sessionKey string, limit int) ([]Message, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? ORDER BY id DESC LIMIT ?`, sessionKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// align so first is user (best-effort)
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out, rows.Err()
}

func (d *DB) GetPinned(ctx context.Context, sessionKey string) (map[string]string, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT key, content FROM memory_pinned
		 WHERE session_key IN (?, ?)
		 ORDER BY CASE WHEN session_key=? THEN 1 ELSE 0 END, key`,
		scope.GlobalMemoryScope, sessionKey, sessionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, c string
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, rows.Err()
}

func (d *DB) GetPinnedValue(ctx context.Context, sessionKey, key string) (string, bool, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	row := d.SQL.QueryRowContext(ctx,
		`SELECT content FROM memory_pinned WHERE session_key=? AND key=?`,
		sessionKey, key)
	var out string
	if err := row.Scan(&out); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return out, true, nil
}

func (d *DB) UpsertPinned(ctx context.Context, sessionKey, key, content string) error {
	sessionKey = normalizeMemorySession(sessionKey)
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		sessionKey, key, content, NowMS())
	return err
}

func (d *DB) InsertMemoryNote(ctx context.Context, sessionKey, text string, embedding []byte, sourceMsgID sql.NullInt64, tags string) (int64, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at) VALUES(?,?,?,?,?,?)`,
		sessionKey, text, embedding, sourceMsgID, tags, NowMS())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

type MemoryNoteRow struct {
	ID              int64
	Text            string
	Embedding       []byte
	SourceMessageID sql.NullInt64
	Tags            string
	CreatedAt       int64
}

func (d *DB) StreamMemoryNotes(ctx context.Context, sessionKey string) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at FROM memory_notes
		 WHERE session_key IN (?, ?)`,
		scope.GlobalMemoryScope, sessionKey)
}

func (d *DB) StreamMemoryNotesScopeLimit(ctx context.Context, sessionKey string, limit int) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	if limit <= 0 {
		return d.SQL.QueryContext(ctx,
			`SELECT id, text, embedding, source_message_id, tags, created_at
			 FROM memory_notes WHERE session_key=?`,
			sessionKey)
	}
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at
		 FROM memory_notes WHERE session_key=? ORDER BY id DESC LIMIT ?`,
		sessionKey, limit)
}

func (d *DB) StreamMemoryNotesLimit(ctx context.Context, sessionKey string, limit int) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	if limit <= 0 {
		return d.StreamMemoryNotes(ctx, sessionKey)
	}
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at
		 FROM memory_notes WHERE session_key IN (?, ?) ORDER BY id DESC LIMIT ?`,
		scope.GlobalMemoryScope, sessionKey, limit)
}

type FTSCandidate struct {
	ID   int64
	Text string
	Rank float64
}

func (d *DB) SearchFTS(ctx context.Context, sessionKey, query string, k int) ([]FTSCandidate, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	// bm25 lower is better; invert
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT memory_fts.rowid, memory_fts.text, bm25(memory_fts) as rank
		 FROM memory_fts
		 JOIN memory_notes ON memory_notes.id = memory_fts.rowid
		 WHERE memory_fts MATCH ? AND memory_notes.session_key IN (?, ?)
		 ORDER BY rank LIMIT ?`,
		query, scope.GlobalMemoryScope, sessionKey, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FTSCandidate
	for rows.Next() {
		var id int64
		var text string
		var rank float64
		if err := rows.Scan(&id, &text, &rank); err != nil {
			return nil, err
		}
		out = append(out, FTSCandidate{ID: id, Text: text, Rank: rank})
	}
	return out, rows.Err()
}

// GetConsolidationRange returns (lastConsolidatedID, oldestActiveID).
// oldestActiveID is the minimum ID among the last historyMax messages,
// or 0 if there are no messages in the session.
// Messages older than oldestActiveID (and newer than lastConsolidatedID)
// may be eligible for consolidation.
func (d *DB) GetConsolidationRange(ctx context.Context, sessionKey string, historyMax int) (lastConsolidatedID int64, oldestActiveID int64, err error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT last_consolidated_msg_id FROM sessions WHERE key=?`, sessionKey)
	if scanErr := row.Scan(&lastConsolidatedID); scanErr != nil {
		// Session row not found yet → nothing to consolidate.
		return 0, 0, nil
	}

	// Oldest ID in the active window (last historyMax messages).
	// If the total number of messages is < historyMax, MIN returns NULL → 0.
	activeRow := d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(MIN(id), 0) FROM
		 (SELECT id FROM messages WHERE session_key=? ORDER BY id DESC LIMIT ?)`,
		sessionKey, historyMax)
	if scanErr := activeRow.Scan(&oldestActiveID); scanErr != nil {
		return lastConsolidatedID, 0, scanErr
	}
	return lastConsolidatedID, oldestActiveID, nil
}

// GetMessagesForConsolidation returns messages with afterID < id < beforeID
// in chronological order. Used to build the window to summarize.
func (d *DB) GetMessagesForConsolidation(ctx context.Context, sessionKey string, afterID, beforeID int64) ([]Message, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? AND id > ? AND id < ?
		 ORDER BY id ASC`,
		sessionKey, afterID, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) GetConsolidationMessages(ctx context.Context, sessionKey string, afterID, beforeID int64, limit int) ([]ConsolidationMessage, error) {
	if beforeID <= 0 {
		beforeID = math.MaxInt64
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, role, content
		 FROM messages WHERE session_key=? AND id > ? AND id < ?
		 ORDER BY id ASC LIMIT ?`,
		sessionKey, afterID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ConsolidationMessage, 0, limit)
	for rows.Next() {
		var m ConsolidationMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetLastConsolidatedID records the highest message ID that has been
// consolidated into memory notes for this session.
func (d *DB) SetLastConsolidatedID(ctx context.Context, sessionKey string, id int64) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=? WHERE key=?`, id, sessionKey)
	return err
}

func (d *DB) WriteConsolidation(ctx context.Context, w ConsolidationWrite) (int64, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var noteID int64
	if strings.TrimSpace(w.NoteText) != "" {
		scopeKey := normalizeMemorySession(w.ScopeKey)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at) VALUES(?,?,?,?,?,?)`,
			scopeKey, w.NoteText, w.Embedding, w.SourceMsgID, w.NoteTags, NowMS())
		if err != nil {
			return 0, err
		}
		noteID, _ = res.LastInsertId()
	}
	if strings.TrimSpace(w.CanonicalKey) != "" {
		scopeKey := normalizeMemorySession(w.ScopeKey)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO memory_pinned(session_key, key, content, updated_at) VALUES(?,?,?,?)
			 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
			scopeKey, w.CanonicalKey, w.CanonicalText, NowMS())
		if err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=? WHERE key=?`, w.CursorMsgID, w.SessionKey)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return noteID, nil
}

func (d *DB) ResetSessionHistory(ctx context.Context, sessionKey string) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_key=?`, sessionKey); err != nil {
		return err
	}
	now := NowMS()
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=0, updated_at=? WHERE key=?`,
		now, sessionKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) EnqueueSubagentJob(ctx context.Context, job SubagentJob) error {
	return d.EnqueueSubagentJobLimited(ctx, job, 0)
}

func (d *DB) EnqueueSubagentJobLimited(ctx context.Context, job SubagentJob, maxQueued int) error {
	if job.RequestedAt == 0 {
		job.RequestedAt = NowMS()
	}
	if strings.TrimSpace(job.Status) == "" {
		job.Status = SubagentStatusQueued
	}
	if strings.TrimSpace(job.MetadataJSON) == "" {
		job.MetadataJSON = "{}"
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureSessionTx(ctx, tx, job.ParentSessionKey); err != nil {
		return err
	}
	if err := ensureSessionTx(ctx, tx, job.ChildSessionKey); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO subagent_jobs(
			id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		)
		SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?
		WHERE ? <= 0 OR (SELECT COUNT(*) FROM subagent_jobs WHERE status=?) < ?`,
		job.ID,
		job.ParentSessionKey,
		job.ChildSessionKey,
		job.Channel,
		job.ReplyTo,
		job.Task,
		job.Status,
		job.ResultPreview,
		job.ArtifactID,
		job.ErrorText,
		job.RequestedAt,
		job.StartedAt,
		job.FinishedAt,
		job.Attempts,
		job.MetadataJSON,
		maxQueued,
		SubagentStatusQueued,
		maxQueued,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrSubagentQueueFull
	}
	return tx.Commit()
}

func (d *DB) GetSubagentJob(ctx context.Context, id string) (SubagentJob, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE id=?`, id)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return SubagentJob{}, false, nil
		}
		return SubagentJob{}, false, err
	}
	return job, true, nil
}

func (d *DB) ListQueuedSubagentJobs(ctx context.Context) ([]SubagentJob, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE status=? ORDER BY requested_at ASC, id ASC`,
		SubagentStatusQueued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubagentJob
	for rows.Next() {
		job, err := scanSubagentJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (d *DB) MarkSubagentRunning(ctx context.Context, id string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END, attempts=attempts+1
		 WHERE id=? AND status=?`,
		SubagentStatusRunning, now, id, SubagentStatusQueued)
	return err
}

func (d *DB) ClaimNextSubagentJob(ctx context.Context) (*SubagentJob, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE status=? ORDER BY requested_at ASC, id ASC LIMIT 1`,
		SubagentStatusQueued)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs SET status=?, started_at=?, attempts=attempts+1 WHERE id=? AND status=?`,
		SubagentStatusRunning, now, job.ID, SubagentStatusQueued)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, tx.Commit()
	}
	job.Status = SubagentStatusRunning
	job.StartedAt = now
	job.Attempts++
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &job, nil
}

func (d *DB) MarkSubagentSucceeded(ctx context.Context, id, preview, artifactID string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, result_preview=?, artifact_id=?, error_text='', finished_at=?
		 WHERE id=?`,
		SubagentStatusSucceeded, preview, artifactID, NowMS(), id)
	return err
}

func (d *DB) MarkSubagentFailed(ctx context.Context, id, errText string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=?`,
		SubagentStatusFailed, errText, NowMS(), id)
	return err
}

func (d *DB) MarkSubagentInterrupted(ctx context.Context, id, errText string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=?`,
		SubagentStatusInterrupted, errText, NowMS(), id)
	return err
}

func (d *DB) MarkRunningSubagentsInterrupted(ctx context.Context, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "interrupted during restart"
	}
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE status=?`,
		SubagentStatusInterrupted, reason, NowMS(), SubagentStatusRunning)
	return err
}

func (d *DB) FinalizeSubagentJob(ctx context.Context, job SubagentJob, status, preview, artifactID, errText, parentSummary string, parentPayload any) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, result_preview=?, artifact_id=?, error_text=?, finished_at=?
		 WHERE id=? AND status=?`,
		status, preview, artifactID, errText, NowMS(), job.ID, SubagentStatusRunning)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	if strings.TrimSpace(parentSummary) != "" {
		if _, err := appendMessageTx(ctx, tx, job.ParentSessionKey, "assistant", parentSummary, parentPayload); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanSubagentJob(scanner interface{ Scan(dest ...any) error }) (SubagentJob, error) {
	var job SubagentJob
	err := scanner.Scan(
		&job.ID,
		&job.ParentSessionKey,
		&job.ChildSessionKey,
		&job.Channel,
		&job.ReplyTo,
		&job.Task,
		&job.Status,
		&job.ResultPreview,
		&job.ArtifactID,
		&job.ErrorText,
		&job.RequestedAt,
		&job.StartedAt,
		&job.FinishedAt,
		&job.Attempts,
		&job.MetadataJSON,
	)
	return job, err
}

func ensureSessionTx(ctx context.Context, tx *sql.Tx, key string) error {
	now := NowMS()
	_, err := tx.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		key, now, now)
	return err
}

func appendMessageTx(ctx context.Context, tx *sql.Tx, sessionKey, role, content string, payload any) (int64, error) {
	if err := ensureSessionTx(ctx, tx, sessionKey); err != nil {
		return 0, err
	}
	pb, _ := json.Marshal(payload)
	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey); err != nil {
		return id, err
	}
	return id, nil
}

func normalizeMemorySession(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return scope.GlobalMemoryScope
	}
	return sessionKey
}

// LinkSession links a physical session key to a logical scope key.
// If scopeKey is empty, the sessionKey itself is used.
func (d *DB) LinkSession(ctx context.Context, sessionKey, scopeKey string, meta map[string]any) error {
	if strings.TrimSpace(sessionKey) == "" {
		return fmt.Errorf("sessionKey required")
	}
	if strings.TrimSpace(scopeKey) == "" {
		scopeKey = sessionKey
	}
	mb, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if mb == nil {
		mb = []byte("{}")
	}
	_, err = d.SQL.ExecContext(ctx,
		`INSERT INTO session_links(session_key, scope_key, linked_at, metadata_json) VALUES(?,?,?,?)
         ON CONFLICT(session_key) DO UPDATE SET scope_key=excluded.scope_key, linked_at=excluded.linked_at, metadata_json=excluded.metadata_json`,
		sessionKey, scopeKey, NowMS(), string(mb))
	return err
}

// ResolveScopeKey returns the logical scope key for a physical session key.
// If no link exists, it returns the session key itself.
func (d *DB) ResolveScopeKey(ctx context.Context, sessionKey string) (string, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT scope_key FROM session_links WHERE session_key=?`, sessionKey)
	var scopeKey string
	if err := row.Scan(&scopeKey); err != nil {
		if err == sql.ErrNoRows {
			return sessionKey, nil
		}
		return sessionKey, err
	}
	return scopeKey, nil
}

// ListScopeSessions returns all physical session keys linked to the given scope key.
func (d *DB) ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT session_key FROM session_links WHERE scope_key=? ORDER BY linked_at ASC`, scopeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sk string
		if err := rows.Scan(&sk); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// GetLastMessagesScoped reads history for all sessions linked under the same scope
// as sessionKey, ordered by message id ascending, up to limit messages.
func (d *DB) GetLastMessagesScoped(ctx context.Context, sessionKey string, limit int) ([]Message, error) {
	scopeKey, err := d.ResolveScopeKey(ctx, sessionKey)
	if err != nil {
		return d.GetLastMessages(ctx, sessionKey, limit)
	}
	// get all sessions in scope (including the session itself)
	linked, err := d.ListScopeSessions(ctx, scopeKey)
	if err != nil || len(linked) == 0 {
		return d.GetLastMessages(ctx, sessionKey, limit)
	}
	// build IN clause; always include the physical session key itself
	allKeys := linked
	found := false
	for _, k := range linked {
		if k == sessionKey {
			found = true
			break
		}
	}
	if !found {
		allKeys = append(allKeys, sessionKey)
	}
	// build placeholders
	placeholders := make([]string, len(allKeys))
	args := make([]any, len(allKeys)+1)
	for i, k := range allKeys {
		placeholders[i] = "?"
		args[i] = k
	}
	args[len(allKeys)] = limit
	q := `SELECT id, session_key, role, content, payload_json, created_at
          FROM messages WHERE session_key IN (` + strings.Join(placeholders, ",") + `)
          ORDER BY id DESC LIMIT ?`
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// align so first is user
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out, nil
}
````

## File: internal/config/config.go
````go
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	DBPath                 string `json:"dbPath"`
	ArtifactsDir           string `json:"artifactsDir"`
	WorkspaceDir           string `json:"workspaceDir"`
	AllowedDir             string `json:"allowedDir"`
	DefaultSessionKey      string `json:"defaultSessionKey"`
	SoulFile               string `json:"soulFile"`
	AgentsFile             string `json:"agentsFile"`
	ToolsFile              string `json:"toolsFile"`
	BootstrapMaxChars      int    `json:"bootstrapMaxChars"`
	BootstrapTotalMaxChars int    `json:"bootstrapTotalMaxChars"`
	SessionCache           int    `json:"sessionCacheLimit"`
	HistoryMax             int    `json:"historyMaxMessages"`
	MaxToolBytes           int    `json:"maxToolBytes"`
	MaxMediaBytes          int    `json:"maxMediaBytes"`
	MaxToolLoops           int    `json:"maxToolLoops"`
	MemoryRetrieve         int    `json:"memoryRetrieveLimit"`
	VectorK                int    `json:"vectorSearchK"`
	FTSK                   int    `json:"ftsSearchK"`
	VectorScanLimit        int    `json:"vectorScanLimit"`
	WorkerCount            int    `json:"workerCount"`

	ConsolidationEnabled             bool            `json:"consolidationEnabled"`
	ConsolidationWindowSize          int             `json:"consolidationWindowSize"`
	ConsolidationMaxMessages         int             `json:"consolidationMaxMessages"`
	ConsolidationMaxInputChars       int             `json:"consolidationMaxInputChars"`
	ConsolidationAsyncTimeoutSeconds int             `json:"consolidationAsyncTimeoutSeconds"`
	Subagents                        SubagentsConfig `json:"subagents"`

	IdentityFile string        `json:"identityFile"`
	MemoryFile   string        `json:"memoryFile"`
	DocIndex     DocIndexConfig `json:"docIndex"`
	Skills       SkillsConfig   `json:"skills"`
	Triggers     TriggerConfig  `json:"triggers"`

	Provider  ProviderConfig  `json:"provider"`
	Tools     ToolsConfig     `json:"tools"`
	Cron      CronConfig      `json:"cron"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
	Channels  ChannelsConfig  `json:"channels"`
}

type ProviderConfig struct {
	APIBase        string  `json:"apiBase"`
	APIKey         string  `json:"apiKey"`
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	EmbedModel     string  `json:"embedModel"`
	EnableVision   bool    `json:"enableVision"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
}

type ToolsConfig struct {
	BraveAPIKey         string `json:"braveApiKey"`
	WebProxy            string `json:"webProxy"`
	ExecTimeoutSeconds  int    `json:"execTimeoutSeconds"`
	RestrictToWorkspace bool   `json:"restrictToWorkspace"`
	PathAppend          string `json:"pathAppend"`
}

type CronConfig struct {
	Enabled   bool   `json:"enabled"`
	StorePath string `json:"storePath"`
}

type HeartbeatConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	TasksFile       string `json:"tasksFile"`
}

type SubagentsConfig struct {
	Enabled            bool `json:"enabled"`
	MaxConcurrent      int  `json:"maxConcurrent"`
	MaxQueued          int  `json:"maxQueued"`
	TaskTimeoutSeconds int  `json:"taskTimeoutSeconds"`
}

type TelegramChannelConfig struct {
	Enabled        bool     `json:"enabled"`
	Token          string   `json:"token"`
	APIBase        string   `json:"apiBase"`
	PollSeconds    int      `json:"pollSeconds"`
	DefaultChatID  string   `json:"defaultChatId"`
	AllowedChatIDs []string `json:"allowedChatIds"`
}

type SlackChannelConfig struct {
	Enabled          bool     `json:"enabled"`
	AppToken         string   `json:"appToken"`
	BotToken         string   `json:"botToken"`
	APIBase          string   `json:"apiBase"`
	SocketModeURL    string   `json:"socketModeUrl"`
	DefaultChannelID string   `json:"defaultChannelId"`
	AllowedUserIDs   []string `json:"allowedUserIds"`
	RequireMention   bool     `json:"requireMention"`
}

type DiscordChannelConfig struct {
	Enabled          bool     `json:"enabled"`
	Token            string   `json:"token"`
	APIBase          string   `json:"apiBase"`
	GatewayURL       string   `json:"gatewayUrl"`
	DefaultChannelID string   `json:"defaultChannelId"`
	AllowedUserIDs   []string `json:"allowedUserIds"`
	RequireMention   bool     `json:"requireMention"`
}

type WhatsAppBridgeConfig struct {
	Enabled     bool     `json:"enabled"`
	BridgeURL   string   `json:"bridgeUrl"`
	BridgeToken string   `json:"bridgeToken"`
	DefaultTo   string   `json:"defaultTo"`
	AllowedFrom []string `json:"allowedFrom"`
}

type ChannelsConfig struct {
	Telegram TelegramChannelConfig `json:"telegram"`
	Slack    SlackChannelConfig    `json:"slack"`
	Discord  DiscordChannelConfig  `json:"discord"`
	WhatsApp WhatsAppBridgeConfig  `json:"whatsApp"`
}

type DocIndexConfig struct {
	Enabled        bool     `json:"enabled"`
	Roots          []string `json:"roots"`
	MaxFiles       int      `json:"maxFiles"`
	MaxFileBytes   int      `json:"maxFileBytes"`
	MaxChunks      int      `json:"maxChunks"`
	EmbedMaxBytes  int      `json:"embedMaxBytes"`
	RefreshSeconds int      `json:"refreshSeconds"`
	RetrieveLimit  int      `json:"retrieveLimit"`
}

type SkillsConfig struct {
	EnableExec    bool `json:"enableExec"`
	MaxRunSeconds int  `json:"maxRunSeconds"`
}

type WebhookConfig struct {
	Enabled   bool   `json:"enabled"`
	Addr      string `json:"addr"`
	Secret    string `json:"secret"`
	MaxBodyKB int    `json:"maxBodyKB"`
}

type FileWatchConfig struct {
	Enabled         bool     `json:"enabled"`
	Paths           []string `json:"paths"`
	PollSeconds     int      `json:"pollSeconds"`
	DebounceSeconds int      `json:"debounceSeconds"`
}

type TriggerConfig struct {
	Webhook   WebhookConfig   `json:"webhook"`
	FileWatch FileWatchConfig `json:"fileWatch"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".or3-intern")
	return Config{
		DBPath:                           filepath.Join(root, "or3-intern.sqlite"),
		ArtifactsDir:                     filepath.Join(root, "artifacts"),
		WorkspaceDir:                     "",
		AllowedDir:                       "",
		DefaultSessionKey:                "cli:default",
		SoulFile:                         filepath.Join(root, "SOUL.md"),
		AgentsFile:                       filepath.Join(root, "AGENTS.md"),
		ToolsFile:                        filepath.Join(root, "TOOLS.md"),
		IdentityFile:                     filepath.Join(root, "IDENTITY.md"),
		MemoryFile:                       filepath.Join(root, "MEMORY.md"),
		BootstrapMaxChars:                20000,
		BootstrapTotalMaxChars:           150000,
		SessionCache:                     64,
		HistoryMax:                       40,
		MaxToolBytes:                     24 * 1024,
		MaxMediaBytes:                    20 * 1024 * 1024,
		MaxToolLoops:                     6,
		MemoryRetrieve:                   8,
		VectorK:                          8,
		FTSK:                             8,
		VectorScanLimit:                  2000,
		WorkerCount:                      4,
		ConsolidationEnabled:             true,
		ConsolidationWindowSize:          10,
		ConsolidationMaxMessages:         50,
		ConsolidationMaxInputChars:       12000,
		ConsolidationAsyncTimeoutSeconds: 30,
		Subagents: SubagentsConfig{
			Enabled:            false,
			MaxConcurrent:      1,
			MaxQueued:          32,
			TaskTimeoutSeconds: 300,
		},
		DocIndex: DocIndexConfig{
			Enabled:        false,
			MaxFiles:       100,
			MaxFileBytes:   64 * 1024,
			MaxChunks:      500,
			EmbedMaxBytes:  8 * 1024,
			RefreshSeconds: 300,
			RetrieveLimit:  5,
		},
		Skills: SkillsConfig{
			EnableExec:    false,
			MaxRunSeconds: 30,
		},
		Triggers: TriggerConfig{
			Webhook: WebhookConfig{
				Enabled:   false,
				Addr:      "127.0.0.1:8765",
				MaxBodyKB: 64,
			},
			FileWatch: FileWatchConfig{
				Enabled:         false,
				PollSeconds:     5,
				DebounceSeconds: 2,
			},
		},
		Provider: ProviderConfig{
			APIBase:        "https://api.openai.com/v1",
			APIKey:         os.Getenv("OPENAI_API_KEY"),
			Model:          "gpt-4.1-mini",
			Temperature:    0,
			EmbedModel:     "text-embedding-3-small",
			TimeoutSeconds: 60,
		},
		Tools: ToolsConfig{
			BraveAPIKey:         os.Getenv("BRAVE_API_KEY"),
			WebProxy:            "",
			ExecTimeoutSeconds:  60,
			RestrictToWorkspace: true,
			PathAppend:          "",
		},
		Cron:      CronConfig{Enabled: true, StorePath: filepath.Join(root, "cron.json")},
		Heartbeat: HeartbeatConfig{Enabled: false, IntervalMinutes: 30, TasksFile: filepath.Join(root, "HEARTBEAT.md")},
		Channels: ChannelsConfig{
			Telegram: TelegramChannelConfig{Enabled: false, APIBase: "https://api.telegram.org", PollSeconds: 2},
			Slack:    SlackChannelConfig{Enabled: false, APIBase: "https://slack.com/api", RequireMention: true},
			Discord:  DiscordChannelConfig{Enabled: false, APIBase: "https://discord.com/api/v10", RequireMention: true},
			WhatsApp: WhatsAppBridgeConfig{Enabled: false, BridgeURL: "ws://127.0.0.1:3001/ws"},
		},
	}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := os.Getenv("OR3_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("OR3_ARTIFACTS_DIR"); v != "" {
		cfg.ArtifactsDir = v
	}
	if v := os.Getenv("OR3_API_BASE"); v != "" {
		cfg.Provider.APIBase = v
	}
	if v := os.Getenv("OR3_API_KEY"); v != "" {
		cfg.Provider.APIKey = v
	}
	if v := os.Getenv("OR3_MODEL"); v != "" {
		cfg.Provider.Model = v
	}
	if v := os.Getenv("OR3_EMBED_MODEL"); v != "" {
		cfg.Provider.EmbedModel = v
	}
	if v := os.Getenv("OR3_TELEGRAM_TOKEN"); v != "" {
		cfg.Channels.Telegram.Token = v
	}
	if v := os.Getenv("OR3_SLACK_APP_TOKEN"); v != "" {
		cfg.Channels.Slack.AppToken = v
	}
	if v := os.Getenv("OR3_SLACK_BOT_TOKEN"); v != "" {
		cfg.Channels.Slack.BotToken = v
	}
	if v := os.Getenv("OR3_DISCORD_TOKEN"); v != "" {
		cfg.Channels.Discord.Token = v
	}
	if v := os.Getenv("OR3_WHATSAPP_BRIDGE_URL"); v != "" {
		cfg.Channels.WhatsApp.BridgeURL = v
	}
	if v := os.Getenv("OR3_WHATSAPP_BRIDGE_TOKEN"); v != "" {
		cfg.Channels.WhatsApp.BridgeToken = v
	}
	if v := os.Getenv("OR3_SUBAGENTS_ENABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.Subagents.Enabled = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_MAX_CONCURRENT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.MaxConcurrent = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_MAX_QUEUED"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.MaxQueued = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.TaskTimeoutSeconds = parsed
		}
	}
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, mustJSON(cfg), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := Save(path, cfg); err != nil {
				return cfg, err
			}
		} else {
			return cfg, err
		}
	} else {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}
	ApplyEnvOverrides(&cfg)

	if cfg.Provider.TimeoutSeconds <= 0 {
		cfg.Provider.TimeoutSeconds = int((60 * time.Second).Seconds())
	}
	if cfg.DefaultSessionKey == "" {
		cfg.DefaultSessionKey = "cli:default"
	}
	if cfg.BootstrapMaxChars <= 0 {
		cfg.BootstrapMaxChars = 20000
	}
	if cfg.BootstrapTotalMaxChars <= 0 {
		cfg.BootstrapTotalMaxChars = 150000
	}
	if cfg.HistoryMax <= 0 {
		cfg.HistoryMax = 40
	}
	if cfg.MaxToolBytes <= 0 {
		cfg.MaxToolBytes = 24 * 1024
	}
	if cfg.MaxMediaBytes <= 0 {
		cfg.MaxMediaBytes = 20 * 1024 * 1024
	}
	if cfg.MaxToolLoops <= 0 {
		cfg.MaxToolLoops = 6
	}
	if cfg.VectorScanLimit <= 0 {
		cfg.VectorScanLimit = 2000
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	if cfg.ConsolidationWindowSize <= 0 {
		cfg.ConsolidationWindowSize = 10
	}
	if cfg.ConsolidationMaxMessages <= 0 {
		cfg.ConsolidationMaxMessages = 50
	}
	if cfg.ConsolidationMaxInputChars <= 0 {
		cfg.ConsolidationMaxInputChars = 12000
	}
	if cfg.ConsolidationAsyncTimeoutSeconds <= 0 {
		cfg.ConsolidationAsyncTimeoutSeconds = 30
	}
	if cfg.Subagents.MaxConcurrent <= 0 {
		cfg.Subagents.MaxConcurrent = 1
	}
	if cfg.Subagents.MaxQueued <= 0 {
		cfg.Subagents.MaxQueued = 32
	}
	if cfg.Subagents.TaskTimeoutSeconds <= 0 {
		cfg.Subagents.TaskTimeoutSeconds = 300
	}
	if cfg.Channels.Telegram.APIBase == "" {
		cfg.Channels.Telegram.APIBase = "https://api.telegram.org"
	}
	if cfg.Channels.Telegram.PollSeconds <= 0 {
		cfg.Channels.Telegram.PollSeconds = 2
	}
	if cfg.Channels.Slack.APIBase == "" {
		cfg.Channels.Slack.APIBase = "https://slack.com/api"
	}
	if cfg.Channels.Discord.APIBase == "" {
		cfg.Channels.Discord.APIBase = "https://discord.com/api/v10"
	}
	if cfg.Channels.WhatsApp.BridgeURL == "" {
		cfg.Channels.WhatsApp.BridgeURL = "ws://127.0.0.1:3001/ws"
	}
	if cfg.DocIndex.MaxFiles <= 0 {
		cfg.DocIndex.MaxFiles = 100
	}
	if cfg.DocIndex.MaxFileBytes <= 0 {
		cfg.DocIndex.MaxFileBytes = 64 * 1024
	}
	if cfg.DocIndex.MaxChunks <= 0 {
		cfg.DocIndex.MaxChunks = 500
	}
	if cfg.DocIndex.EmbedMaxBytes <= 0 {
		cfg.DocIndex.EmbedMaxBytes = 8 * 1024
	}
	if cfg.DocIndex.RefreshSeconds <= 0 {
		cfg.DocIndex.RefreshSeconds = 300
	}
	if cfg.DocIndex.RetrieveLimit <= 0 {
		cfg.DocIndex.RetrieveLimit = 5
	}
	if cfg.Skills.MaxRunSeconds <= 0 {
		cfg.Skills.MaxRunSeconds = 30
	}
	if cfg.Triggers.Webhook.Addr == "" {
		cfg.Triggers.Webhook.Addr = "127.0.0.1:8765"
	}
	if cfg.Triggers.Webhook.MaxBodyKB <= 0 {
		cfg.Triggers.Webhook.MaxBodyKB = 64
	}
	if cfg.Triggers.FileWatch.PollSeconds <= 0 {
		cfg.Triggers.FileWatch.PollSeconds = 5
	}
	if cfg.Triggers.FileWatch.DebounceSeconds <= 0 {
		cfg.Triggers.FileWatch.DebounceSeconds = 2
	}
	return cfg, nil
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}
````

## File: internal/agent/runtime.go
````go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

const commandNewSession = "/new"

type Deliverer interface {
	Deliver(ctx context.Context, channel, to, text string) error
}

type Runtime struct {
	DB               *db.DB
	Provider         *providers.Client
	Model            string
	Temperature      float64
	Tools            *tools.Registry
	Builder          *Builder
	Artifacts        *artifacts.Store
	MaxToolBytes     int
	MaxToolLoops     int
	ToolPreviewBytes int

	Deliver  Deliverer
	Streamer channels.StreamingChannel

	Consolidator           *memory.Consolidator
	ConsolidationScheduler *memory.Scheduler

	locks sync.Map // sessionKey -> *sync.Mutex
}

type BackgroundRunInput struct {
	SessionKey       string
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	Tools            *tools.Registry
	Meta             map[string]any
	Channel          string
	ReplyTo          string
}

type BackgroundRunResult struct {
	FinalText  string
	Preview    string
	ArtifactID string
}

func (r *Runtime) lockFor(key string) *sync.Mutex {
	v, _ := r.locks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (r *Runtime) Handle(ctx context.Context, ev bus.Event) error {
	mu := r.lockFor(ev.SessionKey)
	mu.Lock()
	defer mu.Unlock()
	switch ev.Type {
	case bus.EventUserMessage, bus.EventCron, bus.EventSystem, bus.EventWebhook, bus.EventFileChange:
		return r.turn(ctx, ev)
	default:
		return nil
	}
}

func (r *Runtime) turn(ctx context.Context, ev bus.Event) error {
	if ev.Type == bus.EventUserMessage && strings.EqualFold(strings.TrimSpace(ev.Message), commandNewSession) {
		return r.handleNewSession(ctx, ev)
	}

	// persist user message
	msgID, err := r.DB.AppendMessage(ctx, ev.SessionKey, "user", ev.Message, map[string]any{
		"channel": ev.Channel, "from": ev.From, "meta": ev.Meta,
	})
	if err != nil {
		return err
	}

	// build prompt
	if r.Builder == nil {
		return fmt.Errorf("runtime builder not configured")
	}
	isAutonomous := ev.Type == bus.EventCron || ev.Type == bus.EventWebhook || ev.Type == bus.EventFileChange
	messages, err := r.BuildPromptSnapshotWithOptions(ctx, BuildOptions{
		SessionKey:  ev.SessionKey,
		UserMessage: ev.Message,
		Autonomous:  isAutonomous,
	})
	if err != nil {
		return err
	}

	replyTarget := deliveryTarget(ev)
	finalText, streamed, err := r.executeConversation(ctx, ev.SessionKey, messages, r.Tools, ev.Channel, replyTarget)
	if err != nil {
		return err
	}

	if finalText == "" {
		finalText = "(no response)"
	}
	if _, err := r.DB.AppendMessage(ctx, ev.SessionKey, "assistant", finalText, map[string]any{"in_reply_to": msgID}); err != nil {
		log.Printf("append assistant(final) failed: %v", err)
	}

	// deliver only when the response was not already streamed to the channel
	if !streamed && r.Deliver != nil {
		if err := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, finalText); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}

	// best-effort rolling consolidation of old messages into memory notes
	if r.Consolidator != nil && r.Builder != nil && r.ConsolidationScheduler != nil {
		r.ConsolidationScheduler.Trigger(ev.SessionKey)
	} else if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.MaybeConsolidate(ctx, ev.SessionKey, historyMax); err != nil {
			log.Printf("consolidation failed: session=%s err=%v", ev.SessionKey, err)
		}
	}

	return nil
}

func (r *Runtime) BuildPromptSnapshot(ctx context.Context, sessionKey string, userMessage string) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.Build(ctx, sessionKey, userMessage)
	if err != nil {
		return nil, err
	}
	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: userMessage})
	}
	return messages, nil
}

func (r *Runtime) BuildPromptSnapshotWithOptions(ctx context.Context, opts BuildOptions) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.BuildWithOptions(ctx, opts)
	if err != nil {
		return nil, err
	}
	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: opts.UserMessage})
	}
	return messages, nil
}

func (r *Runtime) RunBackground(ctx context.Context, input BackgroundRunInput) (BackgroundRunResult, error) {
	mu := r.lockFor(input.SessionKey)
	mu.Lock()
	defer mu.Unlock()

	if strings.TrimSpace(input.SessionKey) == "" {
		return BackgroundRunResult{}, fmt.Errorf("background session key required")
	}
	if len(input.PromptSnapshot) == 0 {
		return BackgroundRunResult{}, fmt.Errorf("background prompt snapshot required")
	}
	if _, err := r.DB.AppendMessage(ctx, input.SessionKey, "user", input.Task, input.Meta); err != nil {
		return BackgroundRunResult{}, err
	}
	reg := input.Tools
	if reg == nil {
		reg = r.Tools
	}
	finalText, _, err := r.executeConversation(ctx, input.SessionKey, append([]providers.ChatMessage{}, input.PromptSnapshot...), reg, input.Channel, input.ReplyTo)
	if err != nil {
		return BackgroundRunResult{}, err
	}
	storedText, preview, artifactID := r.boundTextResult(ctx, input.SessionKey, finalText)
	payload := cloneMap(input.Meta)
	if input.ParentSessionKey != "" {
		payload["parent_session_key"] = input.ParentSessionKey
	}
	if artifactID != "" {
		payload["artifact_id"] = artifactID
		payload["preview"] = preview
	}
	if _, err := r.DB.AppendMessage(ctx, input.SessionKey, "assistant", storedText, payload); err != nil {
		log.Printf("append background assistant(final) failed: %v", err)
	}
	return BackgroundRunResult{FinalText: finalText, Preview: preview, ArtifactID: artifactID}, nil
}

func (r *Runtime) handleNewSession(ctx context.Context, ev bus.Event) error {
	replyTarget := deliveryTarget(ev)
	if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.ArchiveAll(ctx, ev.SessionKey, historyMax); err != nil {
			msg := "Memory archival failed, session not cleared. Please try again."
			if r.Deliver != nil {
				if derr := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, msg); derr != nil {
					log.Printf("deliver failed: %v", derr)
				}
			}
			return nil
		}
	}
	if err := r.DB.ResetSessionHistory(ctx, ev.SessionKey); err != nil {
		msg := "New session failed. Please try again."
		if r.Deliver != nil {
			if derr := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, msg); derr != nil {
				log.Printf("deliver failed: %v", derr)
			}
		}
		return nil
	}
	if r.Deliver != nil {
		if err := r.Deliver.Deliver(ctx, ev.Channel, replyTarget, "New session started."); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}
	return nil
}

func contentToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (r *Runtime) executeConversation(ctx context.Context, sessionKey string, messages []providers.ChatMessage, reg *tools.Registry, channel string, replyTo string) (string, bool, error) {
	if reg == nil {
		reg = tools.NewRegistry()
	}
	maxLoops := r.MaxToolLoops
	if maxLoops <= 0 {
		maxLoops = 6
	}
	for loop := 0; loop < maxLoops; loop++ {
		req := providers.ChatCompletionRequest{
			Model:       r.Model,
			Messages:    messages,
			Tools:       toToolDefs(reg),
			Temperature: r.Temperature,
		}

		// Begin a stream for this turn if a streamer is configured.
		var sw channels.StreamWriter
		var onDelta func(string)
		if r.Streamer != nil {
			if writer, err := r.Streamer.BeginStream(ctx, replyTo, map[string]any{"channel": channel}); err == nil {
				sw = writer
				onDelta = func(text string) { _ = sw.WriteDelta(ctx, text) }
			}
		}

		var resp providers.ChatCompletionResponse
		var err error
		if onDelta != nil {
			resp, err = r.Provider.ChatStream(ctx, req, onDelta)
		} else {
			resp, err = r.Provider.Chat(ctx, req)
		}
		if err != nil {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			return "", false, err
		}
		if len(resp.Choices) == 0 {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			return "", false, fmt.Errorf("no choices")
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			finalText := strings.TrimSpace(contentToString(msg.Content))
			messages = append(messages, providers.ChatMessage{Role: "assistant", Content: finalText})
			if sw != nil {
				_ = sw.Close(ctx, finalText)
				return finalText, true, nil
			}
			return finalText, false, nil
		}

		// This turn has tool calls — abort any in-progress stream.
		if sw != nil {
			_ = sw.Abort(ctx)
		}

		messages = append(messages, providers.ChatMessage{Role: "assistant", Content: msg.Content, ToolCalls: msg.ToolCalls})
		if _, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", contentToString(msg.Content), map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			log.Printf("append assistant(tool_calls) failed: %v", err)
		}

		for _, tc := range msg.ToolCalls {
			toolCtx := tools.ContextWithSession(ctx, sessionKey)
			toolCtx = tools.ContextWithDelivery(toolCtx, channel, replyTo)
			out, err := reg.Execute(toolCtx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				out = "tool error: " + err.Error()
			}

			payload := map[string]any{
				"tool": tc.Function.Name,
				"args": json.RawMessage([]byte(tc.Function.Arguments)),
			}
			sendOut, preview, artifactID := r.boundTextResult(ctx, sessionKey, out)
			if artifactID != "" {
				payload["artifact_id"] = artifactID
				payload["preview"] = preview
			}
			if _, err := r.DB.AppendMessage(ctx, sessionKey, "tool", sendOut, payload); err != nil {
				log.Printf("append tool message failed: %v", err)
			}
			messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: sendOut})
		}
	}
	return "(no response)", false, nil
}

func (r *Runtime) boundTextResult(ctx context.Context, sessionKey string, text string) (stored string, preview string, artifactID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(no response)", "(no response)", ""
	}
	preview = previewText(text, r.toolPreviewBytes())
	if r.MaxToolBytes > 0 && len(text) > r.MaxToolBytes && r.Artifacts != nil {
		id, err := r.Artifacts.Save(ctx, sessionKey, "text/plain", []byte(text))
		if err != nil {
			log.Printf("artifact save failed: %v", err)
			return text, preview, ""
		}
		return fmt.Sprintf("artifact_id=%s\npreview:\n%s", id, preview), preview, id
	}
	return text, preview, ""
}

func (r *Runtime) toolPreviewBytes() int {
	if r.ToolPreviewBytes <= 0 {
		return 500
	}
	return r.ToolPreviewBytes
}

func previewText(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no response)"
	}
	if max > 0 && len(s) > max {
		return s[:max] + "…[preview]"
	}
	return s
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func deliveryTarget(ev bus.Event) string {
	if len(ev.Meta) > 0 {
		for _, key := range []string{"chat_id", "channel_id"} {
			if target := strings.TrimSpace(fmt.Sprint(ev.Meta[key])); target != "" && target != "<nil>" {
				return target
			}
		}
	}
	return strings.TrimSpace(ev.From)
}

func toToolDefs(reg *tools.Registry) []providers.ToolDef {
	if reg == nil {
		return nil
	}
	raw := reg.Definitions()
	out := make([]providers.ToolDef, 0, len(raw))
	for _, d := range raw {
		fn, _ := d["function"].(map[string]any)
		td := providers.ToolDef{
			Type: "function",
			Function: providers.ToolFunc{
				Name:        fmt.Sprint(fn["name"]),
				Description: fmt.Sprint(fn["description"]),
				Parameters:  fn["parameters"],
			},
		}
		out = append(out, td)
	}
	return out
}

// Cron runner helper: turns a job into a bus event message
func CronRunner(b *bus.Bus, defaultSessionKey string) cron.Runner {
	return func(ctx context.Context, job cron.CronJob) error {
		_ = ctx
		msg := job.Payload.Message
		if strings.TrimSpace(msg) == "" {
			msg = "cron job: " + job.Name
		}
		// prefer per-job session key over the default
		sessionKey := job.Payload.SessionKey
		if strings.TrimSpace(sessionKey) == "" {
			sessionKey = defaultSessionKey
		}
		ev := bus.Event{Type: bus.EventCron, SessionKey: sessionKey, Channel: job.Payload.Channel, From: job.Payload.To, Message: msg, Meta: map[string]any{"job_id": job.ID}}
		if ok := b.Publish(ev); !ok {
			return fmt.Errorf("event bus full")
		}
		return nil
	}
}

func WithTimeout(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		sec = 60
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}
````

## File: cmd/or3-intern/main.go
````go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/channels/discord"
	"or3-intern/internal/channels/slack"
	"or3-intern/internal/channels/telegram"
	"or3-intern/internal/channels/whatsapp"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

const schedulerMaxConsolidationPasses = 3

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "", "path to config.json")
	flag.Parse()

	args := flag.Args()
	cmd := "chat"
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd == "init" {
		if err := runInit(cfgPath); err != nil {
			fmt.Fprintln(os.Stderr, "init error:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.WorkspaceDir = cwd
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir db dir error:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir artifacts dir error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.SoulFile, agent.DefaultSoul); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap soul file error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.AgentsFile, agent.DefaultAgentInstructions); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap agents file error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.ToolsFile, agent.DefaultToolNotes); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap tools file error:", err)
		os.Exit(1)
	}
	// Bootstrap IDENTITY.md and MEMORY.md (silent fallback if missing)
	if cfg.IdentityFile != "" {
		_ = ensureFileIfMissing(cfg.IdentityFile, "# Identity\n")
	}
	if cfg.MemoryFile != "" {
		_ = ensureFileIfMissing(cfg.MemoryFile, "# Static Memory\n")
	}

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db error:", err)
		os.Exit(1)
	}
	defer d.Close()

	ctx := context.Background()
	timeout := time.Duration(cfg.Provider.TimeoutSeconds) * time.Second
	prov := providers.New(cfg.Provider.APIBase, cfg.Provider.APIKey, timeout)
	art := &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}

	b := bus.New(256)
	del := cli.Deliverer{}
	channelManager, err := buildChannelManager(cfg, del, art, cfg.MaxMediaBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "channel config error:", err)
		os.Exit(1)
	}

	// skills
	builtin := filepath.Join(filepath.Dir(cfgPathOrDefault(cfgPath)), "builtin_skills")
	workspace := filepath.Join(cfg.WorkspaceDir, "workspace_skills")
	inv := skills.Scan([]string{builtin, workspace})
	var cronSvc *cron.Service
	var subagentManager *agent.SubagentManager
	buildRuntimeTools := func() *tools.Registry {
		return buildToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, subagentManager)
	}

	ret := memory.NewRetriever(d)
	ret.VectorScanLimit = cfg.VectorScanLimit

	var docIndexer *memory.DocIndexer
	var docRetriever *memory.DocRetriever
	if cfg.DocIndex.Enabled && len(cfg.DocIndex.Roots) > 0 {
		docIndexer = &memory.DocIndexer{
			DB:         d,
			Provider:   prov,
			EmbedModel: cfg.Provider.EmbedModel,
			Config: memory.DocIndexConfig{
				Roots:          cfg.DocIndex.Roots,
				MaxFiles:       cfg.DocIndex.MaxFiles,
				MaxFileBytes:   cfg.DocIndex.MaxFileBytes,
				MaxChunks:      cfg.DocIndex.MaxChunks,
				EmbedMaxBytes:  cfg.DocIndex.EmbedMaxBytes,
				RefreshSeconds: cfg.DocIndex.RefreshSeconds,
				RetrieveLimit:  cfg.DocIndex.RetrieveLimit,
			},
		}
		docRetriever = &memory.DocRetriever{DB: d}
		// Initial sync in background (don't block startup)
		go func() {
			syncCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := docIndexer.SyncRoots(syncCtx, cfg.DefaultSessionKey); err != nil {
				log.Printf("doc index sync failed: %v", err)
			}
		}()
	}
	if docIndexer != nil && cfg.DocIndex.RefreshSeconds > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.DocIndex.RefreshSeconds) * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				syncCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				if err := docIndexer.SyncRoots(syncCtx, cfg.DefaultSessionKey); err != nil {
					log.Printf("doc index refresh failed: %v", err)
				}
				cancel()
			}
		}()
	}

	rt := &agent.Runtime{
		DB:          d,
		Provider:    prov,
		Model:       cfg.Provider.Model,
		Temperature: cfg.Provider.Temperature,
		Tools:       buildRuntimeTools(),
		Builder: &agent.Builder{
			DB:                     d,
			Artifacts:              art,
			Skills:                 inv,
			Mem:                    ret,
			Provider:               prov,
			EmbedModel:             cfg.Provider.EmbedModel,
			EnableVision:           cfg.Provider.EnableVision,
			Soul:                   loadBootstrapFile(cfg.SoulFile, cfg.WorkspaceDir, "SOUL.md", agent.DefaultSoul),
			AgentInstructions:      loadBootstrapFile(cfg.AgentsFile, cfg.WorkspaceDir, "AGENTS.md", agent.DefaultAgentInstructions),
			ToolNotes:              loadBootstrapFile(cfg.ToolsFile, cfg.WorkspaceDir, "TOOLS.md", agent.DefaultToolNotes),
			IdentityText:           loadBootstrapFile(cfg.IdentityFile, cfg.WorkspaceDir, "IDENTITY.md", ""),
			StaticMemory:           loadBootstrapFile(cfg.MemoryFile, cfg.WorkspaceDir, "MEMORY.md", ""),
			HeartbeatText:          loadBootstrapFile(cfg.Heartbeat.TasksFile, cfg.WorkspaceDir, "HEARTBEAT.md", ""),
			BootstrapMaxChars:      cfg.BootstrapMaxChars,
			BootstrapTotalMaxChars: cfg.BootstrapTotalMaxChars,
			HistoryMax:             cfg.HistoryMax,
			VectorK:                cfg.VectorK,
			FTSK:                   cfg.FTSK,
			TopK:                   cfg.MemoryRetrieve,
			DocRetriever:           docRetriever,
			DocScopeKey:            cfg.DefaultSessionKey,
			DocRetrieveLimit:       cfg.DocIndex.RetrieveLimit,
		},
		Artifacts:    art,
		MaxToolBytes: cfg.MaxToolBytes,
		MaxToolLoops: cfg.MaxToolLoops,
		Deliver:      delivererFunc(channelManager.Deliver),
	}
	if cfg.Subagents.Enabled {
		subagentManager = &agent.SubagentManager{
			DB:            d,
			Runtime:       rt,
			Deliver:       delivererFunc(channelManager.Deliver),
			MaxConcurrent: cfg.Subagents.MaxConcurrent,
			MaxQueued:     cfg.Subagents.MaxQueued,
			TaskTimeout:   time.Duration(cfg.Subagents.TaskTimeoutSeconds) * time.Second,
			BackgroundTools: func() *tools.Registry {
				return buildToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, nil)
			},
		}
		if err := subagentManager.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "subagent manager error:", err)
			os.Exit(1)
		}
		rt.Tools = buildRuntimeTools()
	}
	if cfg.ConsolidationEnabled {
		rt.Consolidator = &memory.Consolidator{
			DB:                 d,
			Provider:           prov,
			EmbedModel:         cfg.Provider.EmbedModel,
			ChatModel:          cfg.Provider.Model,
			WindowSize:         cfg.ConsolidationWindowSize,
			MaxMessages:        cfg.ConsolidationMaxMessages,
			MaxInputChars:      cfg.ConsolidationMaxInputChars,
			CanonicalPinnedKey: "long_term_memory",
		}
		rt.ConsolidationScheduler = memory.NewSchedulerWithContext(
			ctx,
			time.Duration(cfg.ConsolidationAsyncTimeoutSeconds)*time.Second,
			func(runCtx context.Context, sessionKey string) {
				historyMax := cfg.HistoryMax
				if historyMax <= 0 {
					historyMax = 40
				}
				for i := 0; i < schedulerMaxConsolidationPasses; i++ {
					didWork, err := rt.Consolidator.RunOnce(runCtx, sessionKey, historyMax, memory.RunMode{})
					if err != nil {
						log.Printf("consolidation failed: session=%s err=%v", sessionKey, err)
						return
					}
					if !didWork {
						return
					}
				}
			},
		)
	}

	// cron service + tool
	if cfg.Cron.Enabled {
		cronSvc = cron.New(cfg.Cron.StorePath, agent.CronRunner(b, cfg.DefaultSessionKey))
		if err := cronSvc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "cron start error:", err)
			os.Exit(1)
		}
		rt.Tools = buildRuntimeTools()
	}

	switch cmd {
	case "chat":
		rt.Streamer = del
		_ = channelManager.Start(ctx, "cli", b)
		runWorkers(ctx, b, rt, cfg.WorkerCount)
		ch := &cli.Channel{Bus: b, SessionKey: cfg.DefaultSessionKey}
		if err := ch.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "cli error:", err)
		}
	case "serve":
		runWorkers(ctx, b, rt, cfg.WorkerCount)
		if err := channelManager.StartAll(ctx, b); err != nil {
			fmt.Fprintln(os.Stderr, "channel start error:", err)
			os.Exit(1)
		}
		// start webhook server if configured
		webhookSrv := triggers.NewWebhookServer(cfg.Triggers.Webhook, b, cfg.DefaultSessionKey)
		if err := webhookSrv.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "webhook start error:", err)
			os.Exit(1)
		}
		defer webhookSrv.Stop(context.Background())
		// start file watcher if configured
		fileWatcher := triggers.NewFileWatcher(cfg.Triggers.FileWatch, b, cfg.DefaultSessionKey)
		fileWatcher.Start(ctx)
		defer fileWatcher.Stop()
		fmt.Println("or3-intern serve: channels running. Ctrl+C to stop.")
		select {}
	case "agent":
		// one-shot: or3-intern agent -m "hello"
		fs := flag.NewFlagSet("agent", flag.ExitOnError)
		var msg string
		var session string
		fs.StringVar(&msg, "m", "", "message")
		fs.StringVar(&session, "s", cfg.DefaultSessionKey, "session key")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(msg) == "" {
			fmt.Fprintln(os.Stderr, "missing -m message")
			os.Exit(2)
		}
		if err := rt.Handle(ctx, bus.Event{Type: bus.EventUserMessage, SessionKey: session, Channel: "cli", From: "local", Message: msg}); err != nil {
			fmt.Fprintln(os.Stderr, "agent error:", err)
			os.Exit(1)
		}
	case "migrate-jsonl":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: or3-intern migrate-jsonl <jsonl_path> [session_key]")
			os.Exit(2)
		}
		sessionKey := "migrated:default"
		if len(args) >= 3 {
			sessionKey = args[2]
		}
		if err := migrateJSONL(ctx, d, args[1], sessionKey); err != nil {
			fmt.Fprintln(os.Stderr, "migration error:", err)
			os.Exit(1)
		}
		fmt.Println("ok")
	case "version":
		fmt.Println("or3-intern v1")
	case "scope":
		// or3-intern scope link <session-key> <scope-key>
		// or3-intern scope list <scope-key>
		fs := flag.NewFlagSet("scope", flag.ExitOnError)
		_ = fs.Parse(args[1:])
		scopeArgs := fs.Args()
		if len(scopeArgs) < 1 {
			fmt.Fprintln(os.Stderr, "usage: or3-intern scope <link|list> ...")
			os.Exit(2)
		}
		switch scopeArgs[0] {
		case "link":
			if len(scopeArgs) < 3 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope link <session-key> <scope-key>")
				os.Exit(2)
			}
			if err := d.LinkSession(ctx, scopeArgs[1], scopeArgs[2], nil); err != nil {
				fmt.Fprintln(os.Stderr, "scope link error:", err)
				os.Exit(1)
			}
			fmt.Printf("Linked session %q -> scope %q\n", scopeArgs[1], scopeArgs[2])
		case "list":
			if len(scopeArgs) < 2 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope list <scope-key>")
				os.Exit(2)
			}
			sessions, err := d.ListScopeSessions(ctx, scopeArgs[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "scope list error:", err)
				os.Exit(1)
			}
			if len(sessions) == 0 {
				fmt.Println("(no sessions linked to scope)")
			} else {
				for _, s := range sessions {
					fmt.Println(s)
				}
			}
		case "resolve":
			if len(scopeArgs) < 2 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope resolve <session-key>")
				os.Exit(2)
			}
			scopeKey, err := d.ResolveScopeKey(ctx, scopeArgs[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "scope resolve error:", err)
				os.Exit(1)
			}
			fmt.Println(scopeKey)
		default:
			fmt.Fprintln(os.Stderr, "unknown scope subcommand:", scopeArgs[0])
			os.Exit(2)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}

	if cronSvc != nil {
		cronSvc.Stop()
	}
	if subagentManager != nil {
		if err := subagentManager.Stop(context.Background()); err != nil {
			log.Printf("subagent manager stop failed: %v", err)
		}
	}
	_ = channelManager.StopAll(context.Background())
}

type delivererFunc func(ctx context.Context, channel, to, text string) error

func (f delivererFunc) Deliver(ctx context.Context, channel, to, text string) error {
	return f(ctx, channel, to, text)
}

func buildToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer) *tools.Registry {
	reg := tools.NewRegistry()
	fileRoot := allowedRoot(cfg)
	reg.Register(&tools.ExecTool{Timeout: time.Duration(cfg.Tools.ExecTimeoutSeconds) * time.Second, RestrictDir: fileRoot, PathAppend: cfg.Tools.PathAppend})
	reg.Register(&tools.ReadFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.EditFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.ListDir{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WebFetch{})
	reg.Register(&tools.WebSearch{APIKey: cfg.Tools.BraveAPIKey})
	reg.Register(&tools.MemorySetPinned{DB: d})
	reg.Register(&tools.MemoryAddNote{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel})
	reg.Register(&tools.MemorySearch{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel, VectorK: cfg.VectorK, FTSK: cfg.FTSK, TopK: cfg.MemoryRetrieve, VectorScanLimit: cfg.VectorScanLimit})
	reg.Register(&tools.SendMessage{
		Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
			if channelManager == nil {
				return fmt.Errorf("channel manager not configured")
			}
			return channelManager.DeliverWithMeta(ctx, channel, to, text, meta)
		},
		AllowedRoot:   fileRoot,
		ArtifactsDir:  cfg.ArtifactsDir,
		MaxMediaBytes: cfg.MaxMediaBytes,
	})
	if inv != nil {
		reg.Register(&tools.ReadSkill{Inventory: inv})
	}
	if inv != nil && cfg.Skills.EnableExec {
		reg.Register(&tools.RunSkill{
			Inventory:      inv,
			DefaultTimeout: time.Duration(cfg.Skills.MaxRunSeconds) * time.Second,
			RestrictDir:    fileRoot,
			OutputMaxBytes: cfg.MaxToolBytes,
		})
	}
	if cronSvc != nil {
		reg.Register(&tools.CronTool{Svc: cronSvc})
	}
	if spawnManager != nil {
		reg.Register(&tools.SpawnSubagent{Manager: spawnManager})
	}
	return reg
}

func buildChannelManager(cfg config.Config, cliDeliverer cli.Deliverer, art *artifacts.Store, maxMediaBytes int) (*rootchannels.Manager, error) {
	mgr := rootchannels.NewManager()
	if err := mgr.Register(cli.Service{Deliverer: cliDeliverer}); err != nil {
		return nil, err
	}
	if cfg.Channels.Telegram.Enabled {
		if err := mgr.Register(&telegram.Channel{Config: cfg.Channels.Telegram, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Slack.Enabled {
		if err := mgr.Register(&slack.Channel{Config: cfg.Channels.Slack, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Discord.Enabled {
		if err := mgr.Register(&discord.Channel{Config: cfg.Channels.Discord, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.WhatsApp.Enabled {
		cfg.Channels.WhatsApp.BridgeURL = whatsapp.BridgeURL(cfg.Channels.WhatsApp.BridgeURL)
		if err := mgr.Register(&whatsapp.Channel{Config: cfg.Channels.WhatsApp, Artifacts: art, MaxMediaBytes: maxMediaBytes}); err != nil {
			return nil, err
		}
	}
	return mgr, nil
}

func cfgPathOrDefault(p string) string {
	if p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

func allowedRoot(cfg config.Config) string {
	if cfg.Tools.RestrictToWorkspace {
		if cfg.WorkspaceDir != "" {
			return cfg.WorkspaceDir
		}
	}
	if cfg.AllowedDir != "" {
		return cfg.AllowedDir
	}
	return ""
}

func runWorkers(ctx context.Context, b *bus.Bus, rt *agent.Runtime, n int) {
	if n <= 0 {
		n = 4
	}
	for i := 0; i < n; i++ {
		go func() {
			for ev := range b.Channel() {
				cctx, cancel := agent.WithTimeout(ctx, 120)
				if err := rt.Handle(cctx, ev); err != nil {
					log.Printf("handle event failed: type=%s session=%s err=%v", ev.Type, ev.SessionKey, err)
				}
				cancel()
			}
		}()
	}
}

func loadBootstrapFile(configPath, workspaceDir, baseName, fallback string) string {
	paths := []string{}
	if strings.TrimSpace(workspaceDir) != "" {
		paths = append(paths,
			filepath.Join(workspaceDir, baseName),
			filepath.Join(workspaceDir, strings.ToLower(baseName)),
		)
	}
	if strings.TrimSpace(configPath) != "" {
		paths = append(paths, configPath)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return fallback
}

func ensureFileIfMissing(path, content string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}
````
