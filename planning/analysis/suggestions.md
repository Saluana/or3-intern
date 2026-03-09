## What I would do

### 1. Remove “ambient authority” first

This is the biggest win.

Right now or3-intern has several places where code inherits broad host power:

* shell execution through `bash -lc`
* skill scripts
* stdio MCP servers inheriting environment
* webhook/file-watch/heartbeat flows that can become tool-driving agent input    

You do **not** need a full heavy sandbox stack to improve this a lot.

Best lightweight move:

* keep your tools
* add a **tiny capability layer**
* make dangerous tools opt-in per agent/session/channel

Use 3 classes only:

**safe**

* read-only memory lookup
* read-only workspace search
* artifact reads
* weather/search type network calls if you allow them

**guarded**

* write files in workspace
* outbound HTTP to allowlisted hosts
* MCP tools marked trusted
* subagent spawn with quota

**privileged**

* shell
* arbitrary process spawn
* cross-workspace file access
* unrestricted MCP
* skill script execution

Then default to:

* safe = allowed
* guarded = denied unless enabled
* privileged = disabled by default, explicit config only

This is the same general direction as OpenClaw’s hardened baseline, which starts from local bind, pairing, DM isolation, a restricted tool profile, workspace-only FS, and exec denied by default. ([OpenClaw][1])

That gives you a lot of security without much code.

## 2. Replace shell strings with argv execution

Your current `bash -lc` model is one of the highest-risk parts. 

You do not need Docker or Firecracker to improve this. Just stop treating execution as free-form shell whenever possible.

Recommended pattern:

* keep one `exec` tool
* but internally split it into:

  * `exec_cmd(program, args[], cwd, env_allowlist[])`
  * optional legacy `exec_shell(command)` behind privileged mode only

Default behavior:

* only allow `exec_cmd`
* maintain a tiny allowlist of binaries
* strip environment except a short allowlist
* set cwd to workspace
* apply timeout + max output bytes + max child count

That is a huge security jump for very little complexity.

## 3. Enforce workspace-only everywhere

You already have some path confinement logic. 
Make that the central rule of the runtime.

For lightweight hardening, I would make these defaults:

* all file tools are workspace-only
* no symlink traversal outside workspace
* canonicalize every path before use
* deny absolute paths unless explicitly enabled
* one workspace root per agent
* separate temp dir per session/agent

NullClaw explicitly advertises `workspace_only = true` as part of its baseline controls. ([GitHub][2])
That is one of the best “security per line of code” moves you can copy.

## 4. Pairing + allowlists on every external channel

This is another cheap, high-value fix.

OpenClaw’s README and security docs emphasize pairing by default for DMs and explicit allowlists, because inbound messages are untrusted input. ([GitHub][3]) NullClaw also uses pairing and channel allowlists in its baseline. ([GitHub][2])

For or3-intern, I would make the default:

* unknown sender: no agent processing
* return pairing code or deny
* group chats require mention
* each channel peer gets isolated session scope by default
* tools disabled or heavily reduced on shared/group contexts

This matters more than fancy sandboxing for most real-world risk.

## 5. Stop trusting installed skills by default

You already warn users that third-party skills are untrusted, but that is not enough by itself. 

You do **not** need a whole security product here. Just add a small manifest-based policy:

* every skill declares:

  * needs_shell: true/false
  * needs_network: true/false
  * needs_write: true/false
  * allowed_paths
  * allowed_hosts
* installer records these permissions
* runtime enforces them

Even better:

* unsigned/untrusted skills install in **quarantine mode**
* quarantine mode = no shell, no unrestricted network, workspace-only read access until approved

That is much lighter than a real code sandbox, but it closes a lot of the “malicious skill” risk OpenClaw’s ecosystem is now actively dealing with. ([GitHub][4])

## 6. Add one lightweight sandbox backend, not four

NullClaw advertises layered sandbox backends like Landlock, Firejail, Bubblewrap, and Docker. ([GitHub][5])
You do **not** need all that.

For a lightweight Go runtime, I would pick **one** Linux-first isolation option:

* **bubblewrap** if you want practical process/filesystem/network isolation with low implementation effort
* **Landlock** if you want a very light kernel-level FS restriction layer and can tolerate its limits

My recommendation:

* use **no external sandbox for safe/guarded tools**
* use **bubblewrap only for privileged execution paths**
* if bubblewrap is not available, deny privileged exec by default

That gives you a simple model:

* most runtime stays lightweight
* only dangerous actions pay the sandbox cost

## 7. Redact and narrow environment variables

MCP stdio and subprocesses should not inherit the whole process environment. Right now this is a real risk surface. 

Minimal fix:

* parent process has full env
* child tools get a scrubbed env allowlist only:

  * PATH
  * HOME maybe
  * TMPDIR
  * explicitly injected service token only if needed

Also:

* never expose your full provider/API key set to all tools
* bind secrets to the tool or agent that needs them

NullClaw publicly claims encrypted secrets and least-privilege defaults. ([GitHub][5])
You can get part of that value just by removing ambient env inheritance.

## 8. Add quotas before you add more autonomy

This is one of the easiest hardening wins.

Add per-session and per-hour limits for:

* tool calls
* subagent spawns
* shell executions
* outbound HTTP requests
* written bytes
* tokens / model calls if relevant

NullClaw’s security page includes resource limits and supervised autonomy in its recommended config. ([GitHub][2])

This helps against:

* prompt injection loops
* tool spam
* runaway autonomy
* accidental expensive behavior

And it is much simpler than deep behavioral security systems.

## 9. Make autonomy structured, not just prompt-driven

Your heartbeat/webhook/file-watch model is flexible, but raw natural-language event injection is a softer boundary. 

Low-complexity improvement:

* keep natural-language mode
* add an optional structured event mode

Example:

```json
{
  "event_type": "file_changed",
  "workspace": "abc",
  "path": "notes/todo.md",
  "action": "modified",
  "trusted": false
}
```

Then the runtime decides:

* which agent gets it
* whether it becomes only context
* whether tool use is permitted
* whether human approval is needed

This reduces prompt injection surface without heavy machinery.

## 10. Add a “doctor” command instead of more security features

OpenClaw leans hard on security audit and hardened baseline checks. ([OpenClaw][1])
That is actually a very smart lightweight move.

For or3-intern, a simple `doctor` or `security audit` command should flag:

* public bind enabled
* no pairing on public channels
* privileged tools enabled
* unrestricted exec
* unrestricted filesystem
* unrestricted MCP
* inherited env on subprocesses
* no quotas
* world-readable config/db
* unsafe webhook mode

This gives users safer deployments **without** adding lots of runtime complexity.

---

# The smallest roadmap I’d recommend

If you want the shortest path to “much closer to OpenClaw/NullClaw hardening” while staying lightweight, do these in order:

## Phase 1: highest value, low complexity

1. **Capability tiers**: safe / guarded / privileged
2. **Workspace-only FS by default**
3. **Pairing + allowlists by default on channels**
4. **Replace shell-string exec with argv exec**
5. **Scrub child process environments**
6. **Basic quotas and timeouts**

This alone would improve security a lot.

## Phase 2: still light

7. **Skill permission manifests + quarantine mode**
8. **Structured event inputs for heartbeat/webhook/watchers**
9. **One privileged sandbox path using bubblewrap**
10. **`or3-intern doctor` hardening audit**

## Phase 3: only if needed

11. **Encrypted secrets at rest**
12. **Signed audit trail**
13. **Per-agent access profiles**
14. **Trusted-host outbound network policy**

---

# What I would not do

To stay lightweight, I would **not** do these yet:

* full containerization for every tool call
* multi-backend sandbox matrix
* enterprise RBAC
* per-user adversarial multi-tenancy on one runtime
* massive policy DSL
* heavy security scanners built into core runtime

OpenClaw’s docs explicitly warn that one shared gateway is not meant to be a hostile multi-tenant boundary anyway. ([OpenClaw][1]) So trying to solve that inside a tiny runtime is the wrong tradeoff.

---

# My blunt recommendation

Aim for this security posture:

**“single trust boundary, local-first, least privilege, guarded tool execution, workspace confinement, paired channels, optional sandbox for privileged exec.”**

That is realistic, strong, and still lightweight.

In practice, the **best three changes** for you are:

1. kill `bash -lc` as the default execution path,
2. add capability tiers with privileged tools disabled by default,
3. make all external channels paired + allowlisted by default.

Those three changes probably buy more real security than adding ten more subsystems.