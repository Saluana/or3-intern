package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ConnectRuntimeID is the runtime identity persisted with a Connect host.
// Keep this local to the connect command: it is an onboarding contract, not a
// public plugin framework.
type ConnectRuntimeID string

const (
	connectRuntimeOpenClaw ConnectRuntimeID = "openclaw"
	connectRuntimeHermes   ConnectRuntimeID = "hermes"
)

type ConnectDriver string

const connectDriverRuns ConnectDriver = "runs"

// AdapterState is the runtime-owned portion of resumable Connect state. The
// durable Connect State remains the source of truth; this small value keeps
// the adapter contract independent from the tunnel/store implementation.
type AdapterState struct {
	Runtime     ConnectRuntimeID
	Stage       string
	LocalOrigin string
	BasePath    string
	Version     string
	UpdatedAt   time.Time
}

func runtimePreparationPath(stateDir string) string {
	return filepath.Join(stateDir, "runtime-preparation.json")
}

func loadRuntimePreparation(stateDir string) (*AdapterState, error) {
	body, err := os.ReadFile(runtimePreparationPath(stateDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state AdapterState
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, fmt.Errorf("read runtime preparation checkpoint: %w", err)
	}
	if state.Runtime == "" {
		return nil, errors.New("runtime preparation checkpoint is missing its runtime")
	}
	return &state, nil
}

func saveRuntimePreparation(stateDir string, state AdapterState) error {
	if state.Runtime == "" {
		return errors.New("runtime preparation checkpoint is missing its runtime")
	}
	state.UpdatedAt = time.Now().UTC()
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stateDir, ".runtime-preparation-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, runtimePreparationPath(stateDir))
}

func removeRuntimePreparation(stateDir string) error {
	err := os.Remove(runtimePreparationPath(stateDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type PrepareInput struct {
	CloudOrigin string
	StateDir    string
	Confirm     func(action string) (bool, error)
	Resume      *AdapterState
	Stdout      io.Writer
	Stderr      io.Writer
}

type RuntimeConnectionTarget struct {
	Driver      ConnectDriver
	Runtime     ConnectRuntimeID
	LocalOrigin string
	BasePath    string
	AccessToken string
	Version     string
	DisplayName string

	plan *externalRuntimePlan
}

type Verification struct {
	Capabilities map[string]any
	Streaming    string
	Commands     string
	Cancellation string
}

// RuntimeAdapter is deliberately small. Runtime-specific onboarding stays in
// the adapter; Connect only consumes a verified loopback target and owns the
// shared authorization, tunnel, credential, and service lifecycle.
type RuntimeAdapter interface {
	ID() ConnectRuntimeID
	Detect(ctx context.Context) (installed bool, version string, err error)
	Prepare(ctx context.Context, input PrepareInput) (*RuntimeConnectionTarget, error)
	Verify(ctx context.Context, target *RuntimeConnectionTarget) (*Verification, error)
}

type externalRuntimeAdapter struct {
	id      ConnectRuntimeID
	prepare func(context.Context, PrepareInput) (externalRuntimePlan, error)
	verify  func(context.Context, *RuntimeConnectionTarget) (*Verification, error)
}

func (a externalRuntimeAdapter) ID() ConnectRuntimeID { return a.id }

func (a externalRuntimeAdapter) Detect(ctx context.Context) (bool, string, error) {
	bin, err := findRuntimeBinary(string(a.id))
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, "", nil
		}
		return false, "", err
	}
	output, err := runRuntimeContext(ctx, bin, "--version")
	if err != nil {
		return true, "", fmt.Errorf("run %s --version: %w", a.id, err)
	}
	return true, firstLine(output), nil
}

func (a externalRuntimeAdapter) Prepare(ctx context.Context, input PrepareInput) (*RuntimeConnectionTarget, error) {
	if a.prepare == nil {
		return nil, fmt.Errorf("runtime adapter %q has no prepare function", a.id)
	}
	plan, err := a.prepare(ctx, input)
	if err != nil {
		return nil, err
	}
	return &RuntimeConnectionTarget{
		Driver:      ConnectDriver(plan.host.Driver),
		Runtime:     ConnectRuntimeID(plan.host.Runtime),
		LocalOrigin: plan.localOrigin,
		BasePath:    plan.basePath,
		Version:     plan.host.RuntimeVersion,
		DisplayName: plan.host.Name,
		plan:        &plan,
	}, nil
}

func (a externalRuntimeAdapter) Verify(ctx context.Context, target *RuntimeConnectionTarget) (*Verification, error) {
	if target == nil || target.plan == nil {
		return nil, fmt.Errorf("runtime adapter %q received an unprepared target", a.id)
	}
	if a.verify != nil {
		return a.verify(ctx, target)
	}
	if target.plan.verify == nil {
		return nil, fmt.Errorf("runtime adapter %q has no verification function", a.id)
	}
	if err := target.plan.verify(ctx, target.AccessToken); err != nil {
		return nil, err
	}
	return &Verification{
		Capabilities: map[string]any{"sessions": true, "events": true},
		Streaming:    "verified",
		Commands:     "not-tested",
		Cancellation: "not-tested",
	}, nil
}

func externalRuntimeAdapters() map[ConnectRuntimeID]RuntimeAdapter {
	return map[ConnectRuntimeID]RuntimeAdapter{
		connectRuntimeOpenClaw: externalRuntimeAdapter{
			id: connectRuntimeOpenClaw,
			prepare: func(ctx context.Context, input PrepareInput) (externalRuntimePlan, error) {
				return prepareOpenClawWithInput(ctx, input)
			},
			verify: verifyOpenClawTarget,
		},
		connectRuntimeHermes: externalRuntimeAdapter{
			id: connectRuntimeHermes,
			prepare: func(ctx context.Context, input PrepareInput) (externalRuntimePlan, error) {
				return prepareHermesWithInput(ctx, input)
			},
			verify: verifyHermesTarget,
		},
	}
}

func runtimeAdapterFor(name string) (RuntimeAdapter, error) {
	adapter, ok := externalRuntimeAdapters()[ConnectRuntimeID(name)]
	if !ok {
		return nil, newUsageError("unsupported runtime %q; supported runtimes are openclaw and hermes", name)
	}
	return adapter, nil
}
