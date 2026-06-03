package app

import (
	"context"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

// RunnerContextDeps supplies optional memory/doc retrieval for runner prompts.
type RunnerContextDeps struct {
	DB               *db.DB
	Mem              *memory.Retriever
	Docs             *memory.DocRetriever
	Embed            *providers.Client
	EmbedModel       string
	EmbedFingerprint string
	VectorK          int
	FTSK             int
	TopK             int
	DocLimit         int
	BootstrapMax     int
	Cache            *agentcli.RunnerContextCache
}

// RunnerContextBuilder assembles bounded OR3 context blocks without agent.Runtime.
type RunnerContextBuilder struct {
	cfg  config.Config
	deps RunnerContextDeps
}

func NewRunnerContextBuilder(cfg config.Config, deps RunnerContextDeps) *RunnerContextBuilder {
	if deps.BootstrapMax <= 0 {
		deps.BootstrapMax = cfg.BootstrapMaxChars
	}
	if deps.BootstrapMax <= 0 {
		deps.BootstrapMax = 4000
	}
	if deps.VectorK <= 0 {
		deps.VectorK = cfg.VectorK
	}
	if deps.FTSK <= 0 {
		deps.FTSK = cfg.FTSK
	}
	if deps.TopK <= 0 {
		deps.TopK = cfg.MemoryRetrieve
	}
	if deps.DocLimit <= 0 && cfg.DocIndex.Enabled {
		deps.DocLimit = cfg.DocIndex.RetrieveLimit
	}
	return &RunnerContextBuilder{cfg: cfg, deps: deps}
}

func (b *RunnerContextBuilder) BuildContextBlocks(ctx context.Context, sessionKey, userMessage, triggerKind string, bootstrap RunnerBootstrapContext) []string {
	if b == nil {
		return bootstrap.contextBlocks(triggerKind)
	}
	blocks := make([]string, 0, 4)
	if autonomous := bootstrap.contextBlocks(triggerKind); len(autonomous) > 0 {
		blocks = append(blocks, autonomous...)
	}
	scopeKey := strings.TrimSpace(sessionKey)
	if b.deps.DB != nil && scopeKey != "" {
		if resolved, err := b.deps.DB.ResolveScopeKey(ctx, scopeKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	if b.deps.Mem != nil && strings.TrimSpace(userMessage) != "" {
		mem := *b.deps.Mem
		mem.EmbedFingerprint = b.deps.EmbedFingerprint
		var queryVec []float32
		if b.deps.Embed != nil && strings.TrimSpace(b.deps.EmbedModel) != "" {
			vec, err := b.deps.Embed.Embed(ctx, b.deps.EmbedModel, userMessage)
			if err != nil {
				log.Printf("runner context: memory embed failed for %q: %v", scopeKey, err)
			} else {
				queryVec = vec
			}
		}
		retrieved, err := mem.Retrieve(ctx, scopeKey, userMessage, queryVec, b.deps.VectorK, b.deps.FTSK, b.deps.TopK)
		if err != nil {
			log.Printf("runner context: memory retrieve failed for %q: %v", scopeKey, err)
		} else if text := memory.FormatRetrievedBrief(retrieved, b.deps.BootstrapMax); text != "" && text != "(none)" {
			blocks = append(blocks, "retrieved_memory:\n"+text)
		}
	}
	if b.deps.Docs != nil && strings.TrimSpace(userMessage) != "" {
		limit := b.deps.DocLimit
		if limit <= 0 {
			limit = 5
		}
		docs, err := b.deps.Docs.RetrieveDocs(ctx, scope.GlobalMemoryScope, userMessage, limit)
		if err != nil {
			log.Printf("runner context: doc retrieve failed: %v", err)
		} else if len(docs) > 0 {
			var sb strings.Builder
			sb.WriteString("indexed_docs:\n")
			for i, d := range docs {
				sb.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, d.Path, d.Excerpt))
			}
			blocks = append(blocks, strings.TrimSpace(sb.String()))
		}
	}
	return blocks
}
