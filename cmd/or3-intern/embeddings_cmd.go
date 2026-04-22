package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/controlplane"
)

func runEmbeddingsCommand(ctx context.Context, cp *controlplane.Service, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return newUsageError("usage: embeddings <status|rebuild> [memory|docs|all]")
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return printEmbeddingStatus(ctx, cp, stdout)
	case "rebuild":
		target := "memory"
		if len(args) > 1 {
			target = strings.ToLower(strings.TrimSpace(args[1]))
		}
		switch target {
		case "memory", "docs", "all":
		default:
			return newUsageError("usage: embeddings rebuild [memory|docs|all]")
		}
		return rebuildEmbeddings(ctx, cp, target, stdout)
	default:
		return newUsageError("usage: embeddings <status|rebuild> [memory|docs|all]")
	}
}

func printEmbeddingStatus(ctx context.Context, cp *controlplane.Service, stdout io.Writer) error {
	status, err := cp.GetEmbeddingStatus(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "memory_vector_dims: %d\n", status.MemoryVectorDims)
	_, _ = fmt.Fprintf(stdout, "stored_embed_fingerprint: %s\n", emptyAsNone(status.StoredEmbedFingerprint))
	_, _ = fmt.Fprintf(stdout, "current_embed_fingerprint: %s\n", emptyAsNone(status.CurrentEmbedFingerprint))
	_, _ = fmt.Fprintf(stdout, "status: %s\n", status.Status)
	if status.Status != "ok" {
		_, _ = fmt.Fprintln(stdout, "hint: run `or3-intern embeddings rebuild memory` after switching embedding providers or models")
	}
	return nil
}

func rebuildEmbeddings(ctx context.Context, cp *controlplane.Service, target string, stdout io.Writer) error {
	result, err := cp.RebuildEmbeddings(ctx, target)
	if err != nil {
		return err
	}
	printMemory := target == "memory" || target == "all"
	printDocs := target == "docs" || target == "all"
	if printMemory {
		if result.MemoryNotesRebuilt > 0 {
			_, _ = fmt.Fprintf(stdout, "rebuilt %d memory embeddings with %s\n", result.MemoryNotesRebuilt, result.Fingerprint)
		} else if containsSkipReason(result.Skipped, "no_memory_notes") {
			_, _ = fmt.Fprintln(stdout, "no memory notes to rebuild")
		}
	}
	if printDocs {
		switch {
		case result.DocsRebuilt:
			_, _ = fmt.Fprintf(stdout, "rebuilt doc embeddings with %s\n", result.Fingerprint)
		case containsSkipReason(result.Skipped, "doc_index_disabled"):
			_, _ = fmt.Fprintln(stdout, "doc index not enabled; skipping docs rebuild")
		case containsSkipReason(result.Skipped, "doc_index_no_roots"):
			_, _ = fmt.Fprintln(stdout, "doc index has no roots configured; skipping docs rebuild")
		}
	}
	return nil
}

func containsSkipReason(skipped []string, want string) bool {
	for _, item := range skipped {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}
