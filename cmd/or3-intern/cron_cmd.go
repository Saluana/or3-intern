package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"or3-intern/internal/cron"
)

func runCronCommand(ctx context.Context, storePath string, args []string, stdout, stderr io.Writer) error {
	return runCronCommandWithRunner(ctx, storePath, args, stdout, stderr, nil)
}

func runCronCommandWithRunner(ctx context.Context, storePath string, args []string, stdout, stderr io.Writer, runner cron.Runner) error {
	if len(args) == 0 {
		return newUsageError("usage: or3-intern cron <add|list|show|remove|run|pause|resume>")
	}
	cmd := args[0]
	rest := args[1:]

	if runner == nil {
		runner = func(context.Context, cron.CronJob) (cron.RunResult, error) {
			return cron.RunResult{}, fmt.Errorf("cron execution requires the runtime; rerun with a configured runner")
		}
	}
	svc := cron.New(storePath, runner)
	switch cmd {
	case "add":
		return cronAdd(svc, rest, stdout, stderr)
	case "list":
		return cronList(svc, rest, stdout, stderr)
	case "show":
		return cronShow(svc, rest, stdout, stderr)
	case "remove":
		return cronRemove(svc, rest, stdout, stderr)
	case "run":
		return cronRun(ctx, svc, rest, stdout, stderr)
	case "pause":
		return cronPause(svc, rest, stdout, stderr)
	case "resume":
		return cronResume(svc, rest, stdout, stderr)
	default:
		return newUsageError("unknown cron subcommand: %s\nusage: or3-intern cron <add|list|show|remove|run|pause|resume>", cmd)
	}
}

func cronAdd(svc *cron.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cron add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var jsonStr string
	var stdin bool
	fs.StringVar(&jsonStr, "json", "", "Full job JSON (see cron SKILL.md for format)")
	fs.BoolVar(&stdin, "stdin", false, "Read job JSON from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if stdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		jsonStr = strings.TrimSpace(string(data))
	}
	if jsonStr == "" {
		return newUsageError("usage: or3-intern cron add --json '{\"name\":\"...\",\"schedule\":{...},\"payload\":{...}}'\nor pipe JSON via stdin: echo '{...}' | or3-intern cron add --stdin")
	}
	job, err := decodeCronJobJSON(jsonStr)
	if err != nil {
		return fmt.Errorf("invalid job json: %w", err)
	}
	// Default to enabled on create (matches service API behavior)
	if !job.Enabled {
		// Check if user explicitly set enabled=false
		var raw map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
			return fmt.Errorf("invalid job json: %w", err)
		}
		// Try unwrapping {"job":{...}}
		if wrapped, ok := raw["job"]; ok {
			if w, ok := wrapped.(map[string]any); ok {
				raw = w
			}
		}
		if _, explicit := raw["enabled"]; !explicit {
			job.Enabled = true
		}
	}
	hadID := job.ID != ""
	if err := svc.Add(job); err != nil {
		return fmt.Errorf("add cron job: %w", err)
	}
	// If ID was auto-generated, find it from the list
	if !hadID {
		jobs, err := svc.List()
		if err != nil {
			return err
		}
		if len(jobs) > 0 {
			job = jobs[len(jobs)-1]
		}
	}
	renderCronJob(stdout, job)
	return nil
}

func decodeCronJobJSON(raw string) (cron.CronJob, error) {
	raw = strings.TrimSpace(raw)
	// Accept {"job": {...}} or just the job object directly
	var wrapper struct {
		Job *cron.CronJob `json:"job"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err == nil && wrapper.Job != nil {
		return *wrapper.Job, nil
	}
	var job cron.CronJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return cron.CronJob{}, fmt.Errorf("parse error (wrap in {\"job\":{...}} or pass the job object directly): %w", err)
	}
	return job, nil
}

func cronList(svc *cron.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cron list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	jobs, err := svc.List()
	if err != nil {
		return fmt.Errorf("list cron jobs: %w", err)
	}
	if len(jobs) == 0 {
		fmt.Fprintln(stdout, "no cron jobs")
		return nil
	}
	for _, job := range jobs {
		renderCronJobShort(stdout, job)
	}
	return nil
}

func cronShow(svc *cron.Service, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return newUsageError("usage: or3-intern cron show <id>")
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return newUsageError("usage: or3-intern cron show <id>")
	}
	jobs, err := svc.List()
	if err != nil {
		return fmt.Errorf("show cron job: %w", err)
	}
	for _, job := range jobs {
		if job.ID == id {
			renderCronJob(stdout, job)
			return nil
		}
	}
	return fmt.Errorf("cron job %q not found", id)
}

func cronRemove(svc *cron.Service, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return newUsageError("usage: or3-intern cron remove <id>")
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return newUsageError("usage: or3-intern cron remove <id>")
	}
	ok, err := svc.Remove(id)
	if err != nil {
		return fmt.Errorf("remove cron job: %w", err)
	}
	if !ok {
		return fmt.Errorf("cron job %q not found", id)
	}
	fmt.Fprintf(stdout, "removed %s\n", id)
	return nil
}

func cronRun(ctx context.Context, svc *cron.Service, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return newUsageError("usage: or3-intern cron run <id> [--force]")
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return newUsageError("usage: or3-intern cron run <id> [--force]")
	}
	rest := args[1:]
	fs := flag.NewFlagSet("cron run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var force bool
	fs.BoolVar(&force, "force", false, "Run even if the job is disabled")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	job, err := svc.RunNow(ctx, id, force)
	if err != nil {
		if errors.Is(err, cron.ErrNotFound) {
			return fmt.Errorf("cron job %q not found", id)
		}
		return fmt.Errorf("run cron job: %w", err)
	}
	if !force && !job.Enabled {
		fmt.Fprintf(stdout, "job %q is disabled (use --force to run anyway)\n", id)
		return nil
	}
	fmt.Fprintf(stdout, "ran %s\n", id)
	return nil
}

func cronPause(svc *cron.Service, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return newUsageError("usage: or3-intern cron pause <id>")
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return newUsageError("usage: or3-intern cron pause <id>")
	}
	job, err := svc.SetEnabled(id, false)
	if err != nil {
		if errors.Is(err, cron.ErrNotFound) {
			return fmt.Errorf("cron job %q not found", id)
		}
		return fmt.Errorf("pause cron job: %w", err)
	}
	fmt.Fprintf(stdout, "paused %s (%s)\n", id, job.Name)
	return nil
}

func cronResume(svc *cron.Service, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return newUsageError("usage: or3-intern cron resume <id>")
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		return newUsageError("usage: or3-intern cron resume <id>")
	}
	job, err := svc.SetEnabled(id, true)
	if err != nil {
		if errors.Is(err, cron.ErrNotFound) {
			return fmt.Errorf("cron job %q not found", id)
		}
		return fmt.Errorf("resume cron job: %w", err)
	}
	fmt.Fprintf(stdout, "resumed %s (%s)\n", id, job.Name)
	return nil
}

func renderCronJobShort(w io.Writer, job cron.CronJob) {
	status := "enabled"
	if !job.Enabled {
		status = "paused"
	}
	next := "-"
	if job.State.NextRunAtMS != nil && *job.State.NextRunAtMS > 0 {
		t := time.UnixMilli(*job.State.NextRunAtMS)
		next = t.Format(time.RFC3339)
	}
	fmt.Fprintf(w, "%-10s  %-20s  %-8s  next=%s\n", job.ID, job.Name, status, next)
}

func renderCronJob(w io.Writer, job cron.CronJob) {
	next := "-"
	if job.State.NextRunAtMS != nil && *job.State.NextRunAtMS > 0 {
		t := time.UnixMilli(*job.State.NextRunAtMS)
		next = t.Format(time.RFC3339)
	}
	lastRun := "-"
	if job.State.LastRunAtMS != nil && *job.State.LastRunAtMS > 0 {
		t := time.UnixMilli(*job.State.LastRunAtMS)
		lastRun = t.Format(time.RFC3339)
	}
	lastStatus := job.State.LastStatus
	if lastStatus == "" {
		lastStatus = "-"
	}
	sched := renderSchedule(job.Schedule)

	fmt.Fprintf(w, "id:       %s\n", job.ID)
	fmt.Fprintf(w, "name:     %s\n", job.Name)
	fmt.Fprintf(w, "enabled:  %t\n", job.Enabled)
	fmt.Fprintf(w, "schedule: %s\n", sched)
	if job.Payload.AgentRun != nil {
		fmt.Fprintf(w, "runner:   %s\n", job.Payload.AgentRun.RunnerID)
		fmt.Fprintf(w, "task:     %s\n", job.Payload.AgentRun.Task)
	}
	fmt.Fprintf(w, "next:     %s\n", next)
	fmt.Fprintf(w, "last:     %s (%s)\n", lastRun, lastStatus)
	if job.DeleteAfterRun {
		fmt.Fprintf(w, "delete-after-run: true\n")
	}
}

func renderSchedule(s cron.CronSchedule) string {
	switch s.Kind {
	case cron.KindAt:
		t := time.UnixMilli(s.AtMS)
		return fmt.Sprintf("at %s", t.Format(time.RFC3339))
	case cron.KindEvery:
		d := time.Duration(s.EveryMS) * time.Millisecond
		return fmt.Sprintf("every %s", d.Round(time.Second).String())
	case cron.KindCron:
		if s.TZ != "" {
			return fmt.Sprintf("cron %q tz=%s", s.Expr, s.TZ)
		}
		return fmt.Sprintf("cron %q", s.Expr)
	default:
		return string(s.Kind)
	}
}
