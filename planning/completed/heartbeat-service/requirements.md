# Overview

This plan adds a real periodic heartbeat service to `or3-intern` so `HEARTBEAT.md` can drive autonomous recurring work without depending on webhook, file-watch, or cron events.

Scope covers:

- a bounded in-process heartbeat ticker started by `or3-intern serve`
- autonomous event dispatch through the existing bus/runtime path
- prompt injection of the current `HEARTBEAT.md` contents on heartbeat turns
- suppression of unintended auto-replies from heartbeat turns unless the agent explicitly sends messages through tools

Assumptions:

- the first pass should favor simplicity over a separate decision/execution two-phase LLM flow
- the service should reuse existing runtime, memory, and tool behavior rather than creating a second agent path
- heartbeat remains disabled by default

# Requirements

## 1. Start a real periodic heartbeat service

The system shall support a timer-driven heartbeat worker that runs inside the existing `serve` process.

### Acceptance criteria

- when `heartbeat.enabled=true`, `or3-intern serve` starts a periodic heartbeat service
- the service ticks at the configured interval in minutes with a sane lower bound
- the service does not start during `chat` or one-shot `agent` runs
- shutdown cleanly stops the ticker and any in-flight wait state through the existing process lifecycle

## 2. Dispatch heartbeat turns through the existing bus/runtime flow

The system shall route heartbeat work as normal autonomous agent turns instead of adding a second execution path.

### Acceptance criteria

- each heartbeat tick publishes a bounded bus event rather than calling provider code directly from the service
- runtime treats heartbeat turns as autonomous in the same way it treats cron/webhook/file-watch turns
- heartbeat events use a stable session key that is explicit and configurable, with a safe default
- heartbeat turns can use the same tools, memory retrieval, and artifact spilling behavior as other autonomous turns

## 3. Load current heartbeat instructions without restart

The system shall use the latest `HEARTBEAT.md` content on each heartbeat turn.

### Acceptance criteria

- edits to the configured heartbeat tasks file are reflected on subsequent heartbeat ticks without restarting the process
- missing or unreadable heartbeat files degrade gracefully and skip the tick with bounded logging
- empty or comment-only heartbeat files do not trigger unnecessary autonomous turns
- the implementation does not require full config reloads on every tick

## 4. Prevent overlapping or runaway heartbeat work

The system shall keep heartbeat execution bounded and non-overlapping.

### Acceptance criteria

- a new tick does not enqueue another heartbeat turn while a previous heartbeat turn is still in flight
- the service bounds its internal queue to at most one pending tick or equivalent coalesced trigger
- bus saturation or runtime backpressure results in dropped/coalesced heartbeat ticks with logging instead of unbounded accumulation
- heartbeat execution remains subject to existing runtime/tool loop limits and command safety controls

## 5. Avoid unintended automatic delivery

The system shall not auto-deliver a normal assistant reply for heartbeat events unless explicitly intended.

### Acceptance criteria

- a heartbeat turn does not try to send a default reply to a non-existent `heartbeat` or `system` channel
- proactive external delivery only happens through explicit `send_message` tool usage or a clearly defined future extension point
- heartbeat activity can still persist history, memory updates, artifacts, and tool side effects as normal

## 6. Keep session and memory behavior deterministic

The system shall preserve explicit session behavior for heartbeat work.

### Acceptance criteria

- config supports a dedicated heartbeat session key, defaulting to a stable value such as `heartbeat:default`
- all heartbeat turns use that session key unless explicitly reconfigured
- long-term memory and history for heartbeat work remain isolated unless the user links scopes manually
- existing cron, webhook, and file-watch behavior remains unchanged

## 7. Document operator behavior

The system shall make heartbeat usage understandable from config and docs.

### Acceptance criteria

- README documents how heartbeat differs from cron, webhook, and file-watch triggers
- docs explain when to use `HEARTBEAT.md` versus cron jobs
- docs note that heartbeat runs only under `serve` and is disabled by default

# Non-functional constraints

- Keep the implementation inside the existing single-process runtime; no external scheduler or queue service
- Avoid a second provider/runtime stack; reuse the current bus, runtime, and prompt builder
- Bound wake-ups, queued ticks, and log volume
- Maintain safe-by-default tool behavior, workspace restrictions, and bounded outputs during heartbeat turns
- Preserve backward compatibility for existing config files; new fields must have defaults
- Favor a minimal v1 over a sophisticated two-phase heartbeat planner unless later needed for cost optimization
