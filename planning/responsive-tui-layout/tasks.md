# 1. Add responsive layout helpers to configure TUI

- [ ] [Req 1, Req 3] Update `cmd/or3-intern/configure_tui.go` to derive a small layout mode from `width` and `height` instead of assuming a fixed two-column layout for every screen.
- [ ] [Req 1, Req 4] Refactor the section picker and channel picker rendering in `cmd/or3-intern/configure_tui.go` so narrow terminals use a stacked single-column layout with the list above the snapshot panel.
- [ ] [Req 1, Req 4] Refactor `renderFormScreen` in `cmd/or3-intern/configure_tui.go` so narrow terminals stack the field list above the summary/editor panel and compact terminals shorten hint/snapshot content.
- [ ] [Req 1, Req 3] Adjust form row sizing and cursor visibility calculations in `cmd/or3-intern/configure_tui.go` so short terminals still keep the selected field and editing affordances visible.
- [ ] [Req 1, Req 4] Update review, success, and quit-confirm screens in `cmd/or3-intern/configure_tui.go` to render full-width single-column panels when side-by-side rendering would clip content.

# 2. Make chat layout adaptive

- [ ] [Req 2, Req 3] Update `internal/channels/cli/chat_tui.go` to derive a chat layout mode from terminal dimensions and stop assuming the transcript/sidebar split is always horizontal.
- [ ] [Req 2, Req 4] Refactor `chatModel.View()` in `internal/channels/cli/chat_tui.go` to render a stacked transcript/status/activity layout on narrow terminals.
- [ ] [Req 2, Req 4] Refactor `resize()` in `internal/channels/cli/chat_tui.go` so transcript height is prioritized and sidebar/status content becomes compact before the viewport becomes too small.
- [ ] [Req 2, Req 4] Add a compact sidebar/header rendering path in `internal/channels/cli/chat_tui.go` that preserves session/status/activity context with fewer lines on small screens.
- [ ] [Req 2, Req 3] Confirm local command output, streaming updates, and scroll behavior still call the existing viewport refresh path without introducing new state machines.

# 3. Add focused regression coverage

- [ ] [Req 5] Extend `cmd/or3-intern/configure_tui_test.go` with a narrow-width regression test that exercises a stacked configure layout.
- [ ] [Req 5] Extend `cmd/or3-intern/configure_tui_test.go` with a short-height regression test that verifies selection/editing visibility remains intact.
- [ ] [Req 5] Extend `internal/channels/cli/chat_tui_test.go` with a narrow-width regression test that verifies transcript, input, and compact status rendering remain visible.
- [ ] [Req 5] Extend `internal/channels/cli/chat_tui_test.go` with a short-height or post-resize regression test that verifies transcript state survives layout mode changes.

# 4. Validate implementation with existing repo workflows

- [ ] [Req 5] Run focused Go tests for `cmd/or3-intern` configure TUI coverage.
- [ ] [Req 5] Run focused Go tests for `internal/channels/cli` chat TUI coverage.
- [ ] [Req 1, Req 2, Req 3, Req 5] Run the existing workspace build task after the responsive changes to catch compile regressions.

# 5. Out of scope

- [ ] Do not redesign configure navigation, add mouse-only layout interactions, or introduce a new TUI framework.
- [ ] Do not add new config options for terminal breakpoints or responsive tuning in this pass.
- [ ] Do not change chat session semantics, message persistence, channel routing, or tool execution behavior.
- [ ] Do not introduce frontend/web UI work, REST APIs, or non-terminal rendering paths.
