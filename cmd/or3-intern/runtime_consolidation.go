package main

import (
	"context"
	"fmt"
	"log"

	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
)

func startMemoryConsolidation(ctx context.Context, cfg config.Config, d *db.DB, del *cli.Deliverer) (*memory.Consolidator, *memory.Scheduler) {
	if !cfg.ConsolidationEnabled && !cfg.ContextManager.Enabled {
		return nil, nil
	}
	embedRole := cfg.ModelRole(config.ModelRoleEmbeddings)
	consolidator := &memory.Consolidator{
		DB:                 d,
		Provider:           newConsolidationProviderClient(cfg),
		EmbedModel:         embedRole.Primary.Model,
		EmbedFingerprint:   currentEmbedFingerprint(cfg),
		ChatModel:          effectiveConsolidationModel(cfg),
		WindowSize:         cfg.ConsolidationWindowSize,
		MaxMessages:        cfg.ConsolidationMaxMessages,
		MaxInputChars:      cfg.ConsolidationMaxInputChars,
		CanonicalPinnedKey: "long_term_memory",
	}
	scheduler := memory.NewSchedulerWithContext(
		ctx,
		effectiveConsolidationTimeout(cfg),
		func(runCtx context.Context, sessionKey string) {
			historyMax := cfg.HistoryMax
			if historyMax <= 0 {
				historyMax = 40
			}
			for i := 0; i < schedulerMaxConsolidationPasses; i++ {
				didWork, err := consolidator.RunOnce(runCtx, sessionKey, historyMax, memory.RunMode{})
				if err != nil {
					message := fmt.Sprintf("consolidation failed for %s: %v", sessionKey, err)
					if del != nil {
						del.ShowNoticeForSession(sessionKey, message)
					} else {
						log.Printf("%s", message)
					}
					return
				}
				if !didWork {
					return
				}
			}
		},
	)
	return consolidator, scheduler
}
