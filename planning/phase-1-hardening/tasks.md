# 1. Config and policy surface

- [ ] (R1, R2, R5, R6) Extend `internal/config/config.go` with concise hardening config for capability tiers, exec allowlists, child env allowlists, channel trust defaults, and quota limits.
- [ ] (R1, R2, R5, R6) Add focused config tests in `internal/config/config_test.go` for safe defaults and backward-compatible overrides.

# 2. Tool capability enforcement

- [ ] (R1, R2) Add capability metadata and central policy checks in `internal/tools/registry.go` and the relevant tool implementations.
- [ ] (R2) Tighten workspace-only behavior in `internal/tools/files.go` and any subprocess `cwd` validation paths.

# 3. Exec and child-process hardening

- [ ] (R4, R5) Refactor `internal/tools/exec.go` to make argv execution the default, keep shell execution privileged-only, and enforce a program allowlist.
- [ ] (R5) Apply scrubbed child environment handling to `internal/tools/exec.go`, `internal/tools/skill_exec.go`, and `internal/mcp/manager.go`.

# 4. Channel trust defaults

- [ ] (R3) Update channel packages under `internal/channels/` to deny unknown peers by default unless allowlisted or paired.
- [ ] (R3) If dynamic pairing is included in Phase 1, add a small SQLite-backed pairing store in `internal/db` plus regression tests.

# 5. Runtime quotas

- [ ] (R6) Add per-session quota accounting in `internal/agent/runtime.go` and connect it to risky tool actions such as exec, web access, and subagent spawn.
- [ ] (R6) Ensure quota denials are bounded, deterministic, and covered by tests.

# 6. Verification

- [ ] (R1-R6) Extend the relevant tests in `internal/tools`, `internal/channels`, `internal/mcp`, `internal/agent`, and `internal/db` to cover the new defaults and regression cases.
- [ ] (R1-R6) Update `README.md` or operator-facing config docs with the new safe defaults and explicit opt-ins.

# 7. Out of scope

- [ ] No general sandbox backend such as Bubblewrap in Phase 1.
- [ ] No encrypted secret store or signed audit trail in Phase 1.
- [ ] No frontend, service split, or policy engine outside the current Go process.
