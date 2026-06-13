package app

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/runners"
	"or3-intern/internal/scope"
)

const (
	runnerPinnedMemoryMaxChars    = 1000
	runnerRetrievedMemoryMaxChars = 2200
	runnerRetrievedMemoryMaxHits  = 5
	runnerStaticMemoryMaxChars    = 1200
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
	Cache            *runners.RunnerContextCache
}

// RunnerContextBuilder assembles bounded OR3 context blocks without the legacy built-in runtime.
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
	return &RunnerContextBuilder{cfg: cfg, deps: deps}
}

func (b *RunnerContextBuilder) BuildContextBlocks(ctx context.Context, sessionKey, userMessage, triggerKind string, bootstrap RunnerBootstrapContext) []string {
	return b.BuildContextWithMeta(ctx, sessionKey, userMessage, triggerKind, bootstrap).Blocks
}

// BuildContextWithMeta assembles context blocks and memory-source debug flags.
func (b *RunnerContextBuilder) BuildContextWithMeta(ctx context.Context, sessionKey, userMessage, triggerKind string, bootstrap RunnerBootstrapContext) RunnerContextBuildResult {
	return b.buildContext(ctx, sessionKey, userMessage, triggerKind, bootstrap, true)
}

// RunnerContextBuildResult is the metadata-aware output of context assembly.
type RunnerContextBuildResult struct {
	Blocks      []string
	MemoryParts []string
	Debug       runners.RunnerMemoryDebug
}

func (b *RunnerContextBuilder) BuildNativeMemoryRefresh(ctx context.Context, sessionKey, userMessage string, bootstrap RunnerBootstrapContext) (string, runners.RunnerMemoryDebug) {
	result := b.buildContext(ctx, sessionKey, userMessage, "user_message", bootstrap, false)
	if len(result.MemoryParts) == 0 {
		return "", result.Debug
	}
	refresh := "memory_context:\n" + strings.Join(result.MemoryParts, "\n\n")
	result.Debug.NativeRefresh = true
	return refresh, result.Debug
}

func (b *RunnerContextBuilder) buildContext(ctx context.Context, sessionKey, userMessage, triggerKind string, bootstrap RunnerBootstrapContext, includeDocs bool) RunnerContextBuildResult {
	if b == nil {
		return RunnerContextBuildResult{Blocks: bootstrap.contextBlocks(triggerKind)}
	}
	var debug runners.RunnerMemoryDebug
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
	memoryParts := make([]string, 0, 3)
	if b.deps.DB != nil {
		pinned, err := b.deps.DB.GetPinned(ctx, scopeKey)
		if err != nil {
			log.Printf("runner context: pinned memory failed for %q: %v", scopeKey, err)
		} else if text := formatPinnedMemoryContext(pinned, runnerPinnedMemoryMaxChars); text != "" {
			debug.PinnedNonEmpty = true
			memoryParts = append(memoryParts, "pinned_memory:\n"+text)
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
		topK := b.deps.TopK
		if topK <= 0 || topK > runnerRetrievedMemoryMaxHits {
			topK = runnerRetrievedMemoryMaxHits
		}
		retrieved, err := mem.Retrieve(ctx, scopeKey, userMessage, queryVec, b.deps.VectorK, b.deps.FTSK, topK)
		if err != nil {
			log.Printf("runner context: memory retrieve failed for %q: %v", scopeKey, err)
		} else if text := memory.FormatRetrievedBrief(retrieved, runnerRetrievedMemoryMaxChars); text != "" && text != "(none)" {
			debug.RetrievedNonEmpty = true
			memoryParts = append(memoryParts, "retrieved_memory:\n"+text)
		}
	}
	if text := compactRunnerContextText(bootstrap.StaticMemory, runnerStaticMemoryMaxChars); text != "" {
		debug.DigestNonEmpty = true
		memoryParts = append(memoryParts, "memory_digest:\n"+text)
	}
	if len(memoryParts) > 0 {
		debug.PassiveCompiled = true
		blocks = append(blocks, "memory_context:\n"+strings.Join(memoryParts, "\n\n"))
	}
	if includeDocs && b.deps.Docs != nil && strings.TrimSpace(userMessage) != "" {
		limit := b.deps.DocLimit
		if limit <= 0 {
			limit = 5
		}
		docs, err := b.deps.Docs.RetrieveDocs(ctx, scope.GlobalMemoryScope, userMessage, limit)
		if err != nil {
			log.Printf("runner context: doc retrieve failed: %v", err)
		} else if len(docs) > 0 {
			debug.DocsNonEmpty = true
			var sb strings.Builder
			sb.WriteString("indexed_docs:\n")
			for i, d := range docs {
				sb.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, d.Path, d.Excerpt))
			}
			blocks = append(blocks, strings.TrimSpace(sb.String()))
		}
	}
	return RunnerContextBuildResult{Blocks: blocks, MemoryParts: memoryParts, Debug: debug}
}

func formatPinnedMemoryContext(pinned map[string]string, maxChars int) string {
	if len(pinned) == 0 {
		return ""
	}
	keys := make([]string, 0, len(pinned))
	for key := range pinned {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(pinned[key]) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		line := fmt.Sprintf("- %s: %s\n", strings.TrimSpace(key), compactRunnerContextText(pinned[key], 240))
		if maxChars > 0 && b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func compactRunnerContextText(text string, maxChars int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	if maxChars > 0 && len(text) > maxChars {
		if maxChars <= 3 {
			return text[:maxChars]
		}
		return text[:maxChars-3] + "..."
	}
	return text
}
