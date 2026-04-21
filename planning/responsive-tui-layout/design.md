# Overview

This design makes the existing Bubble Tea interfaces responsive by tightening their resize logic and introducing a small number of explicit layout modes based on terminal width and height. It fits the current architecture because both TUIs already react to `tea.WindowSizeMsg`, compute widths/heights locally, and render with Lip Gloss panels and joins.

The core idea is simple:

1. classify the terminal into a few size bands,
2. compute panel dimensions from those bands,
3. render either side-by-side or stacked layouts,
4. compact secondary content before sacrificing the primary interaction surface.

This keeps the implementation bounded, testable, and aligned with the existing repo style.

# Affected areas

- `cmd/or3-intern/configure_tui.go`
  - Add adaptive layout helpers for section picker, channel picker, form, review, and success screens.
  - Reduce reliance on fixed-width side-by-side panels when the terminal is narrow.
  - Keep cursor visibility and editing affordances stable during height changes.
- `cmd/or3-intern/configure_tui_test.go`
  - Add regression coverage for narrow-width and short-height rendering.
- `internal/channels/cli/chat_tui.go`
  - Replace the current always-horizontal transcript/sidebar split with an adaptive layout mode.
  - Resize viewport, sidebar, input, and status panels proportionally for small terminals.
  - Add compact sidebar rendering for narrow screens.
- `internal/channels/cli/chat_tui_test.go`
  - Add regression coverage for stacked or compact chat rendering and resize stability.

No changes are required in:

- `internal/config`
- `internal/db`
- `internal/memory`
- session persistence or channel routing

# Control flow / architecture

## Shared responsive pattern

Both TUIs already receive `tea.WindowSizeMsg`. The new behavior should keep that event as the single source of truth for layout.

Recommended pattern:

1. On `tea.WindowSizeMsg`, store `width` and `height`.
2. Derive a small layout mode from those dimensions, for example:
   - `wide`: horizontal split is safe
   - `narrow`: stack major panels vertically
   - `short`: tighten visible rows and compact summaries
   - `compact`: both narrow and short, prioritize only core content
3. Recompute component sizes from the mode.
4. Render with `lipgloss.JoinHorizontal` in `wide` mode and `lipgloss.JoinVertical` in `narrow`/`compact` modes.

This matches Bubble Tea’s documented resize model and Lip Gloss’s intended composition model.

## Configure TUI flow

The configure TUI currently assumes a two-panel layout for the section picker and form screens. The main changes are:

- introduce helper functions such as `configureLayout(width, height)` and `configurePanelWidths(layout)`;
- compute visible form rows from available height after header/help/footer space is reserved;
- render the field list and summary/editor side-by-side only when there is enough width;
- in narrow mode, place the field list above the summary/editor panel;
- in compact mode, render a shorter snapshot/hint block and prioritize the selected field plus editor;
- preserve `fieldCursor`, `formScroll`, and editing state across mode switches.

Potential helper shape:

```go
type configureLayoutMode struct {
    stacked       bool
    compact       bool
    listWidth     int
    detailWidth   int
    listHeight    int
    detailHeight  int
    inputWidth    int
}
```

This does not need to be shared outside `configure_tui.go`.

## Chat TUI flow

The chat TUI currently hardcodes a transcript/sidebar split and computes viewport width from a minimum sidebar width. The new flow should:

- add a small layout helper such as `chatLayout(width, height)`;
- keep a horizontal transcript + sidebar split only when the terminal is wide enough;
- switch to a vertical layout for transcript first, then compact sidebar/status below, on narrow terminals;
- reserve a minimum meaningful viewport height before allocating extra space to the sidebar;
- reduce header/status verbosity in compact mode rather than shrinking the transcript below usability.

Potential helper shape:

```go
type chatLayoutMode struct {
    stacked         bool
    compactSidebar  bool
    transcriptWidth int
    sidebarWidth    int
    viewportHeight  int
    inputWidth      int
}
```

The existing `resize()` and `View()` paths remain the right integration points.

## Compact content strategy

To keep behavior simple, compact mode should reduce information density rather than invent new navigation.

Examples:

- configure summary panel shows fewer lines and shorter hints;
- configure review/success screens use full-width single-column rendering;
- chat sidebar collapses to status + a few recent activity lines;
- chat header subtitle can shorten when width is limited;
- help/footer remains present, but overly long badges or labels should wrap cleanly.

# Data and persistence

No SQLite, migration, config, env, or session changes are required.

- No new database tables or indexes.
- No new config keys or env overrides.
- No changes to stored session keys, chat history, or memory retrieval.
- No new persistent UI state beyond existing model fields.

# Interfaces and types

Keep interfaces local to the two TUI files. The change likely only needs new private helpers and possibly small private structs.

Likely additions in `cmd/or3-intern/configure_tui.go`:

```go
type configureLayout struct {
    stacked      bool
    compact      bool
    navigationW  int
    detailW      int
    fieldRows    int
    fullWidth    int
}

func deriveConfigureLayout(width, height int) configureLayout
func renderConfigureColumns(m configureTUIModel, layout configureLayout) string
func renderConfigureStack(m configureTUIModel, layout configureLayout) string
```

Likely additions in `internal/channels/cli/chat_tui.go`:

```go
type chatLayout struct {
    stacked         bool
    compactSidebar  bool
    transcriptW     int
    sidebarW        int
    viewportH       int
    panelW          int
}

func deriveChatLayout(width, height int) chatLayout
func (m chatModel) renderWideChat(layout chatLayout) string
func (m chatModel) renderStackedChat(layout chatLayout) string
func (m *chatModel) renderSidebarCompact(width int) string
```

These helpers should stay private and avoid changing public behavior outside rendering.

# Failure modes and safeguards

- **Terminal too small to satisfy prior minimum widths**
  - Clamp widths and heights to practical minimums and switch to stacked rendering instead of forcing side-by-side overflow.
- **Resize causes cursor or scroll drift**
  - Reuse the existing `ensureFieldCursorVisible` logic in configure and preserve viewport content/position in chat after recalculating sizes.
- **Editing field disappears in configure**
  - In stacked/compact mode, prioritize rendering the active field and editor area over the large snapshot panel.
- **Transcript becomes unusably small in chat**
  - Allocate transcript height first and collapse sidebar detail before reducing viewport height further.
- **Wrapped text creates visually noisy panels**
  - Keep compact summaries intentionally short and use existing Lip Gloss width controls so wrapping is predictable.
- **Regression in large terminal layout**
  - Keep the current wide-screen composition as one explicit mode and add tests that still assert key wide-screen elements remain present.

# Testing strategy

Use focused Go tests around the current view-rendering behavior.

- **Configure TUI tests** in `cmd/or3-intern/configure_tui_test.go`
  - narrow width test for stacked section/form layout,
  - short height test for reduced visible field count and preserved selection,
  - editing-state test proving the selected field editor remains visible in compact mode,
  - review/success rendering test for full-width single-column fallback.

- **Chat TUI tests** in `internal/channels/cli/chat_tui_test.go`
  - narrow width test proving the view still contains transcript and compact status/activity without depending on a wide sidebar,
  - short height test proving viewport/input/status remain present,
  - resize-after-stream/update test proving content survives layout mode changes,
  - compact sidebar test verifying low-priority activity is summarized rather than clipped.

- **Validation approach**
  - Prefer direct `Update(tea.WindowSizeMsg{...})` plus `View()` assertions.
  - Keep tests deterministic by asserting specific visible markers rather than pixel-like layout exactness.
  - After implementation, run focused tests for the two TUI packages and then build the workspace with the existing Go build task.
