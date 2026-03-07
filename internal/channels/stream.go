package channels

import "context"

// StreamWriter is an optional interface for channels that can receive
// incremental text deltas (e.g., CLI live output, editable messages).
// Channels that do not implement streaming use final-only delivery.
type StreamWriter interface {
	// WriteDelta appends a text delta to the in-progress response.
	WriteDelta(ctx context.Context, text string) error
	// Close finalizes the stream with the complete text.
	Close(ctx context.Context, finalText string) error
	// Abort cancels the stream cleanly without leaving partial output.
	Abort(ctx context.Context) error
}

// StreamingChannel is an optional interface a channel can implement
// to indicate it supports incremental streaming delivery.
type StreamingChannel interface {
	// BeginStream starts a new streaming response to the given recipient.
	// meta contains channel-specific metadata (e.g., chat_id).
	// Returns a StreamWriter to write deltas, or an error.
	BeginStream(ctx context.Context, to string, meta map[string]any) (StreamWriter, error)
}
