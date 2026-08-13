package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"or3-intern/internal/cron"
)

func TestRunCronCommandExecutesConfiguredRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.json")
	runs := 0
	runner := func(context.Context, cron.CronJob) (cron.RunResult, error) {
		runs++
		return cron.RunResult{}, nil
	}
	seed := cron.New(path, runner)
	if err := seed.Add(cron.CronJob{
		ID:             "once",
		Enabled:        true,
		DeleteAfterRun: true,
		Schedule:       cron.CronSchedule{Kind: cron.KindEvery, EveryMS: 60_000},
		Payload: cron.CronPayload{
			Kind: cron.PayloadRunnerRun,
			AgentRun: &cron.CronAgentRunPayload{
				RunnerID: "opencode",
				Task:     "run once",
			},
		},
	}); err != nil {
		t.Fatalf("seed cron job: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := runCronCommandWithRunner(context.Background(), path, []string{"run", "once"}, &stdout, &stderr, runner); err != nil {
		t.Fatalf("cron run: %v (%s)", err, stderr.String())
	}
	if runs != 1 {
		t.Fatalf("expected one real runner invocation, got %d", runs)
	}
	jobs, err := seed.List()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("delete-after-run job remained after successful execution: %#v", jobs)
	}
}

func TestRunCronCommandWithoutRuntimeDoesNotMutateJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.json")
	runner := func(context.Context, cron.CronJob) (cron.RunResult, error) { return cron.RunResult{}, nil }
	seed := cron.New(path, runner)
	if err := seed.Add(cron.CronJob{
		ID:             "once",
		Enabled:        true,
		DeleteAfterRun: true,
		Schedule:       cron.CronSchedule{Kind: cron.KindEvery, EveryMS: 60_000},
		Payload: cron.CronPayload{
			Kind:     cron.PayloadRunnerRun,
			AgentRun: &cron.CronAgentRunPayload{RunnerID: "opencode", Task: "do not fake"},
		},
	}); err != nil {
		t.Fatalf("seed cron job: %v", err)
	}

	if err := runCronCommand(context.Background(), path, []string{"run", "once"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected cron run without runtime to fail")
	}
	jobs, err := seed.List()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].State.LastStatus == "ok" {
		t.Fatalf("failed execution mutated job as successful: %#v", jobs)
	}
}
