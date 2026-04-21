package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
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

func TestHeartbeatTextRefreshesFromFile(t *testing.T) {
	d := openTestDB(t)
	workspace := t.TempDir()
	heartbeatPath := filepath.Join(workspace, "HEARTBEAT.md")
	if err := os.WriteFile(heartbeatPath, []byte("# Heartbeat\n- first task"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := &Builder{
		DB:                 d,
		HistoryMax:         10,
		HeartbeatTasksFile: heartbeatPath,
		WorkspaceDir:       workspace,
	}

	first, _, err := b.BuildWithOptions(context.Background(), BuildOptions{
		SessionKey:  "sess",
		UserMessage: "check tasks",
		Autonomous:  true,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	if !strings.Contains(first.System[0].Content.(string), "first task") {
		t.Fatalf("expected first heartbeat text, got %q", first.System[0].Content.(string))
	}

	if err := os.WriteFile(heartbeatPath, []byte("# Heartbeat\n- second task"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	second, _, err := b.BuildWithOptions(context.Background(), BuildOptions{
		SessionKey:  "sess",
		UserMessage: "check tasks",
		Autonomous:  true,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	if !strings.Contains(second.System[0].Content.(string), "second task") {
		t.Fatalf("expected refreshed heartbeat text, got %q", second.System[0].Content.(string))
	}
	if strings.Contains(second.System[0].Content.(string), "first task") {
		t.Fatalf("expected stale heartbeat text to be replaced, got %q", second.System[0].Content.(string))
	}
}

// TestDocContextIncluded verifies that DocRetriever results appear in the system prompt.
func TestDocContextIncluded(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert a doc via UpsertDoc so RetrieveDocs can find it.
	err := memory.UpsertDoc(ctx, d, scope.GlobalMemoryScope, "/docs/guide.md", "markdown", "guide.md",
		"A guide for testing", "This document explains testing procedures in detail.", nil, "abc123", 0, 100)
	if err != nil {
		t.Fatalf("UpsertDoc: %v", err)
	}

	b := &Builder{
		DB:               d,
		HistoryMax:       10,
		DocRetriever:     &memory.DocRetriever{DB: d},
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
	noStructured := ""
	noDocContext := ""
	got := b.composeSystemPrompt(pinned, "", retrieved, noIdentity, noStaticMem, noHeartbeat, noStructured, noDocContext, "")
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

func TestPromptIncludesStructuredTriggerContext(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{DB: d, HistoryMax: 10}
	pp, _, err := b.BuildWithOptions(context.Background(), BuildOptions{
		SessionKey:  "sess",
		UserMessage: "incoming webhook",
		Autonomous:  true,
		EventMeta: map[string]any{"structured_event": map[string]any{
			"type":    "webhook",
			"source":  "webhook",
			"trusted": false,
			"details": map[string]any{"route": "github", "request_id": "req-1"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	sys := pp.System[0].Content.(string)
	if !strings.Contains(sys, "Structured Trigger Context") || !strings.Contains(sys, "request_id") {
		t.Fatalf("expected structured trigger context in prompt, got %q", sys)
	}
}

func TestWorkspaceContextIncluded(t *testing.T) {
	d := openTestDB(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# Project\nThis repo handles penguin logistics."), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	day := time.Now().Format("2006-01-02") + ".md"
	if err := os.WriteFile(filepath.Join(workspace, "memory", day), []byte("Discussed penguin migration plans."), 0o644); err != nil {
		t.Fatalf("write daily memory: %v", err)
	}
	b := &Builder{DB: d, HistoryMax: 10, WorkspaceDir: workspace}
	pp, _, err := b.Build(context.Background(), "sess", "penguin plans")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := pp.System[0].Content.(string)
	if !strings.Contains(sys, "Workspace Context") {
		t.Fatalf("expected workspace context section, got %q", sys)
	}
	if !strings.Contains(strings.ToLower(sys), "penguin") {
		t.Fatalf("expected workspace context to include workspace content, got %q", sys)
	}
}

// ---- Memory Digest tests ----

func TestFormatMemoryDigest_IncludesDurableKinds(t *testing.T) {
	retrieved := []memory.Retrieved{
		{ID: 1, Text: "user prefers dark mode", Kind: "preference", Status: "active", Score: 0.9},
		{ID: 2, Text: "project uses golang", Kind: "fact", Status: "active", Score: 0.8},
		{ID: 3, Text: "goal: ship v2 by Q3", Kind: "goal", Status: "active", Score: 0.7},
		{ID: 4, Text: "run make test for CI", Kind: "procedure", Status: "active", Score: 0.6},
		{ID: 5, Text: "rolling summary text", Kind: "summary", Status: "active", Score: 0.5},
	}
	digest := formatMemoryDigest(retrieved, 10)
	if !strings.Contains(digest, "preference") {
		t.Error("expected preference kind in digest")
	}
	if !strings.Contains(digest, "dark mode") {
		t.Error("expected preference text in digest")
	}
	if !strings.Contains(digest, "fact") {
		t.Error("expected fact kind in digest")
	}
	if !strings.Contains(digest, "goal") {
		t.Error("expected goal kind in digest")
	}
	if !strings.Contains(digest, "procedure") {
		t.Error("expected procedure kind in digest")
	}
	// Summaries and notes should NOT appear in digest.
	if strings.Contains(digest, "rolling summary text") {
		t.Error("expected summary to be excluded from digest")
	}
}

func TestFormatMemoryDigest_ExcludesStaleNotes(t *testing.T) {
	retrieved := []memory.Retrieved{
		{ID: 1, Text: "active preference", Kind: "preference", Status: "active", Score: 0.9},
		{ID: 2, Text: "stale preference", Kind: "preference", Status: "stale", Score: 0.8},
	}
	digest := formatMemoryDigest(retrieved, 10)
	if strings.Contains(digest, "stale preference") {
		t.Error("stale note should be excluded from digest")
	}
	if !strings.Contains(digest, "active preference") {
		t.Error("active note should be in digest")
	}
}

func TestFormatMemoryDigest_EmptyWhenNoEligibleNotes(t *testing.T) {
	retrieved := []memory.Retrieved{
		{ID: 1, Text: "just a summary", Kind: "summary", Status: "active", Score: 0.9},
		{ID: 2, Text: "a plain note", Kind: "note", Status: "active", Score: 0.8},
	}
	digest := formatMemoryDigest(retrieved, 10)
	if digest != "" {
		t.Errorf("expected empty digest when no durable kinds, got %q", digest)
	}
}

func TestFormatMemoryDigest_BoundedByMaxLines(t *testing.T) {
	retrieved := make([]memory.Retrieved, 20)
	for i := range retrieved {
		retrieved[i] = memory.Retrieved{
			ID:     int64(i + 1),
			Text:   "some important fact",
			Kind:   "fact",
			Status: "active",
			Score:  float64(20-i) / 20.0,
		}
	}
	digest := formatMemoryDigest(retrieved, 5)
	lines := strings.Split(strings.TrimSpace(digest), "\n")
	if len(lines) > 5 {
		t.Errorf("expected at most 5 digest lines, got %d", len(lines))
	}
}

func TestFormatMemoryDigest_EmptyInput(t *testing.T) {
	digest := formatMemoryDigest(nil, 10)
	if digest != "" {
		t.Errorf("expected empty digest for nil input, got %q", digest)
	}
}

// TestPromptIncludesMemoryDigest verifies that the Memory Digest section
// appears in the system prompt when retrieved notes include durable kinds.
func TestPromptIncludesMemoryDigest(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert a preference note so retrieval can find it.
	_, err := d.InsertMemoryNoteTyped(ctx, "sess", db.TypedNoteInput{
		Text:   "user prefers verbose logging",
		Kind:   db.MemoryKindPreference,
		Status: db.MemoryStatusActive,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}

	b := &Builder{DB: d, HistoryMax: 10}
	pp, _, err := b.BuildWithOptions(ctx, BuildOptions{
		SessionKey:  "sess",
		UserMessage: "verbose logging",
	})
	if err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	sys := pp.System[0].Content.(string)
	// Only check digest section if FTS actually returned results.
	if strings.Contains(sys, "verbose logging") && strings.Contains(sys, "preference") {
		if !strings.Contains(sys, "## Memory Digest") {
			t.Errorf("expected 'Memory Digest' section when durable notes retrieved, got %q", sys)
		}
	}
}

func TestComposeSystemPrompt_MemoryDigestBetweenPinnedAndRetrieved(t *testing.T) {
	b := &Builder{}
	got := b.composeSystemPrompt("pinned content", "digest content", "retrieved content", "", "", "", "", "", "")
	pinnedIdx := strings.Index(got, "## Pinned Memory")
	digestIdx := strings.Index(got, "## Memory Digest")
	retrievedIdx := strings.Index(got, "## Retrieved Memory")
	if pinnedIdx < 0 || digestIdx < 0 || retrievedIdx < 0 {
		t.Fatalf("missing sections: pinned=%d digest=%d retrieved=%d", pinnedIdx, digestIdx, retrievedIdx)
	}
	if !(pinnedIdx < digestIdx && digestIdx < retrievedIdx) {
		t.Errorf("expected order Pinned < Digest < Retrieved, got %d %d %d", pinnedIdx, digestIdx, retrievedIdx)
	}
}

func TestComposeSystemPrompt_DigestOmittedWhenEmpty(t *testing.T) {
	b := &Builder{}
	got := b.composeSystemPrompt("(none)", "", "(none)", "", "", "", "", "", "")
	if strings.Contains(got, "## Memory Digest") {
		t.Error("expected 'Memory Digest' section to be omitted when digestText is empty")
	}
}

func testPromptProvider(t *testing.T) *providers.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embeddings":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": []float32{1, 0}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p := providers.New(srv.URL, "test-key", 5*time.Second)
	p.HTTP = srv.Client()
	return p
}

func TestCachedEmbed_FingerprintSeparatesCacheEntries(t *testing.T) {
	promptEmbedCache.mu.Lock()
	promptEmbedCache.entries = map[embedCacheKey]embedCacheEntry{}
	promptEmbedCache.mu.Unlock()

	var mu sync.Mutex
	embedCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		embedCalls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{1, 0}}},
		})
	}))
	defer srv.Close()

	p := providers.New(srv.URL, "test-key", 5*time.Second)
	p.HTTP = srv.Client()
	ctx := context.Background()
	if _, err := cachedEmbed(ctx, p, "provider-a:embed", "embed", "hello"); err != nil {
		t.Fatalf("cachedEmbed first: %v", err)
	}
	if _, err := cachedEmbed(ctx, p, "provider-a:embed", "embed", "hello"); err != nil {
		t.Fatalf("cachedEmbed second: %v", err)
	}
	if _, err := cachedEmbed(ctx, p, "provider-b:embed", "embed", "hello"); err != nil {
		t.Fatalf("cachedEmbed third: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if embedCalls != 2 {
		t.Fatalf("expected 2 embed calls across distinct fingerprints, got %d", embedCalls)
	}
}

func TestBuildWithOptions_UsageLoggingOnlyForIncludedPromptNotes(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := d.InsertMemoryNoteTyped(ctx, "sess", db.TypedNoteInput{
			Text:   "shared fact for digest and retrieval " + strings.Repeat("x", i+1),
			Kind:   db.MemoryKindFact,
			Status: db.MemoryStatusActive,
		}); err != nil {
			t.Fatalf("InsertMemoryNoteTyped %d: %v", i, err)
		}
	}
	b := &Builder{
		DB:                d,
		Mem:               memory.NewRetriever(d),
		Provider:          testPromptProvider(t),
		EmbedModel:        "embed",
		HistoryMax:        10,
		FTSK:              5,
		TopK:              5,
		BootstrapMaxChars: 70,
	}
	if _, _, err := b.BuildWithOptions(ctx, BuildOptions{SessionKey: "sess", UserMessage: "shared fact"}); err != nil {
		t.Fatalf("BuildWithOptions: %v", err)
	}
	rows, err := d.SearchFTS(ctx, "sess", "shared fact", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	touched := 0
	untouched := 0
	for _, row := range rows {
		if row.UseCount > 0 {
			touched++
		} else {
			untouched++
		}
	}
	if touched == 0 {
		t.Fatalf("expected at least one prompted note to be touched, got %#v", rows)
	}
	if untouched == 0 {
		t.Fatalf("expected at least one excluded note to remain untouched, got %#v", rows)
	}
}
