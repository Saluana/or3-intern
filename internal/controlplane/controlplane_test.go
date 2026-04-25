package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
)

func testBroker(t *testing.T, mutate func(*config.ApprovalConfig), now time.Time) *approval.Broker {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "controlplane-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	approvalCfg := config.Default().Security.Approvals
	approvalCfg.Enabled = true
	approvalCfg.Exec.Mode = config.ApprovalModeAsk
	if mutate != nil {
		mutate(&approvalCfg)
	}
	return &approval.Broker{
		DB:      database,
		Config:  approvalCfg,
		HostID:  approvalCfg.HostID,
		SignKey: []byte("0123456789abcdef0123456789abcdef"),
		Now:     func() time.Time { return now.UTC() },
	}
}

func TestServiceCancelApproval(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)
	broker := testBroker(t, nil, now)
	cp := New(config.Config{}, nil, broker, nil, nil)
	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if err := cp.CancelApproval(context.Background(), decision.RequestID, "tester", "not now"); err != nil {
		t.Fatalf("CancelApproval: %v", err)
	}
	item, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if item.Status != approval.StatusCanceled {
		t.Fatalf("expected canceled status, got %#v", item)
	}
}

func TestServiceExpireApprovals(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)
	broker := testBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.PendingTTLSeconds = 1
	}, now)
	cp := New(config.Config{}, nil, broker, nil, nil)
	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	})
	if err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	broker.Now = func() time.Time { return now.Add(5 * time.Second).UTC() }
	expired, err := cp.ExpireApprovals(context.Background(), "tester")
	if err != nil {
		t.Fatalf("ExpireApprovals: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expected 1 expired request, got %d", expired)
	}
	item, err := broker.DB.GetApprovalRequest(context.Background(), decision.RequestID)
	if err != nil {
		t.Fatalf("GetApprovalRequest: %v", err)
	}
	if item.Status != approval.StatusExpired {
		t.Fatalf("expected expired status, got %#v", item)
	}
}

func TestServiceListApprovalRequestsFilteredByType(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)
	broker := testBroker(t, nil, now)
	cp := New(config.Config{}, nil, broker, nil, nil)
	if _, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/bin/echo",
		Argv:           []string{"hello"},
		WorkingDir:     "/tmp",
		ToolName:       "exec",
	}); err != nil {
		t.Fatalf("EvaluateExec: %v", err)
	}
	if _, _, err := broker.CreatePairingRequest(context.Background(), approval.PairingRequestInput{Role: approval.RoleOperator, DisplayName: "Ops"}); err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	items, err := cp.ListApprovalRequests(context.Background(), ApprovalFilter{Type: string(approval.SubjectExec), Limit: 100})
	if err != nil {
		t.Fatalf("ListApprovalRequests: %v", err)
	}
	if len(items) != 1 || items[0].Type != string(approval.SubjectExec) {
		t.Fatalf("expected one exec approval, got %#v", items)
	}
}

func TestServiceHealthAndReadiness(t *testing.T) {
	cfg := config.Default()
	cfg.Service.Secret = "secret"
	report := New(cfg, nil, nil, nil, nil).GetHealth()
	if report.Status != "degraded" || report.RuntimeAvailable {
		t.Fatalf("unexpected health report: %#v", report)
	}
	readiness := New(cfg, nil, nil, nil, nil).GetReadiness()
	if readiness.Status == "" || len(readiness.Findings) == 0 {
		t.Fatalf("expected readiness findings, got %#v", readiness)
	}
}

func TestServiceScopeOperations(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "scope-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cp := NewLocal(config.Config{}, database, nil, nil, nil)
	linked, err := cp.LinkSessionScope(context.Background(), ScopeLinkInput{SessionKey: "sess-a", ScopeKey: "scope-1"})
	if err != nil {
		t.Fatalf("LinkSessionScope: %v", err)
	}
	if linked.ScopeKey != "scope-1" {
		t.Fatalf("expected scope-1, got %#v", linked)
	}
	resolved, err := cp.ResolveScopeKey(context.Background(), "sess-a")
	if err != nil {
		t.Fatalf("ResolveScopeKey: %v", err)
	}
	if resolved != "scope-1" {
		t.Fatalf("expected resolved scope-1, got %q", resolved)
	}
	if _, err := cp.LinkSessionScope(context.Background(), ScopeLinkInput{SessionKey: "sess-b", ScopeKey: "scope-1"}); err != nil {
		t.Fatalf("LinkSessionScope second: %v", err)
	}
	sessions, err := cp.ListScopeSessions(context.Background(), "scope-1")
	if err != nil {
		t.Fatalf("ListScopeSessions: %v", err)
	}
	if len(sessions) != 2 || sessions[0] != "sess-a" || sessions[1] != "sess-b" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
}

func TestServiceEmbeddingStatusAndRebuild(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "embeddings-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if _, err := database.InsertMemoryNote(ctx, "sess-1", "hello memory", nil, sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{0.1, 0.2}}},
		})
	}))
	defer providerServer.Close()
	cfg := config.Default()
	cfg.Provider.APIBase = providerServer.URL
	cfg.Provider.EmbedModel = "text-embedding-3-small"
	provider := providers.New(providerServer.URL, "", 5*time.Second)
	provider.HTTP = providerServer.Client()
	cp := NewLocal(cfg, database, provider, nil, nil)
	status, err := cp.GetEmbeddingStatus(ctx)
	if err != nil {
		t.Fatalf("GetEmbeddingStatus: %v", err)
	}
	if status.Status != "ok" {
		t.Fatalf("expected ok before rebuild, got %#v", status)
	}
	result, err := cp.RebuildEmbeddings(ctx, "memory")
	if err != nil {
		t.Fatalf("RebuildEmbeddings: %v", err)
	}
	if result.MemoryNotesRebuilt != 1 {
		t.Fatalf("expected one rebuilt memory note, got %#v", result)
	}
	status, err = cp.GetEmbeddingStatus(ctx)
	if err != nil {
		t.Fatalf("GetEmbeddingStatus after rebuild: %v", err)
	}
	if status.MemoryVectorDims != 2 || strings.TrimSpace(status.CurrentEmbedFingerprint) == "" {
		t.Fatalf("unexpected embedding status after rebuild: %#v", status)
	}
}

func TestServiceAuditStatusAndVerify(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "audit-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	audit := &security.AuditLogger{DB: database, Key: []byte(strings.Repeat("k", 32)), Strict: true}
	rt := &agent.Runtime{DB: database, Audit: audit}
	cfg := config.Default()
	cfg.Security.Audit.Enabled = true
	cp := New(cfg, rt, nil, nil, nil)
	if err := audit.Record(context.Background(), "tool.execute", "sess-1", "cli", map[string]any{"tool": "exec"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	status, err := cp.GetAuditStatus(context.Background())
	if err != nil {
		t.Fatalf("GetAuditStatus: %v", err)
	}
	if !status.Available || status.EventCount != 1 || status.LastEventType != "tool.execute" {
		t.Fatalf("unexpected audit status: %#v", status)
	}
	verified, err := cp.VerifyAudit(context.Background())
	if err != nil {
		t.Fatalf("VerifyAudit: %v", err)
	}
	if !verified.Verified || verified.EventCount != 1 {
		t.Fatalf("unexpected verify result: %#v", verified)
	}
}
