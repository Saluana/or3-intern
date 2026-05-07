# Terminal PTY Overhaul — Design

## Backend (`or3-intern`)

### Library choice

Use [`github.com/creack/pty`](https://github.com/creack/pty) (the canonical Go
PTY wrapper). Pure Go, no cgo, ships with `Start`, `StartWithSize`, `Setsize`,
`Getsize`. Works on macOS, Linux, and Windows (ConPTY) — matches our build matrix.

### Session lifecycle

`serviceTerminalSession` keeps the same identity and approval/auth surface; only
its **transport** changes:

```
┌────────────────────────────────────────────────────────────────────┐
│ POST /internal/v1/terminal/sessions      → allocate id, evaluate   │
│                                            approval, start shell    │
│                                            via pty.StartWithSize    │
│                                            store ptyFile (rw)       │
│ GET  /internal/v1/terminal/sessions/:id  → snapshot                │
│ GET  …/stream  (SSE)                     → push raw PTY bytes      │
│                                            as {"chunk": "<utf8>"}  │
│ POST …/input                             → write JSON.input bytes  │
│                                            to ptyFile               │
│ POST …/resize                            → pty.Setsize(rows, cols) │
│ POST …/close                             → close ptyFile + cancel  │
└────────────────────────────────────────────────────────────────────┘
```

Key changes in `cmd/or3-intern/service.go`:

1. Replace the `cmd.StdinPipe / StdoutPipe / StderrPipe` triple with one
   `*os.File` returned by `pty.StartWithSize(cmd, &pty.Winsize{Rows, Cols})`.
2. The session struct keeps `stdin io.WriteCloser` (for test compatibility) but
   it is now backed by the PTY file. We add `ptyFile *os.File` so resize can
   call `pty.Setsize(s.ptyFile, …)`.
3. `collectTerminalOutput` keeps its current signature and behaviour (one
   `output` event per read). It now reads from the single PTY master.
4. New helper `resizePTY(rows, cols)` invoked from `resizeTerminalSession`.
5. On `Wait()` the PTY file is closed; `close(status)` ensures both sides drop.

### Wire format

Output chunks are sent as `{"stream":"pty","chunk":"…"}` (we drop the artificial
stdout/stderr split — a PTY merges them anyway). The chunk is a JSON-encoded
string holding the raw bytes interpreted as UTF-8 (Go's `string(buf[:n])`); ANSI
escapes are pure ASCII so this is safe. Invalid UTF-8 is replaced with U+FFFD by
`json.Marshal`, which is acceptable for a TTY stream.

### Approval / auth

Unchanged. The existing rollout map continues to mark `POST sessions`,
`POST input` and `POST resize` as sensitive (step-up required), and `GET stream`
as low-risk session-only.

### Tests

- The lifecycle test still sends `printf 'hello from test\n'\nexit\n` and looks
  for the substring `hello from test`. With a PTY, the shell echoes input and
  prints the output, so the substring still appears.
- `TestWriteTerminalInputAcceptsNewlineOnlyInput` constructs a session with a
  fake `stdin io.WriteCloser`. We keep the field, so the test compiles and
  passes unchanged.
- Add a smoke test that asserts `resizeTerminalSession` updates rows/cols and
  emits a `resize` event (existing — keep).

---

## Frontend (`or3-app`)

### Dependencies

```
@xterm/xterm
@xterm/addon-fit
@xterm/addon-web-links
```

All three are dynamically imported inside `onMounted` so they never end up in
the SSR or static-prerender bundle (the page is client-only anyway, since it
needs a live host).

### Component layout

```
pages/computer/terminal.vue
└── SurfaceCard (root config)
└── TerminalConsole.vue              ← xterm canvas + status badge
    ├── TerminalQuickCommands.vue    ← horizontal chips (ls -la, cd .., …)
    ├── TerminalInputBar.vue         ← UTextarea + Paste / Clear / Send
    ├── TerminalKeyRow.vue           ← Ctrl, Tab, Esc, ←/→/↑/↓, PgUp/PgDn, Snip
    └── TerminalCtrlPalette.vue      ← bottom sheet w/ scrollable Ctrl combos
```

### TerminalConsole

Owns the xterm instance. Responsibilities:

- Mount xterm into a `<div>` with `FitAddon`.
- Subscribe to `terminalChunks` (a new ref of `string[]`) and `term.write` each
  chunk. We keep the `terminalLines` ref for legacy consumers but route raw
  bytes through `terminalChunks`.
- ResizeObserver → `fitAddon.fit()` → emit `resize(rows, cols)` to composable.
- Apply theme:
  ```
  background: #1c1f1a
  foreground: #d8e6c8
  cursor:     #6fc88a
  selection:  rgba(63,143,88,0.35)
  ```
  Font: `'JetBrains Mono', 'IBM Plex Mono', ui-monospace, monospace`.
- Expose `focus()` and `clear()`. Default font size from `useTerminalPrefs`
  composable (persisted in `localStorage` under `or3:terminal:prefs`).

### TerminalInputBar

- `UTextarea` (autoresize) with placeholder "Type a command, then tap Send."
- "Paste" button: `navigator.clipboard.readText()` → append.
- "Clear" button: empties the textarea.
- "Send" button: emits `send(value + '\n')` and clears. Cmd/Ctrl+Enter also
  sends without newline trim. The send button is the round green pill from the
  screenshot (`UButton color=primary class="rounded-full size-12"`).

### TerminalKeyRow

Single horizontal row, scrolls horizontally on overflow. Buttons:

| Label | Bytes sent          |
| ----- | ------------------- |
| Ctrl  | toggles palette     |
| Tab   | `\t`                |
| Esc   | `\x1b`              |
| ←     | `\x1b[D`            |
| →     | `\x1b[C`            |
| ↑     | `\x1b[A`            |
| ↓     | `\x1b[B`            |
| PgUp  | `\x1b[5~`           |
| PgDn  | `\x1b[6~`           |
| A−    | font size −1        |
| A+    | font size +1        |
| Snip  | open snippet sheet  |

Each button is `or3-touch-target`, square-ish (44×44), uses pixelarticons icon
where appropriate.

### TerminalCtrlPalette

Bottom sheet (uses existing `USlideover` or a simple absolute-positioned panel)
with a horizontally scrollable strip of `Ctrl+<key>` chips for `A`–`Z`,
`Space`, `[`, `]`, `\\`, `/`. Tapping a chip sends the corresponding control
character (`String.fromCharCode(key.toUpperCase().charCodeAt(0) & 0x1f)`) and
closes the palette. A second row offers `Ctrl+Shift+<key>` for the common ones
(`P`, `T`, `R`).

### Composable: `useTerminalSession`

Adds:

```ts
const terminalChunks = ref<string[]>([])     // raw PTY chunks (kept ≤ 2_000)
const fontSize = ref<number>(loadFontSize()) // 10..18
function sendKeys(bytes: string)             // unbuffered, no '\n'
function setFontSize(value: number)
async function resize(rows: number, cols: number)  // existing → already wired
```

`attach()` switches the `output` event handler to **always** push to
`terminalChunks` (and continues to push a stripped line into `terminalLines` so
the Approvals page that quickly previews a transcript still works).

Send paths:

- `sendInput(text)` — used by the input bar, includes trailing `\n`.
- `sendKeys(bytes)` — used by the key row + Ctrl palette + xterm `onData`.

Both hit `POST …/input`. Future: switch to WebSocket for sub-100ms keystroke
latency.

---

## Theming

- The xterm container sits inside a `SurfaceCard` with the dark inner panel
  (`bg-stone-950` keep), but with `border-radius` matching `--or3-radius-card`
  and an inner ring `1px var(--or3-border)`.
- The bottom toolbar uses the same `--or3-surface` cream background so the page
  reads as one cohesive iOS card, with the dark terminal "screen" floating in
  the middle. This matches the screenshots.
- Round Send button: 44×44 (mobile-touch), `bg-[var(--or3-green)]` with white
  pixel icon. Icon: `i-pixelarticons-arrow-up`.
- Status pill in the header keeps the existing `StatusPill` component.

## Test strategy

- Unit (Go): keep current PTY/lifecycle/test compat.
- Smoke (Vue): typecheck only — heavy DOM tests are out of scope; xterm is
  notoriously hard to test in `happy-dom`.
- Manual: matrix in `requirements.md` § "User-facing acceptance".
