package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

func runEmbeddingsCommand(ctx context.Context, cfg config.Config, d *db.DB, prov *providers.Client, args []string, stdout, stderr io.Writer) error {
	if d == nil {
		return fmt.Errorf("db is not configured")
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: embeddings <status|rebuild> [memory|docs|all]")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return printEmbeddingStatus(ctx, cfg, d, stdout)
	case "rebuild":
		target := "memory"
		if len(args) > 1 {
			target = strings.ToLower(strings.TrimSpace(args[1]))
		}
		switch target {
		case "memory":
			return rebuildMemoryEmbeddings(ctx, cfg, d, prov, stdout)
		case "docs":
			return rebuildDocEmbeddings(ctx, cfg, d, prov, stdout)
		case "all":
			if err := rebuildMemoryEmbeddings(ctx, cfg, d, prov, stdout); err != nil {
				return err
			}
			return rebuildDocEmbeddings(ctx, cfg, d, prov, stdout)
		default:
			return fmt.Errorf("usage: embeddings rebuild [memory|docs|all]")
		}
	default:
		return fmt.Errorf("usage: embeddings <status|rebuild> [memory|docs|all]")
	}
}

func printEmbeddingStatus(ctx context.Context, cfg config.Config, d *db.DB, stdout io.Writer) error {
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	storedFingerprint, err := d.MemoryVectorFingerprint(ctx)
	if err != nil {
		return err
	}
	currentFingerprint := currentEmbedFingerprint(cfg)
	status := "ok"
	if strings.TrimSpace(storedFingerprint) == "" && dims > 0 {
		status = "legacy-unknown"
	} else if strings.TrimSpace(storedFingerprint) != strings.TrimSpace(currentFingerprint) {
		status = "mismatch"
	}
	_, _ = fmt.Fprintf(stdout, "memory_vector_dims: %d\n", dims)
	_, _ = fmt.Fprintf(stdout, "stored_embed_fingerprint: %s\n", emptyAsNone(storedFingerprint))
	_, _ = fmt.Fprintf(stdout, "current_embed_fingerprint: %s\n", emptyAsNone(currentFingerprint))
	_, _ = fmt.Fprintf(stdout, "status: %s\n", status)
	if status != "ok" {
		_, _ = fmt.Fprintln(stdout, "hint: run `or3-intern embeddings rebuild memory` after switching embedding providers or models")
	}
	return nil
}

func rebuildMemoryEmbeddings(ctx context.Context, cfg config.Config, d *db.DB, prov *providers.Client, stdout io.Writer) error {
	if prov == nil {
		return fmt.Errorf("provider is not configured")
	}
	model := strings.TrimSpace(cfg.Provider.EmbedModel)
	if model == "" {
		return fmt.Errorf("provider.embedModel is not configured")
	}
	rows, err := d.ListMemoryNotesForReembed(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(stdout, "no memory notes to rebuild")
		return nil
	}
	fingerprint := currentEmbedFingerprint(cfg)
	wantDims := 0
	for _, row := range rows {
		vec, err := prov.Embed(ctx, model, strings.TrimSpace(row.Text))
		if err != nil {
			return fmt.Errorf("rebuild memory note %d: %w", row.ID, err)
		}
		if wantDims == 0 {
			wantDims = len(vec)
		} else if len(vec) != wantDims {
			return fmt.Errorf("embedding dimension changed during rebuild: have %d want %d", len(vec), wantDims)
		}
		if err := d.ReplaceMemoryNoteEmbedding(ctx, row.ID, memory.PackFloat32(vec), fingerprint); err != nil {
			return fmt.Errorf("persist memory note %d: %w", row.ID, err)
		}
	}
	if wantDims > 0 {
		if err := d.RebuildMemoryVecIndexWithProfile(ctx, wantDims, fingerprint); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(stdout, "rebuilt %d memory embeddings with %s\n", len(rows), fingerprint)
	return nil
}

func rebuildDocEmbeddings(ctx context.Context, cfg config.Config, d *db.DB, prov *providers.Client, stdout io.Writer) error {
	if !cfg.DocIndex.Enabled {
		_, _ = fmt.Fprintln(stdout, "doc index not enabled; skipping docs rebuild")
		return nil
	}
	if len(cfg.DocIndex.Roots) == 0 {
		_, _ = fmt.Fprintln(stdout, "doc index has no roots configured; skipping docs rebuild")
		return nil
	}
	indexer := &memory.DocIndexer{
		DB:               d,
		Provider:         prov,
		EmbedModel:       cfg.Provider.EmbedModel,
		EmbedFingerprint: currentEmbedFingerprint(cfg),
		Config: memory.DocIndexConfig{
			Roots:          cfg.DocIndex.Roots,
			MaxFiles:       cfg.DocIndex.MaxFiles,
			MaxFileBytes:   cfg.DocIndex.MaxFileBytes,
			MaxChunks:      cfg.DocIndex.MaxChunks,
			EmbedMaxBytes:  cfg.DocIndex.EmbedMaxBytes,
			RefreshSeconds: cfg.DocIndex.RefreshSeconds,
			RetrieveLimit:  cfg.DocIndex.RetrieveLimit,
		},
	}
	if err := indexer.SyncRoots(ctx, scope.GlobalMemoryScope); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "rebuilt doc embeddings with %s\n", currentEmbedFingerprint(cfg))
	return nil
}
