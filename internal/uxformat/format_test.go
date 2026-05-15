package uxformat

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderErrorIncludesTitleBodyAndDetails(t *testing.T) {
	got := RenderError(ErrorBlock{Title: "Settings error", Body: "Config is invalid", Details: []string{"Run doctor", ""}}, ColorNever)
	for _, want := range []string{"Settings error", "Config is invalid", "- Run doctor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestRenderLoadingHonorsActiveState(t *testing.T) {
	if got := RenderLoading(LoadingState{Label: "Checking", Active: true}, ColorNever); !strings.Contains(got, "Checking") || !strings.Contains(got, "…") {
		t.Fatalf("expected active loading indicator, got %q", got)
	}
	if got := RenderLoading(LoadingState{Label: "Checking", Active: false}, ColorNever); strings.Contains(got, "…") {
		t.Fatalf("expected inactive loading text without indicator, got %q", got)
	}
}

func TestWriteEmptyStateSkipsBlankHints(t *testing.T) {
	var out bytes.Buffer
	WriteEmptyState(&out, "No devices", "Pair one first.", []string{"or3-intern pairing approve-code 123456", ""})
	got := out.String()
	for _, want := range []string{"No devices", "Pair one first.", "or3-intern pairing approve-code 123456"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}
