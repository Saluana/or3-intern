package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"or3-intern/internal/bus"
)

type Channel struct {
	Bus *bus.Bus
	SessionKey string
}

func (c *Channel) Run(ctx context.Context) error {
	if c.SessionKey == "" { c.SessionKey = "cli:default" }
	in := bufio.NewScanner(os.Stdin)
	fmt.Println("or3-intern CLI. Type /exit to quit.")
	for {
		fmt.Print("> ")
		if !in.Scan() { return nil }
		line := strings.TrimSpace(in.Text())
		if line == "" { continue }
		if line == "/exit" { return nil }
		c.Bus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: c.SessionKey, Channel: "cli", From: "local", Message: line})
	}
}
