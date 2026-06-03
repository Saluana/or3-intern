package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memorysvc"
)

func TestRunnerMemoryPinnedRoundTrip(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := config.Default()
	server := newDoctorTestServer(t, database, cfg)
	server.memorySvc = memorysvc.New(cfg, database, nil, "fp-test")

	setBody := `{"session_key":"sess:runner","key":"locale","content":"en-NZ"}`
	setReq := httptest.NewRequest(http.MethodPost, "/internal/v1/runner-memory/pinned", strings.NewReader(setBody))
	setReq = setReq.WithContext(serviceContextWithAuthIdentity(setReq.Context(), serviceAuthIdentity{
		Kind: "auth-session", Actor: "runner:test", Role: approval.RoleOperator,
	}))
	setRec := httptest.NewRecorder()
	server.handleRunnerMemory(setRec, setReq)
	if setRec.Code != http.StatusOK {
		t.Fatalf("set pinned status = %d body=%s", setRec.Code, setRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/internal/v1/runner-memory/pinned?session_key=sess:runner&key=locale", nil)
	getReq = getReq.WithContext(serviceContextWithAuthIdentity(getReq.Context(), serviceAuthIdentity{
		Kind: "auth-session", Actor: "runner:test", Role: approval.RoleOperator,
	}))
	getRec := httptest.NewRecorder()
	server.handleRunnerMemory(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get pinned status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var payload struct {
		Entries []struct {
			Key     string `json:"key"`
			Content string `json:"content"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Entries) != 1 || payload.Entries[0].Content != "en-NZ" {
		t.Fatalf("unexpected pinned payload: %#v", payload.Entries)
	}
}

func TestRunnerMemoryRequiresOperatorRole(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	server := newDoctorTestServer(t, database, config.Default())
	server.memorySvc = memorysvc.New(config.Default(), database, nil, "fp")

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/runner-memory/search", strings.NewReader(`{"session_key":"s","query":"x"}`))
	req = req.WithContext(serviceContextWithAuthIdentity(req.Context(), serviceAuthIdentity{
		Kind: "auth-session", Actor: "viewer", Role: approval.RoleViewer,
	}))
	rec := httptest.NewRecorder()
	server.handleRunnerMemory(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d", rec.Code)
	}
}
