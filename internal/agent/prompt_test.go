package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/memory"
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
	noDocContext := ""
	got := b.composeSystemPrompt(pinned, retrieved, noIdentity, noStaticMem, noHeartbeat, noDocContext, "")
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
