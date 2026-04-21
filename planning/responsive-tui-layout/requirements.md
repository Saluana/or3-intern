# Overview

This plan improves the Bubble Tea terminal layouts so `or3-intern configure` and the full-screen chat UI remain readable and usable in smaller terminal windows. The work stays inside the existing Go CLI/TUI architecture and focuses on adaptive sizing, stacking, truncation, and compact summaries rather than redesigning the flows.

Scope assumptions:

- The change targets the current full-screen Bubble Tea screens in `cmd/or3-intern/configure_tui.go` and `internal/channels/cli/chat_tui.go`.
- The plain-text configure flow in `cmd/or3-intern/configure.go` remains unchanged except where shared helpers are needed.
- No config, SQLite schema, session model, or runtime/tool safety semantics change as part of this work.
- The goal is graceful degradation for narrow or short terminals, not pixel-perfect parity with the largest layout.

# Requirements

## 1. Configure TUI must adapt to narrow and short terminals

The configure interface must remain usable when the terminal is substantially smaller than a full-screen desktop window.

Acceptance criteria:

- The section picker, channel picker, form screen, review screen, and success screen continue rendering without major clipping when the terminal is resized smaller.
- On narrow widths, the configure layout switches from side-by-side panels to a vertically stacked layout or another compact arrangement that preserves the currently selected content.
- On short heights, the form view reduces visible rows predictably, preserves cursor visibility, and keeps editing affordances visible.
- The selected field summary and editing input remain visible even when the right-side summary cannot fit at full width.
- The UI continues to respond to `tea.WindowSizeMsg` updates during live resizing without corrupting scroll state or field selection.

## 2. Chat TUI must remain readable and usable on smaller screens

The chat interface must keep the transcript, input, and core status information accessible in reduced terminal sizes.

Acceptance criteria:

- The chat view continues rendering without major truncation when the terminal becomes narrow or short.
- On narrow widths, the transcript and sidebar no longer require a fixed horizontal split; the layout switches to a stacked or compact mode that prioritizes transcript and input usability.
- On short heights, the viewport, input, header, and status areas are resized so the transcript still has meaningful space and the input remains visible.
- Recent activity and session metadata remain available in some form, but lower-priority content can be condensed when space is limited.
- Resizing while streaming, scrolling, or after local commands continues to preserve transcript state and bottom-stick behavior.

## 3. Responsive layout behavior must stay deterministic and low-complexity

The new layout behavior must fit the repo’s current Bubble Tea model style without introducing a separate layout framework or excessive state.

Acceptance criteria:

- Layout decisions are derived from the current terminal width and height plus existing model state.
- Breakpoints and size calculations are implemented as small local helpers in the current TUI packages.
- The code continues to use existing Bubble Tea/Lip Gloss primitives such as `WindowSizeMsg`, viewport sizing, and horizontal/vertical joins.
- The implementation does not add new services, background goroutines, or third-party layout frameworks.
- The implementation keeps rendering logic understandable enough to test directly from Go view output.

## 4. Lower-priority content must degrade gracefully

When the terminal is too small to show the full “large screen” experience, the UI must reduce non-essential detail before sacrificing primary actions.

Acceptance criteria:

- In configure, summaries, hints, and section snapshots can switch to compact text or fewer lines before field navigation/editing is compromised.
- In chat, the sidebar can collapse into a compact status/activity summary before transcript width or input usability falls below the current minimum practical size.
- Truncation or wrapping behavior remains intentional and readable; important labels and controls are still recognizable.
- Placeholder text, help text, and badges do not cause the layout to overflow horizontally on common small terminal sizes.

## 5. Responsive behavior must be covered by focused regression tests

The change must include automated tests for the major small-screen cases in the current Go test suite.

Acceptance criteria:

- `cmd/or3-intern/configure_tui_test.go` gains focused coverage for narrow and short terminal rendering behavior.
- `internal/channels/cli/chat_tui_test.go` gains focused coverage for narrow and short terminal rendering behavior.
- Tests verify concrete view characteristics such as stacked layout markers, preserved selected-field context, compact status rendering, or absence of obviously clipped critical sections.
- Existing behavior for standard terminal sizes remains covered by current tests and is not regressed by the new responsive logic.

# Non-functional constraints

- Keep the implementation inside the existing Bubble Tea models and helpers in `cmd/or3-intern` and `internal/channels/cli`.
- Preserve deterministic rendering and bounded viewport/list sizes during resize handling.
- Avoid materially increasing memory use or adding new persistent state.
- Do not change session routing, chat history loading, channel behavior, tool safety, approvals, or config compatibility.
- Prefer small, readable layout helpers over generalized layout abstractions.