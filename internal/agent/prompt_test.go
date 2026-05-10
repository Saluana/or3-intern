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
	"sync"
	"testing"
	"time"

	"or3-intern/internal/artifacts"
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

func TestBuildWithOptions_IncludesPinnedMemoryAndSkillsInventory(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := d.UpsertPinned(ctx, "sess", "project_rule", "always keep prompts deterministic"); err != nil {
		t.Fatalf("UpsertPinned: %v", err)
	}
	b := &Builder{DB: d, HistoryMax: 10}
	pp, _, err := b.Build(context.Background(), "sess", "hello")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sys := pp.System[0].Content.(string)
	if !strings.Contains(sys, "## Pinned Memory") || !strings.Contains(sys, "always keep prompts deterministic") {
		t.Fatalf("expected pinned memory section and content, got %q", sys)
	}
	if !strings.Contains(sys, "## Skills Inventory") {
		t.Fatalf("expected skills inventory section, got %q", sys)
	}
	for _, section := range []string{"## SOUL.md", "## AGENTS.md", "## TOOLS.md", "## Retrieved Memory"} {
		if !strings.Contains(sys, section) {
			t.Fatalf("expected section %q in system prompt, got %q", section, sys)
		}
	}
}

func TestDefaultToolNotesCoverCoreBuiltins(t *testing.T) {
	for _, want := range []string{
		"Use list_dir before reading a directory",
		"Use read_artifact when another tool returns an artifact_id",
		"Use memory_recent for recent conversation context",
		"Use web_search to discover candidate URLs",
		"Prefer program + args over shell command strings",
		"When a skill or doc shows a CLI like",
		"Use run_skill for approved skills",
		"run_skill freezes a plan before approval",
		"retry the identical executable and argv after approval",
		"Use send_message only when delivery is part of the task",
		"Use spawn_subagent for longer background work",
		"Use cron only for scheduled reminders or recurring tasks",
	} {
		if !strings.Contains(DefaultToolNotes, want) {
			t.Fatalf("expected default tool notes to include %q, got %q", want, DefaultToolNotes)
		}
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
	if !strings.Contains(digest, "Preference:") {
		t.Error("expected readable preference label in digest")
	}
	if !strings.Contains(digest, "dark mode") {
		t.Error("expected preference text in digest")
	}
	if !strings.Contains(digest, "Fact:") {
		t.Error("expected readable fact label in digest")
	}
	if !strings.Contains(digest, "Goal:") {
		t.Error("expected readable goal label in digest")
	}
	if !strings.Contains(digest, "Procedure:") {
		t.Error("expected readable procedure label in digest")
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

func TestBuildUserContentAndImagePartBranches(t *testing.T) {
	ctx := context.Background()
	store, image := newPromptTestArtifact(t, []byte("image-bytes"))
	builder := &Builder{Artifacts: store, EnableVision: true}

	if got := (&Builder{}).buildUserContent(ctx, "hello", []artifacts.Attachment{image}, newVisionBudget()); got != "hello" {
		t.Fatalf("expected disabled vision to keep plain text, got %#v", got)
	}
	if got := (&Builder{EnableVision: true}).buildUserContent(ctx, "hello", []artifacts.Attachment{image}, newVisionBudget()); got != "hello" {
		t.Fatalf("expected nil artifact store to keep plain text, got %#v", got)
	}
	if got := builder.buildUserContent(ctx, "hello", nil, newVisionBudget()); got != "hello" {
		t.Fatalf("expected empty attachments to keep plain text, got %#v", got)
	}
	if got := builder.buildUserContent(ctx, "hello", []artifacts.Attachment{{ArtifactID: "file-1", Filename: "notes.txt", Mime: "text/plain", Kind: artifacts.KindFile}}, newVisionBudget()); got != "hello" {
		t.Fatalf("expected non-image attachments to keep plain text, got %#v", got)
	}
	if got := builder.buildUserContent(ctx, "hello", []artifacts.Attachment{{ArtifactID: "missing", Filename: "photo.png", Mime: "image/png", Kind: artifacts.KindImage}}, newVisionBudget()); got != "hello" {
		t.Fatalf("expected failed image lookup to fall back to text, got %#v", got)
	}

	content := builder.buildUserContent(ctx, "hello", []artifacts.Attachment{
		{ArtifactID: "file-1", Filename: "notes.txt", Mime: "text/plain", Kind: artifacts.KindFile},
		image,
	}, newVisionBudget())
	parts, ok := content.([]map[string]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("expected text plus one image part, got %#v", content)
	}
	if parts[0]["type"] != "text" || parts[1]["type"] != "image_url" {
		t.Fatalf("unexpected content parts: %#v", parts)
	}
}

func TestImagePartAndReadCappedFileBranches(t *testing.T) {
	ctx := context.Background()
	store, image := newPromptTestArtifact(t, []byte("1234"))
	builder := &Builder{Artifacts: store, EnableVision: true}

	for name, budget := range map[string]*visionBudget{
		"nil budget":     nil,
		"no images left": {remainingImages: 0, remainingBytes: 10},
		"no bytes left":  {remainingImages: 1, remainingBytes: 0},
		"lookup error":   {remainingImages: 1, remainingBytes: 10},
	} {
		t.Run(name, func(t *testing.T) {
			att := image
			if name == "lookup error" {
				att.ArtifactID = "missing"
			}
			if part, ok := builder.imagePart(ctx, att, budget); ok || part != nil {
				t.Fatalf("expected imagePart to skip for %s, got %#v ok=%v", name, part, ok)
			}
		})
	}

	budget := &visionBudget{remainingImages: 1, remainingBytes: 10}
	part, ok := builder.imagePart(ctx, image, budget)
	if !ok || part["type"] != "image_url" {
		t.Fatalf("expected valid image part, got %#v ok=%v", part, ok)
	}
	if budget.remainingImages != 0 || budget.remainingBytes != 6 {
		t.Fatalf("expected budget to decrement after success, got %+v", budget)
	}

	stored, err := store.Lookup(ctx, image.ArtifactID)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if data, err := readCappedFile(stored.Path, 4); err != nil || string(data) != "1234" {
		t.Fatalf("readCappedFile success: data=%q err=%v", data, err)
	}
	if _, err := readCappedFile(stored.Path, 3); err == nil || !strings.Contains(err.Error(), "vision limit") {
		t.Fatalf("expected vision limit error, got %v", err)
	}

	if err := os.Remove(stored.Path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if part, ok := builder.imagePart(ctx, image, &visionBudget{remainingImages: 1, remainingBytes: 10}); ok || part != nil {
		t.Fatalf("expected deleted artifact path to fail, got %#v ok=%v", part, ok)
	}

	store, image = newPromptTestArtifact(t, []byte("1234"))
	if _, err := store.DB.SQL.ExecContext(ctx, `UPDATE artifacts SET size_bytes=1 WHERE id=?`, image.ArtifactID); err != nil {
		t.Fatalf("shrink artifact size metadata: %v", err)
	}
	if part, ok := (&Builder{Artifacts: store, EnableVision: true}).imagePart(ctx, image, &visionBudget{remainingImages: 1, remainingBytes: 2}); ok || part != nil {
		t.Fatalf("expected post-read remainingBytes guard to fail, got %#v ok=%v", part, ok)
	}
}

func TestPayloadHelpersAndAttachmentDecoding(t *testing.T) {
	toolShapes := []struct {
		name string
		raw  any
	}{
		{
			name: "tool call slice",
			raw: []providers.ToolCall{{Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "lookup", Arguments: `{"id":1}`}}},
		},
		{
			name: "any slice",
			raw: []any{
				map[string]any{"id": "a", "index": json.Number("2"), "function": map[string]any{"name": "lookup", "arguments": `{"id":2}`}},
				map[string]any{"function": map[string]any{"name": "", "arguments": `{}`}},
			},
		},
		{
			name: "map slice",
			raw: []map[string]any{
				{"id": "b", "index": float64(3), "type": "", "function": map[string]any{"name": "search", "arguments": `{}`}},
			},
		},
	}
	for _, tc := range toolShapes {
		t.Run(tc.name, func(t *testing.T) {
			got := toolCallsFromPayload(tc.raw)
			if len(got) != 1 || got[0].Function.Name == "" || got[0].Type != "function" {
				t.Fatalf("toolCallsFromPayload(%s)=%#v", tc.name, got)
			}
		})
	}
	if got := toolCallsFromPayload("not-a-slice"); got != nil {
		t.Fatalf("expected unsupported tool payload shape to return nil, got %#v", got)
	}

	atts := attachmentsFromPayload(map[string]any{
		"meta": map[string]any{
			"attachments": []any{
				map[string]any{"artifact_id": "img-1", "filename": "photo.png", "mime": "image/png", "size_bytes": json.Number("12")},
				map[string]any{"artifact_id": "file-1", "filename": "", "mime": "text/plain"},
				map[string]any{"artifact_id": "", "filename": "skip.txt"},
			},
		},
	})
	if len(atts) != 2 {
		t.Fatalf("expected two valid attachments, got %#v", atts)
	}
	if atts[0].Kind != artifacts.KindImage || atts[1].Filename != "attachment" || atts[1].Kind != artifacts.KindFile {
		t.Fatalf("expected attachment defaults and kind detection, got %#v", atts)
	}
	if got := attachmentsFromRaw([]artifacts.Attachment{{ArtifactID: "id-1", Filename: "doc.txt"}}); len(got) != 1 {
		t.Fatalf("expected attachment slice passthrough, got %#v", got)
	}
	if got := attachmentsFromRaw("bad-shape"); got != nil {
		t.Fatalf("expected unsupported attachment payload shape to return nil, got %#v", got)
	}
}

func TestPayloadScalarHelpers(t *testing.T) {
	if got := payloadStringValue("  hi "); got != "hi" {
		t.Fatalf("payloadStringValue(string)=%q", got)
	}
	if got := payloadStringValue(json.Number("42")); got != "42" {
		t.Fatalf("payloadStringValue(json.Number)=%q", got)
	}
	if got := payloadStringValue(nil); got != "" {
		t.Fatalf("payloadStringValue(nil)=%q", got)
	}
	if got := payloadIntValue(json.Number("7")); got != 7 {
		t.Fatalf("payloadIntValue(json.Number)=%d", got)
	}
	if got := payloadIntValue(float64(8.9)); got != 8 {
		t.Fatalf("payloadIntValue(float64)=%d", got)
	}
	if got := payloadInt64Value(9); got != 9 {
		t.Fatalf("payloadInt64Value(int)=%d", got)
	}
	if got := payloadInt64Value(float64(10.9)); got != 10 {
		t.Fatalf("payloadInt64Value(float64)=%d", got)
	}
	if got := payloadInt64Value(nil); got != 0 {
		t.Fatalf("payloadInt64Value(nil)=%d", got)
	}
}

func TestCachedEmbed_ErrorPathsAndLRUEviction(t *testing.T) {
	resetPromptEmbedCache(t)
	if _, err := cachedEmbed(context.Background(), nil, "fp", "embed", "hello"); err == nil || !strings.Contains(err.Error(), "provider not configured") {
		t.Fatalf("expected provider nil error, got %v", err)
	}

	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer errServer.Close()
	errProvider := providers.New(errServer.URL, "test-key", 5*time.Second)
	errProvider.HTTP = errServer.Client()
	if _, err := cachedEmbed(context.Background(), errProvider, "fp", "embed", "hello"); err == nil {
		t.Fatalf("expected embed error")
	}

	now := time.Now()
	promptEmbedCache.mu.Lock()
	for i := 0; i < embedCacheMaxEntries; i++ {
		key := embedCacheKey{fingerprint: "fp", model: "embed", input: fmt.Sprintf("input-%03d", i)}
		promptEmbedCache.entries[key] = embedCacheEntry{
			vec:       []float32{float32(i)},
			expiresAt: now.Add(time.Minute),
			usedAt:    now.Add(time.Duration(i) * time.Second),
		}
	}
	promptEmbedCache.mu.Unlock()

	p := testPromptProvider(t)
	if _, err := cachedEmbed(context.Background(), p, "fp", "embed", "new-input"); err != nil {
		t.Fatalf("cachedEmbed eviction path: %v", err)
	}
	promptEmbedCache.mu.Lock()
	defer promptEmbedCache.mu.Unlock()
	if len(promptEmbedCache.entries) != embedCacheMaxEntries {
		t.Fatalf("expected cache to stay capped at %d, got %d", embedCacheMaxEntries, len(promptEmbedCache.entries))
	}
	if _, ok := promptEmbedCache.entries[embedCacheKey{fingerprint: "fp", model: "embed", input: "input-000"}]; ok {
		t.Fatalf("expected least-recently-used entry to be evicted")
	}
	if _, ok := promptEmbedCache.entries[embedCacheKey{fingerprint: "fp", model: "embed", input: "new-input"}]; !ok {
		t.Fatalf("expected new entry to be cached")
	}
}

func TestCurrentHeartbeatTextFallbacks(t *testing.T) {
	if got := (*Builder)(nil).currentHeartbeatText(); got != "" {
		t.Fatalf("expected nil builder to return empty string, got %q", got)
	}

	workspace := t.TempDir()
	heartbeatPath := filepath.Join(workspace, "HEARTBEAT.md")
	if err := os.Mkdir(heartbeatPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if got := (&Builder{WorkspaceDir: workspace, HeartbeatText: " fallback "}).currentHeartbeatText(); got != "fallback" {
		t.Fatalf("expected read error to fall back to HeartbeatText, got %q", got)
	}

	if err := os.Remove(heartbeatPath); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.WriteFile(heartbeatPath, []byte("# Heartbeat\n<!-- comments only -->"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := (&Builder{WorkspaceDir: workspace, HeartbeatText: "fallback"}).currentHeartbeatText(); got != "" {
		t.Fatalf("expected comment-only file to suppress heartbeat text, got %q", got)
	}
}

func newPromptTestArtifact(t *testing.T, data []byte) (*artifacts.Store, artifacts.Attachment) {
	t.Helper()
	store := &artifacts.Store{
		Dir: filepath.Join(t.TempDir(), "artifacts"),
		DB:  openTestDB(t),
	}
	att, err := store.SaveNamed(context.Background(), "sess", "photo.png", "image/png", data)
	if err != nil {
		t.Fatalf("SaveNamed: %v", err)
	}
	return store, att
}

func resetPromptEmbedCache(t *testing.T) {
	t.Helper()
	promptEmbedCache.mu.Lock()
	promptEmbedCache.entries = map[embedCacheKey]embedCacheEntry{}
	promptEmbedCache.mu.Unlock()
	t.Cleanup(func() {
		promptEmbedCache.mu.Lock()
		promptEmbedCache.entries = map[embedCacheKey]embedCacheEntry{}
		promptEmbedCache.mu.Unlock()
	})
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

func TestStablePrefixIsByteStableAcrossTurns(t *testing.T) {
	b := &Builder{
		Soul:              "Soul",
		AgentInstructions: "Agent",
		ToolNotes:         "Tools",
		IdentityText:      "Identity",
		StaticMemory:      "Static",
	}
	pinned := "- key: value"
	digest := "- [fact] x"
	retrieved := "1) [memory] y"
	docs := "1) [file] z"
	workspace := "workspace snippets"

	first := b.renderStablePrefix(pinned, digest, retrieved, b.IdentityText, b.StaticMemory, docs, workspace)
	second := b.renderStablePrefix(pinned, digest, retrieved, b.IdentityText, b.StaticMemory, docs, workspace)
	if first != second {
		t.Fatalf("expected byte-stable prefix across identical turns")
	}
}

func TestStablePrefixExcludesHeartbeatAndTriggerMetadata(t *testing.T) {
	b := &Builder{HeartbeatText: "tick"}
	stable := b.renderStablePrefix("(none)", "", "(none)", "", "", "", "")
	if strings.Contains(stable, "Heartbeat") || strings.Contains(stable, "Structured Trigger Context") || strings.Contains(stable, "Runtime Context") {
		t.Fatalf("stable prefix must not include volatile heartbeat or trigger metadata: %q", stable)
	}
	volatile := b.renderVolatileSuffix("tick", "{\"event\":\"cron\"}")
	if !strings.Contains(volatile, "Heartbeat") || !strings.Contains(volatile, "Structured Trigger Context") || !strings.Contains(volatile, "Runtime Context") {
		t.Fatalf("volatile suffix should include runtime, heartbeat, and structured context: %q", volatile)
	}
}

func TestVolatileSuffixIncludesDateAndWorkingDirectory(t *testing.T) {
	b := &Builder{WorkspaceDir: "/tmp/or3-workspace"}
	volatile := b.renderVolatileSuffix("", "")
	if !strings.Contains(volatile, "Runtime Context") {
		t.Fatalf("expected runtime context section, got %q", volatile)
	}
	if !strings.Contains(volatile, time.Now().Format("2006-01-02")) {
		t.Fatalf("expected current date in runtime context, got %q", volatile)
	}
	if !strings.Contains(volatile, "/tmp/or3-workspace") {
		t.Fatalf("expected working directory in runtime context, got %q", volatile)
	}
}

func TestComposeSystemContent_UsesExplicitCacheBoundaryWhenSupported(t *testing.T) {
	b := &Builder{
		Provider: &providers.Client{APIBase: "https://api.anthropic.example/v1"},
		Soul:     "Soul",
	}
	content := b.composeSystemContent("- key: value", "digest", "retrieved", "identity", "static", "heartbeat", "trigger", "docs", "workspace")
	parts, ok := content.([]map[string]any)
	if !ok {
		t.Fatalf("expected structured content parts for explicit cache provider, got %T", content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected stable+volatile parts, got %#v", parts)
	}
	cacheControl, ok := parts[0]["cache_control"].(map[string]any)
	if !ok || cacheControl["type"] != "ephemeral" {
		t.Fatalf("expected cache_control breakpoint on stable prefix, got %#v", parts[0])
	}
	if strings.Contains(fmt.Sprint(parts[0]["text"]), "Heartbeat") || strings.Contains(fmt.Sprint(parts[0]["text"]), "Structured Trigger Context") {
		t.Fatalf("stable part should not contain volatile sections: %#v", parts[0])
	}
	if !strings.Contains(fmt.Sprint(parts[1]["text"]), "Heartbeat") || !strings.Contains(fmt.Sprint(parts[1]["text"]), "Structured Trigger Context") {
		t.Fatalf("volatile part missing heartbeat/trigger content: %#v", parts[1])
	}
}

func TestBuildWithOptions_PopulatesBudgetReport(t *testing.T) {
	d := openTestDB(t)
	b := &Builder{DB: d, HistoryMax: 10}
	pp, _, err := b.Build(context.Background(), "sess", "hello")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if pp.Budget.EstimatedInputTokens <= 0 {
		t.Fatalf("expected non-zero estimated token count, got %+v", pp.Budget)
	}
}
