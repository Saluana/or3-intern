package cli

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/bus"
)

func mustWriteInput(t *testing.T, w *os.File, input string) {
	t.Helper()
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
}

func TestChannel_Run_Exit(t *testing.T) {
	// Create a pipe to simulate stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	// Replace stdin
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test"}

	// Write /exit command
	go func() {
		mustWriteInput(t, w, "/exit\n")
		_ = w.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error from Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to exit")
	}
}

func TestChannel_Run_PublishesMessage(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test-sess"}

	// Write a message then exit
	go func() {
		mustWriteInput(t, w, "hello world\n")
		mustWriteInput(t, w, "/exit\n")
		_ = w.Close()
	}()

	done := make(chan struct{})
	go func() {
		if err := ch.Run(context.Background()); err != nil {
			t.Errorf("Run: %v", err)
		}
		close(done)
	}()

	// Wait for event on bus
	select {
	case ev := <-b.Channel():
		if ev.Message != "hello world" {
			t.Errorf("expected message 'hello world', got %q", ev.Message)
		}
		if ev.SessionKey != "test-sess" {
			t.Errorf("expected session 'test-sess', got %q", ev.SessionKey)
		}
		if ev.Channel != "cli" {
			t.Errorf("expected channel 'cli', got %q", ev.Channel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	<-done
}

func TestChannel_Run_SkipsBlankLines(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test"}

	go func() {
		mustWriteInput(t, w, "  \n")
		mustWriteInput(t, w, "\n")
		mustWriteInput(t, w, "real message\n")
		mustWriteInput(t, w, "/exit\n")
		_ = w.Close()
	}()

	done := make(chan struct{})
	go func() {
		if err := ch.Run(context.Background()); err != nil {
			t.Errorf("Run: %v", err)
		}
		close(done)
	}()

	select {
	case ev := <-b.Channel():
		if ev.Message != "real message" {
			t.Errorf("expected 'real message', got %q", ev.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	<-done
}

func TestChannel_Run_DefaultSessionKey(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	// No SessionKey set - should default to "default"
	ch := &Channel{Bus: b}

	go func() {
		mustWriteInput(t, w, "msg\n")
		mustWriteInput(t, w, "/exit\n")
		_ = w.Close()
	}()

	done := make(chan struct{})
	go func() {
		if err := ch.Run(context.Background()); err != nil {
			t.Errorf("Run: %v", err)
		}
		close(done)
	}()

	select {
	case ev := <-b.Channel():
		if ev.SessionKey != "default" {
			t.Errorf("expected default session key 'default', got %q", ev.SessionKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	<-done
}

func TestChannel_Run_FullBus(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	// Create a bus with buffer=0 to simulate full bus
	b := bus.New(1)
	// Fill the bus
	b.Publish(bus.Event{})

	ch := &Channel{Bus: b, SessionKey: "test"}

	go func() {
		// This message should be dropped (bus full) but not crash
		mustWriteInput(t, w, "dropped message\n")
		mustWriteInput(t, w, "/exit\n")
		_ = w.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestChannel_Run_EOFOnStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "test"}

	// Close write end to simulate EOF
	w.Close()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error on EOF: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestChannel_Run_LargeInputDoesNotTripScannerLimit(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "large"}
	large := strings.Repeat("x", 128*1024)

	go func() {
		mustWriteInput(t, w, large+"\n")
		mustWriteInput(t, w, "/exit\n")
		_ = w.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case ev := <-b.Channel():
		if ev.Message != large {
			t.Fatalf("expected large message to publish intact, got len=%d", len(ev.Message))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for large message")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error from Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Run to exit after large input")
	}
}

func TestChannel_Run_UsesPlaintextWhenStdinIsNotTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	oldStdin := os.Stdin
	oldStdoutTTY := stdoutIsTTY
	oldStdinTTY := stdinIsTTY
	os.Stdin = r
	stdoutIsTTY = true
	stdinIsTTY = false
	defer func() {
		os.Stdin = oldStdin
		stdoutIsTTY = oldStdoutTTY
		stdinIsTTY = oldStdinTTY
		r.Close()
	}()

	b := bus.New(10)
	ch := &Channel{Bus: b, SessionKey: "plaintext-fallback", Deliverer: &Deliverer{}}

	go func() {
		mustWriteInput(t, w, "/exit\n")
		_ = w.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ch.Run(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error from Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for plaintext fallback")
	}
}
