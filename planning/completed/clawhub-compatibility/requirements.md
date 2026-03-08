# Overview

Add first-class ClawHub/OpenClaw skill compatibility to `or3-intern` so the project can reuse the existing skill ecosystem instead of recreating it skill-by-skill. The implementation must stay lightweight, secure by default, and viable on a low-cost Raspberry Pi.

Scope assumptions:
- The goal is practical compatibility with ClawHub skill bundles and the OpenClaw skill-loading model, not a full reimplementation of the OpenClaw gateway, plugin system, or UI.
- `or3-intern` should support installing, loading, evaluating, and using compatible skills from ClawHub without requiring Bun, Node, or Nix on the target machine.
- Skills that depend on unsupported OpenClaw-specific features must be surfaced as ineligible with a clear reason, not silently ignored.

# Requirements

1. **`or3-intern` must support ClawHub skill bundles as a native input format**
   The runtime must understand the same on-disk skill bundle shape ClawHub distributes: a skill directory containing `SKILL.md` plus supporting text/scripts/resources.
   Acceptance criteria:
   - A skill installed from ClawHub into an `or3-intern` skill directory is discovered without manual file rewriting.
   - Supporting files inside the bundle remain available to the skill via stable relative paths.
   - The loader tolerates bundles that contain only `SKILL.md`.

2. **Skill metadata parsing must be compatible with ClawHub/OpenClaw conventions**
   The loader must parse OpenClaw-style frontmatter and the ClawHub registry aliases that appear in published skills.
   Acceptance criteria:
   - `name`, `description`, `homepage`, `user-invocable`, `disable-model-invocation`, `command-dispatch`, `command-tool`, and `command-arg-mode` are recognized when present.
   - Metadata aliases under `metadata.openclaw`, `metadata.clawdbot`, and `metadata.clawdis` are all recognized.
   - Invalid frontmatter fails that skill cleanly without breaking unrelated skills.

3. **The skill inventory must implement OpenClaw-style precedence and eligibility checks**
   `or3-intern` must load skills from built-in, managed, and workspace locations with deterministic precedence and explicit gating.
   Acceptance criteria:
   - Precedence is `workspace` > `managed` > `bundled`.
   - Eligibility checks cover at least OS restrictions, required binaries, any-of binaries, required env vars, and required config flags.
   - Disabled or ineligible skills remain inspectable and report why they are unavailable.

4. **Prompt integration must match OpenClaw’s lightweight skill-discovery model**
   The model should see a compact list of eligible skills and read the full `SKILL.md` only when needed.
   Acceptance criteria:
   - The prompt contains a compact per-skill entry with name, description, and location.
   - Full skill bodies are not injected by default.
   - Skills marked `disable-model-invocation` are omitted from the model-facing list but remain available for explicit user invocation if supported.

5. **Skill-scoped environment injection must be supported and reversible**
   `or3-intern` must support per-skill configuration and temporary env injection at run time, similar to OpenClaw.
   Acceptance criteria:
   - Config supports per-skill `enabled`, `env`, `apiKey`, and `config` fields.
   - Skill env injection is scoped to the agent run and is restored afterward.
   - Secrets do not get copied into prompts, logs, or persistent message history.

6. **`or3-intern` must provide native ClawHub skill management without depending on the ClawHub CLI runtime**
   Users should be able to inspect, install, list, update, and remove ClawHub skills from Go code directly.
   Acceptance criteria:
   - A lightweight Go client can inspect a skill, download a version zip, and extract it into the configured skills directory.
   - A CLI surface exists for listing skills and reporting eligibility/missing requirements.
   - Update logic detects local modifications and avoids destructive overwrite by default.

7. **Portable skill execution must be supported for the ClawHub subset that maps cleanly to `or3-intern`**
   Skills that instruct the model to use existing tools or skill-local scripts must be runnable within the current safety model.
   Acceptance criteria:
   - Skill-local scripts/resources can be referenced via their bundle path.
   - A bounded execution path exists for script-backed skills without requiring shell interpolation of arbitrary model strings.
   - Skills that require unsupported OpenClaw-only tools, frontmatter-defined custom tools, nodes, plugins, or UI surfaces are marked ineligible with explicit reasons.

8. **User-invocable skill commands must be supported in a minimal compatible form**
   `or3-intern` must support the smallest useful subset of OpenClaw-style user skill invocation.
   Acceptance criteria:
   - If a skill is marked user-invocable, the runtime can resolve an explicit user command to that skill.
   - For `command-dispatch: tool`, the runtime dispatches directly to the named tool with the raw argument string.
   - Non-dispatch user invocations fall back to a model turn seeded with the selected skill rather than requiring a separate slash-command engine.

9. **The compatibility layer must stay Pi-friendly and fail closed**
   ClawHub compatibility must not drag in a heavyweight package/runtime stack or unsafe auto-install behavior.
   Acceptance criteria:
   - No Bun/Node/Nix dependency is required to install and use ClawHub skills in `or3-intern`.
   - Auto-running skill-declared installers is disabled by default.
   - The loader/watcher path has bounded CPU, RAM, and file I/O costs.

# Non-functional constraints

- **Deterministic behavior**
  - Keep skill discovery and precedence deterministic.
  - Keep local install/update behavior transactional and testable.
- **Low memory usage**
  - Parse frontmatter once and cache compact skill metadata.
  - Avoid loading full `SKILL.md` bodies into the prompt unless the model or user asks for one.
- **Bounded loops, output, and history**
  - Reuse current output caps, artifact spill, file-root restrictions, and exec timeouts for skill execution.
  - Keep skill watchers optional and debounce-bound.
- **SQLite and config compatibility**
  - Config additions must be additive and backward compatible.
  - Installing skills must not require schema changes unless a lock/status table clearly improves safety.
- **Secure handling of files, network access, and secrets**
  - Treat third-party skills as untrusted.
  - Do not auto-run registry-provided install commands by default.
  - Keep per-skill env injection scoped to a single agent run.
