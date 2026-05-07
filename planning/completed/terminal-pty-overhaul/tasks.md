# Terminal PTY Overhaul — Tasks

## Backend (or3-intern)

- [x] Add `github.com/creack/pty` dependency.
- [x] Add `ptyFile *os.File` field to `serviceTerminalSession`.
- [x] Replace stdin/stdout/stderr pipe wiring in `createTerminalSession` with
      `pty.StartWithSize` and use the PTY master file as both reader and writer.
- [x] In `resizeTerminalSession`, call `pty.Setsize` on the PTY file.
- [x] In `serviceTerminalSession.close`, also close `ptyFile` if non-nil.
- [x] Verify `go test ./cmd/or3-intern -run Terminal` (lifecycle + helpers green).

## Frontend (or3-app)

- [x] Install `@xterm/xterm`, `@xterm/addon-fit`, `@xterm/addon-web-links`.
- [x] Create `composables/useTerminalPrefs.ts` (font size, persisted).
- [x] Extend `composables/useTerminalSession.ts` with `terminalChunks` +
      `sendKeys`; raw chunks routed to xterm, stripped lines kept for legacy.
- [x] `components/computer/terminal/TerminalConsole.vue`.
- [x] `components/computer/terminal/TerminalInputBar.vue`.
- [x] `components/computer/terminal/TerminalQuickCommands.vue`.
- [x] `components/computer/terminal/TerminalKeyRow.vue`.
- [x] `components/computer/terminal/TerminalCtrlPalette.vue`.
- [x] `components/computer/terminal/TerminalSurface.vue` (new orchestrator;
      replaces the old `TerminalPanel.vue`, which has been deleted).
- [x] `pages/computer/terminal.vue` rewritten to use TerminalSurface + prefs.
- [x] Typecheck on the new files: 0 errors. (Pre-existing `MarkdownEditor.vue`
      tiptap chain typing failures are out of scope.)

## Follow-ups (not in this iteration)

- [ ] WebSocket transport for sub-100ms keystroke latency.
- [ ] Snippet sheet (currently the quick commands strip covers the use case).
- [ ] Per-host persistence of font size (currently localStorage global).
