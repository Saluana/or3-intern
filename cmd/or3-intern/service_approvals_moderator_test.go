package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

func TestServiceApprovalsListIncludesModeratorMetadata(t *testing.T) {
	broker, cleanup := buildServiceTestBroker(t, func(cfg *config.ApprovalConfig) {
		cfg.Exec.Mode = config.ApprovalModeAsk
		cfg.Moderator.Enabled = true
	})
	defer cleanup()
	broker.Moderator = &approval.FakeModerator{Result: approval.ModeratorReviewResult{
		Risk: approval.RiskLow, Action: approval.ModeratorApprove, Reason: "bounded command",
	}}

	decision, err := broker.EvaluateExec(context.Background(), approval.ExecEvaluation{
		ExecutablePath: "/usr/bin/go", Argv: []string{"test"}, WorkingDir: "/tmp", ToolName: "exec",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("EvaluateExec: err=%v decision=%#v", err, decision)
	}

	server := &serviceServer{broker: broker, jobs: agent.NewJobRegistry(time.Minute, 32)}
	httpServer := newServiceTestHTTPServer(t, strings.Repeat("m", 32), server)
	defer httpServer.Close()

	listReq := mustServiceRequest(t, httpServer, strings.Repeat("m", 32), http.MethodGet, "/internal/v1/approvals?limit=10", "")
	listResp, err := httpServer.Client().Do(listReq)
	if err != nil {
		t.Fatalf("Do list approvals: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status: %d (%s)", listResp.StatusCode, mustReadBody(t, listResp.Body))
	}
	payload := mustDecodeJSONBody(t, listResp.Body)
	items, ok := payload["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected items, got %#v", payload)
	}
	found := false
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["id"].(float64)
		if int64(id) != decision.RequestID {
			continue
		}
		found = true
		mod, _ := item["moderator"].(map[string]any)
		if mod == nil || mod["risk"] != "low" || mod["action"] != "approve" {
			t.Fatalf("expected moderator metadata, got %#v", mod)
		}
	}
	if !found {
		t.Fatalf("approval %d not found in list", decision.RequestID)
	}
}
