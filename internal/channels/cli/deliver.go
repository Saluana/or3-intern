package cli

import (
	"context"
	"fmt"
)

type Deliverer struct{}

func (Deliverer) Deliver(ctx context.Context, channel, to, text string) error {
	_ = ctx
	if channel == "" { channel = "cli" }
	fmt.Printf("\n[%s] %s\n\n", channel, text)
	return nil
}
