# 1. Config updates

- [ ] (R1, R6) Extend `HeartbeatConfig` in `internal/config/config.go` with a dedicated `SessionKey` and normalize defaults during config load.
- [ ] (R1, R6) Add config tests in `internal/config/config_test.go` for default heartbeat settings and backward-compatible loading.

# 2. Heartbeat service package

- [ ] (R1, R2, R4) Add `internal/heartbeat/service.go` implementing a ticker-driven, coalescing heartbeat publisher.
- [ ] (R1, R3, R4) Implement bounded file checks for missing, unreadable, empty, and comment-only `HEARTBEAT.md` content.
- [ ] (R2, R4) Publish a dedicated heartbeat event with stable session key and non-user channel metadata.
- [ ] (R4) Ensure the service never queues overlapping heartbeat turns.

# 3. Bus and runtime integration

- [ ] (R2, R5) Add `EventHeartbeat` to `internal/bus/bus.go` or an equivalent explicit autonomous event discriminator.
- [ ] (R2, R5, R6) Update `internal/agent/runtime.go` so heartbeat events are autonomous, persist normally, and skip unintended default reply delivery.
- [ ] (R5) Verify explicit `send_message` tool usage still works from heartbeat turns.

# 4. Prompt freshness

- [ ] (R3) Refactor `internal/agent/prompt.go` and/or builder inputs so heartbeat text is refreshed from file for autonomous turns without requiring process restart.
- [ ] (R3) Keep non-heartbeat bootstrap loading behavior bounded and avoid unnecessary per-turn file reads for normal user chats.

# 5. Startup wiring

- [ ] (R1, R2, R7) Update `cmd/or3-intern/main.go` to construct, start, and stop the heartbeat service during `serve` only.
- [ ] (R1) Ensure `chat` and one-shot `agent` commands do not start the heartbeat loop.
- [ ] (R4) Ensure shutdown cleanly cancels the service before channel manager teardown completes.

# 6. Tests

- [ ] (R1, R3, R4) Add `internal/heartbeat/service_test.go` covering start/stop, skip behavior, event publication, and coalescing.
- [ ] (R2, R3, R5) Extend `internal/agent/prompt_test.go` and `internal/agent/runtime_test.go` for autonomous classification, fresh heartbeat text loading, and no auto-delivery.
- [ ] (R1, R7) Add focused startup/regression tests where practical for `cmd/or3-intern` or adjacent integration points.

# 7. Documentation

- [ ] (R7) Update `README.md` with heartbeat config, session behavior, and how heartbeat differs from cron and other triggers.
- [ ] (R7) Document that heartbeat runs only in `serve`, is disabled by default, and relies on explicit tool-driven outbound delivery.

# 8. Out of scope

- [ ] No separate LLM decision phase or cost-optimized planner in the first pass.
- [ ] No external scheduler, distributed worker, or daemon outside the main process.
- [ ] No automatic notification target selection beyond what the agent does itself through normal tools.
- [ ] No general live-reload system for all bootstrap files beyond the heartbeat-specific freshness needed here.
