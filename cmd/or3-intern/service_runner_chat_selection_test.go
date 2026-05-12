package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/agentcli"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type runnerChatServiceFixture struct {
	server     *serviceServer
	httpServer *httptest.Server
	database   *db.DB
	secret     string
	cleanup    func()
}

func newRunnerChatServiceFixture(t *testing.T, cfg config.Config, database *db.DB, agentManager *agentcli.Manager, chatManager *agentcli.ChatManager) *runnerChatServiceFixture {
	t.Helper()
	secret := strings.Repeat("r", 32)
	runtime := &agent.Runtime{
		DB:      database,
		Tools:   tools.NewRegistry(),
		Builder: &agent.Builder{DB: database, HistoryMax: 10},
	}
	server := &serviceServer{
		config:          cfg,
		runtime:         runtime,
		jobs:            agent.NewJobRegistry(time.Minute, 32),
		agentCLIManager: agentManager,
		chatManager:     chatManager,
	}
	httpServer := newServiceTestHTTPServer(t, secret, server)
	return &runnerChatServiceFixture{
		server:     server,
		httpServer: httpServer,
		database:   database,
		secret:     secret,
		cleanup: func() {
			httpServer.Close()
			database.Close()
		},
	}
}

func newDiscoveryRegistry() *agentcli.RunnerRegistry {
	specs := agentcli.AllRunners()
	return agentcli.NewRunnerRegistry(specs, []agentcli.RunnerAdapter{
		agentcli.NewOpenCodeAdapter(),
		agentcli.NewCodexAdapter(),
		agentcli.NewClaudeAdapter(),
		agentcli.NewGeminiAdapter(),
	})
}

func writeExecutableScript(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}

func decodeServiceResponseMap(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	return mustDecodeJSONBody(t, resp.Body)
}

func findRunnerByID(t *testing.T, payload map[string]any, id string) map[string]any {
	t.Helper()
	runners, ok := payload["runners"].([]any)
	if !ok {
		t.Fatalf("expected runners array, got %#v", payload)
	}
	for _, raw := range runners {
		item, ok := raw.(map[string]any)
		if ok && item["id"] == id {
			return item
		}
	}
	t.Fatalf("runner %q not found in %#v", id, payload)
	return nil
}

func mustServiceDoJSON(t *testing.T, fixture *runnerChatServiceFixture, method, path, body string) map[string]any {
	t.Helper()
	req := mustServiceRequest(t, fixture.httpServer, fixture.secret, method, path, body)
	resp, err := fixture.httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do %s %s: %v", method, path, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		t.Fatalf("expected success for %s %s, got %d (%s)", method, path, resp.StatusCode, mustReadBody(t, resp.Body))
	}
	return decodeServiceResponseMap(t, resp)
}

func seedRunnerChatTerminalTurn(t *testing.T, database *db.DB) (db.RunnerChatSession, db.RunnerChatTurn) {
	t.Helper()
	ctx := context.Background()
	sess, err := database.CreateOrGetRunnerChatSession(ctx, db.RunnerChatSession{
		ID:               "rcs-stream",
		AppSessionKey:    "svc:stream",
		RunnerID:         string(agentcli.RunnerOpenCode),
		ContinuationMode: string(agentcli.ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRunnerChatSession: %v", err)
	}
	turn, err := database.CreateRunnerChatTurn(ctx, db.RunnerChatTurn{
		ID:               "rct-stream",
		SessionID:        sess.ID,
		Status:           db.RunnerChatTurnStatusRunning,
		UserMessage:      "tell me more",
		ContinuationMode: string(agentcli.ContinuationReplay),
	})
	if err != nil {
		t.Fatalf("CreateRunnerChatTurn: %v", err)
	}
	if err := database.MarkRunnerChatTurnStarted(ctx, turn.ID, "run-stream", "job-stream"); err != nil {
		t.Fatalf("MarkRunnerChatTurnStarted: %v", err)
	}
	for _, ev := range []db.RunnerChatEvent{
		{TurnID: turn.ID, SessionID: sess.ID, JobID: "job-stream", Seq: 1, Type: "text_delta", Text: "first"},
		{TurnID: turn.ID, SessionID: sess.ID, JobID: "job-stream", Seq: 2, Type: "text_delta", Text: "second"},
		{TurnID: turn.ID, SessionID: sess.ID, JobID: "job-stream", Seq: 3, Type: "assistant", Text: "done"},
	} {
		if err := database.AppendRunnerChatEvent(ctx, ev); err != nil {
			t.Fatalf("AppendRunnerChatEvent seq=%d: %v", ev.Seq, err)
		}
	}
	if err := database.FinalizeRunnerChatTurn(ctx, turn.ID, db.RunnerChatTurnFinalize{
		Status:             db.RunnerChatTurnStatusSucceeded,
		FinalText:          "done",
		CompletedAt:        db.NowMS(),
		AssistantMessageID: 42,
	}); err != nil {
		t.Fatalf("FinalizeRunnerChatTurn: %v", err)
	}
	return sess, turn
}

func readSSEEvents(t *testing.T, resp *http.Response) []string {
	t.Helper()
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	events := make([]string, 0, 4)
	var currentType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			events = append(events, currentType+"|"+strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan SSE: %v", err)
	}
	return events
}

func TestServiceChatRunners_DiscoveryStatusesAndAgentRunnerContract(t *testing.T) {
	database, closeDB := openServiceTestDB(t)
	defer closeDB()

	binDir := t.TempDir()
	writeExecutableScript(t, binDir, "opencode", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo 'OpenCode 1.2.3'\n  exit 0\nfi\nif [ \"$1\" = \"auth\" ] && [ \"$2\" = \"list\" ]; then\n  exit 1\nfi\nexit 0\n")
	writeExecutableScript(t, binDir, "codex", "#!/bin/sh\nif [ \"$1\" = \"--help\" ]; then\n  echo 'Codex 0.9.0'\n  exit 0\nfi\nif [ \"$1\" = \"login\" ] && [ \"$2\" = \"status\" ]; then\n  exit 0\nfi\nexit 0\n")
	writeExecutableScript(t, binDir, "claude", "#!/bin/sh\necho 'Claude 9.9.9'\nexit 0\n")
	writeExecutableScript(t, binDir, "gemini", "#!/bin/sh\nexit 1\n")
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", binDir); err != nil {
		t.Fatalf("Setenv PATH: %v", err)
	}
	defer os.Setenv("PATH", oldPath)

	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	cfg.AgentCLI.DisabledRunners = []string{string(agentcli.RunnerClaude)}
	registry := newDiscoveryRegistry()
	manager := &agentcli.Manager{DB: database, Cfg: cfg.AgentCLI, Registry: registry}
	fixture := newRunnerChatServiceFixture(t, cfg, database, manager, &agentcli.ChatManager{DB: database, Manager: manager})
	defer fixture.httpServer.Close()

	chatReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodGet, "/internal/v1/chat-runners", "")
	chatResp, err := fixture.httpServer.Client().Do(chatReq)
	if err != nil {
		t.Fatalf("Do chat-runners: %v", err)
	}
	if chatResp.StatusCode != http.StatusOK {
		defer chatResp.Body.Close()
		t.Fatalf("expected 200, got %d (%s)", chatResp.StatusCode, mustReadBody(t, chatResp.Body))
	}
	chatPayload := decodeServiceResponseMap(t, chatResp)

	or3Runner := findRunnerByID(t, chatPayload, string(agentcli.RunnerOR3))
	if or3Runner["status"] != string(agentcli.RunnerStatusAvailable) || or3Runner["auth_status"] != string(agentcli.AuthReady) {
		t.Fatalf("expected OR3 always available/ready, got %#v", or3Runner)
	}
	if _, ok := or3Runner["chat_capabilities"]; !ok {
		t.Fatalf("expected chat_capabilities decoration, got %#v", or3Runner)
	}
	if got := findRunnerByID(t, chatPayload, string(agentcli.RunnerOpenCode))["status"]; got != string(agentcli.RunnerStatusAuthMissing) {
		t.Fatalf("expected OpenCode auth_missing, got %#v", got)
	}
	if got := findRunnerByID(t, chatPayload, string(agentcli.RunnerCodex))["status"]; got != string(agentcli.RunnerStatusAvailable) {
		t.Fatalf("expected Codex available, got %#v", got)
	}
	if got := findRunnerByID(t, chatPayload, string(agentcli.RunnerClaude))["status"]; got != string(agentcli.RunnerStatusDisabledByConfig) {
		t.Fatalf("expected Claude disabled_by_config, got %#v", got)
	}
	if got := findRunnerByID(t, chatPayload, string(agentcli.RunnerGemini))["status"]; got != string(agentcli.RunnerStatusError) {
		t.Fatalf("expected Gemini error, got %#v", got)
	}
	codexCaps, ok := findRunnerByID(t, chatPayload, string(agentcli.RunnerCodex))["chat_capabilities"].(map[string]any)
	if !ok || codexCaps["chatNativeSession"] != true || codexCaps["streamToolEvents"] != true {
		t.Fatalf("expected Codex native/tool chat capabilities to be enabled, got %#v", codexCaps)
	}
	openCodeCaps, ok := findRunnerByID(t, chatPayload, string(agentcli.RunnerOpenCode))["chat_capabilities"].(map[string]any)
	if !ok || openCodeCaps["chatNativeSession"] != true {
		t.Fatalf("expected OpenCode native session capability to remain enabled, got %#v", openCodeCaps)
	}

	agentReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodGet, "/internal/v1/agent-runners", "")
	agentResp, err := fixture.httpServer.Client().Do(agentReq)
	if err != nil {
		t.Fatalf("Do agent-runners: %v", err)
	}
	if agentResp.StatusCode != http.StatusOK {
		defer agentResp.Body.Close()
		t.Fatalf("expected 200, got %d (%s)", agentResp.StatusCode, mustReadBody(t, agentResp.Body))
	}
	agentPayload := decodeServiceResponseMap(t, agentResp)
	rawOpenCode := findRunnerByID(t, agentPayload, string(agentcli.RunnerOpenCode))
	if rawOpenCode["status"] != string(agentcli.RunnerStatusAuthMissing) {
		t.Fatalf("expected raw agent-runners status passthrough, got %#v", rawOpenCode)
	}
	if _, ok := rawOpenCode["chat_capabilities"]; ok {
		t.Fatalf("expected raw agent-runners contract without chat_capabilities, got %#v", rawOpenCode)
	}
}

func TestServiceChatRunners_HidesExternalWhenAgentCLIDisabled(t *testing.T) {
	database, closeDB := openServiceTestDB(t)
	defer closeDB()

	fixture := newRunnerChatServiceFixture(t, config.Default(), database, nil, &agentcli.ChatManager{DB: database})
	defer fixture.httpServer.Close()

	req := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodGet, "/internal/v1/chat-runners", "")
	resp, err := fixture.httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do chat-runners: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	payload := decodeServiceResponseMap(t, resp)
	runners := payload["runners"].([]any)
	if len(runners) != 1 {
		t.Fatalf("expected only OR3 runner when CLI disabled, got %#v", payload)
	}
	if got := runners[0].(map[string]any)["id"]; got != string(agentcli.RunnerOR3) {
		t.Fatalf("expected OR3 runner, got %#v", got)
	}
}

func TestServiceRunnerChat_DisabledWriteAndUnsupportedNative(t *testing.T) {
	t.Run("disabled manager returns 503", func(t *testing.T) {
		database, closeDB := openServiceTestDB(t)
		defer closeDB()
		fixture := newRunnerChatServiceFixture(t, config.Default(), database, nil, &agentcli.ChatManager{DB: database})
		defer fixture.httpServer.Close()

		req := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodPost, "/internal/v1/runner-chat/sessions", `{"app_session_key":"svc:1","runner_id":"opencode"}`)
		resp, err := fixture.httpServer.Client().Do(req)
		if err != nil {
			t.Fatalf("Do create runner session: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
		}
		payload := mustDecodeJSONBody(t, resp.Body)
		if payload["code"] != "agent_cli_disabled" {
			t.Fatalf("expected agent_cli_disabled, got %#v", payload)
		}
	})

	t.Run("unsupported native returns stable code", func(t *testing.T) {
		database, closeDB := openServiceTestDB(t)
		defer closeDB()
		cfg := config.Default()
		cfg.AgentCLI.Enabled = true
		specs := agentcli.AllRunners()
		for i := range specs {
			if specs[i].ID == agentcli.RunnerCodex {
				specs[i].Supports.Chat.ChatNativeSession = false
				specs[i].Supports.Chat.ChatResume = false
				specs[i].Supports.Chat.ChatSessionRefExtractable = false
			}
		}
		registry := agentcli.NewRunnerRegistry(specs, []agentcli.RunnerAdapter{agentcli.NewCodexAdapter()})
		jobs := agent.NewJobRegistry(time.Minute, 32)
		manager := &agentcli.Manager{DB: database, Jobs: jobs, Cfg: cfg.AgentCLI, Registry: registry}
		chatManager := &agentcli.ChatManager{DB: database, Manager: manager, Jobs: jobs}
		fixture := newRunnerChatServiceFixture(t, cfg, database, manager, chatManager)
		defer fixture.httpServer.Close()

		created := mustServiceDoJSON(t, fixture, http.MethodPost, "/internal/v1/runner-chat/sessions", `{"app_session_key":"svc:native","runner_id":"codex","continuation_mode":"replay"}`)
		sessionID := created["id"].(string)

		req := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodPost, "/internal/v1/runner-chat/sessions/"+sessionID+"/turns", `{"user_message":"hello","continuation_mode":"native"}`)
		resp, err := fixture.httpServer.Client().Do(req)
		if err != nil {
			t.Fatalf("Do start native turn: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
		}
		payload := mustDecodeJSONBody(t, resp.Body)
		if payload["code"] != "unsupported_native_session" {
			t.Fatalf("expected unsupported_native_session, got %#v", payload)
		}
	})
}

func TestServiceRunnerChat_ActiveTurnConflictAndAbort(t *testing.T) {
	database, closeDB := openServiceTestDB(t)
	defer closeDB()
	cfg := config.Default()
	cfg.AgentCLI.Enabled = true
	registry := newDiscoveryRegistry()
	jobs := agent.NewJobRegistry(time.Minute, 32)
	manager := &agentcli.Manager{DB: database, Jobs: jobs, Cfg: cfg.AgentCLI, Registry: registry}
	chatManager := &agentcli.ChatManager{DB: database, Manager: manager, Jobs: jobs}
	fixture := newRunnerChatServiceFixture(t, cfg, database, manager, chatManager)
	defer fixture.httpServer.Close()

	created := mustServiceDoJSON(t, fixture, http.MethodPost, "/internal/v1/runner-chat/sessions", `{"app_session_key":"svc:conflict","runner_id":"opencode","continuation_mode":"replay"}`)
	sessionID := created["id"].(string)
	first := mustServiceDoJSON(t, fixture, http.MethodPost, "/internal/v1/runner-chat/sessions/"+sessionID+"/turns", `{"user_message":"first turn"}`)
	turnID := first["turn_id"].(string)

	conflictReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodPost, "/internal/v1/runner-chat/sessions/"+sessionID+"/turns", `{"user_message":"second turn"}`)
	conflictResp, err := fixture.httpServer.Client().Do(conflictReq)
	if err != nil {
		t.Fatalf("Do conflicting start: %v", err)
	}
	defer conflictResp.Body.Close()
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", conflictResp.StatusCode, mustReadBody(t, conflictResp.Body))
	}
	conflict := mustDecodeJSONBody(t, conflictResp.Body)
	if conflict["code"] != "runner_chat_turn_active" {
		t.Fatalf("expected runner_chat_turn_active, got %#v", conflict)
	}

	abortReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodPost, fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns/%s/abort", sessionID, turnID), "")
	abortResp, err := fixture.httpServer.Client().Do(abortReq)
	if err != nil {
		t.Fatalf("Do abort: %v", err)
	}
	defer abortResp.Body.Close()
	if abortResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", abortResp.StatusCode, mustReadBody(t, abortResp.Body))
	}
	aborted := mustDecodeJSONBody(t, abortResp.Body)
	if aborted["status"] != "aborting" {
		t.Fatalf("expected aborting status, got %#v", aborted)
	}
}

func TestServiceRunnerChat_StreamReplaysAfterSeqAndEmitsDoneSnapshot(t *testing.T) {
	database, closeDB := openServiceTestDB(t)
	defer closeDB()
	fixture := newRunnerChatServiceFixture(t, config.Default(), database, nil, &agentcli.ChatManager{DB: database})
	defer fixture.httpServer.Close()

	sess, turn := seedRunnerChatTerminalTurn(t, database)
	path := fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns/%s/stream?after_seq=1", sess.ID, turn.ID)
	req := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodGet, path, "")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := fixture.httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	events := readSSEEvents(t, resp)
	joined := strings.Join(events, "\n")
	if strings.Contains(joined, `"seq":1`) {
		t.Fatalf("expected after_seq filter to skip seq=1, got %s", joined)
	}
	if !strings.Contains(joined, `text_delta|{"id":2,"job_id":"job-stream","seq":2`) {
		t.Fatalf("expected seq=2 replay event, got %s", joined)
	}
	if !strings.Contains(joined, `assistant|{"id":3,"job_id":"job-stream","seq":3`) {
		t.Fatalf("expected seq=3 replay event, got %s", joined)
	}
	if !strings.Contains(joined, `done|{"status":"succeeded"}`) {
		t.Fatalf("expected done snapshot, got %s", joined)
	}
}

func TestServiceRunnerChat_StreamAndListExposeCanonicalPayload(t *testing.T) {
	database, closeDB := openServiceTestDB(t)
	defer closeDB()
	fixture := newRunnerChatServiceFixture(t, config.Default(), database, nil, &agentcli.ChatManager{DB: database})
	defer fixture.httpServer.Close()

	sess, turn := seedRunnerChatTerminalTurn(t, database)
	canonical := `{"type":"content.delta","stream_kind":"command_output","delta":"ok"}`
	if err := database.AppendRunnerChatEvent(context.Background(), db.RunnerChatEvent{TurnID: turn.ID, SessionID: sess.ID, JobID: "job-stream", Seq: 4, Type: "content.delta", Text: "ok", PayloadJSON: canonical}); err != nil {
		t.Fatalf("AppendRunnerChatEvent canonical: %v", err)
	}

	list := mustServiceDoJSON(t, fixture, http.MethodGet, fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns/%s/events?after_seq=3", sess.ID, turn.ID), "")
	items := list["events"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one canonical event, got %#v", list)
	}
	item := items[0].(map[string]any)
	if item["type"] != "content.delta" || item["text"] != "ok" {
		t.Fatalf("expected legacy fields with canonical event, got %#v", item)
	}
	payload := item["payload"].(map[string]any)
	if payload["type"] != "content.delta" || payload["stream_kind"] != "command_output" || payload["delta"] != "ok" {
		t.Fatalf("expected canonical payload in list response, got %#v", payload)
	}

	path := fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns/%s/stream?after_seq=3", sess.ID, turn.ID)
	req := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodGet, path, "")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := fixture.httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, mustReadBody(t, resp.Body))
	}
	joined := strings.Join(readSSEEvents(t, resp), "\n")
	if !strings.Contains(joined, `content.delta|{"id":4,"job_id":"job-stream","payload":{"type":"content.delta","stream_kind":"command_output","delta":"ok"}`) {
		t.Fatalf("expected canonical payload in SSE stream, got %s", joined)
	}
}

func TestServiceChatSessions_LifecyclePaginationAndForkErrors(t *testing.T) {
	database, closeDB := openServiceTestDB(t)
	defer closeDB()
	fixture := newRunnerChatServiceFixture(t, config.Default(), database, nil, &agentcli.ChatManager{DB: database})
	defer fixture.httpServer.Close()
	ctx := context.Background()

	created := mustServiceDoJSON(t, fixture, http.MethodPost, "/internal/v1/chat-sessions", `{"session_key":"svc:history","title":"Original","runner_id":"or3-intern","runner_label":"OR3"}`)
	if created["title"] != "Original" || created["runner_id"] != "or3-intern" {
		t.Fatalf("unexpected create payload %#v", created)
	}

	firstID, err := database.AppendMessage(ctx, "svc:history", "user", "one", map[string]any{"safe": true})
	if err != nil {
		t.Fatalf("AppendMessage first: %v", err)
	}
	secondID, err := database.AppendMessage(ctx, "svc:history", "assistant", "two", map[string]any{"status": "completed"})
	if err != nil {
		t.Fatalf("AppendMessage second: %v", err)
	}
	_, err = database.AppendMessage(ctx, "svc:history", "assistant", "three", map[string]any{"status": "streaming"})
	if err != nil {
		t.Fatalf("AppendMessage third: %v", err)
	}

	list := mustServiceDoJSON(t, fixture, http.MethodGet, "/internal/v1/chat-sessions?include_archived=true&limit=5", "")
	items := list["sessions"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one session, got %#v", list)
	}

	patched := mustServiceDoJSON(t, fixture, http.MethodPatch, "/internal/v1/chat-sessions/svc:history", `{"title":"Renamed","archived":true}`)
	if patched["title"] != "Renamed" || patched["archived"] != true {
		t.Fatalf("unexpected patch payload %#v", patched)
	}

	page := mustServiceDoJSON(t, fixture, http.MethodGet, fmt.Sprintf("/internal/v1/chat-sessions/svc:history/messages?after_id=%d&limit=1", firstID), "")
	messages := page["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["id"].(float64) != float64(secondID) {
		t.Fatalf("expected paginated second message, got %#v", page)
	}

	forkReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodPost, "/internal/v1/chat-sessions/svc:history/fork", fmt.Sprintf(`{"new_session_key":"svc:fork-fail","anchor_message_id":%d}`, secondID+1))
	forkResp, err := fixture.httpServer.Client().Do(forkReq)
	if err != nil {
		t.Fatalf("Do incomplete fork: %v", err)
	}
	defer forkResp.Body.Close()
	if forkResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for incomplete anchor, got %d (%s)", forkResp.StatusCode, mustReadBody(t, forkResp.Body))
	}
	incomplete := mustDecodeJSONBody(t, forkResp.Body)
	if incomplete["code"] != "fork_anchor_incomplete" {
		t.Fatalf("expected fork_anchor_incomplete, got %#v", incomplete)
	}

	success := mustServiceDoJSON(t, fixture, http.MethodPost, "/internal/v1/chat-sessions/svc:history/fork", fmt.Sprintf(`{"new_session_key":"svc:fork-ok","anchor_message_id":%d,"target_runner_id":"opencode","title":"Forked","allow_incomplete_anchor":true}`, secondID+1))
	if success["session_key"] != "svc:fork-ok" || success["parent_session_key"] != "svc:history" || success["runner_id"] != "opencode" {
		t.Fatalf("unexpected fork payload %#v", success)
	}

	badAnchorReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodPost, "/internal/v1/chat-sessions/svc:history/fork", `{"new_session_key":"svc:fork-bad","anchor_message_id":999999}`)
	badAnchorResp, err := fixture.httpServer.Client().Do(badAnchorReq)
	if err != nil {
		t.Fatalf("Do invalid fork: %v", err)
	}
	defer badAnchorResp.Body.Close()
	if badAnchorResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid anchor, got %d (%s)", badAnchorResp.StatusCode, mustReadBody(t, badAnchorResp.Body))
	}
	invalid := mustDecodeJSONBody(t, badAnchorResp.Body)
	if invalid["code"] != "invalid_fork_anchor" {
		t.Fatalf("expected invalid_fork_anchor, got %#v", invalid)
	}
}

func TestServiceRunnerChat_ReadNotFoundAndEventsValidation(t *testing.T) {
	database, closeDB := openServiceTestDB(t)
	defer closeDB()
	fixture := newRunnerChatServiceFixture(t, config.Default(), database, nil, &agentcli.ChatManager{DB: database})
	defer fixture.httpServer.Close()

	missingReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodGet, "/internal/v1/runner-chat/sessions/rcs-missing/turns/rct-missing", "")
	missingResp, err := fixture.httpServer.Client().Do(missingReq)
	if err != nil {
		t.Fatalf("Do missing turn read: %v", err)
	}
	defer missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", missingResp.StatusCode, mustReadBody(t, missingResp.Body))
	}
	missing := mustDecodeJSONBody(t, missingResp.Body)
	if missing["code"] != "runner_chat_turn_not_found" {
		t.Fatalf("expected runner_chat_turn_not_found, got %#v", missing)
	}

	sess, turn := seedRunnerChatTerminalTurn(t, database)
	badReq := mustServiceRequest(t, fixture.httpServer, fixture.secret, http.MethodGet, fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns/%s/events?after_seq=-1", sess.ID, turn.ID), "")
	badResp, err := fixture.httpServer.Client().Do(badReq)
	if err != nil {
		t.Fatalf("Do events validation: %v", err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", badResp.StatusCode, mustReadBody(t, badResp.Body))
	}
	validation := mustDecodeJSONBody(t, badResp.Body)
	if validation["error"] != "invalid after_seq" {
		t.Fatalf("expected invalid after_seq, got %#v", validation)
	}

	events := mustServiceDoJSON(t, fixture, http.MethodGet, fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns/%s/events?after_seq=1&limit=1", sess.ID, turn.ID), "")
	items := events["events"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["seq"].(float64) != 2 {
		t.Fatalf("expected replayed event list from seq 2, got %#v", events)
	}
	if items[0].(map[string]any)["job_id"] != "job-stream" {
		t.Fatalf("expected agent_cli linkage in event payload, got %#v", events)
	}

	read := mustServiceDoJSON(t, fixture, http.MethodGet, fmt.Sprintf("/internal/v1/runner-chat/sessions/%s", sess.ID), "")
	if read["id"] != sess.ID {
		t.Fatalf("expected session read payload, got %#v", read)
	}
	turns := mustServiceDoJSON(t, fixture, http.MethodGet, fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns?limit=1", sess.ID), "")
	turnItems := turns["turns"].([]any)
	if len(turnItems) != 1 || turnItems[0].(map[string]any)["id"] != turn.ID {
		t.Fatalf("expected turn list payload, got %#v", turns)
	}
	turnRead := mustServiceDoJSON(t, fixture, http.MethodGet, fmt.Sprintf("/internal/v1/runner-chat/sessions/%s/turns/%s", sess.ID, turn.ID), "")
	if turnRead["id"] != turn.ID || turnRead["status"] != db.RunnerChatTurnStatusSucceeded {
		t.Fatalf("expected turn read payload, got %#v", turnRead)
	}
	if _, err := json.Marshal(turnRead); err != nil {
		t.Fatalf("expected JSON-safe turn payload: %v", err)
	}
}
