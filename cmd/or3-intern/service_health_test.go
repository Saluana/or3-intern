package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
)

type serviceHealthChannel struct {
	name   string
	status string
}

func (c *serviceHealthChannel) Name() string { return c.name }
func (c *serviceHealthChannel) Start(context.Context, *bus.Bus) error {
	return nil
}
func (c *serviceHealthChannel) Stop(context.Context) error {
	return nil
}
func (c *serviceHealthChannel) Deliver(context.Context, string, string, map[string]any) error {
	return nil
}
func (c *serviceHealthChannel) ConnectionStatus() string { return c.status }

func TestServiceHealthReportsLiveChannelFailures(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	channelManager := rootchannels.NewManager()
	channel := &serviceHealthChannel{name: "slack", status: "reconnecting"}
	if err := channelManager.Register(channel); err != nil {
		t.Fatalf("register channel: %v", err)
	}
	if err := channelManager.Start(context.Background(), "slack", bus.New(1)); err != nil {
		t.Fatalf("start channel: %v", err)
	}
	t.Cleanup(func() { _ = channelManager.Stop(context.Background(), "slack") })

	server := &serviceServer{
		config:           config.Default(),
		database:         database,
		jobs:             jobs.NewRegistry(time.Minute, 16),
		channelDeliverer: channelManager,
	}
	health := server.serviceHealth()
	if health.Status != "degraded" {
		t.Fatalf("expected reconnecting channel to degrade health, got %#v", health)
	}
	if health.ChannelStatuses["slack"] != "reconnecting" {
		t.Fatalf("expected live Slack status, got %#v", health.ChannelStatuses)
	}
}
