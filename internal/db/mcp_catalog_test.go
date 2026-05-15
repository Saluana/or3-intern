package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMCPToolCatalogPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "or3.sqlite")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if err := database.UpsertMCPToolCatalog(ctx, []MCPToolCatalogRecord{{
		ServerName: "alpha",
		RemoteName: "echo",
		LocalName:  "mcp_alpha_echo",
		Status:     "connected",
	}}); err != nil {
		t.Fatalf("UpsertMCPToolCatalog: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	records, err := reopened.ListMCPToolCatalog(ctx)
	if err != nil {
		t.Fatalf("ListMCPToolCatalog: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one persisted record, got %#v", records)
	}
	if records[0].ServerName != "alpha" || records[0].RemoteName != "echo" || records[0].LocalName != "mcp_alpha_echo" || records[0].Status != "connected" {
		t.Fatalf("unexpected persisted record: %#v", records[0])
	}
}
