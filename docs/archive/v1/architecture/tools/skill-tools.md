# Skill Tools

Three tools handle skills: reading, running with plans, and running raw scripts.

## read_skill

Name: `read_skill` | Capability: `safe` | Group: `skills, read`

Reads a skill's content from the skills inventory.

Parameters:
- `name` (required) - exact skill name
- `mode` - preview, full, or outline (default: preview)
- `maxBytes` - max bytes (default: 6000)

Before returning content, the tool checks if the skill is eligible and not hidden. If unavailable, it returns an error with the reason (missing dependencies, parse error, etc.).

Source: `internal/tools/skill.go:11-173`

## run_skill

Name: `run_skill` | Capability: `privileged` | Group: `skills, exec`

Runs a skill through a frozen `SkillRunPlan`. The plan captures all parameters so approval can be granted for the exact same execution.

Parameters:
- `plan_id` - resume a previous plan
- `skill` - skill name
- `path` - bundle-relative script path
- `entrypoint` - named entrypoint from skill.json
- `args` - argument array
- `stdin` - stdin text
- `timeoutSeconds` - timeout override

Source: `internal/tools/skill_run.go:26-85`

### Execution flow

1. If `plan_id` is given, load the frozen plan and resume
2. Otherwise, prepare a new plan:
   - Validate skill exists and is approved
   - Resolve the command (entrypoint or script path)
   - Hash all inputs for the plan (skill ID, version, origin, trust state, command, script, env binding)
3. Store the plan in the database (via `GetOrCreateActiveSkillRunPlan`)
4. Evaluate with the approval broker
5. If approval required, return `SkillRunStatusPendingApproval` with the plan ID
6. If approved, claim the plan (atomic) and run the command
7. Return the result with bounded stdout/stderr

Source: `internal/tools/skill_run.go:87-119` (executeNamed), `internal/tools/skill_run.go:285-427` (authorizeAndRunSkill, runPreparedSkillRun)

### Plan states

Plans transition through these states:
- `planned` - initial state
- `pending_approval` - waiting for operator approval
- `approved` - approved, ready to run
- `running` - currently executing (heartbeat keeps it alive)
- `succeeded` - completed successfully
- `failed` - execution error
- `timed_out` - exceeded timeout
- `preflight_failed` - plan validation failed
- `blocked_by_policy` - denied by policy
- `stale_plan` - plan inputs changed since creation
- `expired`, `cancelled` - terminal states

Source: `internal/tools/skill_run.go` (db.SkillRunStatus* constants, referenced throughout)

### Stdin encryption

Sensitive stdin text is encrypted with AES-256-GCM before storage. The encryption key is derived from the approval broker's signing key via HKDF with label "skill-run-encryption". Decryption happens when loading the plan.

Source: `internal/tools/skill_run.go:597-667` (persistableSkillRunStdin, loadSkillRunStdin, encryption)

### Command resolution

Commands resolve either from:
- A named entrypoint (declared in skill.json with `{baseDir}` template substitution)
- A raw bundle-relative script path (`.sh` uses bash, `.py` uses python3/python, already-executable files run directly)

Source: `internal/tools/skill_exec.go:52-130`

## run_skill_script

Name: `run_skill_script` | Capability: `privileged` | Group: `skills, exec`

Legacy low-level skill execution. Runs a skill-local script or entrypoint without plan-based approval. Has the same parameters as `run_skill` but bypasses the plan system.

Source: `internal/tools/skill_exec.go:19-79`
