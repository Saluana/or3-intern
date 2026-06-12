package app

import (
	"context"
	"testing"

	"or3-intern/internal/config"
)

func TestDetectAgentCLIRunnersWithoutManager(t *testing.T) {
	svc := NewServiceAppWithAgentCLI(config.Default(), nil, nil, nil)
	runners, err := svc.DetectAgentCLIRunners(context.Background())
	if err != nil {
		t.Fatalf("DetectAgentCLIRunners: %v", err)
	}
	if len(runners) == 0 {
		t.Fatal("expected default registry to detect at least one runner without manager")
	}
}
