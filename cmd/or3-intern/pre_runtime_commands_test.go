package main

import (
	"context"
	"strings"
	"testing"

	"or3-intern/internal/controlplane"
)

func TestRunScopeCommand_UsageErrors(t *testing.T) {
	cp := controlplane.NewLocal(hostedNoExecBaseConfig(), nil, nil, nil, nil)
	for _, args := range [][]string{
		nil,
		{"--badflag"},
		{"link", "session-only"},
		{"list"},
		{"resolve"},
		{"bogus"},
	} {
		err := runScopeCommand(context.Background(), cp, args, &strings.Builder{}, &strings.Builder{})
		if err == nil {
			t.Fatalf("expected usage error for args=%v", args)
		}
		if !isUsageError(err) {
			t.Fatalf("expected usage error type for args=%v, got %v", args, err)
		}
	}
}

func TestRunEmbeddingsCommand_UsageErrors(t *testing.T) {
	cp := controlplane.NewLocal(hostedNoExecBaseConfig(), nil, nil, nil, nil)
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"rebuild", "nope"},
	} {
		err := runEmbeddingsCommand(context.Background(), cp, args, &strings.Builder{}, &strings.Builder{})
		if err == nil {
			t.Fatalf("expected usage error for args=%v", args)
		}
		if !isUsageError(err) {
			t.Fatalf("expected usage error type for args=%v, got %v", args, err)
		}
	}
}

func TestIsUsageError(t *testing.T) {
	if !isUsageError(newUsageError("usage: something")) {
		t.Fatal("expected usage error to be detected")
	}
	if isUsageError(context.Canceled) {
		t.Fatal("did not expect context.Canceled to be treated as usage error")
	}
}
