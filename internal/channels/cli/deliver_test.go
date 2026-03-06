package cli

import (
	"context"
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
	// Should default to "cli" if channel is empty
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
