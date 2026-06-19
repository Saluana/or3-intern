# Tasks

## 1. Add `filesystem_browsing` config to `or3-intern`

- [ ] (Req 4, 5) Add `FilesystemBrowsing bool` to the `Config` struct in `internal/config/config.go`, defaulting to `false`.
- [ ] (Req 4, 5) In `cmd/or3-intern/service_files.go`, `serviceFileRoots()`: conditionally append `{id:"filesystem", label:"Full Filesystem", path:"/", writable:false}` when `config.FilesystemBrowsing` is `true`.
- [ ] (Req 4) `gofmt` touched Go files.

## 2. Backend tests

- [ ] (Req 4, 5) Add test: `serviceFileRoots` includes filesystem root when `FilesystemBrowsing=true`.
- [ ] (Req 4, 5) Add test: `serviceFileRoots` excludes filesystem root when `FilesystemBrowsing=false` (default).
- [ ] (Req 4) Run `go test ./cmd/or3-intern ./internal/config` to verify.

## 3. Create `useThreadCwd` composable in `or3-app`

- [ ] (Req 1, 2) Create `app/composables/useThreadCwd.ts` — read `runnerCwd` from active `ChatSession`, expose `cwd` computed ref and `setCwd(path)`.
- [ ] (Req 2) `setCwd` calls `patchSession({ runnerCwd: path })` which triggers existing localStorage persistence.
- [ ] (Req 2) Add unit test: `setCwd` patches the session; `cwd` reflect the active session value.

## 4. Add CWD indicator to the composer

- [ ] (Req 1) In `AssistantComposer.vue`, add a CWD chip in the composer toolbar (next to runner selection chips). Show folder icon + abbreviated path, or "Workspace root" when unset.
- [ ] (Req 1) Click handler opens `CwdPickerSheet` with props `open`, `purpose="directory"`, `initialPath`.
- [ ] (Req 1) On `@select` event, call `setCwd(path)` from `useThreadCwd` and close the sheet.

## 5. Wire `runnerCwd` into the send payload

- [ ] (Req 3) In `AssistantComposer.vue` around the payload builder (~line 1290), add `runnerCwd: cwd.value` to the `AssistantSendPayload` object.
- [ ] (Req 3) Verify: payload with `runnerCwd` flows through `normalizePayload()` → `streamRunnerChat()` → backend runner-chat API.
- [ ] (Req 3) Verify: when `cwd` is `undefined`, `runnerCwd` is omitted from the payload (existing behavior preserved).

## 6. Integration validation

- [ ] (Req 1, 2, 3) Manual test: start a thread, set CWD to a subdirectory, send a message, verify tools run in that directory.
- [ ] (Req 2) Manual test: close and reopen the app, verify CWD is restored per thread.
- [ ] (Req 2) Manual test: switch between two threads with different CWDs, verify each shows its own.
- [ ] (Req 4, 5) Manual test: set `filesystem_browsing: false`, verify only workspace/home/artifacts roots appear.
- [ ] (Req 4, 5) Manual test: set `filesystem_browsing: true`, verify filesystem root appears and full tree is navigable.
- [ ] Run `go test ./...` on `or3-intern`.
- [ ] Run lint/typecheck on `or3-app`.

## Out of scope

- [ ] Do not add a settings UI for `filesystem_browsing` (config file / env var only).
- [ ] Do not add CWD display in the thread header — the composer indicator is enough.
- [ ] Do not add backend `cwd` validation at runner-chat session creation — the runner handles missing paths.
- [ ] Do not modify `CwdPickerSheet.vue` — it works as-is for directory picking.
- [ ] Do not modify any SQLite schema or add migrations.
