package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/db"
)

func connectScopedRequest(method, target, namespace string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	return serviceRequestWithAuthIdentity(req, serviceAuthIdentity{
		Kind:      "paired-device",
		Role:      approval.RoleConnect,
		Namespace: namespace,
	})
}

func TestRunnerChatConnectScopeFiltersListsAndHidesOtherSessions(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	ctx := context.Background()
	for _, session := range []db.RunnerChatSession{
		{ID: "session-a", AppSessionKey: "or3-chat:workspace-a:thread-1", RunnerID: "codex"},
		{ID: "session-b", AppSessionKey: "or3-chat:workspace-b:thread-1", RunnerID: "codex"},
		{ID: "session-local", AppSessionKey: "local:thread-1", RunnerID: "codex"},
	} {
		if _, err := database.CreateOrGetRunnerChatSession(ctx, session); err != nil {
			t.Fatalf("CreateOrGetRunnerChatSession(%s): %v", session.ID, err)
		}
	}

	server := &serviceServer{}
	listRecorder := httptest.NewRecorder()
	server.handleRunnerChatSessionsList(
		listRecorder,
		connectScopedRequest(http.MethodGet, "/internal/v1/runner-chat/sessions", "or3-chat:workspace-a:"),
		database,
	)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d (%s)", listRecorder.Code, listRecorder.Body.String())
	}
	body := listRecorder.Body.String()
	if !strings.Contains(body, "session-a") || strings.Contains(body, "session-b") || strings.Contains(body, "session-local") {
		t.Fatalf("scoped list leaked sessions: %s", body)
	}

	crossPrefixRecorder := httptest.NewRecorder()
	server.handleRunnerChatSessionsList(
		crossPrefixRecorder,
		connectScopedRequest(http.MethodGet, "/internal/v1/runner-chat/sessions?app_session_key_prefix=or3-chat%3Aworkspace-b%3A", "or3-chat:workspace-a:"),
		database,
	)
	if crossPrefixRecorder.Code != http.StatusBadRequest {
		t.Fatalf("cross-scope prefix status = %d (%s)", crossPrefixRecorder.Code, crossPrefixRecorder.Body.String())
	}

	readRecorder := httptest.NewRecorder()
	if server.requireRunnerChatSessionScope(
		readRecorder,
		connectScopedRequest(http.MethodGet, "/internal/v1/runner-chat/sessions/session-b", "or3-chat:workspace-a:"),
		database,
		"session-b",
	) {
		t.Fatal("cross-scope session was authorized")
	}
	if readRecorder.Code != http.StatusNotFound {
		t.Fatalf("cross-scope read status = %d (%s)", readRecorder.Code, readRecorder.Body.String())
	}
}
