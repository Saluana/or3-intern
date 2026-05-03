package db

import (
	"context"
	"testing"
)

func TestSkillRunPlanStore_CreateRoundTripAndUpdate(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	plan, err := d.CreateSkillRunPlan(ctx, SkillRunPlanRecord{
		SkillID:            "runner",
		Version:            "1.0.0",
		Origin:             "workspace",
		TrustState:         "approved",
		SkillDir:           "/tmp/runner",
		RelativePath:       "tool.sh",
		ArgsJSON:           `["tail"]`,
		TimeoutSeconds:     45,
		CommandJSON:        `["bash","/tmp/runner/tool.sh","tail"]`,
		ScriptHash:         "script-hash",
		EnvBindingHash:     "env-hash",
		PlanHash:           "plan-hash",
		RequesterAgentID:   "agent-1",
		RequesterSessionID: "sess-1",
		ExecutionHostID:    "local",
		Status:             "pending_approval",
		CreatedAt:          100,
	})
	if err != nil {
		t.Fatalf("CreateSkillRunPlan: %v", err)
	}
	if plan.ID == "" {
		t.Fatal("expected generated plan ID")
	}
	if plan.UpdatedAt != 100 {
		t.Fatalf("expected UpdatedAt to default to CreatedAt, got %d", plan.UpdatedAt)
	}

	approvalReq, err := d.CreateApprovalRequest(ctx, ApprovalRequestRecord{Type: "skill_execution", SubjectHash: "subject-hash", SubjectJSON: `{"type":"skill_execution"}`, ExecutionHostID: "local", Status: "pending", PolicyMode: "ask", RequestedAt: 105, ExpiresAt: 205})
	if err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}

	if err := d.UpdateSkillRunPlanApproval(ctx, plan.ID, approvalReq.ID, "subject-hash", "awaiting_resume", 110); err != nil {
		t.Fatalf("UpdateSkillRunPlanApproval: %v", err)
	}
	if err := d.UpdateSkillRunPlanResult(ctx, plan.ID, "succeeded", `{"ok":true}`, "", 120); err != nil {
		t.Fatalf("UpdateSkillRunPlanResult: %v", err)
	}

	stored, err := d.GetSkillRunPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetSkillRunPlan: %v", err)
	}
	if stored.ApprovalRequestID != approvalReq.ID || stored.SubjectHash != "subject-hash" {
		t.Fatalf("unexpected approval linkage: %#v", stored)
	}
	if stored.Status != "succeeded" || stored.ResultJSON != `{"ok":true}` {
		t.Fatalf("unexpected stored result: %#v", stored)
	}
	if stored.UpdatedAt != 120 {
		t.Fatalf("expected UpdatedAt to track latest write, got %d", stored.UpdatedAt)
	}

	plans, err := d.ListSkillRunPlansByApprovalRequest(ctx, approvalReq.ID, 10)
	if err != nil {
		t.Fatalf("ListSkillRunPlansByApprovalRequest: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != plan.ID {
		t.Fatalf("unexpected plans for approval request: %#v", plans)
	}
}

func TestSkillRunPlanStore_UpdateApprovalPreservesExistingRequestID(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	approvalReq, err := d.CreateApprovalRequest(ctx, ApprovalRequestRecord{Type: "skill_execution", SubjectHash: "subject-hash", SubjectJSON: `{"type":"skill_execution"}`, ExecutionHostID: "local", Status: "pending", PolicyMode: "ask", RequestedAt: 105, ExpiresAt: 205})
	if err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}
	plan, err := d.CreateSkillRunPlan(ctx, SkillRunPlanRecord{
		SkillID:           "runner",
		SkillDir:          "/tmp/runner",
		CommandJSON:       `["bash","/tmp/runner/tool.sh"]`,
		ScriptHash:        "script-hash",
		EnvBindingHash:    "env-hash",
		PlanHash:          "plan-hash",
		ApprovalRequestID: approvalReq.ID,
		Status:            "pending_approval",
	})
	if err != nil {
		t.Fatalf("CreateSkillRunPlan: %v", err)
	}
	if err := d.UpdateSkillRunPlanApproval(ctx, plan.ID, 0, "subject-hash", "approved", 110); err != nil {
		t.Fatalf("UpdateSkillRunPlanApproval: %v", err)
	}
	stored, err := d.GetSkillRunPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetSkillRunPlan: %v", err)
	}
	if stored.ApprovalRequestID != approvalReq.ID {
		t.Fatalf("expected approval request id %d to be preserved, got %#v", approvalReq.ID, stored)
	}
}

func TestSkillRunPlanStore_GetOrCreateActiveAndClaim(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	input := SkillRunPlanRecord{
		SkillID:            "runner",
		SkillDir:           "/tmp/runner",
		CommandJSON:        `["bash","/tmp/runner/tool.sh"]`,
		ScriptHash:         "script-hash",
		EnvBindingHash:     "env-hash",
		PlanHash:           "plan-hash",
		RequesterSessionID: "sess-1",
		Status:             "prepared",
	}
	first, reused, err := d.GetOrCreateActiveSkillRunPlan(ctx, input)
	if err != nil {
		t.Fatalf("GetOrCreateActiveSkillRunPlan first: %v", err)
	}
	if reused {
		t.Fatal("expected first plan creation to insert a new row")
	}
	second, reused, err := d.GetOrCreateActiveSkillRunPlan(ctx, input)
	if err != nil {
		t.Fatalf("GetOrCreateActiveSkillRunPlan second: %v", err)
	}
	if !reused || second.ID != first.ID {
		t.Fatalf("expected active plan reuse, got first=%#v second=%#v reused=%v", first, second, reused)
	}
	claimed, err := d.ClaimSkillRunPlan(ctx, first.ID, 200)
	if err != nil {
		t.Fatalf("ClaimSkillRunPlan first: %v", err)
	}
	if !claimed {
		t.Fatal("expected first claim to succeed")
	}
	claimed, err = d.ClaimSkillRunPlan(ctx, first.ID, 201)
	if err != nil {
		t.Fatalf("ClaimSkillRunPlan second: %v", err)
	}
	if claimed {
		t.Fatal("expected second claim to fail once plan is running")
	}
}

func TestSkillRunPlanStore_RequiresSkillAndDir(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.CreateSkillRunPlan(ctx, SkillRunPlanRecord{}); err == nil {
		t.Fatal("expected validation error for empty plan")
	}
}
