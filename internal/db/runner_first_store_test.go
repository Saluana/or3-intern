package db

import (
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/runnerfirst"
)

func TestCreateSkillRunPlan_DisabledInRunnerFirst(t *testing.T) {
	runnerfirst.SetEnabled(true)
	t.Cleanup(func() { runnerfirst.SetEnabled(false) })

	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	_, err = d.CreateSkillRunPlan(context.Background(), SkillRunPlanRecord{
		SkillID:         "demo",
		SkillDir:        t.TempDir(),
		TimeoutSeconds:  30,
		CommandJSON:     `["echo"]`,
		ScriptHash:      "h",
		EnvBindingHash:  "e",
		PlanHash:        "p",
		ExecutionHostID: "local",
	})
	if err == nil {
		t.Fatal("expected error creating skill run plan in runner-first mode")
	}
}

func TestEnqueueSubagentJob_DisabledInRunnerFirst(t *testing.T) {
	runnerfirst.SetEnabled(true)
	t.Cleanup(func() { runnerfirst.SetEnabled(false) })

	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	err = d.EnqueueSubagentJob(context.Background(), SubagentJob{
		ID:               "job-1",
		ParentSessionKey: "parent",
		ChildSessionKey:  "child",
		Task:             "task",
	})
	if err == nil {
		t.Fatal("expected error enqueueing subagent job in runner-first mode")
	}
}
