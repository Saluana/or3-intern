package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestDeliver_Basic(t *testing.T) {
	d := Deliverer{}
	err := d.Deliver(context.Background(), "cli", "user", "hello there")
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}

func TestDeliver_EmptyChannel(t *testing.T) {
	d := Deliverer{}
	err := d.Deliver(context.Background(), "", "user", "message")
	if err != nil {
		t.Fatalf("Deliver with empty channel: %v", err)
	}
}

func TestDeliver_LongMessage(t *testing.T) {
	d := Deliverer{}
	msg := strings.Repeat("x", 10000)
	err := d.Deliver(context.Background(), "cli", "user", msg)
	if err != nil {
		t.Fatalf("Deliver long message: %v", err)
	}
}

// captureStdout swaps os.Stdout with a buffer during a test.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	return buf.String()
}

func TestCLIStreamWriter_WriteDeltaAndClose(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		ctx := context.Background()
		_ = w.WriteDelta(ctx, "hello")
		_ = w.WriteDelta(ctx, " world")
		_ = w.Close(ctx, "hello world")
	})
	// text printed incrementally; Close adds trailing spacing and prompt
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", out)
	}
}

func TestCLIStreamWriter_CloseWithoutDelta(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		ctx := context.Background()
		_ = w.Close(ctx, "fallback text")
	})
	if !strings.Contains(out, "fallback text") {
		t.Errorf("expected 'fallback text' in output, got %q", out)
	}
}

func TestCLIStreamWriter_AbortAfterDelta(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		ctx := context.Background()
		_ = w.WriteDelta(ctx, "partial")
		_ = w.Abort(ctx)
	})
	if !strings.Contains(out, "partial") {
		t.Errorf("expected 'partial' in output, got %q", out)
	}
	if !strings.Contains(out, "[aborted]") {
		t.Errorf("expected '[aborted]' in output, got %q", out)
	}
}

func TestCLIStreamWriter_AbortWithoutDelta(t *testing.T) {
	out := captureStdout(t, func() {
		w := &CLIStreamWriter{}
		_ = w.Abort(context.Background())
	})
	// No output expected when nothing was written
	if strings.Contains(out, "[aborted]") {
		t.Errorf("unexpected '[aborted]' when nothing was written, got %q", out)
	}
}

func TestCLIStreamWriter_WriteAfterClose(t *testing.T) {
	w := &CLIStreamWriter{}
	ctx := context.Background()
	_ = w.WriteDelta(ctx, "hello")
	_ = w.Close(ctx, "hello")
	// Further writes should be silently ignored
	err := w.WriteDelta(ctx, "extra")
	if err != nil {
		t.Errorf("unexpected error after close: %v", err)
	}
}

func TestBeginStream_ReturnsWriter(t *testing.T) {
	out := captureStdout(t, func() {
		d := Deliverer{}
		sw, err := d.BeginStream(context.Background(), "user", nil)
		if err != nil {
			t.Errorf("BeginStream: %v", err)
			return
		}
		_ = sw.WriteDelta(context.Background(), "streamed")
		_ = sw.Close(context.Background(), "streamed")
	})
	if !strings.Contains(out, "streamed") {
		t.Errorf("expected 'streamed' in output, got %q", out)
	}
	fmt.Print() // flush
}

func TestShowError(t *testing.T) {
	out := captureStdout(t, func() {
		ShowError(nil, fmt.Errorf("provider error 401 Unauthorized"))
	})
	if !strings.Contains(out, "Error: provider error 401 Unauthorized") {
		t.Fatalf("expected rendered error, got %q", out)
	}
}
