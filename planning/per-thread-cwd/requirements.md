# Requirements

## Overview

Allow the user to set a per-thread working directory from the chat composer in `or3-app`. The directory is persisted per conversation, sent to the runner as `cwd`, and can point to any folder on the computer (within security policy). The frontend reuses the existing `CwdPickerSheet` — no new UI primitives.

Scope assumptions:

- The frontend (`or3-app`) is a Nuxt 4 / Capacitor / Electron app. The chat composer already has `CwdPickerSheet` integration (for file picking), `ChatSession.runnerCwd` field, and `AssistantSendPayload.runnerCwd` field — all unused by the composer.
- The backend (`or3-intern`) already serves file roots via `GET /internal/v1/files/roots`, lists directories via `GET /internal/v1/files/list`, and accepts `cwd` in runner-chat endpoints.
- No SQLite changes, no new API endpoints, no new UI components. All changes are additive to existing surfaces.

## 1. The chat composer must show and let the user change the current working directory

The user needs to see what directory the thread is working in and change it from the composer area.

Acceptance criteria:

- A clickable CWD indicator appears in the composer toolbar area showing the current directory path.
- Clicking it opens the existing `CwdPickerSheet` in directory-picking mode.
- When a directory is selected, the sheet closes and the CWD indicator updates immediately.
- The default for new threads is the workspace root (or `undefined`, which the backend treats as "use default").

## 2. The working directory must be persisted per thread

Each thread (conversation) must remember its own working directory. Switching threads restores that thread's CWD.

Acceptance criteria:

- `ChatSession.runnerCwd` is set when the user picks a directory in the composer.
- It persists across page reloads and app restarts (already handled by the existing localStorage persistence of `ChatSession`).
- Backend `session_meta.runner_cwd` is synced (already handled by `patchFromBackendSessionMeta` / `localSessionToMeta`).
- New threads default to no explicit CWD (runner uses its default).

## 3. The working directory must be sent with each message

The CWD must reach the runner so tool execution happens in the right directory.

Acceptance criteria:

- `AssistantSendPayload.runnerCwd` is populated from the current thread's CWD on every send.
- The execution path (`streamRunnerChat`) already resolves `cwd` as `payload.runnerCwd || session.runnerCwd` — this continues to work.
- The execution path prioritizes payload-level `runnerCwd`, so a per-message override remains possible if needed later.

## 4. The backend must support browsing the full filesystem when policy allows

The user must be able to navigate to any directory on the computer, not just pre-configured roots (workspace, home, artifacts).

Acceptance criteria:

- A new boolean config field `filesystem_browsing` (default `false`) controls whether full filesystem browsing is available.
- When `true`, `GET /internal/v1/files/roots` includes a root with `id="filesystem"`, `label="Full Filesystem"`, `path="/"`, `writable=false`.
- When `false`, the current behavior is unchanged (workspace, home, artifacts roots only).
- The `filesystem` root is read-only — no writes, uploads, or modifications through it.
- The existing `resolveServiceFilePath` path-traversal protection applies to the filesystem root (blocks `..` escape).

## 5. Security policy must be respected

If the backend restricts to workspace only, the user must not be able to browse or select directories outside it.

Acceptance criteria:

- When the backend does not return a root, the frontend cannot list directories under it (already enforced — the files API requires a valid `root_id`).
- If an admin sets `AllowedDir` and does not enable `filesystem_browsing`, only workspace/allowed roots appear.
- The frontend's `CwdPickerSheet` naturally enforces this — it can only show roots the backend returns.
- No new frontend permission logic needed. The roots API is the authority.

## Non-functional constraints

- No new SQLite tables or migrations.
- No new API endpoints. Backend changes are a single new config field and one conditional root addition in `serviceFileRoots()`.
- No new frontend components. Reuse `CwdPickerSheet` with `purpose="directory"`.
- Backward compatible: existing sessions without `runnerCwd` behave identically.
- No new state management — `useChatSessions` already handles `ChatSession` lifecycle.
- The `CwdPickerSheet` is already a slideover component with breadcrumb navigation, search, and directory listing. No need to build anything custom.
