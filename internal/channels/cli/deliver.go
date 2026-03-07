package cli

import (
	"context"
	"fmt"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
)

type Deliverer struct{}

func (Deliverer) Name() string { return "cli" }

func (Deliverer) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	return nil
}

func (Deliverer) Stop(ctx context.Context) error {
	_ = ctx
	return nil
}

func (Deliverer) Deliver(ctx context.Context, channel, to, text string) error {
	_ = ctx
	if channel == "" { channel = "cli" }
	fmt.Printf("\n[%s] %s\n\n", channel, text)
	return nil
}

// CLIStreamWriter writes deltas directly to stdout.
type CLIStreamWriter struct {
	started bool
	closed  bool
	aborted bool
}

func (w *CLIStreamWriter) WriteDelta(ctx context.Context, text string) error {
	_ = ctx
	if w.closed || w.aborted {
		return nil
	}
	w.started = true
	fmt.Print(text)
	return nil
}

func (w *CLIStreamWriter) Close(ctx context.Context, finalText string) error {
	_ = ctx
	if w.aborted {
		return nil
	}
	w.closed = true
	if w.started {
		fmt.Println() // newline after streamed content
	} else {
		// Never streamed - print the final text now
		fmt.Printf("\n[cli] %s\n\n", finalText)
	}
	return nil
}

func (w *CLIStreamWriter) Abort(ctx context.Context) error {
	_ = ctx
	w.aborted = true
	if w.started {
		fmt.Println("\n[aborted]")
	}
	return nil
}

// BeginStream implements channels.StreamingChannel.
func (Deliverer) BeginStream(ctx context.Context, to string, meta map[string]any) (channels.StreamWriter, error) {
	_ = ctx
	_ = to
	_ = meta
	fmt.Print("\n[cli] ")
	return &CLIStreamWriter{}, nil
}
