package cli

import (
	"context"
	"fmt"

	"or3-intern/internal/bus"
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
