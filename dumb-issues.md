# or3-intern audit findings

Audit date: 2026-08-11

Audited revision: `2f2b42e380c81cd19746bca48ca26083790609c3` (`main`, equal to `origin/main`)

Scope: Go CLI/runtime, service API, configuration persistence/live reload, channel lifecycles, terminal sessions, release/bootstrap path, help and first-run UX.

## Verdict

This report preserves the original audit evidence and records the completed remediation below. All identified source, durability, memory-bounding, and CLI-UX issues are now addressed. The zero-install Connect bootstrap is coordinated at `v0.1.3`; its tag workflow must publish both npm packages and all matching binary assets before the release is considered complete. Managed remote Connect remains withheld.

## Original priority summary

| Priority | Count | Meaning |
| --- | ---: | --- |
| P1 | 6 | Fix before the next release or any serious long-running deployment. |
| P2 | 11 | Important correctness, durability, memory, and UX work. |
| P3 | 1 | Discoverability cleanup that should ride with the CLI UX pass. |

## Remediation status

| # | Status |
| --- | --- |
| 1 | [x] — Go 1.26.5, x/net 0.53.0, and a pinned CI vulnerability gate; scanner now reports 0 reachable vulnerabilities. |
| 2 | [x] — `agent` now waits for a terminal turn, prints its result, exits non-zero on failure/timeout, and aborts an unfinished turn. Runner chat events are now available outside service mode; a fake-runner CLI check completed with the expected output and no active record. |
| 3 | [x] — Service config now has a single serialized mutation path with deep immutable snapshots for readers. A live update also refreshes the embedding provider, memory service, runner orchestrator, control plane, and service app together; concurrent read, provider-map, and skill-map updates pass repeated `-race` coverage. |
| 4 | [x] — Discord, Slack, and WhatsApp now supervise inbound sockets with context-bound, jittered exponential reconnects, interruptible initial handshakes, atomic connection replacement, clean stop/join behavior, and permanent-auth failure detection. The manager safely cancels an in-flight `Start`; live state drives `/health` `channelStatuses` and degradation. Inbound-ID and Discord display-name caches are bounded and cleared on shutdown. |
| 5 | [x] — The coordinated `v0.1.3` release builds all Intern assets, publishes both immutable npm packages, verifies registry propagation, and smokes the exact `npx @or3/connect@0.1.3 intern` path before completion. |
| 6 | [x] — Channel approvals now use the live runner-turn continuation path when available, give one deterministic truthful response when it is not, and close unrecoverable fallback turns instead of leaving them running. |
| 7 | [x] — Config writes are serialized, written to a synced 0600 sibling temp file, atomically renamed, and directory-synced; a concurrent reader/writer regression test passes under `-race`. |
| 8 | [x] — Root globals now work before or after the command, including `--config=value`; malformed/unknown root flags fail clearly, and `version`, `config-path`, `init`, and `setup` reject stray positional arguments. Parser coverage and an actual post-command `config-path --config` run pass. |
| 9 | [x] — Local-only Relaxed mode now reports its deliberate lack of audit/secret-store/profile hardening as one informational posture note, not unresolved warnings. The same choices regain security warnings as soon as service access, channels, or webhooks are enabled. Status and setup ignore informational findings as problems. |
| 10 | [x] — Deprecated session-cache, max-tool-bytes, and all legacy context-packet controls are hidden from configure/settings/status and rejected by the live configuration API. Their JSON fields remain loadable without validation. Status/capabilities now describe runner-managed execution and the separately gated service terminal rather than legacy host-command flags. |
| 11 | [x] — Terminal capacity is now reserved atomically and counts only running (or in-flight creation) sessions. Exited sessions remain available for short replay/status retrieval without blocking a new terminal; terminal list/status reads are synchronized with live session updates. |
| 12 | [x] — Discord heartbeats are now connection-scoped, canceled, and joined before a disconnected socket is retried. Repeated gateway reconnect tests exercise the lifecycle under `-race`. |
| 13 | [x] — Authentication failures now sweep all expired keys on normal reads/writes and enforce a 4,096-entry maximum by evicting the oldest state before admitting a new credential key. |
| 14 | [x] — Secure-relay routes now purge expired entries on registration and lookup, remove an expired route immediately when it is used, and cap live in-memory routes at 4,096 with earliest-expiry eviction. |
| 15 | [x] — Email thread metadata is now a 512-entry LRU-style cache with database fallback and is cleared on channel stop. |
| 16 | [x] — `version` now prints release/source provenance (version, commit, dirty state, build time, Go version, and platform). Release builds inject those values through ldflags, and the source installer injects and verifies the checkout commit. |
| 17 | [x] — One root-command catalog now drives the help index and validates top-level command names before runtime bootstrap. Every listed executable command has a help topic; `memory` and `access` are documented, duplicates are prevented, and command-local `--advanced` flags are preserved. |
| 18 | [x] — dotenv loading is explicit opt-in and memory-skill registration now saves only the persisted config snapshot, so environment-only values cannot be written back to disk. |

## Final remediation validation

- `go test ./...` passed across the full repository after the final shutdown-path repair.
- `go test -race ./cmd/or3-intern ./internal/channels/... ./internal/config ./internal/controlplane ./internal/uxstate -count=1` passed.
- Repeated race checks passed: the channel reconnect/initial-dial/manager lifecycle scope at `-count=50`, and the live-config, persistence, and health scope at `-count=25`.
- `go vet ./...`, `go mod verify`, and `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` passed; the vulnerability scanner reports 0 reachable vulnerabilities.
- `npm test` in `packages/or3` passed all 13 bootstrap tests; installer syntax and the runner-first policy check passed.
- Direct source CLI smoke passed for `version`, `help memory`, post-command `config-path --config`, and `settings --advanced` against an isolated config.
- `git diff --check` passed.

## Original audit validation

- Built a fresh binary from `2f2b42e` and ran it against an isolated config, workspace, and SQLite database.
- Completed non-interactive setup, selected the Solo + Relaxed path, opened settings, and exercised `status`, `health`, `doctor`, `capabilities`, `skills`, `help`, `config-path`, `version`, and `agent`.
- Compared the source binary, the installed `/opt/homebrew/bin/or3-intern`, npm metadata, and GitHub releases.
- Ran `go test ./...`, `go vet ./...`, `go mod verify`, `npm test`, and `scripts/check-runner-first-forbidden.sh` successfully once.
- Ran the full race suite; it failed on the channel-approval behavior below. The focused approval test failed 3/30 normal runs and 30/30 race-enabled runs.
- Ran official `govulncheck` against all packages and queried available module updates.

## P1 — Release and reliability blockers

### 1. Current builds reach 16 vulnerability IDs across the standard library and `x/net`

**Location:** `go.mod:3`, `go.mod:22`, `.github/workflows/publish.yml:50`, `.github/workflows/publish.yml:65`

**Evidence:** `govulncheck ./...` reported 16 called vulnerability IDs in the Go 1.26.0 standard library; one of those IDs, GO-2026-4918, also reaches `golang.org/x/net@v0.52.0`. Called paths include inbound email parsing, TLS, x509/WebAuthn verification, HTTP clients, Discord WebSocket setup, and network-policy transports. Notable results include x509 authentication/constraint bugs, quadratic email/header parsing, TLS connection-retention DoS, and an HTTP/2 infinite loop. The scanner identifies Go 1.26.5 and `x/net` 0.53.0 as the respective minimum fixes. The workflow selects its toolchain from the bare `go 1.25.0` directive and has no vulnerability gate.

**Consequence:** A binary can pass all existing tests and still ship known remotely influenced denial-of-service and authentication/TLS defects. Rebuilding with the current local Go 1.26.0 does not fix them.

**Fix:** Pin a patched supported Go toolchain (on the current branch, at least 1.26.5), upgrade `golang.org/x/net` to at least 0.53.0, run the affected tests, and add `govulncheck ./...` to CI/release qualification. Rebuild every release asset after the toolchain change.

**Validation:** Require a zero-exit `govulncheck ./...` on the exact release build graph and confirm `go version -m` on every archive reports the patched toolchain.

### 2. `agent` returns success while abandoning a foreground turn in `running`/`queued`

**Location:** `cmd/or3-intern/main.go:556`, `cmd/or3-intern/main.go:577`, `internal/app/turn_orchestrator.go:277`

**Evidence:** The command is advertised as “Run a one-shot foreground turn,” but it only calls `StartTurn`, discards the returned IDs, prints no result, and exits zero. In the isolated run, `or3-intern agent -m hello` exited immediately; SQLite was left with runner chat turn `rct_9346ddcbf041bfaa347d` in `running` and runner run `rr_job-runner-b72f5` in `queued`. The same package already has `WaitForTurnResult`, used for external channel delivery, but `agent` never calls it.

**Consequence:** Scripts see a false success, users get no answer or durable job handle, and process shutdown interrupts the worker that was expected to execute the turn. The unique active-turn row can also block the next turn until reconciliation.

**Fix:** Make `agent` genuinely foreground: retain the `RunnerTurnResult`, wait with a bounded/signal-aware context, print final text, and map terminal failure states to non-zero exits. If asynchronous enqueue is intentional, require a persistent service, return the turn/job IDs, rename the command/copy, and never claim foreground completion.

**Validation:** An end-to-end fake-runner test must prove the command waits, prints the response, returns the right status, and leaves no `queued`/`running` record after process exit.

**Remediation:** `agent` now waits on the durable turn result with the configured timeout, aborts on cancellation/timeout, and emits the final turn text. The runtime now creates its bounded job registry for every command capable of starting a runner turn, rather than only service mode. The process runner also drains stdout/stderr before closing process pipes, preventing a fast runner from losing its final structured message. An isolated fake Codex runner now completes with `OR3_AGENT_E2E_OK` and leaves a terminal `succeeded` turn.

### 18. dotenv loading can override an explicitly selected config and persist environment-only secrets

**Location:** `internal/config/dotenv.go`, `internal/config/load.go:16`, `cmd/or3-intern/bundled_skills_dir.go:14`

**Evidence:** Every normal config load searched the current and parent directories for `.env` files and applied their values automatically. That behavior could override a supplied `--config` file's storage paths and provider settings. During runtime bootstrap, memory-skill policy registration then saved the already environment-overlaid `Config` value, which could copy environment-only API credentials into the persisted config file.

**Consequence:** A command intended to operate on an isolated config can instead touch a different database/artifact location, and a secret intentionally kept only in the process environment can be written to disk. The old auto-load behavior also contradicted the repository's own `.env` guidance.

**Fix:** Require explicit `OR3_LOAD_DOTENV=true` before reading dotenv files. When auto-registering the bundled memory skill, reload the persisted config and save only that policy mutation; retain runtime/environment values in memory only.

**Validation:** Added dotenv opt-in coverage and a regression test proving memory-skill registration does not overwrite the stored provider key with a runtime-only value.

### 3. Live configuration updates race every concurrent service request and can lose changes

**Location:** `cmd/or3-intern/service.go:33`, `cmd/or3-intern/service_configure.go:260`, `cmd/or3-intern/service_configure.go:324`, `cmd/or3-intern/service_skills.go:118`, `cmd/or3-intern/service_skills.go:146`

**Evidence:** `serviceServer.config` is a plain struct with nested maps and slices. HTTP handlers read it concurrently across auth, models, files, channels, skills, terminal, doctor, and middleware. Configure and skill handlers shallow-copy it, mutate nested state, save, and assign it back without a mutex or atomic snapshot. Two simultaneous writes can both start from the same old config, and a shallow-copy map mutation can overlap a reader.

**Consequence:** The service has real data-race and lost-update paths. Concurrent map access can panic the process; otherwise one settings change can silently overwrite another or readers can observe a partially coherent live configuration.

**Fix:** Give configuration one owner. Serialize mutations, deep-clone map/slice state, persist before publishing an immutable snapshot, and make readers load that snapshot atomically or under an `RWMutex`. Apply the same mechanism to skills and all live-reloaded dependent services.

**Validation:** Add concurrent configure/read and configure/configure race tests, including nested provider/skill maps, and run them with `-race -count=100`.

### 4. Discord, Slack, and WhatsApp silently stop receiving after one transient WebSocket failure

**Location:** `internal/channels/discord/discord.go:81`, `internal/channels/discord/discord.go:144`, `internal/channels/slack/slack.go:61`, `internal/channels/slack/slack.go:118`, `internal/channels/whatsapp/whatsapp.go:52`, `internal/channels/whatsapp/whatsapp.go:138`

**Evidence:** Each channel dials exactly once in `Start`. Any `ReadMessage`/`ReadJSON` error simply returns from the only read goroutine. There is no reconnect loop, backoff, health transition, supervisor notification, or restart. Existing tests cover only the first successful connection.

**Consequence:** Routine network changes, server restarts, Discord reconnect requests, Slack Socket Mode rotations, or bridge restarts permanently disable inbound messages while the OR3 process continues to look healthy.

**Fix:** Add a context-bound reconnect supervisor with bounded exponential backoff and jitter, replace/close connections atomically, re-authenticate/resume where the protocol permits, and expose degraded/reconnecting health. Avoid retrying invalid credentials forever.

**Validation:** For all three adapters, force the first test server connection closed, bring up a second connection, and prove a later inbound message is delivered exactly once without restarting OR3.

### 5. The primary zero-install quick start is not available from npm or GitHub releases

**Location:** `README.md:7`, `README.md:12`, `packages/or3/package.json:3`, `docs/connect-release-status.md:21`

**Evidence:** The README leads with `npx @or3/connect intern` as the simplest setup. npm currently serves `@or3/connect@0.1.0`, while the `intern` implementation is source version 0.1.1. GitHub has only release `v0.1.0`; no matching v0.1.1 assets exist. The later caveat documents the gap, but the first command remains nonfunctional for a new user.

**Consequence:** The highest-visibility onboarding path fails before setup begins. Users must notice a caveat, clone the source, install Go, and infer an alternate command—the opposite of the promised simple UX.

**Fix:** Either publish one coordinated new immutable Connect version plus matching checksummed Intern assets, following the repository release contract, or make the currently executable source/install path the primary quick start until publication is complete.

**Validation:** From a clean machine/cache, run the exact README command for every supported OS/architecture, finish setup, run `version`, and verify exact npm version and release asset provenance.

**Remediation:** The source, `@or3/connect`, and `@or3/intern-client` manifests now share version `0.1.3`. The release workflow builds all four platform archives from the same tag, injects build provenance, creates the GitHub release, qualifies immutable npm tarballs from their package directories, publishes the client before Connect, waits for exact registry propagation, and smokes the exact bootstrap on Node 20 Alpine. Local docs use the pinned command while managed remote Connect stays explicitly withheld. Tag `v0.1.2` built verified binaries but stopped before npm publication because `npm pack --prefix` resolved the repository root; the workflow fix and immutable replacement use `v0.1.3`.

**Validation:** Pre-release qualification passed the complete Go suite, vet, module verification, a pinned vulnerability scan with 0 reachable vulnerabilities, both npm test suites, tarball inspection, and a built `v0.1.3` CLI smoke. Final closure still requires the tag workflow and exact npm/asset propagation checks to succeed independently.

## P2 — Correctness, durability, leaks, and UX

### 6. Channel approval tells users work is continuing even though continuation is explicitly unsupported

**Location:** `cmd/or3-intern/channel_approvals.go:59`, `cmd/or3-intern/channel_approvals.go:65`, `cmd/or3-intern/channel_approvals.go:100`, `cmd/or3-intern/channel_approvals_test.go:46`

**Evidence:** Approval synchronously sends “Continuing now,” then starts a goroutine whose only behavior is to send “Built-in tool execution resume is no longer supported… Retry the turn.” Delivery order is nondeterministic. The focused test failed 3/30 ordinary runs and 30/30 race-enabled runs because the second message overtook the acknowledgement.

**Consequence:** The user receives mutually exclusive instructions, approved actions do not resume, and CI is flaky. This is a product failure exposed by timing, not just a test-order problem.

**Fix:** Route native runner approval responses through the live `ChatManager` continuation path when possible. When continuation is impossible, make the single acknowledgement truthful and include a concrete retry action. Do not use an untracked goroutine for ordered conversational output.

**Validation:** Test native resume, dead-runtime fallback, denial, and deterministic message order under normal and race-enabled repetition.

### 7. Config persistence can truncate the only config file on crash, disk-full, or competing writes

**Location:** `internal/config/save.go:12`, `internal/config/save.go:24`

**Evidence:** `config.Save` writes JSON directly over the destination with `os.WriteFile`. There is no same-directory temporary file, sync, atomic rename, backup, or write serialization.

**Consequence:** An interrupted or partial write can leave invalid/truncated JSON and prevent startup. Combined with concurrent service settings writes, last-writer-wins can also destroy a valid intervening edit.

**Fix:** Serialize saves and use a 0600 same-directory temp file, write/sync/close, atomic rename, and directory sync where supported. Preserve the previous valid file on any pre-rename failure.

**Validation:** Add injected short-write/failure tests and concurrent save tests; after each failure, the destination must parse as either the complete old or complete new config, never a partial mix.

### 8. Global flags after the command are silently ignored and can target the wrong config

**Location:** `cmd/or3-intern/help.go:522`, `cmd/or3-intern/help.go:538`, `cmd/or3-intern/main.go:207`

**Evidence:** Go's root `FlagSet` stops at the first positional command. `or3-intern config-path --config /tmp/a.json` exited zero and printed `/Users/brendon/.or3-intern/config.json`; only `or3-intern --config /tmp/a.json config-path` worked. Pre-config commands also ignore extra arguments: `or3-intern version unexpected` exits zero.

**Consequence:** A natural flag placement can read or mutate the user's real config instead of an isolated/test config without any warning. Typos look successful.

**Fix:** Parse documented global options in either position, or reject post-command globals with an explicit usage error. Require exact argument counts for `version`, `config-path`, and other fixed-shape commands.

**Validation:** Table-test globals before/after commands, `--flag=value`, unknown flags, missing values, and unexpected positional arguments; wrong placement must never silently fall back.

### 9. Relaxed first-run setup immediately reports its own intentional choices as problems

**Location:** `internal/safetymode/safetymode.go:143`, `internal/doctor/engine_security.go:12`, `cmd/or3-intern/setup_cmd.go:193`

**Evidence:** Relaxed mode deliberately disables audit, approvals, network policy, and sandboxing. Immediately after saving, setup runs unconditional security diagnostics. The isolated local-only Solo + Relaxed setup reported “Review recommended” with four items; three were the intentionally disabled audit log, access profiles, and secret store. `health --check --json` likewise returned `ready with warnings`.

**Consequence:** The recommended setup flow cannot end in a clean state for the mode the user just selected. This trains users to ignore health warnings and makes the safety choice feel broken.

**Fix:** Make diagnostics mode/exposure-aware. For a local-only Relaxed instance, render deliberate tradeoffs as acknowledged informational posture, not unresolved warnings. Escalate them when service/external ingress/integrations make them materially unsafe.

**Validation:** Snapshot setup/status/health for every scenario × safety mode. Each intentional standard posture should be internally consistent, while genuinely unsafe cross-combinations still warn or block.

### 10. Settings and status expose deprecated controls as if they still govern runner-first behavior

**Location:** `internal/config/types.go:101`, `internal/config/types.go:155`, `cmd/or3-intern/configure_tui.go:1083`, `cmd/or3-intern/configure_tui.go:1107`, `internal/uxstate/uxstate.go:297`, `internal/uxstate/uxstate.go:311`

**Evidence:** The config types mark session cache, max tool bytes, and most context packet fields deprecated and unenforced. The advanced TUI still offers “Session cache limit,” “Max tool bytes,” retrieval thresholds, pressure levels, task card, and artifact summary controls with descriptions promising runtime effects. The simple Settings home also reports context mode/max tokens and “commands enabled” from legacy host fields, while `capabilities` hardcodes host exec unavailable in runner-first mode.

**Consequence:** Users spend time tuning knobs that do nothing and receive conflicting descriptions of what runners can actually do. This is unnecessary complexity and a correctness bug in the control surface.

**Fix:** Hide deprecated fields from all settings/status/help surfaces while retaining JSON compatibility. Replace them with real runner/model context and permission controls, or explicitly label read-only legacy values as ignored.

**Validation:** Maintain a test mapping every visible setting to at least one production consumer; deprecated/consumerless fields must not appear in simple or advanced UI snapshots.

### 11. Exited terminal sessions consume the four-session “active” quota for up to ten minutes

**Location:** `cmd/or3-intern/service.go:129`, `cmd/or3-intern/service_terminal.go:306`, `cmd/or3-intern/service_terminal.go:391`, `cmd/or3-intern/service_terminal.go:553`

**Evidence:** Session creation treats `len(sessions)` as the active count. Natural process exit only calls `session.close(status)` and leaves the entry in the map. Cleanup deletes only expired entries; explicit close is the only immediate deletion path.

**Consequence:** Four short-lived shells that exit naturally can make the fifth request fail with HTTP 429 “too many active terminal sessions” for the remainder of the ten-minute TTL, even though no process is active.

**Fix:** Count only running sessions for the concurrency limit. Retain completed replay records in a separately bounded/history structure or evict them after subscribers drain; natural exit must free the execution slot immediately.

**Validation:** Start and naturally exit four shells without calling close, then immediately create a fifth. It must succeed while prior output remains retrievable according to the documented replay policy.

### 12. Discord leaks its heartbeat goroutine after the gateway read loop exits

**Location:** `internal/channels/discord/discord.go:130`, `internal/channels/discord/discord.go:161`, `internal/channels/discord/discord.go:162`

**Evidence:** The child goroutine selects on `ctx.Done()` and `heartbeatTicker.C`. When `ReadMessage` fails, `readLoop` stops the ticker and returns but does not cancel the context. A stopped ticker does not close its channel, so the heartbeat goroutine remains blocked until the entire channel is explicitly stopped. The closure also captures the mutable ticker variable, making repeated Hello frames unsafe.

**Consequence:** Every dropped Discord gateway connection retains a goroutine and captured connection/channel state for the lifetime of the service. Reconnect work would multiply the leak unless lifecycle ownership is fixed first.

**Fix:** Keep heartbeat in the same connection-scoped lifecycle or give it a dedicated cancel/done pair that is always canceled and joined on read-loop exit. Capture an immutable ticker/connection per goroutine and replace prior heartbeat state safely.

**Validation:** Repeatedly connect, receive Hello, force disconnect, and assert goroutine count/state returns to baseline under `goleak` or an explicit done-channel test.

### 13. Authentication failure tracking retains arbitrary credential hashes indefinitely

**Location:** `cmd/or3-intern/service_auth.go:237`, `cmd/or3-intern/service_auth.go:242`, `cmd/or3-intern/service_auth.go:271`, `cmd/or3-intern/service_auth.go:316`

**Evidence:** Every distinct failed credential creates a map key. Expired entries are removed only when `RetryAfter` is later called for that exact key; there is no global sweep, size cap, or LRU. An attacker can keep presenting new bearer/session strings, so old credential keys are never revisited.

**Consequence:** A long-running exposed service grows memory with attacker-controlled cardinality. IP backoff slows a single source but does not bound distributed abuse or lifetime accumulation.

**Fix:** Periodically or opportunistically sweep all expired entries, enforce a hard maximum, and use bounded per-IP/global accounting rather than permanent per-credential cardinality.

**Validation:** Record a large set of unique credentials, advance time past the window, trigger maintenance, and prove the map returns to a small bounded size.

### 14. Expired secure-relay routes accumulate unless an operator calls a separate purge API

**Location:** `cmd/or3-intern/service_secure_relay.go:36`, `cmd/or3-intern/service_secure_relay.go:92`, `cmd/or3-intern/service_secure_relay.go:118`, `cmd/or3-intern/service_secure_connections.go:409`

**Evidence:** `registerRoute` always inserts. `forward` recognizes an expired route but does not delete it. The only sweep is tied to the explicit secure-connection purge endpoint, not route creation, lookup, expiry, a timer, or service maintenance.

**Consequence:** Normal secure-connection churn permanently grows the in-memory route map during the service lifetime; abuse accelerates it.

**Fix:** Delete expired routes on lookup and insertion, add bounded periodic cleanup, and cap live routes. Route/session teardown should remove associated entries directly.

**Validation:** Create and expire many routes without calling the admin purge endpoint and prove automatic maintenance bounds the map.

### 15. Email thread cache grows once per allowed sender with no eviction

**Location:** `internal/channels/email/email.go:70`, `internal/channels/email/email.go:82`, `internal/channels/email/email.go:814`

**Evidence:** `threadBySender` stores every sender encountered and is never capped, expired, or cleared while the channel runs. The adjacent processed-message cache is correctly bounded, showing the omission is specific to thread state. A database fallback already exists for cache misses.

**Consequence:** Open/pairing/domain-wide email configurations can grow resident memory indefinitely with unique allowed senders. The map duplicates state already persisted in SQLite.

**Fix:** Use a small TTL/LRU cache or remove the cache and rely on the bounded database lookup. Clear it on channel stop/reconfiguration.

**Validation:** Feed more unique senders than the configured cap and verify old entries evict while replies still reconstruct threading from SQLite.

### 16. `version` cannot identify a build, so the installed CLI was stale without saying so

**Location:** `cmd/or3-intern/main.go:209`, `scripts/install-cli.sh:18`, `.github/workflows/publish.yml:65`

**Evidence:** `version` always prints `or3-intern v1`; neither local nor release builds inject a semantic version/commit. The installed `/opt/homebrew/bin/or3-intern` was built from `c8d68e1` while the checkout/current fresh binary is `2f2b42e` (two commits newer), yet both report the same `v1`. Only `go version -m` exposed the difference.

**Consequence:** Users, support, and automation cannot tell whether a fix is installed, diagnose mismatched assets, or safely qualify upgrades. “Run version” in the install script is not a freshness check.

**Fix:** Print semantic release version, commit, dirty state, build time, Go version, and platform from ldflags/build info. Make source installs identifiable and have the installer verify the resulting revision/version.

**Validation:** Release, source, dirty, and stale binaries must produce distinguishable machine-readable output; archive smoke tests must assert the exact tag and commit rather than merely a zero exit.

## P3 — Discoverability

### 17. Root help is internally inconsistent and omits the real `memory` command

**Location:** `cmd/or3-intern/help.go:25`, `cmd/or3-intern/help.go:47`, `cmd/or3-intern/help.go:50`, `cmd/or3-intern/help.go:75`, `cmd/or3-intern/main.go:610`

**Evidence:** Root help lists `chat` twice, exposes a no-op `--advanced` compatibility flag, describes `agent` as foreground, and omits `memory` entirely. `or3-intern help memory` exits 2 with “unknown help topic,” even though `memory search`, `add-note`, and `pinned` are implemented.

**Consequence:** Users cannot discover an important feature from the CLI and cannot trust the command taxonomy.

**Fix:** Generate the root index and topics from one command registry, add complete memory help/examples, remove duplicate/no-op entries, and make help claims match actual sync/async behavior.

**Validation:** Add a test that every executable top-level command has exactly one root entry and a valid help topic, except intentionally hidden internal commands.

**Remediation:** A single root-command catalog now produces the three root-help sections and is also used to reject unknown top-level commands before configuration/runtime bootstrap. `memory` and `access` have complete topics and examples. The global parser no longer consumes command-local `--advanced`, so settings, health, and status receive their own flag normally.

**Validation:** Focused CLI tests verify every catalog command has a unique entry and a topic (except the root `help` alias), preserve command-local advanced flags, and reject duplicate root help entries. A real `go run ./cmd/or3-intern help memory` smoke run printed the new memory guide.

## Recommended execution order

1. Patch the Go toolchain and `x/net`, add the vulnerability gate, then rebuild all artifacts.
2. Fix foreground `agent`, configuration ownership/persistence, and WebSocket reconnect/lifecycle behavior.
3. Resolve channel approval continuation and the bounded-memory issues.
4. Remove deprecated/no-op controls and make setup/status/help describe runner-first reality.
5. Before reintroducing a zero-install bootstrap, publish and smoke-test one coordinated Connect + Intern release, then reinstall the local CLI and verify exact build metadata.
