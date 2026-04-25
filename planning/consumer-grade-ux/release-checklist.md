# Consumer UX release checklist

Run this checklist before merging or shipping the simple UX surface.

## Backwards compatibility

- [ ] `or3-intern configure` still works for advanced section-based edits.
- [ ] `or3-intern doctor` still works for operator troubleshooting.
- [ ] `or3-intern capabilities` still works for posture inspection.
- [ ] `or3-intern approvals` still works for explicit approval management.
- [ ] `or3-intern devices` and `or3-intern pairing` still work as advanced tools.
- [ ] `or3-intern service`, `audit`, `secrets`, `embeddings`, and `scope` still work.

## Simple-mode validation

- [ ] `or3-intern help` shows the short simple-command list.
- [ ] `or3-intern --advanced --help` shows the full operator command list.
- [ ] `or3-intern setup` completes and writes config successfully.
- [ ] `or3-intern status` prints a plain-language summary.
- [ ] `or3-intern settings` reopens the current settings flow.
- [ ] `or3-intern connect-device` creates and displays a pairing code.

## Verification

- [ ] Focused Go tests for the consumer UX surface pass.
- [ ] `go test ./cmd/or3-intern/...` passes.
- [ ] `go build ./...` passes.
- [ ] Beginner docs and CLI reference match the implemented command names and output model.