# 1. Skill permissions and quarantine

- [ ] (R1, R5) Extend `internal/skills/skills.go` to parse and normalize skill permission metadata from frontmatter or `skill.json`.
- [ ] (R1) Enforce quarantine and approval checks in `internal/tools/skill_exec.go` and any adjacent runtime policy helpers.
- [ ] (R1, R5) Update `cmd/or3-intern/skills_cmd.go` so `skills list`, `skills info`, and `skills check` show permission and quarantine state.

# 2. Structured autonomous events

- [ ] (R2, R5) Add bounded structured metadata for heartbeat, webhook, and file-watch events in `internal/heartbeat/service.go`, `internal/triggers/webhook.go`, and `internal/triggers/filewatch.go`.
- [ ] (R2) Update `internal/agent/prompt.go` and/or `internal/agent/runtime.go` to surface structured trigger context without breaking the current text-based autonomous prompt path.
- [ ] (R2) Add regression tests for structured event shape, bounded metadata, and session-key stability.

# 3. Bubblewrap privileged path

- [ ] (R3) Add config and launcher support for optional Bubblewrap-backed privileged execution in `internal/config/config.go` and the relevant exec helpers under `internal/tools/`.
- [ ] (R3) Route privileged exec and privileged skill execution through the Bubblewrap path when enabled, and deny when unavailable.
- [ ] (R3) Add focused tests for enabled, disabled, unavailable, and fallback-denied scenarios.

# 4. Doctor command

- [ ] (R4) Add a `doctor` command path under `cmd/or3-intern` that loads config and runs deterministic hardening checks.
- [ ] (R4) Implement checks for channel exposure, filesystem restrictions, privileged exec settings, child env inheritance, webhook safety, and missing quotas using existing config/runtime wiring.
- [ ] (R4) Add command tests for readable output and optional strict-mode exit behavior.

# 5. Validation and docs

- [ ] (R1-R5) Add focused tests across `internal/skills`, `internal/tools`, `internal/triggers`, `internal/heartbeat`, `internal/agent`, and `cmd/or3-intern` for the new defaults and regression cases.
- [ ] (R1-R5) Update `README.md` with skill quarantine behavior, structured trigger inputs, Bubblewrap limitations, and `doctor` usage.

# 6. Out of scope

- [ ] No multi-backend sandbox matrix in Phase 2.
- [ ] No external policy server or separate autonomous event service.
- [ ] No full encrypted secret store or signed audit trail in Phase 2.
