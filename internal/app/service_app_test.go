package app

import (
	"context"
	"testing"

	"or3-intern/internal/config"
)

func TestDetectRunnerRunnersWithoutManager(t *testing.T) {
	svc := NewServiceAppWithRunner(config.Default(), nil, nil, nil)
	runners, err := svc.DetectRunnerRunners(context.Background())
	if err != nil {
		t.Fatalf("DetectRunnerRunners: %v", err)
	}
	if len(runners) == 0 {
		t.Fatal("expected default registry to detect at least one runner without manager")
	}
}
