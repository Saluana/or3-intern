# CLI Channel

Source: `internal/channels/cli/`

The CLI (command-line interface) channel is the built-in terminal chat experience. It is always active when or3-intern runs in interactive mode.

## Architecture

Two components adapt the CLI to the channel system:

1. **`cli.Channel`** (`cli.go:15`) — reads user input from stdin, publishes messages to the bus, manages the session.
2. **`cli.Service`** (`service.go:11`) — wraps the Deliverer to satisfy the `channels.Channel` interface, allowing the CLI to be registered with the channel Manager.
3. **`cli.Deliverer`** (`deliver.go:14`) — renders responses and errors to stdout, plus provides streaming and notice output.

## Inbound: Reading User Input

`Channel.Run()` in `cli.go:24` is the main entry point. It checks whether stdin and stdout are interactive TTYs:

- **Interactive TTY** → runs `runBubbleTea()` for a full-screen TUI experience.
- **Non-interactive** (pipe, redirect, script) → runs `runPlaintext()` for simple line-by-line interaction.

### Plaintext Mode (`cli.go:34`)

- Reads stdin with a `bufio.Scanner` (1MB max token size for long messages).
- Prints a banner and prompt.
- Each non-blank line becomes a `bus.EventUserMessage` event with `Channel: "cli"` and `From: "local"`.
- Blank lines are ignored.
- `/exit` ends the session.
- If the publish succeeds, the raw prompt line is restyled into a labeled user message block.
- A **spinner** starts ("thinking…") while the agent processes the message.

### BubbleTea TUI Mode (`chat_tui.go:883`)

- Uses the [BubbleTea](https://github.com/charmbracelet/bubbletea) framework for a full-screen terminal UI.
- Features: transcript viewport, scrollable message history, sidebar with tool activity, slash commands, multiple session switching, live status bar.
- User messages typed in the input box are published to the bus.
- A `bubbleChatBridge` (line 40) carries events between the agent runtime and the TUI via Go channels. The deliverer routes streaming deltas, tool calls, errors, and notices through this bridge.
- Keyboard shortcuts: `ctrl+c` quit, `ctrl+u` clear input, `ctrl+/` commands, `ctrl+s` session info, `ctrl+l` clear view.
- Slash commands: `/commands`, `/session`, `/scope`, `/clear`, `/new`, `/prune`, `/exit`.
- Transcribed runtime log output (e.g., from `log.Printf`) is captured and displayed as system notices in the chat.

## Outbound: Rendering Responses

### Full Responses (`deliver.go:29`)

`Deliverer.Deliver()` renders a complete assistant response. In TUI mode it emits a `chatAssistantCloseMsg` to the bridge. In plaintext mode it:

1. Stops the spinner.
2. Prints the formatted response with an assistant header.
3. Prints a separator line.
4. Shows a new prompt.

### Streaming (`deliver.go:118`)

`CLIStreamWriter` implements the `channels.StreamWriter` interface:

- **WriteDelta()** — on first call, stops the spinner and prints the response header. Subsequent calls print the delta text. In TUI mode, deltas go through the bridge as `chatAssistantDeltaMsg`.
- **Close()** — finalizes the stream. If nothing was streamed, prints the full `finalText` as a formatted response.
- **Abort()** — marks the stream aborted. If streaming had started, prints an `[aborted]` marker. If nothing was streamed yet, leaves the spinner running (for tool-call loops).

### Errors and Notices (`deliver.go:86-115`)

- `ShowError(err)` — stops spinner, prints a formatted error, separator, and prompt.
- `ShowNotice(text)` — same format but with `"Notice: "` prefix.
- `ShowErrorForSession(sessionKey, err)` and `ShowNoticeForSession(sessionKey, text)` — variants that route through the bridge when the TUI is active.

## Service Adapter (`service.go`)

The `cli.Service` wraps the Deliverer to conform to the `channels.Channel` interface:

- `Name()` → `"cli"`
- `Start()` / `Stop()` → no-op (the CLI has no background lifecycle).
- `Deliver()` → forwards to `Deliverer.Deliver()`, rejecting media attachments (the CLI does not support inline media display).

## Session Management

- Session key defaults to `"default"` if not set.
- The BubbleTea TUI supports switching sessions with `/session <key>`, reloading message history from the database.
- The `historyStore` interface (`chat_tui.go:34`) requires methods `GetLastMessagesScoped`, `ResolveScopeKey`, and `ListScopeSessions` to provide scoped message history.
