package cli

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
)

// Deliverer handles final and streaming output to the CLI terminal.
type Deliverer struct {
	Spinner *Spinner // shared with Channel; stopped before any output
}

func (Deliverer) Name() string { return "cli" }

func (Deliverer) Start(ctx context.Context, eventBus *bus.Bus) error { return nil }

func (Deliverer) Stop(ctx context.Context) error { return nil }

func (d Deliverer) Deliver(ctx context.Context, channel, to, text string) error {
	d.stopSpinner()
	fmt.Print(FormatResponse(text))
	fmt.Println()
	fmt.Println()
	if sep := Separator(); sep != "" {
		fmt.Println(sep)
	}
	ShowPrompt()
	return nil
}

func (d Deliverer) stopSpinner() {
	if d.Spinner != nil {
		d.Spinner.Stop()
	}
}

// ──────────────────────── streaming ────────────────────────

// CLIStreamWriter renders incremental text deltas to stdout with styling.
type CLIStreamWriter struct {
	started bool
	closed  bool
	aborted bool
	spinner *Spinner
}

func (w *CLIStreamWriter) WriteDelta(ctx context.Context, text string) error {
	if w.closed || w.aborted {
		return nil
	}
	if !w.started {
		// Stop the spinner and print the response header on the first delta.
		if w.spinner != nil {
			w.spinner.Stop()
		}
		w.started = true
		fmt.Print(ResponsePrefix())
	}
	// Indent any embedded newlines so multi-line streamed text stays aligned.
	if isTTY {
		text = strings.ReplaceAll(text, "\n", "\n    ")
	}
	fmt.Print(text)
	return nil
}

func (w *CLIStreamWriter) Close(ctx context.Context, finalText string) error {
	if w.aborted {
		return nil
	}
	w.closed = true
	if w.started {
		// End the streamed block with spacing.
		fmt.Println()
		fmt.Println()
		if sep := Separator(); sep != "" {
			fmt.Println(sep)
		}
		ShowPrompt()
	} else if strings.TrimSpace(finalText) != "" {
		// Nothing was streamed — print the full response now.
		if w.spinner != nil {
			w.spinner.Stop()
		}
		fmt.Print(FormatResponse(finalText))
		fmt.Println()
		fmt.Println()
		if sep := Separator(); sep != "" {
			fmt.Println(sep)
		}
		ShowPrompt()
	}
	// If not started AND no text, do nothing (tool-call turn — spinner may keep running).
	return nil
}

func (w *CLIStreamWriter) Abort(ctx context.Context) error {
	w.aborted = true
	if w.started {
		fmt.Println()
		fmt.Println(style(ansiYellow, "  ⚠ [aborted]"))
		ShowPrompt()
	}
	// If not started, leave spinner untouched so it carries through tool-call loops.
	return nil
}

// BeginStream implements channels.StreamingChannel.
func (d Deliverer) BeginStream(ctx context.Context, to string, meta map[string]any) (channels.StreamWriter, error) {
	return &CLIStreamWriter{spinner: d.Spinner}, nil
}
