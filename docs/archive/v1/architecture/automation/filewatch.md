# File Watch Triggers

The file watcher polls specified files for changes and publishes events when files are modified.

## Configuration

- `enabled` - whether file watching is active
- `paths` - list of file paths to watch
- `pollSeconds` - polling interval in seconds (default: 5)
- `debounceSeconds` - minimum time between events for the same file (default: 2)

Source: `internal/triggers/filewatch.go:18-26` (FileWatcher struct)

## Polling loop

The watcher runs in a goroutine with a ticker at the configured poll interval. On each tick, it checks all configured paths.

Source: `internal/triggers/filewatch.go:61-72` (loop)

## Change detection

For each path:
1. Resolve to absolute path
2. Skip symlinks (uses `os.Lstat`, not `os.Stat`)
3. Compare current modification time and size against the last known state
4. First observation records a baseline without publishing
5. Subsequent changes publish events

Source: `internal/triggers/filewatch.go:74-152` (poll)

## Debouncing

If a file changed recently (within `debounceSeconds`), the state is updated but no event is published. This prevents rapid-fire events when a file is being written.

Source: `internal/triggers/filewatch.go:107-111`

## Published events

Each file change event includes:
- Event type: `EventFileChange`
- Channel: "filewatch"
- Source: the absolute file path
- Message: "file changed: <path>"
- Metadata: path, size, modification time, StructuredEvent (trusted=true)

Source: `internal/triggers/filewatch.go:121-148`

## Structured tasks from files

If the changed file is small (<= 12KB), the watcher reads its content and tries to parse structured tasks. If successful, the tasks are included in the event metadata.

Source: `internal/triggers/filewatch.go:133-138`

## Lifecycle

- `Start()` begins polling (only if enabled and paths configured)
- `Stop()` cancels the context, stopping the polling loop

Source: `internal/triggers/filewatch.go:43-59`
