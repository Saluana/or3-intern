package main

import (
	"context"
	"fmt"
	"io"
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

type PrepareInput struct {
	CloudOrigin string
	StateDir    string
	Confirm     func(action string) (bool, error)
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
	Prepare(ctx context.Context, input PrepareInput) (*RuntimeConnectionTarget, error)
	Verify(ctx context.Context, target *RuntimeConnectionTarget) (*Verification, error)
}

type externalRuntimeAdapter struct {
	id      ConnectRuntimeID
	prepare func(context.Context, PrepareInput) (externalRuntimePlan, error)
	verify  func(context.Context, *RuntimeConnectionTarget) (*Verification, error)
}

func (a externalRuntimeAdapter) ID() ConnectRuntimeID { return a.id }

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
	if a.verify == nil {
		return nil, fmt.Errorf("runtime adapter %q has no verification function", a.id)
	}
	return a.verify(ctx, target)
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
