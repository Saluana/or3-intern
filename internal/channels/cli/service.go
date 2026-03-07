package cli

import (
	"context"
	"fmt"

	"or3-intern/internal/bus"
)

type Service struct {
	Deliverer Deliverer
}

func (s Service) Name() string { return "cli" }

func (s Service) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	return nil
}

func (s Service) Stop(ctx context.Context) error {
	_ = ctx
	return nil
}

func (s Service) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	if len(meta) > 0 {
		if raw, ok := meta["media_paths"].([]string); ok && len(raw) > 0 {
			return fmt.Errorf("cli channel does not support media attachments")
		}
	}
	return s.Deliverer.Deliver(ctx, "cli", to, text)
}
