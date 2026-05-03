# Terminal PTY Overhaul — Requirements

## Why

Today the `or3-app` "Terminal" page is unusable for any real shell work:

- **Backend** spawns the shell via `exec.CommandContext` with stdin/stdout pipes. There is no
  PTY, so `isatty(0)` returns false in the child, prompts (`$PS1`) collapse, line
  editing is dead, and TUIs (`vim`, `htop`, `top`, `less`, `nano`, `git log`) refuse to
  render or render scrambled.
- **Frontend** renders the transcript in a plain `<pre>` block. ANSI escape sequences
  appear as garbage, there's no scrollback, no zoom, no key controls.
- **Mobile** is the worst case: hardware keyboard tricks like `Ctrl+C`, `Esc`, arrow
  keys, `PgUp`/`PgDn` are impossible. Pasting requires triggering the native context
  menu inside an inert `<pre>`.

We learned from the sibling `file-portal` repo (Bun + xterm.js + WebSocket +
`node-pty`) that a usable mobile shell needs:

1. A **real PTY** end-to-end so TUIs work and `\e[…` sequences are well-formed.
2. **xterm.js** on the client to actually render the byte stream.
3. A **native input bar** for typing (paste-friendly, locale-friendly, big tap target)
   that pushes its contents to the PTY when the user hits Send.
4. A **scrollable key row** for `Ctrl`, `Tab`, `Esc`, arrow keys, `PgUp`/`PgDn`.
5. A **zoom control**: TUIs need ~10–12px on phones, ~13–14px on tablets.
6. A **Ctrl-chord palette** so users can do `Ctrl+X`, `Ctrl+L`, `Ctrl+R`, etc.

## Goals

- TUIs render correctly on iOS Safari, Android Chrome, and desktop.
- Paste works reliably (we don't rely on xterm's hidden textarea).
- The page matches the existing retro/iOS theme (cream surface, soft green accent,
  pill controls, generous touch targets).
- Existing auth, approval and rate-limit guards stay intact.
- All current tests in `cmd/or3-intern` keep passing.

## Non-goals (this iteration)

- WebSocket transport. Existing SSE+POST is fine; latency is dominated by user
  typing speed, not network. We may revisit later.
- Multi-user / shared session collaboration.
- Recording / playback of sessions.
- Theming the xterm canvas beyond a single curated palette.

## User-facing acceptance

- Open Terminal → `vim`, `htop`, `nano`, `less` all render and accept arrows / `q`.
- Tap the input bar, paste a multi-line script, hit Send → it lands in the PTY.
- Tap `Ctrl` → `C` from the chord palette → the running command is killed.
- Pinch-to-zoom is not required: `A−` / `A+` buttons step font size 10–18px and
  persist across reloads.
- The page visually matches the screenshots in the kickoff thread (sticky bottom
  toolbar, round green Send button, chips for quick commands).
