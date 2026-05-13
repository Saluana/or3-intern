# Streaming Output

Runner output is streamed line-by-line from the subprocess and emitted as typed events. The streaming logic is in `internal/agentcli/stream.go`.

## Two Streaming Modes

### Plain Mode

Used for stderr and for stdout when the runner does not produce structured output (OutputPlain).

The `readStream` function (`internal/agentcli/stream.go:62-106`) reads lines with `bufio.NewReader.ReadSlice('\n')`. Each complete line is emitted as an `"output"` event. If a line exceeds the buffer size, it is split into chunks.

### Structured Mode

Used for stdout when the runner produces JSON or JSONL output (`OutputJSON` or `OutputJSONL`).

The `readStructuredStream` function (`internal/agentcli/stream.go:108-151`) tries to decode each line as JSON. Successfully decoded JSON payloads are emitted as `"structured"` events. Lines that cannot be decoded fall back to `"output"` events.

When a line contains multiple JSON objects (concatenated, as some CLIs do), the decoder consumes as many as it can. Any unconsumed tail is kept in a buffer and prepended to the next read.

## Event Types

Events are `AgentRunEvent` values (`internal/agentcli/runners.go:169-183`):

| Type | Description |
|------|-------------|
| `"output"` | Raw stdout/stderr chunk |
| `"structured"` | Parsed JSON payload |
| `"error"` | Stream or process error |
| `"started"` | Run started (with argv preview) |
| `"completion"` | Run finished (with exit code, duration, final text) |

Each event has a monotonic sequence number (`Seq`), a timestamp (`TS`), and identifies its stream as `"stdout"` or `"stderr"`.

## Event Chunking

Large output lines are split into chunks of `ChunkMaxBytes` (default 16384 bytes). The `splitChunks` function (`internal/agentcli/stream.go:270-286`) breaks data at chunk boundaries so no single event exceeds the max size.

## Output Collection

An `outputCollector` (`internal/agentcli/stream.go:16-28`) uses ring buffers to store stdout and stderr previews. Ring buffers (`internal/agentcli/stream.go:30-60`) keep the most recent bytes up to `PreviewMaxBytes`. This ensures memory use is bounded even for long-running processes.

The collector also holds a `finalTextExtractor` that watches structured events to find the most relevant final text (see result extraction docs).

## Structured Payload Buffer

If JSON decoding fails partway through a line, the pending buffer can grow. If it exceeds `maxStructuredBufferBytes` (65536 bytes), the buffered content is flushed as raw output chunks to prevent unbounded memory growth (`internal/agentcli/stream.go:124-127`).
