package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/app"
)

func TestWaitForForegroundAgentTurn_ReturnsTerminalResult(t *testing.T) {
	result := app.RunnerTurnResult{RunnerChatTurnID: "turn-1"}
	final, err := waitForForegroundAgentTurn(context.Background(), time.Second, result,
		func(context.Context, app.RunnerTurnResult) (app.RunnerTurnFinalResult, bool) {
			return app.RunnerTurnFinalResult{Status: "succeeded", FinalText: "done"}, true
		},
		nil,
	)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if final.FinalText != "done" || !foregroundAgentTurnSucceeded(final) {
		t.Fatalf("unexpected final result: %#v", final)
	}
}

func TestWaitForForegroundAgentTurn_AbortsWhenWaitTimesOut(t *testing.T) {
	result := app.RunnerTurnResult{RunnerChatTurnID: "turn-2"}
	aborted := make(chan string, 1)
	_, err := waitForForegroundAgentTurn(context.Background(), 10*time.Millisecond, result,
		func(ctx context.Context, _ app.RunnerTurnResult) (app.RunnerTurnFinalResult, bool) {
			<-ctx.Done()
			return app.RunnerTurnFinalResult{}, false
		},
		func(_ context.Context, turnID string) error {
			aborted <- turnID
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	select {
	case turnID := <-aborted:
		if turnID != result.RunnerChatTurnID {
			t.Fatalf("aborted %q, want %q", turnID, result.RunnerChatTurnID)
		}
	default:
		t.Fatal("expected timed-out turn to be aborted")
	}
}

func TestWaitForForegroundAgentTurn_PropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitForForegroundAgentTurn(ctx, time.Second, app.RunnerTurnResult{},
		func(ctx context.Context, _ app.RunnerTurnResult) (app.RunnerTurnFinalResult, bool) {
			<-ctx.Done()
			return app.RunnerTurnFinalResult{}, false
		},
		nil,
	)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
}
