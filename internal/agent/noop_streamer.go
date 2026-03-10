package agent

import (
	"context"

	"or3-intern/internal/channels"
)

type NullStreamer struct{}

type nullStreamWriter struct{}

func (NullStreamer) BeginStream(context.Context, string, map[string]any) (channels.StreamWriter, error) {
	return nullStreamWriter{}, nil
}

func (nullStreamWriter) WriteDelta(context.Context, string) error { return nil }
func (nullStreamWriter) Close(context.Context, string) error      { return nil }
func (nullStreamWriter) Abort(context.Context) error              { return nil }
