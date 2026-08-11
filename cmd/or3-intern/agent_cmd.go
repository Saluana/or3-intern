package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/db"
)

const defaultForegroundAgentTimeout = 15 * time.Minute

func waitForForegroundAgentTurn(
	ctx context.Context,
	timeout time.Duration,
	result app.RunnerTurnResult,
	wait func(context.Context, app.RunnerTurnResult) (app.RunnerTurnFinalResult, bool),
	abort func(context.Context, string) error,
) (app.RunnerTurnFinalResult, error) {
	if wait == nil {
		return app.RunnerTurnFinalResult{}, fmt.Errorf("runner turn wait is unavailable")
	}
	if timeout <= 0 {
		timeout = defaultForegroundAgentTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	final, ok := wait(waitCtx, result)
	if ok {
		return final, nil
	}
	if abort != nil && strings.TrimSpace(result.RunnerChatTurnID) != "" {
		abortCtx, abortCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer abortCancel()
		_ = abort(abortCtx, result.RunnerChatTurnID)
	}
	if err := waitCtx.Err(); err != nil {
		if err == context.DeadlineExceeded {
			return app.RunnerTurnFinalResult{}, fmt.Errorf("runner turn timed out after %s", timeout)
		}
		return app.RunnerTurnFinalResult{}, fmt.Errorf("runner turn interrupted: %w", err)
	}
	return app.RunnerTurnFinalResult{}, fmt.Errorf("runner turn finished without a terminal result")
}

func foregroundAgentTurnSucceeded(final app.RunnerTurnFinalResult) bool {
	return final.Status == db.RunnerChatTurnStatusSucceeded
}
