# 1. Skill loader compatibility

- [x] (R1, R2, R3) Refactor `internal/skills/skills.go` so it discovers skill directories by `SKILL.md`, not arbitrary `.md`/`.txt` files.
- [x] (R2) Add frontmatter parsing for `name`, `description`, `homepage`, `user-invocable`, `disable-model-invocation`, `command-dispatch`, `command-tool`, and `command-arg-mode`.
- [x] (R2) Normalize `metadata.openclaw`, `metadata.clawdbot`, and `metadata.clawdis` into one internal runtime metadata struct.
- [x] (R3) Add precedence handling for bundled, managed, workspace, and extra dirs with deterministic override rules.
- [x] (R3) Add eligibility checks for OS, required bins, any-of bins, required env, required config, and explicit disable flags.
- [x] (R3) Extend `internal/skills/skills_test.go` for parse errors, precedence, alias handling, and eligibility reasons.

# 2. Config and runtime wiring

- [x] (R3, R5, R9) Extend `internal/config/config.go` with `skills.managedDir`, `skills.load.*`, `skills.entries.*`, and `skills.clawHub.*` while preserving backward compatibility.
- [x] (R3) Update `cmd/or3-intern/main.go` to load skills from bundled, managed, workspace, and extra directories using the new precedence model.
- [x] (R5) Add per-run skill env injection and restore logic around the agent runtime so `skills.entries.<key>.env` and `apiKey` apply only during a live run.
- [x] (R5, R9) Add tests in `internal/config/config_test.go` and runtime-focused tests for env scoping and defaults.

# 3. Prompt and inspection behavior

- [x] (R4) Update `internal/agent/prompt.go` so the Skills section emits compact entries with `name`, `description`, and `location`, matching the OpenClaw pattern.
- [x] (R4) Keep full skill bodies out of the system prompt and continue using `read_skill` for on-demand access.
- [x] (R4) Exclude `disable-model-invocation` skills from the model-facing list while preserving inspection support.
- [x] (R4) Add prompt regression tests in `internal/agent` for compact skill list formatting and disabled skill filtering.

# 4. Native ClawHub client

- [x] (R6, R9) Add a lightweight `internal/clawhub` Go package that can inspect skill metadata and download version zips directly from the documented ClawHub endpoints.
- [x] (R6) Implement transactional extraction into managed/workspace skill roots with safe temp directories and atomic rename.
- [x] (R6) Mirror ClawHub’s local metadata enough to support `list`, `info`, `update`, and modification checks without depending on Bun/Node.
- [x] (R6, R9) Add HTTP client tests for inspect/download flows and local install/update safety behavior.

# 5. CLI skill management

- [x] (R6) Add `or3-intern skills` subcommands in `cmd/or3-intern/main.go` or a small adjacent command file.
- [x] (R6) Implement at least `list`, `list --eligible`, `info`, `check`, `search`, `install`, `update`, and `remove`.
- [x] (R3, R6) Make `skills check` report missing binaries, env, config, OS mismatch, and unsupported OpenClaw-only features.
- [x] (R6) Add command-level tests for output shape and overwrite/refuse behavior when local edits are present.

# 6. Portable skill execution

- [x] (R7) Extend `internal/tools` with a safe execution helper for skill-local scripts/resources that reuses existing exec/file restrictions.
- [x] (R7) Add bundle-path resolution helpers so skills can refer to supporting files deterministically.
- [x] (R7, R9) Detect unsupported OpenClaw-only dependencies such as plugin-only binaries, remote node assumptions, or missing tool surfaces and surface them as explicit incompatibility reasons.
- [x] (R7, R9) Detect frontmatter-defined custom tool declarations and either map them to supported `or3-intern` tools or mark the skill ineligible with a specific reason.
- [x] (R7) Add tests for safe path resolution, bounded execution, and unsupported-feature detection.

# 7. User-invocable skill commands

- [x] (R8) Add minimal explicit user skill invocation support for messages like `/<skill> ...` in the runtime/channel entry path.
- [x] (R8) Implement direct tool dispatch when a skill uses `command-dispatch: tool`, passing the raw args string to the selected tool.
- [x] (R8) Implement fallback model-seeded invocation for user-invocable skills that do not dispatch directly to a tool.
- [x] (R8) Add tests covering explicit skill invocation, missing skill handling, dispatch to unsupported tools, and raw-argument forwarding.

# 8. Docs and migration guidance

- [x] (R1-R9) Update `README.md` to document ClawHub/OpenClaw skill compatibility, managed/workspace skill locations, and the trust model for third-party skills.
- [x] (R3, R6) Document the difference between bundled, managed, and workspace skills and how precedence works in `or3-intern`.
- [x] (R7, R9) Add a compatibility note that not every ClawHub skill is portable to `or3-intern`; unsupported features are reported instead of silently failing.

# 9. Out of scope

- [x] No Bun/Node-based dependency on the official `clawhub` CLI.
- [x] No Nix plugin installation pipeline.
- [x] No remote macOS node support.
- [x] No automatic execution of skill-declared installers by default.
- [x] No promise of first-pass support for arbitrary frontmatter-defined custom tools without a stable public spec to target.
- [x] No promise of 100% runtime compatibility with OpenClaw-specific tools, plugins, or UI surfaces in the first pass.
