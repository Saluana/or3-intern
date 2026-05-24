package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"or3-intern/internal/config"
	"or3-intern/internal/tools"
)

func TestDoctorDocsCorpusSearchRanksHealthAndApprovals(t *testing.T) {
	docsDir, err := doctorDocsV1Dir()
	if err != nil {
		t.Skipf("docs/v1 unavailable: %v", err)
	}
	corpus, err := loadDoctorDocsCorpus(context.Background(), docsDir)
	if err != nil {
		t.Fatalf("loadDoctorDocsCorpus: %v", err)
	}
	if len(corpus.pages) < 50 {
		t.Fatalf("expected substantial docs corpus, got %d pages", len(corpus.pages))
	}

	health := corpus.search("health checks readiness", 5)
	if len(health) == 0 {
		t.Fatalf("expected health doc matches")
	}
	if !strings.Contains(strings.ToLower(fmt.Sprint(health[0]["path"])), "health") {
		t.Fatalf("expected health path first, got %#v", health[0])
	}

	approvals := corpus.search("approval workflow tool quota", 5)
	if len(approvals) == 0 {
		t.Fatalf("expected approval doc matches")
	}
	foundApproval := false
	for _, item := range approvals {
		if strings.Contains(strings.ToLower(fmt.Sprint(item["path"])), "approval") {
			foundApproval = true
			break
		}
	}
	if !foundApproval {
		t.Fatalf("expected approval-related path in results: %#v", approvals)
	}
}

func TestDoctorDocsIndexAndSectionTools(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	server := newDoctorTestServer(t, database, config.Default())
	server.registerDoctorAdminBrainTools()

	indexOut, err := server.runtime.Tools.ExecuteParams(context.Background(), doctorToolNameDocsIndex, nil)
	if err != nil {
		t.Fatalf("doctor_docs_index: %v", err)
	}
	var indexResult tools.ToolResult
	if err := json.Unmarshal([]byte(indexOut), &indexResult); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if indexResult.Kind != doctorToolNameDocsIndex || !indexResult.OK {
		t.Fatalf("unexpected index result: %s", indexOut)
	}
	total, _ := indexResult.Stats["total_pages"].(float64)
	if total < 50 {
		t.Fatalf("expected indexed pages, got %#v", indexResult.Stats)
	}

	searchOut, err := server.runtime.Tools.ExecuteParams(context.Background(), doctorToolNameDocsSearch, map[string]any{
		"query": "runner chat",
		"limit": 3,
	})
	if err != nil {
		t.Fatalf("doctor_docs_search: %v", err)
	}
	var searchResult tools.ToolResult
	if err := json.Unmarshal([]byte(searchOut), &searchResult); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	results, _ := searchResult.Stats["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("expected search results: %s", searchOut)
	}
	first, _ := results[0].(map[string]any)
	path := fmt.Sprint(first["path"])

	sectionOut, err := server.runtime.Tools.ExecuteParams(context.Background(), doctorToolNameDocsSection, map[string]any{
		"path": path,
	})
	if err != nil {
		t.Fatalf("doctor_docs_section: %v", err)
	}
	var sectionResult tools.ToolResult
	if err := json.Unmarshal([]byte(sectionOut), &sectionResult); err != nil {
		t.Fatalf("decode section: %v", err)
	}
	if sectionResult.Kind != doctorToolNameDocsSection || !sectionResult.OK {
		t.Fatalf("unexpected section result: %s", sectionOut)
	}
	if strings.TrimSpace(fmt.Sprint(sectionResult.Stats["content"])) == "" {
		t.Fatalf("expected section content: %#v", sectionResult.Stats)
	}
}
