package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"or3-intern/internal/adminflow"
	"or3-intern/internal/configmeta"
	"or3-intern/internal/db"
	"or3-intern/internal/doctor"
	"or3-intern/internal/skilldiag"
	"or3-intern/internal/tools"
)

const (
	doctorToolNameStatus           = "doctor_status"
	doctorToolNameLogs             = "doctor_logs"
	doctorToolNameDocsSearch       = "doctor_docs_search"
	doctorToolNameConfigSearch     = "doctor_config_search"
	doctorToolNameConfigMetadata   = "doctor_config_metadata"
	doctorToolNameSkillDiagnostics = "doctor_skill_diagnostics"
	doctorToolNameCreatePlan       = "doctor_create_plan"
	doctorToolNameReadPlan         = "doctor_read_plan"
	doctorToolNameRunPostChecks    = "doctor_run_post_checks"
)

type doctorServiceTool struct {
	tools.Base
	server *serviceServer
	name   string
	desc   string
	params map[string]any
	run    func(context.Context, map[string]any) (string, error)
}

func (t doctorServiceTool) Name() string                      { return t.name }
func (t doctorServiceTool) Description() string               { return t.desc }
func (t doctorServiceTool) Parameters() map[string]any        { return t.params }
func (t doctorServiceTool) Schema() map[string]any            { return t.SchemaFor(t.name, t.desc, t.params) }
func (t doctorServiceTool) Capability() tools.CapabilityLevel { return tools.CapabilitySafe }
func (t doctorServiceTool) Metadata() tools.ToolMetadata {
	return tools.ToolMetadata{Groups: []string{tools.ToolGroupService}, Capabilities: []string{string(tools.CapabilitySafe)}}
}
func (t doctorServiceTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.server == nil {
		return "", fmt.Errorf("doctor service unavailable")
	}
	if params == nil {
		params = map[string]any{}
	}
	return t.run(ctx, params)
}

func (s *serviceServer) registerDoctorAdminBrainTools() {
	if s == nil || s.runtime == nil {
		return
	}
	if s.runtime.Tools == nil {
		s.runtime.Tools = tools.NewRegistry()
	}
	for _, tool := range s.doctorAdminBrainTools() {
		s.runtime.Tools.Register(tool)
	}
}

func (s *serviceServer) doctorAdminBrainTools() []tools.Tool {
	return []tools.Tool{
		doctorServiceTool{
			server: s,
			name:   doctorToolNameStatus,
			desc:   "Return the current Basic Doctor status, readiness, finding cards, recent diagnostic logs, and pending recovery summary. Use this first for evidence before proposing repairs.",
			params: map[string]any{"type": "object", "properties": map[string]any{}},
			run:    s.executeDoctorStatusTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameLogs,
			desc:   "Query redacted Doctor diagnostic logs. Use this to inspect recent known failures, startup errors, and repair audit events without reading arbitrary files.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"source":                map[string]any{"type": "string", "description": "Optional log source filter, such as doctor, service, app, or runner."},
				"level":                 map[string]any{"type": "string", "description": "Optional level filter, such as info, warn, or error."},
				"correlation_id":        map[string]any{"type": "string", "description": "Optional correlation, session, plan, or checkpoint ID."},
				"event_type":            map[string]any{"type": "string", "description": "Optional exact event type filter."},
				"pattern":               map[string]any{"type": "string", "description": "Optional redacted text pattern to search in log payloads."},
				"known_failure_pattern": map[string]any{"type": "string", "description": "Alias for pattern when looking for a known failure signature."},
				"since_ms":              map[string]any{"type": "integer", "description": "Optional lower bound in Unix milliseconds."},
				"until_ms":              map[string]any{"type": "integer", "description": "Optional upper bound in Unix milliseconds."},
				"limit":                 map[string]any{"type": "integer", "description": "Maximum rows to return. Defaults to 100 and is capped at 250."},
			}},
			run: s.executeDoctorLogsTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameDocsSearch,
			desc:   "Search the bundled OR3 v1 documentation and return short redacted snippets with doc paths. Use this when explaining how OR3 works or before giving setup guidance.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Plain-language search query, for example agent runtime tools, config, service auth, or v1 docs."},
				"limit": map[string]any{"type": "integer", "description": "Maximum documentation matches to return. Defaults to 5 and is capped at 8."},
			}, "required": []string{"query"}},
			run: s.executeDoctorDocsSearchTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameConfigSearch,
			desc:   "Search Doctor-safe OR3 config fields and redacted current values. Use this before proposing config edits; actual changes must be created with doctor_create_plan so the UI can show an Apply button.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"query":                  map[string]any{"type": "string", "description": "Plain-language setting or problem to search for, such as provider key, exec, auth, runner, or service."},
				"section":                map[string]any{"type": "string", "description": "Optional section filter, such as provider, tools, skills, auth, service, or agentCLI."},
				"path":                   map[string]any{"type": "string", "description": "Optional config path substring filter."},
				"limit":                  map[string]any{"type": "integer", "description": "Maximum fields to return. Defaults to 10 and is capped at 25."},
				"include_current_values": map[string]any{"type": "boolean", "description": "Whether to include redacted current values. Defaults to true."},
			}},
			run: s.executeDoctorConfigSearchTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameConfigMetadata,
			desc:   "Return backend-owned configuration metadata, including safe field paths, risk levels, restart requirements, validation rules, and rollback behavior. Use this before creating a settings plan.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"section": map[string]any{"type": "string", "description": "Optional section filter, such as skills, service, modelRouting, channels, or hardening."},
			}},
			run: s.executeDoctorConfigMetadataTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameSkillDiagnostics,
			desc:   "Run a skill's declared diagnostic manifest and return redacted findings plus suggested settings plans. Use this for skill-specific setup and API key problems.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"skill": map[string]any{"type": "string", "description": "Skill name or key to diagnose."},
			}, "required": []string{"skill"}},
			run: s.executeDoctorSkillDiagnosticsTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameCreatePlan,
			desc:   "Validate and persist a structured OR3 settings-change plan. This does not apply changes; the UI must present cards and collect user approval before apply.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"conversation_id":    map[string]any{"type": "string", "description": "Current Doctor conversation/session ID."},
				"accepted_card_id":   map[string]any{"type": "string", "description": "Optional finding or recommendation card ID that led to this plan."},
				"approved_authority": map[string]any{"type": "string", "enum": []string{"safe", "notice", "warning", "danger"}, "description": "Maximum risk authority for validation. Omit unless the user explicitly scoped authority."},
				"plan":               map[string]any{"type": "object", "description": "SettingsChangePlan JSON with title, summary, changes, and optional post_apply_checks."},
			}, "required": []string{"plan"}},
			run: s.executeDoctorCreatePlanTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameReadPlan,
			desc:   "Read a persisted Doctor settings-change plan, status, rollback ID, checkpoint, and post-check state. Use this after creating or applying a plan.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Plan ID returned by doctor_create_plan or visible in a Doctor card."},
			}, "required": []string{"plan_id"}},
			run: s.executeDoctorReadPlanTool,
		},
		doctorServiceTool{
			server: s,
			name:   doctorToolNameRunPostChecks,
			desc:   "Run post-apply checks for an already applied Doctor plan and update its checkpoint. Use after the UI applies a plan or after restart recovery.",
			params: map[string]any{"type": "object", "properties": map[string]any{
				"plan_id": map[string]any{"type": "string", "description": "Applied plan ID whose post-check checkpoint should run."},
			}, "required": []string{"plan_id"}},
			run: s.executeDoctorRunPostChecksTool,
		},
	}
}

func (s *serviceServer) executeDoctorStatusTool(ctx context.Context, params map[string]any) (string, error) {
	_ = params
	configmeta.EnsureFirstSliceFieldsRegistered()
	report := doctor.Evaluate(s.config, doctor.Options{Mode: doctor.ModeAdvisory})
	response := s.buildDoctorStatusResponse(nil, report)
	summary := fmt.Sprintf("Basic Doctor found %d blocking, %d error, and %d warning findings.", report.Summary.BlockCount, report.Summary.ErrorCount, report.Summary.WarnCount)
	return encodeDoctorToolResult("doctor_status", true, summary, map[string]any{
		"admin_brain":      response["admin_brain"],
		"health":           response["health"],
		"readiness":        response["readiness"],
		"report":           response["report"],
		"finding_cards":    response["finding_cards"],
		"recent_logs":      response["recent_logs"],
		"pending_recovery": response["pending_recovery"],
	}), nil
}

func (s *serviceServer) executeDoctorLogsTool(ctx context.Context, params map[string]any) (string, error) {
	store := s.doctorDB()
	if store == nil {
		return "", fmt.Errorf("doctor database unavailable")
	}
	limit := doctorToolInt(params, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 250 {
		limit = 250
	}
	sinceMS := doctorToolInt64(params, "since_ms", 0)
	untilMS := doctorToolInt64(params, "until_ms", 0)
	if sinceMS > 0 && untilMS > 0 && sinceMS > untilMS {
		return "", fmt.Errorf("since_ms must be before until_ms")
	}
	items, err := store.QueryDiagnosticLogEvents(ctx, db.DiagnosticLogQuery{
		Source:        doctorToolString(params, "source"),
		Level:         doctorToolString(params, "level"),
		CorrelationID: doctorToolString(params, "correlation_id"),
		EventType:     doctorToolString(params, "event_type"),
		Pattern:       serviceFirstNonEmpty(doctorToolString(params, "pattern"), doctorToolString(params, "known_failure_pattern")),
		SinceUnixMS:   sinceMS,
		UntilUnixMS:   untilMS,
		Limit:         limit,
	})
	if err != nil {
		return "", err
	}
	summary := fmt.Sprintf("Returned %d redacted Doctor diagnostic log events.", len(items))
	return encodeDoctorToolResult("doctor_logs", true, summary, map[string]any{"items": items, "count": len(items)}), nil
}

func (s *serviceServer) executeDoctorConfigMetadataTool(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	configmeta.EnsureFirstSliceFieldsRegistered()
	section := doctorToolString(params, "section")
	fields := configmeta.List()
	if section != "" {
		filtered := fields[:0]
		for _, field := range fields {
			if strings.EqualFold(strings.TrimSpace(field.Section), section) {
				filtered = append(filtered, field)
			}
		}
		fields = filtered
	}
	sort.Slice(fields, func(i, j int) bool {
		left := strings.TrimSpace(fields[i].Path)
		right := strings.TrimSpace(fields[j].Path)
		if left == right {
			return fields[i].Key < fields[j].Key
		}
		return left < right
	})
	summary := fmt.Sprintf("Returned %d Doctor-safe config metadata fields.", len(fields))
	return encodeDoctorToolResult("doctor_config_metadata", true, summary, map[string]any{"fields": fields, "count": len(fields), "section": section}), nil
}

func (s *serviceServer) executeDoctorDocsSearchTool(ctx context.Context, params map[string]any) (string, error) {
	query := doctorToolString(params, "query")
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := doctorToolInt(params, "limit", 5)
	if limit <= 0 {
		limit = 5
	}
	if limit > 8 {
		limit = 8
	}
	docsDir, err := doctorDocsV1Dir(s.configPath)
	if err != nil {
		return encodeDoctorToolResult("doctor_docs_search", false, "OR3 v1 documentation is not available on this host.", map[string]any{"query": query, "error": err.Error()}), nil
	}
	terms := doctorSearchTerms(query)
	if len(terms) == 0 {
		return encodeDoctorToolResult("doctor_docs_search", true, "Found 0 OR3 v1 documentation matches.", map[string]any{"query": query, "count": 0, "results": []map[string]any{}}), nil
	}
	type docMatch struct {
		Score  int
		Result map[string]any
	}
	matches := []docMatch{}
	walkErr := filepath.WalkDir(docsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 256*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		rel, _ := filepath.Rel(docsDir, path)
		rel = filepath.ToSlash(filepath.Join("docs", "v1", rel))
		title := doctorDocsTitle(content, rel)
		score := doctorDocsScore(rel, title, content, terms, query)
		if score == 0 {
			return nil
		}
		matches = append(matches, docMatch{Score: score, Result: map[string]any{
			"path":    rel,
			"title":   adminflow.SanitizeForAI(title),
			"snippet": doctorDocsSnippet(content, terms),
			"score":   score,
		}})
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return fmt.Sprint(matches[i].Result["path"]) < fmt.Sprint(matches[j].Result["path"])
		}
		return matches[i].Score > matches[j].Score
	})
	results := make([]map[string]any, 0, minInt(limit, len(matches)))
	for i, item := range matches {
		if i == limit {
			break
		}
		results = append(results, item.Result)
	}
	summary := fmt.Sprintf("Found %d OR3 v1 documentation matches.", len(results))
	return encodeDoctorToolResult("doctor_docs_search", true, summary, map[string]any{"query": query, "count": len(results), "results": results}), nil
}

func (s *serviceServer) executeDoctorConfigSearchTool(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	configmeta.EnsureFirstSliceFieldsRegistered()
	query := doctorToolString(params, "query")
	section := doctorToolString(params, "section")
	pathFilter := doctorToolString(params, "path")
	includeCurrent := doctorToolBool(params, "include_current_values", true)
	limit := doctorToolInt(params, "limit", 10)
	if limit <= 0 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}
	type configMatch struct {
		Score int
		Path  string
		Item  map[string]any
	}
	matches := []configMatch{}
	for _, field := range configmeta.List() {
		if section != "" && !strings.EqualFold(strings.TrimSpace(field.Section), section) {
			continue
		}
		if pathFilter != "" && !strings.Contains(strings.ToLower(field.Path), strings.ToLower(pathFilter)) {
			continue
		}
		score := doctorConfigFieldScore(field, query)
		if strings.TrimSpace(query) != "" && score == 0 {
			continue
		}
		item := doctorConfigSearchItem(s, field, includeCurrent)
		matches = append(matches, configMatch{Score: score, Path: field.Path, Item: item})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Score > matches[j].Score
	})
	fields := make([]map[string]any, 0, minInt(limit, len(matches)))
	for i, item := range matches {
		if i == limit {
			break
		}
		fields = append(fields, item.Item)
	}
	summary := fmt.Sprintf("Found %d Doctor-safe config fields.", len(fields))
	return encodeDoctorToolResult("doctor_config_search", true, summary, map[string]any{"query": query, "section": section, "path": pathFilter, "count": len(fields), "fields": fields}), nil
}

func (s *serviceServer) executeDoctorSkillDiagnosticsTool(ctx context.Context, params map[string]any) (string, error) {
	skillName := doctorToolString(params, "skill")
	if skillName == "" {
		return "", fmt.Errorf("skill is required")
	}
	inv := s.serviceSkillsInventory(ctx, s.config)
	skill, ok := inv.Get(skillName)
	if !ok {
		return "", fmt.Errorf("skill %q not found", skillName)
	}
	entry := s.config.Skills.Entries[serviceSkillEntryKey(skill)]
	result, err := skilldiag.Evaluate(ctx, skill.Dir, skilldiag.Options{
		Entry: skilldiag.SkillEntryState{
			SkillKey: serviceSkillEntryKey(skill),
			Enabled:  entry.Enabled,
			APIKey:   entry.APIKey,
			Env:      cloneSkillEnv(entry.Env),
			Config:   cloneSkillConfig(entry.Config),
		},
		Runner: skilldiag.ExecRunner{},
	})
	plans := serviceDoctorPlansFromSkillDiag(result.SuggestedPlans)
	summary := fmt.Sprintf("Skill %s diagnostics status: %s; suggested plans: %d.", skill.Name, result.Status, len(plans))
	stats := map[string]any{"skill": serviceSkillItemFromMeta(skill, s.config.Skills), "diagnostics": result, "plans": plans}
	if err != nil {
		stats["error"] = err.Error()
		return encodeDoctorToolResult("doctor_skill_diagnostics", false, summary+" "+err.Error(), stats), nil
	}
	return encodeDoctorToolResult("doctor_skill_diagnostics", true, summary, stats), nil
}

func (s *serviceServer) executeDoctorCreatePlanTool(ctx context.Context, params map[string]any) (string, error) {
	store := s.doctorDB()
	if store == nil {
		return "", fmt.Errorf("doctor database unavailable")
	}
	var plan adminflow.SettingsChangePlan
	if err := doctorToolDecode(params["plan"], &plan); err != nil {
		return "", fmt.Errorf("invalid plan: %w", err)
	}
	if strings.TrimSpace(plan.ID) == "" {
		plan.ID = newDoctorID("scp")
	}
	if strings.TrimSpace(plan.CreatedBy) == "" {
		plan.CreatedBy = "doctor-admin-brain"
	}
	state, err := (adminflow.PlanValidator{}).Stage(s.config, &plan, adminflow.ValidationOptions{ApprovedAuthority: configmeta.RiskLevel(doctorToolString(params, "approved_authority"))})
	if err != nil {
		return encodeDoctorToolResult("doctor_plan", false, err.Error(), map[string]any{"plan": plan, "validation": state.Validation, "error": err.Error()}), nil
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	approvalJSON, _ := json.Marshal(adminflow.ApprovalContext{PlanID: plan.ID})
	liveReloadJSON, _ := json.Marshal(state.LiveReloadKeys)
	conversationID := doctorToolString(params, "conversation_id")
	acceptedCardID := doctorToolString(params, "accepted_card_id")
	if err := store.CreateSettingsChangePlan(ctx, db.SettingsChangePlanRecord{
		ID:             plan.ID,
		Status:         "validated",
		ConversationID: conversationID,
		AcceptedCardID: acceptedCardID,
		CreatedBy:      plan.CreatedBy,
		PlanJSON:       string(planJSON),
		ApprovalJSON:   string(approvalJSON),
		LiveReloadJSON: string(liveReloadJSON),
	}); err != nil {
		return "", err
	}
	identity := serviceAuthIdentityFromContext(ctx)
	_ = s.recordDoctorAudit(ctx, identity, "doctor.plan.created", serviceDoctorAuditPlanPayload(plan, identity, adminflow.ApprovalContext{}, map[string]any{
		"conversation_id":  conversationID,
		"accepted_card_id": acceptedCardID,
		"live_reloaded":    state.LiveReloadKeys,
		"validated_at":     db.NowMS(),
		"source":           "doctor_tool",
	}))
	_ = s.appendDoctorLog(ctx, db.DiagnosticLogEvent{Source: "doctor", Level: "info", CorrelationID: plan.ID, EventType: "doctor.plan.create", Payload: json.RawMessage(serviceDoctorMustJSON(serviceDoctorRedactedPlanForAudit(plan)))})
	summary := fmt.Sprintf("Created validated Doctor settings plan %s: %s", plan.ID, serviceFirstNonEmpty(plan.Title, plan.Summary))
	return encodeDoctorToolResult("doctor_plan", true, summary, map[string]any{
		"card_type":     "settings_change_preview",
		"plan":          plan,
		"plan_id":       plan.ID,
		"doctor_report": state.DoctorReport,
		"live_reloaded": state.LiveReloadKeys,
		"status":        "validated",
	}), nil
}

func (s *serviceServer) executeDoctorReadPlanTool(ctx context.Context, params map[string]any) (string, error) {
	planID := doctorToolString(params, "plan_id")
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	record, plan, ok := s.loadDoctorPlan(ctx, planID)
	if !ok {
		return "", fmt.Errorf("plan %q not found", planID)
	}
	stats := map[string]any{
		"card_type":          "settings_change_preview",
		"plan":               plan,
		"plan_id":            plan.ID,
		"status":             record.Status,
		"approval":           json.RawMessage(record.ApprovalJSON),
		"live_reloaded":      json.RawMessage(record.LiveReloadJSON),
		"rollback_id":        record.RollbackID,
		"post_check_pending": record.PostCheckPending,
		"error":              record.ErrorText,
	}
	if store := s.doctorDB(); store != nil {
		if checkpoint, ok, err := store.GetLatestDoctorCheckpointForPlan(ctx, planID); err == nil && ok {
			stats["checkpoint"] = checkpoint
		}
	}
	summary := fmt.Sprintf("Read Doctor settings plan %s with status %s.", plan.ID, record.Status)
	return encodeDoctorToolResult("doctor_plan", true, summary, stats), nil
}

func (s *serviceServer) executeDoctorRunPostChecksTool(ctx context.Context, params map[string]any) (string, error) {
	store := s.doctorDB()
	if store == nil {
		return "", fmt.Errorf("doctor database unavailable")
	}
	planID := doctorToolString(params, "plan_id")
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	record, _, ok := s.loadDoctorPlan(ctx, planID)
	if !ok {
		return "", fmt.Errorf("plan %q not found", planID)
	}
	checkpoint, ok, err := store.GetLatestDoctorCheckpointForPlan(ctx, planID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("post-check checkpoint not found for plan %q", planID)
	}
	checks := []adminflow.PostApplyCheck{}
	if err := json.Unmarshal([]byte(checkpoint.ChecksJSON), &checks); err != nil || len(checks) == 0 {
		checks = []adminflow.PostApplyCheck{{ID: "doctor.configure_post_save", Description: "Re-run Doctor post-save checks", Timeout: 10}}
	}
	results, status, report := s.executeDoctorPostChecks(ctx, checks)
	if err := store.UpdateDoctorCheckpoint(ctx, checkpoint.ID, status, serviceDoctorMustJSON(results)); err != nil {
		return "", err
	}
	planStatus := "post_checked"
	if status == "failed" {
		planStatus = "post_check_failed"
	}
	if err := store.UpdateSettingsChangePlanStatus(ctx, planID, planStatus, record.RollbackID, "", false, record.ApprovalJSON, record.LiveReloadJSON, record.AppliedAt); err != nil {
		return "", err
	}
	identity := serviceAuthIdentityFromContext(ctx)
	_ = s.recordDoctorAudit(ctx, identity, "doctor.checkpoint.completed", map[string]any{"plan_id": planID, "checkpoint_id": checkpoint.ID, "status": status, "results": results, "completed_at": db.NowMS(), "source": "doctor_tool"})
	_ = s.recordDoctorAudit(ctx, identity, "doctor.post_check.completed", map[string]any{"plan_id": planID, "checkpoint_id": checkpoint.ID, "status": status, "results": results, "completed_at": db.NowMS(), "source": "doctor_tool"})
	stats := map[string]any{"card_type": "post_fix_check", "plan_id": planID, "checkpoint_id": checkpoint.ID, "status": status, "results": results}
	if report != nil {
		stats["doctor_report"] = report
	}
	summary := fmt.Sprintf("Doctor post-checks for plan %s completed with status %s.", planID, status)
	return encodeDoctorToolResult("doctor_post_check", status != "failed", summary, stats), nil
}

func doctorDocsV1Dir(extraRoots ...string) (string, error) {
	seedDirs := []string{}
	cwd, err := os.Getwd()
	if err == nil {
		seedDirs = append(seedDirs, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		seedDirs = append(seedDirs, filepath.Dir(exe))
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		seedDirs = append(seedDirs, filepath.Dir(file))
	}
	for _, root := range extraRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if info, err := os.Stat(root); err == nil && !info.IsDir() {
			root = filepath.Dir(root)
		}
		seedDirs = append(seedDirs, root)
	}
	seen := map[string]struct{}{}
	for _, seed := range seedDirs {
		if seed == "" {
			continue
		}
		for dir := seed; ; dir = filepath.Dir(dir) {
			if _, ok := seen[dir]; ok {
				break
			}
			seen[dir] = struct{}{}
			for _, rel := range []string{filepath.Join("docs", "v1"), filepath.Join("or3-intern", "docs", "v1")} {
				candidate := filepath.Join(dir, rel)
				if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
					return candidate, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", fmt.Errorf("docs/v1 not found from %s", strings.Join(seedDirs, ", "))
}

func doctorSearchTerms(query string) []string {
	seen := map[string]struct{}{}
	terms := []string{}
	for _, term := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
	}) {
		term = strings.TrimSpace(term)
		if len(term) < 2 {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func doctorDocsTitle(content, relPath string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	base := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func doctorDocsScore(relPath, title, content string, terms []string, query string) int {
	lowerPath := strings.ToLower(relPath)
	lowerTitle := strings.ToLower(title)
	lowerContent := strings.ToLower(content)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	score := 0
	if lowerQuery != "" {
		if strings.Contains(lowerTitle, lowerQuery) {
			score += 30
		}
		if strings.Contains(lowerPath, lowerQuery) {
			score += 15
		}
		if strings.Contains(lowerContent, lowerQuery) {
			score += 10
		}
	}
	for _, term := range terms {
		if strings.Contains(lowerTitle, term) {
			score += 12
		}
		if strings.Contains(lowerPath, term) {
			score += 6
		}
		count := strings.Count(lowerContent, term)
		if count > 8 {
			count = 8
		}
		score += count
	}
	return score
}

func doctorDocsSnippet(content string, terms []string) string {
	lines := strings.Split(content, "\n")
	bestIndex := -1
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, term := range terms {
			if strings.Contains(lower, term) {
				bestIndex = i
				break
			}
		}
		if bestIndex >= 0 {
			break
		}
	}
	if bestIndex < 0 {
		bestIndex = 0
	}
	start := bestIndex - 1
	if start < 0 {
		start = 0
	}
	end := bestIndex + 2
	if end > len(lines) {
		end = len(lines)
	}
	parts := []string{}
	for _, line := range lines[start:end] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts = append(parts, strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
	}
	snippet := adminflow.SanitizeForAI(strings.Join(parts, " "))
	if len(snippet) > 520 {
		snippet = strings.TrimSpace(snippet[:520]) + "..."
	}
	return snippet
}

func doctorConfigFieldScore(field configmeta.ConfigFieldMetadata, query string) int {
	terms := doctorSearchTerms(query)
	if len(terms) == 0 {
		return 1
	}
	haystack := strings.ToLower(strings.Join([]string{
		field.Section,
		field.Key,
		field.Path,
		field.Label,
		field.Description,
		field.Docs,
		strings.Join(field.UserIntents, " "),
		strings.Join(field.AllowedValues, " "),
	}, " "))
	score := 0
	for _, term := range terms {
		count := strings.Count(haystack, term)
		if count > 4 {
			count = 4
		}
		score += count
		if strings.Contains(strings.ToLower(field.Label), term) {
			score += 5
		}
		if strings.Contains(strings.ToLower(field.Path), term) {
			score += 4
		}
		if strings.Contains(strings.ToLower(field.Key), term) {
			score += 3
		}
	}
	return score
}

func doctorConfigSearchItem(s *serviceServer, field configmeta.ConfigFieldMetadata, includeCurrent bool) map[string]any {
	item := map[string]any{
		"section":                 field.Section,
		"key":                     field.Key,
		"path":                    field.Path,
		"label":                   field.Label,
		"description":             field.Description,
		"risk_level":              field.Risk,
		"restart_required":        field.RestartRequired,
		"requires_approval":       field.RequiresApproval,
		"requires_step_up_auth":   field.RequiresStepUp,
		"advanced_only":           field.AdvancedOnly,
		"secret":                  field.Secret,
		"rollback_behavior":       field.Rollback,
		"validation_rules":        field.Validation,
		"user_intents":            field.UserIntents,
		"docs":                    field.Docs,
		"allowed_values":          field.AllowedValues,
		"plan_change_section":     field.Section,
		"plan_change_field":       field.Key,
		"plan_change_config_path": field.Path,
	}
	if includeCurrent && s != nil {
		if value, ok := doctorConfigValueForField(s.config, field); ok {
			item["current_value"] = value
		}
	}
	return item
}

func doctorConfigValueForField(cfg any, field configmeta.ConfigFieldMetadata) (adminflow.RedactedValue, bool) {
	value, ok := doctorResolveConfigPathValue(cfg, field.Path)
	if !ok {
		return adminflow.RedactedValue{}, false
	}
	if field.Secret {
		if doctorConfigValuePresent(value) {
			return adminflow.RedactedValue{Redacted: true, Present: true, Summary: "set"}, true
		}
		return adminflow.RedactedValue{Redacted: true, Present: false, Summary: "not set"}, true
	}
	return doctorPublicConfigValue(value), true
}

func doctorResolveConfigPathValue(source any, path string) (any, bool) {
	segments := strings.Split(strings.TrimSpace(path), ".")
	if len(segments) == 0 || strings.TrimSpace(path) == "" || strings.Contains(path, "*") {
		return nil, false
	}
	value := reflect.ValueOf(source)
	for _, segment := range segments {
		if segment == "" {
			return nil, false
		}
		for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
			if value.IsNil() {
				return nil, false
			}
			value = value.Elem()
		}
		if !value.IsValid() {
			return nil, false
		}
		switch value.Kind() {
		case reflect.Struct:
			found := false
			typ := value.Type()
			for i := 0; i < value.NumField(); i++ {
				field := typ.Field(i)
				jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
				if jsonName == segment || strings.EqualFold(field.Name, segment) {
					value = value.Field(i)
					found = true
					break
				}
			}
			if !found {
				return nil, false
			}
		case reflect.Map:
			if value.Type().Key().Kind() != reflect.String {
				return nil, false
			}
			item := value.MapIndex(reflect.ValueOf(segment).Convert(value.Type().Key()))
			if !item.IsValid() {
				return nil, false
			}
			value = item
		case reflect.Slice, reflect.Array:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= value.Len() {
				return nil, false
			}
			value = value.Index(index)
		default:
			return nil, false
		}
	}
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	if !value.IsValid() || !value.CanInterface() {
		return nil, false
	}
	return value.Interface(), true
}

func doctorPublicConfigValue(value any) adminflow.RedactedValue {
	if text, ok := value.(string); ok {
		text = adminflow.SanitizeForAI(text)
		if len(text) > 300 {
			return adminflow.RedactedValue{Present: true, Summary: strings.TrimSpace(text[:300]) + "..."}
		}
		return adminflow.RedactedValue{Value: text, Present: true}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return adminflow.RedactedValue{Present: true, Summary: fmt.Sprint(value)}
	}
	if len(data) > 500 {
		return adminflow.RedactedValue{Present: true, Summary: string(data[:500]) + "..."}
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return adminflow.RedactedValue{Present: true, Summary: string(data)}
	}
	return adminflow.RedactedValue{Value: decoded, Present: true}
}

func doctorConfigValuePresent(value any) bool {
	if value == nil {
		return false
	}
	reflectValue := reflect.ValueOf(value)
	for reflectValue.IsValid() && (reflectValue.Kind() == reflect.Pointer || reflectValue.Kind() == reflect.Interface) {
		if reflectValue.IsNil() {
			return false
		}
		reflectValue = reflectValue.Elem()
	}
	if !reflectValue.IsValid() {
		return false
	}
	switch reflectValue.Kind() {
	case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
		return reflectValue.Len() > 0
	case reflect.Bool:
		return reflectValue.Bool()
	default:
		return !reflectValue.IsZero()
	}
}

func encodeDoctorToolResult(kind string, ok bool, summary string, stats map[string]any) string {
	if stats == nil {
		stats = map[string]any{}
	}
	preview := doctorToolPreview(stats)
	return tools.EncodeToolResult(tools.ToolResult{Kind: kind, OK: ok, Summary: summary, Preview: preview, PlanID: doctorToolPlanID(stats), Stats: stats})
}

func doctorToolPreview(stats map[string]any) string {
	preview := map[string]any{}
	for _, key := range []string{"card_type", "plan_id", "status", "count", "rollback_id", "post_check_pending", "error"} {
		if value, ok := stats[key]; ok {
			preview[key] = value
		}
	}
	if plan, ok := stats["plan"].(adminflow.SettingsChangePlan); ok {
		preview["plan"] = map[string]any{"id": plan.ID, "title": plan.Title, "summary": plan.Summary, "risk_level": plan.RiskLevel, "restart_required": plan.RestartRequired, "changes": len(plan.Changes)}
	}
	if len(preview) == 0 {
		return ""
	}
	data, err := json.MarshalIndent(preview, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func doctorToolPlanID(stats map[string]any) string {
	if value, ok := stats["plan_id"].(string); ok {
		return strings.TrimSpace(value)
	}
	if plan, ok := stats["plan"].(adminflow.SettingsChangePlan); ok {
		return strings.TrimSpace(plan.ID)
	}
	return ""
}

func doctorToolString(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func doctorToolInt(params map[string]any, key string, fallback int) int {
	value := doctorToolInt64(params, key, int64(fallback))
	return int(value)
}

func doctorToolInt64(params map[string]any, key string, fallback int64) int64 {
	if params == nil {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func doctorToolBool(params map[string]any, key string, fallback bool) bool {
	if params == nil {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

func doctorToolDecode(value any, target any) error {
	if value == nil {
		return fmt.Errorf("value is required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
