# or3-intern manual test walkthrough

This is a human-run testlist for the parts of `or3-intern` you can validate manually.

Use it like a hybrid of a walkthrough and a checklist:

- Work top-to-bottom once for a clean baseline.
- Mark each item `PASS`, `FAIL`, or `BLOCKED`.
- If a section depends on credentials, another device, or a remote service, mark it `BLOCKED` instead of guessing.
- Keep one running notes file with timestamps, screenshots, terminal output, and any config changes you made.

---

## 0. Test rhythm and evidence

Before starting:

- [ ] Create a scratch note for results.
- [ ] Decide whether you are testing against a fresh local config or your normal config.
- [ ] Back up your config before changing anything.

Suggested evidence to capture for each section:

- command you ran
- config keys you changed
- expected result
- actual result
- follow-up issue if something failed

Useful backup commands:

```bash
mkdir -p ~/.or3-intern
cp ~/.or3-intern/config.json ~/.or3-intern/config.manual-test.backup.json 2>/dev/null || true
cp -R ~/.or3-intern ~/.or3-intern.manual-test.snapshot 2>/dev/null || true
```

---

## 1. Preconditions

Goal: confirm you have the minimum needed to run meaningful manual tests.

- [ ] Build the workspace.

```bash
go build ./...
```

Expected:

- build succeeds with no compile errors

- [ ] Confirm you have a working model provider key or endpoint.
- [ ] Confirm `python3` and `curl` are available for service/API testing.
- [ ] Decide whether you also want to test optional integrations: Telegram, Slack, Discord, Email, WhatsApp bridge, MCP, ClawHub skills.

If you do not have provider access yet, you can still test `init`, `version`, `doctor`, some config validation, and startup failure modes.

---

## 2. Fresh setup path

Goal: validate first-run setup and local bootstrap files.

- [ ] Run guided setup.

```bash
go run ./cmd/or3-intern init
```

Walkthrough:

1. Pick a provider preset.
2. Accept or edit the API base.
3. Accept or edit the chat model.
4. Accept or edit the embedding model.
5. Choose whether to store the API key in config.
6. Accept the SQLite and artifacts paths.
7. Keep workspace restriction enabled unless you specifically want to test the opposite.
8. Finish and note the config path shown.

Expected:

- `~/.or3-intern/config.json` is created or updated
- setup ends with a `Next step: go run ./cmd/or3-intern chat` message
- the config reflects the values you entered

- [ ] Verify bootstrap files exist after the first normal runtime launch.

```bash
ls -1 ~/.or3-intern
```

Expected after any normal command like `chat`, `agent`, or `doctor`:

- `SOUL.md`
- `AGENTS.md`
- `TOOLS.md`
- `IDENTITY.md`
- `MEMORY.md`

Note: `init` writes config, but the bootstrap markdown files are created during normal runtime startup.

---

## 3. Baseline CLI sanity

Goal: prove the basic CLI flows work before testing advanced features.

- [ ] Check version output.

```bash
go run ./cmd/or3-intern version
```

Expected:

- prints `or3-intern v1`

- [ ] Run doctor in normal mode.

```bash
go run ./cmd/or3-intern doctor
```

Expected:

- command completes cleanly
- output contains warnings only if your config actually needs attention

- [ ] Run doctor in strict mode.

```bash
go run ./cmd/or3-intern doctor --strict
```

Expected:

- stricter warnings than normal mode
- no crash or confusing panic output

- [ ] Run a one-shot turn.

```bash
go run ./cmd/or3-intern agent -m "Reply with the word READY and nothing else"
```

Expected:

- one assistant response
- no interactive prompt
- response proves provider access is working

- [ ] Run interactive chat.

```bash
go run ./cmd/or3-intern chat
```

Inside chat, test all of these:

- [ ] ask a normal question
- [ ] ask a follow-up that depends on the previous answer
- [ ] ask it to summarize the conversation so far
- [ ] exit cleanly with your normal quit flow (`Ctrl+C` if needed)

Expected:

- responses stream normally
- follow-up context is remembered within the session
- exiting does not corrupt the local DB

---

## 4. Persistent memory and bootstrap context

Goal: verify that standing context and session history are actually used.

- [ ] Put a unique sentence in `~/.or3-intern/IDENTITY.md`.
- [ ] Put a different unique sentence in `~/.or3-intern/MEMORY.md`.

Example:

```text
Identity test sentence: My codename is ManualTestIdentity.
Memory test sentence: The favorite deployment color is ultraviolet.
```

- [ ] Start a new chat session and ask questions that should reveal each fact.

Examples:

```text
What is your codename in this environment?
What is the favorite deployment color?
```

Expected:

- the identity answer comes from `IDENTITY.md`
- the preference/fact answer comes from `MEMORY.md`
- answers should not feel like random guesses

- [ ] Validate conversation persistence across separate one-shot turns.

```bash
go run ./cmd/or3-intern agent -s manual-memory-test -m "Remember that the canary number is 314159"
go run ./cmd/or3-intern agent -s manual-memory-test -m "What is the canary number?"
```

Expected:

- the second turn recalls `314159`

---

## 5. Session scope linking

Goal: verify multiple session keys can share one logical scope.

- [ ] Seed one session.

```bash
go run ./cmd/or3-intern agent -s manual-scope-a -m "Remember that project codename is Moonglass"
```

- [ ] Link two session keys to one scope.

```bash
go run ./cmd/or3-intern scope link manual-scope-a demo-scope
go run ./cmd/or3-intern scope link manual-scope-b demo-scope
go run ./cmd/or3-intern scope list demo-scope
go run ./cmd/or3-intern scope resolve manual-scope-a
go run ./cmd/or3-intern scope resolve manual-scope-b
```

Expected:

- both sessions appear in `scope list`
- both resolve to `demo-scope`

- [ ] Query from the second session.

```bash
go run ./cmd/or3-intern agent -s manual-scope-b -m "What is the project codename?"
```

Expected:

- the answer can recover `Moonglass`

---

## 6. Import / migration

Goal: validate legacy JSONL import.

- [ ] Create a tiny JSONL fixture in `/tmp/manual-session.jsonl` using a real old-format sample if you have one.
- [ ] Run the migration.

```bash
go run ./cmd/or3-intern migrate-jsonl /tmp/manual-session.jsonl migrated:manual
```

Expected:

- command prints `ok`
- no panic or partial-write error

- [ ] Ask a question against the migrated session.

```bash
go run ./cmd/or3-intern agent -s migrated:manual -m "Summarize what you already know from this session"
```

Expected:

- the summary reflects imported content instead of an empty session

If you do not have a known-good legacy JSONL sample, mark this section `BLOCKED` rather than inventing the format.

---

## 7. Skills: read-only checks first

Goal: validate bundled and local skill discovery without changing remote state.

- [ ] List skills.

```bash
go run ./cmd/or3-intern skills list
```

- [ ] List eligible skills only.

```bash
go run ./cmd/or3-intern skills list --eligible
```

- [ ] Check skill status.

```bash
go run ./cmd/or3-intern skills check
```

- [ ] Inspect at least one bundled skill.

```bash
go run ./cmd/or3-intern skills info cron
```

Expected:

- output shows source, location, eligibility, and permission state
- quarantined or blocked skills explain why
- read-only commands work even if no remote registry is configured

---

## 8. Skills: remote registry lifecycle

Goal: validate search/install/update/remove against ClawHub when network access is available.

Run this only if you want to exercise managed installs.

- [ ] Search for a skill.

```bash
go run ./cmd/or3-intern skills search "calendar"
```

Expected:

- either relevant results appear, or `(no results)` appears cleanly

- [ ] Install one low-risk test skill.

```bash
go run ./cmd/or3-intern skills install <slug>
```

- [ ] Re-run list/info/check after install.

```bash
go run ./cmd/or3-intern skills list
go run ./cmd/or3-intern skills info <installed-name>
go run ./cmd/or3-intern skills check
```

- [ ] Update the installed skill.

```bash
go run ./cmd/or3-intern skills update <installed-name>
```

- [ ] Remove the installed skill.

```bash
go run ./cmd/or3-intern skills remove <installed-name>
```

Expected:

- install prints `installed ...`
- update prints either `updated ...` or `up-to-date ...`
- remove prints `removed ...`
- any quarantine or trust drift is clearly explained

---

## 9. Secret store and audit chain

Goal: validate encrypted secret references and audit verification.

First enable these in your config if they are off:

- `security.secretStore.enabled=true`
- `security.audit.enabled=true`

Optional but useful:

- `security.audit.strict=true`
- `security.audit.verifyOnStart=true`

- [ ] Start with a normal admin health check.

```bash
go run ./cmd/or3-intern doctor --strict
```

- [ ] Store a secret.

```bash
go run ./cmd/or3-intern secrets set manual_test_key super-secret-value
```

- [ ] List secrets.

```bash
go run ./cmd/or3-intern secrets list
```

- [ ] Delete the secret.

```bash
go run ./cmd/or3-intern secrets delete manual_test_key
```

- [ ] Verify the audit chain.

```bash
go run ./cmd/or3-intern audit verify
```

Expected:

- `secrets set` prints `stored`
- `secrets list` includes the stored name but not the secret value
- `secrets delete` prints `deleted`
- audit verify prints `[ok] audit chain verified`

Nice extra check:

- [ ] Replace a config secret value with `secret:<name>` and verify the dependent feature still works.

---

## 10. Document indexing

Goal: verify local docs can be retrieved into prompts.

Enable in config:

- `docIndex.enabled=true`
- `docIndex.roots=["/Users/brendon/Documents/or3-intern/docs"]`

Recommended for a quick test:

- keep `refreshSeconds` low, like `10`

- [ ] Start a normal turn after saving config.

```bash
go run ./cmd/or3-intern agent -s manual-docindex -m "What HTTP path does the webhook server use? Answer from indexed docs if available."
```

- [ ] Ask a second question about a precise doc fact not mentioned in your current prompt.

Example:

```bash
go run ./cmd/or3-intern agent -s manual-docindex -m "What listener address is shown for the default internal service config?"
```

Expected:

- answers line up with repo docs instead of hallucinated defaults
- the first run may take longer while indexing warms up

---

## 11. Service mode HTTP API

Goal: validate authenticated loopback service mode, foreground turns, streaming, aborts, and subagents.

Config needed:

- `service.enabled=true`
- `service.listen="127.0.0.1:9100"`
- `service.secret="<long-random-secret>"`

For subagent testing also enable:

- `subagents.enabled=true`

- [ ] Start service mode in one terminal.

```bash
go run ./cmd/or3-intern service
```

Expected:

- terminal shows it is listening on the configured address

- [ ] Generate a bearer token in another terminal.

```bash
export OR3_SERVICE_SECRET='replace-with-your-service-secret'
TOKEN=$(python3 - <<'PY'
import base64, hashlib, hmac, json, os, secrets, time
secret = os.environ['OR3_SERVICE_SECRET']
payload = base64.urlsafe_b64encode(json.dumps({
    'iat': int(time.time()),
    'nonce': secrets.token_hex(12),
}, separators=(',', ':')).encode()).decode().rstrip('=')
sig = hmac.new(secret.encode(), payload.encode(), hashlib.sha256).hexdigest()
print(f"{payload}.{sig}")
PY
)
echo "$TOKEN"
```

- [ ] Send a normal non-streaming turn.

```bash
curl -s \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:9100/internal/v1/turns \
  -d '{"session_key":"svc-manual","message":"Reply with SERVICE_OK only"}'
```

Expected:

- JSON response with a job id and completed result
- assistant output is attached to the completed payload

- [ ] Send a streaming turn.

```bash
curl -N \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: text/event-stream" \
  -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:9100/internal/v1/turns \
  -d '{"session_key":"svc-stream","message":"Count from 1 to 3 slowly"}'
```

Expected:

- SSE events stream until terminal status

- [ ] Check job lookup behavior with an unknown job id.

```bash
curl -s \
  -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:9100/internal/v1/jobs/not-a-real-job/stream
```

Expected:

- `404` style error response

- [ ] If subagents are enabled, queue one background job.

```bash
curl -s \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:9100/internal/v1/subagents \
  -d '{"parent_session_key":"svc-parent","task":"Write a five-word status update","timeout_seconds":60}'
```

Expected:

- response status is accepted
- response contains `job_id` and `child_session_key`

- [ ] Abort a live or completed job and note the behavior.

```bash
curl -s \
  -H "Authorization: Bearer $TOKEN" \
  -X POST http://127.0.0.1:9100/internal/v1/jobs/<job_id>/abort
```

Expected:

- live job: abort may succeed
- completed job: response should still be well-formed

---

## 12. Webhook trigger

Goal: validate authenticated webhook ingestion through `serve`.

Enable in config:

- `triggers.webhook.enabled=true`
- `triggers.webhook.addr="127.0.0.1:8765"`
- `triggers.webhook.secret="manual-webhook-secret"`

- [ ] Start `serve` in one terminal.

```bash
go run ./cmd/or3-intern serve
```

- [ ] Post a simple webhook in another terminal.

```bash
curl -i \
  -H 'X-Webhook-Secret: manual-webhook-secret' \
  -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1:8765/webhook \
  -d '{"event":"manual-test","message":"hello from webhook"}'
```

Expected:

- HTTP `200 OK`
- `serve` keeps running
- the event is processed as a webhook turn

- [ ] Test auth failure.

```bash
curl -i \
  -H 'X-Webhook-Secret: wrong-secret' \
  -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1:8765/webhook \
  -d '{"event":"manual-test"}'
```

Expected:

- HTTP `401 Unauthorized`

- [ ] Test structured tasks via webhook.

```bash
curl -i \
  -H 'X-Webhook-Secret: manual-webhook-secret' \
  -H 'Content-Type: application/json' \
  -X POST http://127.0.0.1:8765/webhook \
  -d '{"version":1,"structured_tasks":[{"tool":"echo_tool","params":{"text":"webhook structured task test"}}]}'
```

Expected:

- request succeeds
- runtime handles the structured payload without crashing

---

## 13. File-watch trigger

Goal: validate local file polling and event publication.

Enable in config:

- `triggers.fileWatch.enabled=true`
- `triggers.fileWatch.paths=["/tmp/or3-filewatch-test.txt"]`
- `triggers.fileWatch.pollSeconds=2`
- `triggers.fileWatch.debounceSeconds=1`

- [ ] Create the watched file before starting `serve`.

```bash
echo 'baseline' > /tmp/or3-filewatch-test.txt
```

- [ ] Start `serve`.

```bash
go run ./cmd/or3-intern serve
```

Important note: the watcher uses the first observation as baseline. The first edit after startup is the one that should trigger.

- [ ] Modify the file after `serve` is already running.

```bash
echo 'first change' >> /tmp/or3-filewatch-test.txt
```

- [ ] Modify it again with structured task content.

```bash
cat > /tmp/or3-filewatch-test.txt <<'EOF'
{"version":1,"structured_tasks":[{"tool":"echo_tool","params":{"text":"filewatch structured task test"}}]}
EOF
```

Expected:

- file changes are noticed after the initial baseline
- repeated writes honor debounce instead of spamming
- structured content does not crash the runtime

---

## 14. Heartbeat automation

Goal: validate timer-based autonomous turns.

Enable in config:

- `heartbeat.enabled=true`
- `heartbeat.intervalMinutes=1`

Put a very obvious instruction in `~/.or3-intern/HEARTBEAT.md`, for example:

```text
When this heartbeat runs, produce a short status message that includes the phrase HEARTBEAT_OK.
```

- [ ] Start `serve` and wait a bit longer than one interval.

```bash
go run ./cmd/or3-intern serve
```

Expected:

- background heartbeat turns occur without manual input
- editing `HEARTBEAT.md` should affect later heartbeat runs without restarting `serve`

Extra manual check:

- [ ] While `serve` is running, edit `HEARTBEAT.md` and confirm the next cycle reflects the new instruction.

---

## 15. Cron jobs

Goal: validate scheduled autonomous turns from the cron store.

By default, cron is enabled and stored in `~/.or3-intern/cron.json`.

- [ ] Write a one-minute repeating cron job into that file.

Example store:

```json
{
  "version": 1,
  "jobs": [
    {
      "id": "manual-cron-job",
      "name": "manual cron test",
      "enabled": true,
      "schedule": { "kind": "every", "every_ms": 60000 },
      "payload": {
        "kind": "agent_turn",
        "message": "Respond with CRON_OK only",
        "session_key": "cron:manual"
      }
    }
  ]
}
```

- [ ] Start `serve` and wait at least one interval.

```bash
go run ./cmd/or3-intern serve
```

Expected:

- the cron service loads the file cleanly
- the scheduled turn runs at roughly the expected interval
- malformed cron store content should fail clearly, not silently

Optional negative test:

- [ ] Put invalid JSON in `cron.json` and confirm startup fails loudly.

---

## 16. External channels

Goal: validate each inbound/outbound adapter you actually plan to use.

Run these as separate mini-tests. Do not enable every channel at once unless you really want to test combined behavior.

Common checks for every channel:

- [ ] inbound message reaches the runtime
- [ ] response is delivered back to the channel
- [ ] allowlist/open-access behavior matches config
- [ ] mention requirements behave correctly where applicable
- [ ] `send_message` can target the configured default destination if relevant

### Telegram

Config:

- `channels.telegram.enabled=true`
- token set
- optional `allowedChatIds`

Test:

- [ ] send a DM or message from an allowed chat
- [ ] confirm the bot replies
- [ ] try a blocked chat if you want to test access control

### Slack

Config:

- `channels.slack.enabled=true`
- app token set
- bot token set
- optional `allowedUserIds`
- recommended `requireMention=true`

Test:

- [ ] send a message with mention in an allowed workspace/channel
- [ ] confirm reply arrives
- [ ] send a message without mention and confirm it is ignored when `requireMention=true`

### Discord

Config:

- `channels.discord.enabled=true`
- token set
- optional `allowedUserIds`
- recommended `requireMention=true`

Test:

- [ ] mention the bot in a channel
- [ ] confirm reply arrives
- [ ] test non-mentioned traffic if `requireMention=true`

### Email

Config:

- `channels.email.enabled=true`
- `channels.email.consentGranted=true`
- IMAP and SMTP fully configured
- either `openAccess=true` or `allowedSenders` is set

Test:

- [ ] send an email from an allowed sender
- [ ] confirm it is ingested
- [ ] if `autoReplyEnabled=true`, confirm a reply is sent
- [ ] if `autoReplyEnabled=false`, confirm normal inbound processing does not auto-reply

### WhatsApp bridge

Config:

- `channels.whatsApp.enabled=true`
- bridge URL set
- optional bridge token set

Test:

- [ ] send a message through the bridge-backed chat
- [ ] confirm inbound/outbound round-trip

---

## 17. MCP tool integrations

Goal: validate that configured MCP servers connect and expose tools safely.

Only run this if you have an MCP server ready.

Suggested test sequence:

- [ ] add one MCP server under `tools.mcpServers`
- [ ] start `chat` or `serve`
- [ ] confirm startup does not fail
- [ ] ask the model to use the MCP-provided tool for a simple task
- [ ] if using HTTP transport, confirm host/network policy allows it intentionally

Expected:

- tools appear usable through the normal runtime flow
- timeouts and failures are reported cleanly
- insecure HTTP is rejected unless explicitly allowed for loopback/localhost use

---

## 18. Security profile startup checks

Goal: validate that stricter runtime profiles fail closed when misconfigured.

Test these one at a time.

- [ ] Set `runtimeProfile` to `single-user-hardened` and confirm normal startup still works.
- [ ] Set `runtimeProfile` to `hosted-service` without enabling secret store/audit/network and confirm `serve` or `service` refuses to start.
- [ ] Add the required security sections and confirm startup now succeeds.
- [ ] Set `runtimeProfile` to `hosted-no-exec` and confirm risky exec posture is rejected if enabled.
- [ ] Set `runtimeProfile` to `hosted-remote-sandbox-only` and confirm sandbox requirements are enforced.

Expected:

- hosted profiles fail loudly and clearly when requirements are missing
- `doctor --strict` points out the same kinds of problems before startup does

---

## 19. Final wrap-up

Goal: finish with a useful artifact instead of vague impressions.

- [ ] List every section as `PASS`, `FAIL`, or `BLOCKED`.
- [ ] Record the config variants you used.
- [ ] Save any terminal output that showed failures.
- [ ] Turn each real failure into a concrete issue with reproduction steps.

Suggested summary template:

```text
Environment:
- provider:
- config path:
- runtime profile:

Passed:
-

Failed:
-

Blocked:
-

Most important follow-ups:
-
```

If you want the fastest possible pass first, do sections 1 through 5, then 9, then 11 through 15, and leave channels/MCP for last.