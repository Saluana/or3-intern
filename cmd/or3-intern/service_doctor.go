package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/configmeta"
	"or3-intern/internal/db"
	"or3-intern/internal/diagnosticlog"
	"or3-intern/internal/doctor"
)

// serviceDoctorBodyLimit caps the POST body for `/internal/v1/doctor/run`.
const serviceDoctorBodyLimit = 256 * 1024

// doctorDiagnosticLogMaxLimit caps how many log rows a single /logs query
// can return. The clamp helper below keeps callers honest.
const doctorDiagnosticLogMaxLimit = 200

// serviceDoctorStatusRequest is the optional body for POST
// `/internal/v1/doctor/run`. Both fields are best-effort client
// diagnostics that augment the server-side advisory report.
type serviceDoctorStatusRequest struct {
	ClientFindings    []doctor.Finding                 `json:"client_findings"`
	ClientDiagnostics *diagnosticlog.ClientDiagnostics `json:"client_diagnostics,omitempty"`
}

func (s *serviceServer) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	configmeta.EnsureFirstSliceFieldsRegistered()
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/doctor"), "/")
	switch {
	case path == "" || path == "status":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorStatus(w, r, nil)
	case path == "run":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceDoctorBodyLimit)
		var req serviceDoctorStatusRequest
		if err := decodeServiceRequestBody(r.Body, &req); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		clientFindings := append([]doctor.Finding{}, req.ClientFindings...)
		if req.ClientDiagnostics != nil {
			clientFindings = append(clientFindings, diagnosticlog.FindingsFromClientDiagnostics(*req.ClientDiagnostics)...)
		}
		s.handleDoctorStatus(w, r, clientFindings)
	case path == "config-metadata":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		writeServiceValue(w, http.StatusOK, map[string]any{"fields": configmeta.ListForConfig(s.configSnapshot())})
	case path == "logs":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleDoctorLogs(w, r)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "doctor route not found"})
	}
}

func (s *serviceServer) handleDoctorStatus(w http.ResponseWriter, r *http.Request, clientFindings []doctor.Finding) {
	report := doctor.Evaluate(s.configSnapshot(), doctor.Options{Mode: doctor.ModeAdvisory})
	if len(clientFindings) > 0 {
		combined := append(append([]doctor.Finding{}, report.Findings...), clientFindings...)
		report = doctor.NewReport(doctor.ModeAdvisory, combined)
	}
	writeServiceValue(w, http.StatusOK, s.buildDoctorStatusResponse(r, report))
}

func (s *serviceServer) handleDoctorLogs(w http.ResponseWriter, r *http.Request) {
	store := s.doctorDB()
	if store == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "database unavailable"})
		return
	}
	requestedLimit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid limit"})
			return
		}
		requestedLimit = n
	}
	limit := clampDoctorDiagnosticLogLimit(requestedLimit)
	sinceMS, ok := parseOptionalInt64Query(w, r, "since_ms")
	if !ok {
		return
	}
	untilMS, ok := parseOptionalInt64Query(w, r, "until_ms")
	if !ok {
		return
	}
	if sinceMS > 0 && untilMS > 0 && sinceMS > untilMS {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "since_ms must be before until_ms"})
		return
	}
	items, err := store.QueryDiagnosticLogEvents(r.Context(), db.DiagnosticLogQuery{
		Source:        strings.TrimSpace(r.URL.Query().Get("source")),
		Level:         strings.TrimSpace(r.URL.Query().Get("level")),
		CorrelationID: strings.TrimSpace(r.URL.Query().Get("correlation_id")),
		EventType:     strings.TrimSpace(r.URL.Query().Get("event_type")),
		Pattern:       serviceFirstNonEmpty(strings.TrimSpace(r.URL.Query().Get("pattern")), strings.TrimSpace(r.URL.Query().Get("known_failure_pattern"))),
		SinceUnixMS:   sinceMS,
		UntilUnixMS:   untilMS,
		Limit:         limit,
	})
	if err != nil {
		writeServiceError(w, r, http.StatusServiceUnavailable, "doctor log query failed", err)
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{"items": items})
}

func (s *serviceServer) buildDoctorStatusResponse(r *http.Request, report doctor.Report) map[string]any {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	cfg := s.configSnapshot()
	inventory := s.serviceSkillsInventory(ctx, cfg)
	recentLogs := []db.DiagnosticLogEvent{}
	if store := s.doctorDB(); store != nil {
		if items, err := store.QueryDiagnosticLogEvents(ctx, db.DiagnosticLogQuery{Limit: 25}); err == nil {
			recentLogs = items
		}
	}
	health := s.serviceHealth()
	readiness := s.control().GetReadiness()
	var bootstrap any
	if r != nil {
		bootstrap = s.buildAppBootstrap(r)
	}
	return map[string]any{
		"basic_doctor_available": true,
		"health":                 health,
		"readiness":              readiness,
		"app_bootstrap":          bootstrap,
		"report":                 report,
		"finding_cards":          serviceDoctorFindingCards(report.Findings),
		"skills": map[string]any{
			"count": len(inventory.Skills),
			"items": serviceSkillItems(inventory, cfg.Skills),
		},
		"recent_logs": recentLogs,
	}
}

func serviceDoctorFindingCards(findings []doctor.Finding) []map[string]any {
	items := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		risk := serviceDoctorRiskFromSeverity(finding.Severity)
		items = append(items, map[string]any{
			"id":               finding.ID,
			"what_i_found":     finding.Summary,
			"what_this_means":  serviceFirstNonEmpty(strings.TrimSpace(finding.Detail), finding.Summary),
			"recommended_fix":  strings.TrimSpace(finding.FixHint),
			"risk_level":       risk,
			"approval_needed":  risk == configmeta.RiskWarning || risk == configmeta.RiskDanger,
			"restart_needed":   false,
			"advanced_details": finding,
		})
	}
	return items
}

func serviceDoctorRiskFromSeverity(severity doctor.Severity) configmeta.RiskLevel {
	switch severity {
	case doctor.SeverityInfo:
		return configmeta.RiskSafe
	case doctor.SeverityWarn:
		return configmeta.RiskNotice
	case doctor.SeverityError:
		return configmeta.RiskWarning
	case doctor.SeverityBlock:
		return configmeta.RiskDanger
	default:
		return configmeta.RiskNotice
	}
}

func (s *serviceServer) doctorDB() *db.DB {
	if s != nil {
		if d := s.serviceDB(); d != nil {
			return d
		}
		if ctrl := s.control(); ctrl != nil {
			return ctrl.DB
		}
	}
	return nil
}

func parseOptionalInt64Query(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid " + key})
		return 0, false
	}
	return n, true
}

// clampDoctorDiagnosticLogLimit caps a requested log query limit at the
// server-side maximum.
func clampDoctorDiagnosticLogLimit(requested int) (effective int) {
	effective = requested
	if effective <= 0 {
		effective = 100
	}
	if effective > doctorDiagnosticLogMaxLimit {
		effective = doctorDiagnosticLogMaxLimit
	}
	return effective
}
