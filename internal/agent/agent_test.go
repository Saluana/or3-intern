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
	got := b.composeSystemPrompt("(none)", "", "(none)", "", "", "", "", "", "")
	if !strings.Contains(got, "System Prompt") {
		t.Errorf("expected '# System Prompt' in output, got %q", got)
	}
	if !strings.Contains(got, "SOUL.md") {
		t.Errorf("expected 'SOUL.md' section, got %q", got)
	}
}

func TestBuilder_ComposeSystemPrompt_CustomSoul(t *testing.T) {
	b := &Builder{Soul: "Custom soul text"}
	got := b.composeSystemPrompt("(none)", "", "(none)", "", "", "", "", "", "")
	if !strings.Contains(got, "Custom soul text") {
		t.Errorf("expected custom soul in output, got %q", got)
	}
}

func TestBuilder_ComposeSystemPrompt_Truncation(t *testing.T) {
	b := &Builder{
		BootstrapTotalMaxChars: 50,
	}
	got := b.composeSystemPrompt("(none)", "", "(none)", "", "", "", "", "", "")
	if len(got) > 100 { // allow for "[truncated]" suffix
		t.Fatalf("expected bounded prompt, got len=%d", len(got))
	}
}

func TestBuilder_ComposeSystemPrompt_WithPinned(t *testing.T) {
	b := &Builder{}
	got := b.composeSystemPrompt("- name: Alice", "", "(none)", "", "", "", "", "", "")
	if !strings.Contains(got, "name: Alice") {
		t.Errorf("expected pinned memory in output, got %q", got)
	}
}

func TestBuilder_ComposeSystemPrompt_WithSkills(t *testing.T) {
	dir := t.TempDir()
	_ = skills.Scan([]string{dir}) // use empty skills
	b := &Builder{}
	got := b.composeSystemPrompt("(none)", "", "(none)", "", "", "", "", "", "")
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

	if _, err := d.AppendMessage(ctx, "s1", "user", "first message", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "s1", "assistant", "first response", nil); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}

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

	if _, err := d.AppendMessage(ctx, "s2", "user", "user msg", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	// Append assistant message with tool_calls payload
	if _, err := d.AppendMessage(ctx, "s2", "assistant", "tool content", map[string]any{
		"tool_calls": []map[string]any{
			{"id": "tc1", "type": "function", "function": map[string]any{"name": "test_tool", "arguments": "{}"}},
		},
	}); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}

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

func TestBuilder_Build_ToolResultHistoryPreservesToolCallID(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.AppendMessage(ctx, "s-tool", "user", "user msg", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "s-tool", "assistant", "", map[string]any{
		"tool_calls": []map[string]any{
			{"id": "tc1", "type": "function", "function": map[string]any{"name": "test_tool", "arguments": "{}"}},
		},
	}); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "s-tool", "tool", "tool result", map[string]any{
		"tool":         "test_tool",
		"tool_call_id": "tc1",
		"args":         map[string]any{},
	}); err != nil {
		t.Fatalf("AppendMessage tool: %v", err)
	}

	b := &Builder{DB: d, HistoryMax: 10}
	pp, _, err := b.Build(ctx, "s-tool", "next msg")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, m := range pp.History {
		if m.Role == "tool" {
			if m.ToolCallID != "tc1" {
				t.Fatalf("expected tool_call_id tc1, got %#v", m)
			}
			return
		}
	}
	t.Fatal("expected tool message in history")
}

func TestBuilder_Build_LegacyToolResultHistoryBackfillsToolCallID(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.AppendMessage(ctx, "s-legacy", "user", "user msg", nil); err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "s-legacy", "assistant", "", map[string]any{
		"tool_calls": []map[string]any{
			{"id": "tc-legacy", "type": "function", "function": map[string]any{"name": "test_tool", "arguments": "{}"}},
		},
	}); err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "s-legacy", "tool", "tool result", map[string]any{
		"tool": "test_tool",
		"args": map[string]any{},
	}); err != nil {
		t.Fatalf("AppendMessage tool: %v", err)
	}

	b := &Builder{DB: d, HistoryMax: 10}
	pp, _, err := b.Build(ctx, "s-legacy", "next msg")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, m := range pp.History {
		if m.Role == "tool" {
			if m.ToolCallID != "tc-legacy" {
				t.Fatalf("expected legacy tool message to backfill tool_call_id, got %#v", m)
			}
			return
		}
	}
	t.Fatal("expected tool message in history")
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

	if err := runner(context.Background(), job); err != nil {
		t.Fatalf("runner: %v", err)
	}

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
