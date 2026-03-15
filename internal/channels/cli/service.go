package cli

import (
	"context"
	"fmt"

	"or3-intern/internal/bus"
)

// Service adapts the CLI deliverer to the shared channel manager interface.
type Service struct {
	Deliverer Deliverer
}

// Name returns the registered channel name.
func (s Service) Name() string { return "cli" }

// Start is a no-op because the CLI service has no background lifecycle.
func (s Service) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	return nil
}

// Stop is a no-op because the CLI service has no background lifecycle.
func (s Service) Stop(ctx context.Context) error {
	_ = ctx
	return nil
}

// Deliver forwards a CLI response while rejecting unsupported attachments.
func (s Service) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	if len(meta) > 0 {
		if raw, ok := meta["media_paths"].([]string); ok && len(raw) > 0 {
			return fmt.Errorf("cli channel does not support media attachments")
		}
	}
	return s.Deliverer.Deliver(ctx, "cli", to, text)
}
