package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"or3-intern/internal/bus"
)

// Channel reads user input from stdin and publishes messages to the bus.
type Channel struct {
	Bus        *bus.Bus
	SessionKey string
	Spinner    *Spinner // shared with Deliverer so it can be stopped on output
}

func (c *Channel) Run(ctx context.Context) error {
	if c.SessionKey == "" {
		c.SessionKey = "default"
	}
	in := bufio.NewScanner(os.Stdin)
	fmt.Print(Banner())
	ShowPrompt() // initial prompt
	for {
		// Prompt is printed either above (first iteration) or by the
		// Deliverer after finishing a response. We block on Scan here.
		if !in.Scan() {
			return nil
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			fmt.Print(Prompt())
			continue
		}
		if line == "/exit" {
			if isTTY {
				fmt.Println(style(ansiDim+ansiGray, "  Goodbye 👋"))
			}
			return nil
		}

		ok := c.Bus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: c.SessionKey,
			Channel:    "cli",
			From:       "local",
			Message:    line,
		})
		if !ok {
			fmt.Println(style(ansiYellow, "  ⚠ queue full — message dropped"))
			fmt.Print(Prompt())
		} else {
			// Restyle the raw prompt line into a labeled user message block.
			RewriteUserMessage(line)
			if c.Spinner != nil {
				c.Spinner.Start("thinking…")
			}
			// Don't print the prompt — the Deliverer will show it
			// after the response is fully rendered.
		}
	}
}
