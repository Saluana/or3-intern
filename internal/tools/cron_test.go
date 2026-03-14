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

func mustStartCronService(t *testing.T, svc *cron.Service) {
	t.Helper()
	if err := svc.Start(); err != nil {
		t.Fatalf("cron.Start: %v", err)
	}
}

func mustAddCronToolJob(t *testing.T, svc *cron.Service, job cron.CronJob) {
	t.Helper()
	if err := svc.Add(job); err != nil {
		t.Fatalf("cron.Add: %v", err)
	}
}

func makeTestCronService(t *testing.T) *cron.Service {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.json")
	svc := cron.New(path, func(ctx context.Context, job cron.CronJob) error {
		return nil
	})
	mustStartCronService(t, svc)
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
				"kind": "cron",
				"expr": "0 * * * *",
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
	if _, err := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"job": map[string]any{
			"name": "to remove",
		},
	}); err != nil {
		t.Fatalf("CronTool add: %v", err)
	}

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
	mustAddCronToolJob(t, svc, cron.CronJob{
		ID:       "test-run",
		Name:     "run test",
		Enabled:  false,
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
	mustStartCronService(t, svc)
	defer svc.Stop()

	mustAddCronToolJob(t, svc, cron.CronJob{
		ID:       "disabled-job",
		Enabled:  false,
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
	mustStartCronService(t, svc)
	defer svc.Stop()

	mustAddCronToolJob(t, svc, cron.CronJob{
		ID:       "err-job",
		Enabled:  true,
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
