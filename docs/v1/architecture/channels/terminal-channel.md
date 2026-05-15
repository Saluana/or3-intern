# Terminal / PTY Support

Source: `internal/channels/cli/terminal.go`

The CLI channel includes terminal-aware formatting and interactivity through the helpers in `terminal.go`. These are used by the plaintext and TUI delivery paths in the CLI channel.

## TTY Detection

The file detects terminal capabilities at init time (`terminal.go:14-16`):

```go
var stdoutIsTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
var stdinIsTTY  = isatty.IsTerminal(os.Stdin.Fd())  || isatty.IsCygwinTerminal(os.Stdin.Fd())
var isTTY       = stdoutIsTTY && colorEnabled()
```

- Uses `github.com/mattn/go-isatty`.
- `isTTY` is true only when stdout is a TTY **and** color is enabled.
- Color is disabled if `NO_COLOR` env var is set or `TERM=dumb`.

`isInteractiveTTY()` returns true only when **both** stdin and stdout are TTYs. This determines whether to use the BubbleTea TUI or plaintext mode.

## ANSI Escape Codes

Terminal styling constants defined at `terminal.go:32-44`:

| Constant | Code |
|---|---|
| `ansiReset` | `\033[0m` |
| `ansiBold` | `\033[1m` |
| `ansiDim` | `\033[2m` |
| `ansiItalic` | `\033[3m` |
| `ansiCyan` | `\033[36m` |
| `ansiYellow` | `\033[33m` |
| `ansiGray` | `\033[90m` |
| `ansiWhite` | `\033[97m` |
| `ansiGreen` | `\033[32m` |
| `ansiCursorUp` | `\033[1A` |
| `ansiClearLine` | `\033[2K` |

The `style(codes, text)` function wraps text with ANSI codes only when `isTTY` is true. In non-TTY mode, raw text is returned.

## Banner and Prompt

- **`Banner()`** — renders a startup box with "or3-intern" and "Type /exit to quit · /new for new session" when TTY, or a plain text line otherwise.
- **`Prompt()`** — returns `"❯ "` (cyan bold) when TTY, or `"> "` otherwise.
- **`ShowPrompt()`** — prints a blank line gap then the prompt to signal input readiness.

## User Message Rewriting

**`RewriteUserMessage(text)`** — moves the cursor up one line, clears it, and redraws the user's input as a styled block: `▌ username` in bold white with a cyan indicator. This transforms the raw prompt line into a clearly labeled user message. No-op when not TTY.

## Response Formatting

- **`AssistantHeader()`** — prints `◆ or3-intern` (green bold) with a dim underline before each response. Returns empty string when not TTY.
- **`ResponsePrefix()`** — returns the assistant header plus a newline and 4-space indent for streaming output.
- **`FormatResponse(text)`** — wraps a complete non-streamed response with the header and 4-space indentation per line. Returns `[cli] text` when not TTY.
- **`Separator()`** — returns a dim horizontal rule (50 dashes) after a response block. Empty when not TTY.

## Spinner Animation

`Spinner` (`terminal.go:158`) provides a braille-dot animation on stdout while the agent thinks:

- Frames: `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`
- Tick interval: 80ms
- **`Start(label)`** — begins the animation. Renders as `⠋ thinking…` (dim) on the current line. Only animates when `isTTY` is true.
- **`Stop()`** — halts the animation, clears the spinner line, blocks until the goroutine exits.
- Safe for concurrent `Start`/`Stop` calls (mutex-protected "running" flag).
- The CLI channel creates one spinner, shared between the input reader and the deliverer. The deliverer stops the spinner before printing any output.

## No PTY Channel

There is no separate PTY/terminal channel implementation in the codebase. All terminal interaction is handled through the CLI channel's TTY detection and the BubbleTea TUI, which runs on the user's local terminal. If both stdin and stdout are interactive TTYs, the full TUI launches; otherwise a simple line-reader fallback is used.
