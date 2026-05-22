package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

func TestServiceDoctorStatusAndMetadata(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	server := newDoctorTestServer(t, database, config.Default())

	statusReq := doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/status", "")
	statusRec := httptest.NewRecorder()
	server.handleDoctor(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", statusRec.Code, statusRec.Body.String())
	}
	statusBody := mustDecodeJSONBody(t, statusRec.Body)
	if statusBody["basic_doctor_available"] != true {
		t.Fatalf("expected basic doctor available, got %#v", statusBody)
	}
	if _, ok := statusBody["admin_brain"].(map[string]any); !ok {
		t.Fatalf("expected admin_brain object, got %#v", statusBody["admin_brain"])
	}
	if _, ok := statusBody["finding_cards"].([]any); !ok {
		t.Fatalf("expected finding_cards array, got %#v", statusBody["finding_cards"])
	}

	metaReq := doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/config-metadata", "")
	metaRec := httptest.NewRecorder()
	server.handleDoctor(metaRec, metaReq)
	if metaRec.Code != http.StatusOK {
		t.Fatalf("expected metadata 200, got %d (%s)", metaRec.Code, metaRec.Body.String())
	}
	metaBody := mustDecodeJSONBody(t, metaRec.Body)
	fields, ok := metaBody["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("expected metadata fields, got %#v", metaBody)
	}
}

func TestServiceDoctorSessionsPersistMessagesAndLogs(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	server := newDoctorTestServer(t, database, config.Default())

	createRec := httptest.NewRecorder()
	server.handleDoctor(createRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/sessions", `{"title":"Doctor Session"}`))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	createBody := mustDecodeJSONBody(t, createRec.Body)
	session, ok := createBody["session"].(map[string]any)
	if !ok {
		t.Fatalf("expected session payload, got %#v", createBody)
	}
	sessionKey, _ := session["SessionKey"].(string)
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		t.Fatalf("expected session key, got %#v", session)
	}

	messageRec := httptest.NewRecorder()
	server.handleDoctor(messageRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/sessions/"+sessionKey+"/messages", `{"content":"the service keeps failing to start"}`))
	if messageRec.Code != http.StatusAccepted {
		t.Fatalf("expected message 202, got %d (%s)", messageRec.Code, messageRec.Body.String())
	}
	messageBody := mustDecodeJSONBody(t, messageRec.Body)
	messages, ok := messageBody["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("expected persisted messages, got %#v", messageBody)
	}

	eventsRec := httptest.NewRecorder()
	server.handleDoctor(eventsRec, doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/sessions/"+sessionKey+"/events", ""))
	if eventsRec.Code != http.StatusOK {
		t.Fatalf("expected events 200, got %d (%s)", eventsRec.Code, eventsRec.Body.String())
	}
	eventsBody := mustDecodeJSONBody(t, eventsRec.Body)
	if events, ok := eventsBody["events"].([]any); !ok || len(events) < 2 {
		t.Fatalf("expected doctor events, got %#v", eventsBody)
	}

	logsReq := doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/logs?correlation_id="+sessionKey, "")
	logsRec := httptest.NewRecorder()
	server.handleDoctor(logsRec, logsReq)
	if logsRec.Code != http.StatusOK {
		t.Fatalf("expected logs 200, got %d (%s)", logsRec.Code, logsRec.Body.String())
	}
	logsBody := mustDecodeJSONBody(t, logsRec.Body)
	if items, ok := logsBody["items"].([]any); !ok || len(items) == 0 {
		t.Fatalf("expected diagnostic log items, got %#v", logsBody)
	}
}

func TestServiceDoctorPlanLifecycle(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := config.Default()
	server := newDoctorTestServer(t, database, cfg)

	change := adminflow.SettingsPlanChange{
		ConfigPath: "skills.load.disableGlobalDir",
		Section:    "skills",
		Field:      "skills_global_disabled",
		Operation:  "toggle",
		OldValue:   adminflow.RedactedValue{Value: false},
		NewValue:   adminflow.RedactedValue{Value: true},
	}
	createBody, err := json.Marshal(serviceDoctorPlanCreateRequest{
		ConversationID:    "conv-1",
		AcceptedCardID:    "card-1",
		ApprovedAuthority: "notice",
		Plan: adminflow.SettingsChangePlan{
			Title:   "Disable global skills loading",
			Summary: "Stop loading skills from the shared global directory.",
			Changes: []adminflow.SettingsPlanChange{change},
			PostApplyChecks: []adminflow.PostApplyCheck{
				{ID: "config.validate", Description: "Validate current config", Timeout: 1},
				{ID: "doctor.configure_post_save", Description: "Run doctor post-save checks", Timeout: 1},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	createRec := httptest.NewRecorder()
	server.handleDoctor(createRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans", string(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	createPayload := mustDecodeJSONBody(t, createRec.Body)
	planBody, ok := createPayload["plan"].(map[string]any)
	if !ok {
		t.Fatalf("expected plan payload, got %#v", createPayload)
	}
	planID := strings.TrimSpace(planBody["id"].(string))
	if planID == "" {
		t.Fatalf("expected persisted plan id, got %#v", planBody)
	}

	applyRec := httptest.NewRecorder()
	server.handleDoctor(applyRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans/"+planID+"/apply", `{"approval":{"approved":true}}`))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("expected apply 200, got %d (%s)", applyRec.Code, applyRec.Body.String())
	}
	applyPayload := mustDecodeJSONBody(t, applyRec.Body)
	rollbackID, _ := applyPayload["rollback_id"].(string)
	if strings.TrimSpace(rollbackID) == "" {
		t.Fatalf("expected rollback id, got %#v", applyPayload)
	}
	loaded, err := config.Load(server.configPath)
	if err != nil {
		t.Fatalf("reload config after apply: %v", err)
	}
	if !loaded.Skills.Load.DisableGlobalDir {
		t.Fatalf("expected skill global dir to be disabled after apply")
	}

	postCheckRec := httptest.NewRecorder()
	server.handleDoctor(postCheckRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans/"+planID+"/post-checks", `{}`))
	if postCheckRec.Code != http.StatusOK {
		t.Fatalf("expected post-check 200, got %d (%s)", postCheckRec.Code, postCheckRec.Body.String())
	}
	postCheckPayload := mustDecodeJSONBody(t, postCheckRec.Body)
	if postCheckPayload["status"] != "complete" {
		t.Fatalf("expected complete post-check status, got %#v", postCheckPayload)
	}
	results, ok := postCheckPayload["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("expected per-check results, got %#v", postCheckPayload)
	}

	readRec := httptest.NewRecorder()
	server.handleDoctor(readRec, doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/plans/"+planID, ""))
	if readRec.Code != http.StatusOK {
		t.Fatalf("expected read 200, got %d (%s)", readRec.Code, readRec.Body.String())
	}
	readPayload := mustDecodeJSONBody(t, readRec.Body)
	if readPayload["status"] != "post_checked" {
		t.Fatalf("expected post_checked status, got %#v", readPayload)
	}

	rollbackRec := httptest.NewRecorder()
	server.handleDoctor(rollbackRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans/"+planID+"/rollback", `{}`))
	if rollbackRec.Code != http.StatusOK {
		t.Fatalf("expected rollback 200, got %d (%s)", rollbackRec.Code, rollbackRec.Body.String())
	}
	loaded, err = config.Load(server.configPath)
	if err != nil {
		t.Fatalf("reload config after rollback: %v", err)
	}
	if loaded.Skills.Load.DisableGlobalDir {
		t.Fatalf("expected skill global dir disable flag to be restored after rollback")
	}

	record, ok, err := database.GetSettingsChangePlan(context.Background(), planID)
	if err != nil {
		t.Fatalf("GetSettingsChangePlan: %v", err)
	}
	if !ok || record.RollbackID != rollbackID {
		t.Fatalf("expected persisted rollback id %q, got ok=%t record=%#v", rollbackID, ok, record)
	}
	eventTypes := queryDoctorAuditEventTypes(t, database)
	for _, want := range []string{"doctor.plan.created", "doctor.plan.applied", "doctor.checkpoint.created", "doctor.checkpoint.completed", "doctor.post_check.completed", "doctor.plan.rollback"} {
		if !containsString(eventTypes, want) {
			t.Fatalf("expected audit event %q, got %#v", want, eventTypes)
		}
	}
}

func TestServiceDoctorPlanLifecycle_AuditsRedactedSecretPlanValues(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := config.Default()
	cfg.Skills.Entries = map[string]config.SkillEntryConfig{"demo": {APIKey: "old-secret-value"}}
	server := newDoctorTestServer(t, database, cfg)

	change := adminflow.SettingsPlanChange{
		ConfigPath: "skills.entries.demo.apiKey",
		Section:    "skills_entry",
		Channel:    "demo",
		Field:      "api_key",
		Operation:  "set",
		OldValue:   adminflow.RedactedValue{Value: "old-secret-value"},
		NewValue:   adminflow.RedactedValue{Value: "new-secret-value"},
	}
	body, err := json.Marshal(serviceDoctorPlanCreateRequest{
		ApprovedAuthority: "warning",
		Plan: adminflow.SettingsChangePlan{
			Title:              "Rotate demo skill API key",
			Summary:            "Update the stored API key for the demo skill.",
			Changes:            []adminflow.SettingsPlanChange{change},
			RequiresApproval:   true,
			RequiresStepUpAuth: true,
		},
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	createRec := httptest.NewRecorder()
	server.handleDoctor(createRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans", string(body)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	createPayload := mustDecodeJSONBody(t, createRec.Body)
	planBody, ok := createPayload["plan"].(map[string]any)
	if !ok {
		t.Fatalf("expected plan payload, got %#v", createPayload)
	}
	planID, _ := planBody["id"].(string)
	applyRec := httptest.NewRecorder()
	server.handleDoctor(applyRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans/"+planID+"/apply", `{"approval":{"approved":true}}`))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("expected apply 200, got %d (%s)", applyRec.Code, applyRec.Body.String())
	}
	var payloadJSON string
	row := database.SQL.QueryRow(`SELECT payload_json FROM audit_events WHERE event_type='doctor.plan.applied' ORDER BY id DESC LIMIT 1`)
	if err := row.Scan(&payloadJSON); err != nil {
		t.Fatalf("scan audit payload: %v", err)
	}
	if strings.Contains(payloadJSON, "old-secret-value") || strings.Contains(payloadJSON, "new-secret-value") {
		t.Fatalf("expected audit payload to redact secret values, got %s", payloadJSON)
	}
	if !strings.Contains(payloadJSON, `"redacted":true`) {
		t.Fatalf("expected audit payload to preserve redaction markers, got %s", payloadJSON)
	}
	var logPayload string
	row = database.SQL.QueryRow(`SELECT payload_json FROM diagnostic_log_events WHERE event_type='doctor.plan.create' ORDER BY id DESC LIMIT 1`)
	if err := row.Scan(&logPayload); err != nil {
		t.Fatalf("scan diagnostic log payload: %v", err)
	}
	if strings.Contains(logPayload, "old-secret-value") || strings.Contains(logPayload, "new-secret-value") {
		t.Fatalf("expected diagnostic log payload to redact secret values, got %s", logPayload)
	}
}

func TestServiceDoctorPlanApplyRejectsMismatchedApprovalPlanID(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	cfg := config.Default()
	cfg.Skills.Entries = map[string]config.SkillEntryConfig{"demo": {Config: map[string]any{"managed_reference": "managed://cred-1"}}}
	server := newDoctorTestServer(t, database, cfg)

	body, err := json.Marshal(serviceDoctorPlanCreateRequest{
		ApprovedAuthority: "warning",
		Plan: adminflow.SettingsChangePlan{
			Title: "Clear stale managed reference",
			Changes: []adminflow.SettingsPlanChange{{
				ConfigPath: "skills.entries.demo.config.managed_reference",
				Section:    "skills_entry",
				Channel:    "demo",
				Field:      "config.managed_reference",
				Operation:  "set",
				OldValue:   adminflow.RedactedValue{Value: "managed://cred-1"},
				NewValue:   adminflow.RedactedValue{Value: "clear"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	createRec := httptest.NewRecorder()
	server.handleDoctor(createRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans", string(body)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	planID := mustDecodeJSONBody(t, createRec.Body)["plan"].(map[string]any)["id"].(string)

	applyRec := httptest.NewRecorder()
	server.handleDoctor(applyRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans/"+planID+"/apply", `{"approval":{"plan_id":"different","approved":true}}`))
	if applyRec.Code != http.StatusBadRequest {
		t.Fatalf("expected apply 400, got %d (%s)", applyRec.Code, applyRec.Body.String())
	}
	loaded, err := config.Load(server.configPath)
	if err != nil {
		t.Fatalf("reload config after rejected apply: %v", err)
	}
	if got := loaded.Skills.Entries["demo"].Config["managed_reference"]; got != "managed://cred-1" {
		t.Fatalf("config mutated after rejected apply, got %#v", got)
	}
}

func TestServiceDoctorPlanPostChecks_ReportFailureStatus(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	server := newDoctorTestServer(t, database, config.Default())

	createBody, err := json.Marshal(serviceDoctorPlanCreateRequest{
		ApprovedAuthority: "notice",
		Plan: adminflow.SettingsChangePlan{
			Title:   "Disable global skills loading",
			Summary: "Stop loading skills from the shared global directory.",
			Changes: []adminflow.SettingsPlanChange{{
				ConfigPath: "skills.load.disableGlobalDir",
				Section:    "skills",
				Field:      "skills_global_disabled",
				Operation:  "toggle",
				OldValue:   adminflow.RedactedValue{Value: false},
				NewValue:   adminflow.RedactedValue{Value: true},
			}},
			PostApplyChecks: []adminflow.PostApplyCheck{{ID: "unsupported.check", Description: "Unsupported test check", Timeout: 1}},
		},
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	createRec := httptest.NewRecorder()
	server.handleDoctor(createRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans", string(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d (%s)", createRec.Code, createRec.Body.String())
	}
	planID := mustDecodeJSONBody(t, createRec.Body)["plan"].(map[string]any)["id"].(string)

	applyRec := httptest.NewRecorder()
	server.handleDoctor(applyRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans/"+planID+"/apply", `{"approval":{"approved":true}}`))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("expected apply 200, got %d (%s)", applyRec.Code, applyRec.Body.String())
	}
	postCheckRec := httptest.NewRecorder()
	server.handleDoctor(postCheckRec, doctorAuthedRequest(http.MethodPost, "/internal/v1/doctor/plans/"+planID+"/post-checks", `{}`))
	if postCheckRec.Code != http.StatusOK {
		t.Fatalf("expected post-check 200, got %d (%s)", postCheckRec.Code, postCheckRec.Body.String())
	}
	postCheckPayload := mustDecodeJSONBody(t, postCheckRec.Body)
	if postCheckPayload["status"] != "failed" {
		t.Fatalf("expected failed post-check status, got %#v", postCheckPayload)
	}
	readRec := httptest.NewRecorder()
	server.handleDoctor(readRec, doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/plans/"+planID, ""))
	if readRec.Code != http.StatusOK {
		t.Fatalf("expected read 200, got %d (%s)", readRec.Code, readRec.Body.String())
	}
	if mustDecodeJSONBody(t, readRec.Body)["status"] != "post_check_failed" {
		t.Fatalf("expected persisted post_check_failed status")
	}
}

func TestServiceDoctorSkillDiagnostics(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	root := filepath.Join(t.TempDir(), "skills")
	skillDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: demo
description: Demo skill
---
# Demo
`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.diagnostic.yaml"), []byte(`version: 1
checks:
  - id: stale_managed_reference
    kind: config
    label: Managed reference
    summary: Stale managed reference is still configured
    severity: warning
    config_key: managed_reference
    require_absent: true
known_fixes:
  - id: clear_managed_reference
    summary: Clear the stale managed reference
    match_check: stale_managed_reference
    match_status: fail
    risk: warning
    restart_required: true
    change:
      type: config
      key: managed_reference
      clear: true
`), 0o644); err != nil {
		t.Fatalf("write diagnostic manifest: %v", err)
	}
	cfg := config.Default()
	cfg.Skills.Load.GlobalDir = root
	cfg.Skills.Entries = map[string]config.SkillEntryConfig{
		"demo": {Config: map[string]any{"managed_reference": "managed://cred-1"}},
	}
	server := newDoctorTestServer(t, database, cfg)

	rec := httptest.NewRecorder()
	server.handleDoctor(rec, doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/skills/demo/diagnostics", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := mustDecodeJSONBody(t, rec.Body)
	diagnostics, ok := body["diagnostics"].(map[string]any)
	if !ok || diagnostics["status"] != "issues" {
		t.Fatalf("expected diagnostics issues payload, got %#v", body)
	}
	plans, ok := body["plans"].([]any)
	if !ok || len(plans) != 1 {
		t.Fatalf("expected one suggested plan, got %#v", body)
	}
	planJSON, err := json.Marshal(plans[0])
	if err != nil {
		t.Fatalf("marshal suggested plan: %v", err)
	}
	var plan adminflow.SettingsChangePlan
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatalf("decode suggested plan: %v", err)
	}
	if _, err := (adminflow.PlanValidator{}).Stage(cfg, &plan, adminflow.ValidationOptions{ApprovedAuthority: "warning"}); err != nil {
		t.Fatalf("suggested plan should validate: %v plan=%#v", err, plan)
	}
}

func TestServiceDoctorSkillDiagnostics_DoesNotLeakSecretValuesInPlans(t *testing.T) {
	database, cleanup := openServiceTestDB(t)
	defer cleanup()
	root := filepath.Join(t.TempDir(), "skills")
	skillDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.diagnostic.yaml"), []byte(`version: 1
checks:
  - id: env-token
    kind: env
    label: Token
    summary: Token env is configured
    env_key: API_TOKEN
known_fixes:
  - id: clear_api_token
    summary: Clear stored API token
    match_check: env-token
    match_status: fail
    risk: warning
    change:
      type: env
      key: API_TOKEN
      clear: true
`), 0o644); err != nil {
		t.Fatalf("write diagnostic manifest: %v", err)
	}
	cfg := config.Default()
	cfg.Skills.Load.GlobalDir = root
	cfg.Skills.Entries = map[string]config.SkillEntryConfig{
		"demo": {Env: map[string]string{"API_TOKEN": "super-secret-token"}},
	}
	server := newDoctorTestServer(t, database, cfg)

	rec := httptest.NewRecorder()
	server.handleDoctor(rec, doctorAuthedRequest(http.MethodGet, "/internal/v1/doctor/skills/demo/diagnostics", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret-token") {
		t.Fatalf("expected diagnostics response to redact secret values, got %s", rec.Body.String())
	}
}

func newDoctorTestServer(t *testing.T, database *db.DB, cfg config.Config) *serviceServer {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "or3-intern.json")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return &serviceServer{
		config:     cfg,
		configPath: cfgPath,
		jobs:       agent.NewJobRegistry(time.Minute, 32),
		runtime:    &agent.Runtime{DB: database, Audit: &security.AuditLogger{DB: database, Key: []byte(strings.Repeat("a", 32))}},
	}
}

func doctorAuthedRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	return req.WithContext(serviceContextWithAuthIdentity(req.Context(), serviceAuthIdentity{Kind: "auth-session", Actor: "user:test", Role: approval.RoleAdmin, Session: "session-1", StepUpOK: true}))
}

func queryDoctorAuditEventTypes(t *testing.T, database *db.DB) []string {
	t.Helper()
	rows, err := database.SQL.Query(`SELECT event_type FROM audit_events WHERE event_type LIKE 'doctor.%' ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan audit event: %v", err)
		}
		items = append(items, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit events: %v", err)
	}
	return items
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
