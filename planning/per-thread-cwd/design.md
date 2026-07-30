# Design

## Overview

The entire feature is ~3 frontend changes + 1 backend change. Every piece already exists:

- `ChatSession.runnerCwd` — field exists, persisted, synced with backend
- `AssistantSendPayload.runnerCwd` — field exists, used at execution time
- `CwdPickerSheet.vue` — component exists with `purpose="directory"` mode, used by `AgentCommandCenter` for the same purpose
- `serviceFileRoots()` — backend returns file roots; listing is constrained to roots
- `streamRunnerChat()` — already resolves `cwd` as `payload.runnerCwd || session.runnerCwd`

The plan: wire these existing pieces together. No new architecture.

## Affected areas

### `or3-intern` (backend)

**`internal/config/config.go`**
- Add `FilesystemBrowsing bool` config field (default `false`)

**`cmd/or3-intern/service_files.go`**
- In `serviceFileRoots()`: if `config.FilesystemBrowsing` is true, append root `{id:"filesystem", label:"Full Filesystem", path:"/", writable:false}`

That's the full backend change. ~5 lines of Go.

### `or3-app` (frontend)

**`app/composables/useThreadCwd.ts`** (new)
- Thin composable that reads/writes `runnerCwd` on the active `ChatSession`
- Exposes `cwd: ComputedRef<string | undefined>`, `setCwd(path)` that patches the session

**`app/components/assistant/AssistantComposer.vue`**
- Add a CWD indicator chip in the composer toolbar (next to existing runner/metadata chips)
- Clicking it opens `CwdPickerSheet` with `purpose="directory"`
- On `select` event, call `setCwd(path)` and close the sheet
- In the `send` payload builder (around line 1290), include `runnerCwd: cwd.value`

**`app/components/agents/CwdPickerSheet.vue`**
- No changes needed. Already supports `purpose="directory"`. Already emits `select` with the full directory path.

## Control flow

User opens thread → thread has `runnerCwd` (from localStorage) → composer shows CWD indicator
User clicks indicator → `CwdPickerSheet` opens → user browses roots → selects directory
  → `setCwd(path)` → `ChatSession.runnerCwd = path` → localStorage persist (debounced, existing)
User types message, hits send → payload includes `runnerCwd: session.runnerCwd`
  → `streamRunnerChat()` → `cwd = payload.runnerCwd || session.runnerCwd` → sent to runner-chat API
User switches thread → `ChatSession.runnerCwd` is whatever that thread has → CWD indicator updates

## Data and persistence

- **No SQLite changes.** `runnerCwd` is already on `ChatSession`. The session is already persisted to localStorage and synced with the backend's `session_meta.runner_cwd`.
- **No config file changes** for the existing behavior. The new `filesystem_browsing` config field defaults to `false`.

## Interfaces and types

### New backend config field

```go
// internal/config/config.go (existing Config struct)
FilesystemBrowsing bool `json:"filesystem_browsing"` // default false
```

### New backend root

```go
// cmd/or3-intern/service_files.go, in serviceFileRoots()
if s.config.FilesystemBrowsing {
    add("filesystem", "Full Filesystem", "/", false)
}
```

### New frontend composable

```typescript
// composables/useThreadCwd.ts
export function useThreadCwd() {
  const { activeSession, patchSession } = useChatSessions()
  const cwd = computed(() => activeSession.value?.runnerCwd)
  function setCwd(path: string) {
    patchSession({ runnerCwd: path })
  }
  return { cwd, setCwd }
}
```

### Composer payload change

```typescript
// AssistantComposer.vue, around line 1290
const payload: AssistantSendPayload = {
  text: visiblePayloadText(),
  transportText: buildTransportTextForAttachments(stagedWorkspaceAttachments),
  attachments,
  runnerCwd: cwd.value,  // ← this line added
  // ... rest unchanged
}
```

## UI placement

```
┌─────────────────────────────────────────────────┐
│       [Ask ▼]  [opencode ▼]  [claude ▼]  📁 cwd │  ← composer toolbar
│                                                 │
│  ┌─────────────────────────────────────────┐    │
│  │  Type a message...                      │    │
│  └─────────────────────────────────────────┘    │
│                                                 │
│  [📎]  [@ Mention file]                        │
└─────────────────────────────────────────────────┘
```

The CWD chip sits in the existing metadata toolbar alongside runner selection. It shows:
- Folder icon + abbreviated path when set (e.g., `📁 ~/projects/or3`)
- "📁 Workspace root" when unset (or just the workspace root path)
- Truncated with ellipsis when path is long

`CwdPickerSheet` opens as a slideover (already its default rendering). The sheet shows file roots at the top level, then directory listing when a root is selected. User navigates to the desired directory and confirms.

## Failure modes and safeguards

| Scenario | Behavior |
|----------|----------|
| CWD directory was deleted | Runner fails gracefully on first tool execution; error message appears in chat |
| `filesystem_browsing` disabled | Filesystem root not returned; user can only browse workspace/home/artifacts |
| User types an invalid path in the picker | Picker rejects it (no matching root); user stays on root selection |
| Runner does not support custom CWD | `cwd` field is ignored by the runner; no harm done |
| Backend not reachable | File roots fail to load; picker shows error state (existing `fileError` in `useComputerFiles`) |
| Session is forked | `runnerCwd` is inherited from parent session (existing fork behavior copies session fields) |
| Cross-tab session switch | `localStorage` sync handles it; both tabs see the same `ChatSession` data |

## Testing strategy

### Backend (`or3-intern`)

- `service_files_test.go`: verify that `serviceFileRoots()` includes the filesystem root when `FilesystemBrowsing` is `true`, and excludes it when `false`.
- `service_test.go`: add a minimal integration test for `GET /internal/v1/files/roots` with the new config.

### Frontend (`or3-app`)

- `useThreadCwd.test.ts`: verify `setCwd` patches the session, `cwd` reflects the active session's `runnerCwd`.
- `AssistantComposer.test.ts`: verify `runnerCwd` is included in the send payload when set, and absent when not set.
- Manual/E2E: Open thread, change CWD, send message, verify the runner receives the correct `cwd`.

## Out of scope

- A settings UI for `filesystem_browsing` (it's a config file / env var setting for now)
- Showing CWD in the thread header or message list (composer indicator is sufficient for MVP)
- Server-side validation of `cwd` against allowed roots at runner-chat session creation (the runner will fail gracefully if the path doesn't exist; we can add this later if needed)
- Multi-root directory navigation (you must pick a root first, then navigate within it — existing behavior)
