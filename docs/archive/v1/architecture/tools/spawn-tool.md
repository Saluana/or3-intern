# Spawn Subagent Tool

Name: `spawn_subagent` | Capability: `guarded` | Group: `service`

Queues a background subagent task and returns immediately with a job ID. Used for long-running or parallel work.

## Parameters

- `task` (required) - complete task instructions for the subagent
- `channel` - optional delivery channel override
- `to` - optional recipient override

Source: `internal/tools/spawn.go:41-51`

## How it works

1. Validates the task is non-empty
2. Resolves the channel and recipient (from params, then context, then defaults)
3. Calls `Manager.Enqueue` with:
   - The parent session key (from context)
   - The active access profile name
   - The task instructions
   - Channel and recipient for delivery
4. Returns a job ID for tracking

Source: `internal/tools/spawn.go:57-85`

## Spawn manager

The `SpawnEnqueuer` interface creates a `SpawnJob` with:
- `ID` - the job identifier for tracking
- `ChildSessionKey` - the session the subagent runs in

Source: `internal/tools/spawn.go:17-20` (SpawnJob)

## Capability

Marked as `CapabilityGuarded` because it delegates work to a background process. Access control is handled through access profiles and the approval system.

Source: `internal/tools/spawn.go:33`

## When to use

The tool description advises using spawn_subagent only for work that can continue independently in the background, not for quick steps better handled in the current turn.

Source: `internal/tools/spawn.go:37-39`
