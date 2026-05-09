package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/cron"
)

func (s *serviceServer) handleCron(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/cron"), "/")
	if path == "" {
		path = "status"
	}
	switch path {
	case "status":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		status := map[string]any{"enabled": s.config.Cron.Enabled, "available": s.cronSvc != nil}
		if s.cronSvc != nil {
			schedulerStatus, err := s.cronSvc.Status()
			if err != nil {
				writeServiceError(w, r, http.StatusBadGateway, "cron status unavailable", err)
				return
			}
			for key, value := range schedulerStatus {
				status[key] = value
			}
		}
		writeServiceJSON(w, http.StatusOK, status)
	case "jobs":
		svc := s.requireCronService(w)
		if svc == nil {
			return
		}
		switch r.Method {
		case http.MethodGet:
			jobs, err := svc.List()
			if err != nil {
				writeServiceError(w, r, http.StatusBadGateway, "cron jobs unavailable", err)
				return
			}
			writeServiceJSON(w, http.StatusOK, map[string]any{"items": jobs})
		case http.MethodPost:
			limitServiceRequestBody(w, r, serviceCronBodyLimit)
			job, err := decodeServiceCronJobRequest(r.Body, true)
			if err != nil {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if err := svc.Add(job); err != nil {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			jobs, _ := svc.List()
			created := findServiceCronJob(jobs, job.ID)
			if created == nil && len(jobs) > 0 {
				created = &jobs[len(jobs)-1]
			}
			writeServiceJSON(w, http.StatusCreated, map[string]any{"job": created})
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	default:
		if !strings.HasPrefix(path, "jobs/") {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron route not found"})
			return
		}
		svc := s.requireCronService(w)
		if svc == nil {
			return
		}
		parts := strings.Split(strings.TrimPrefix(path, "jobs/"), "/")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job route not found"})
			return
		}
		id := strings.TrimSpace(parts[0])
		if len(parts) == 1 {
			s.handleCronJob(w, r, svc, id)
			return
		}
		if len(parts) != 2 {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job route not found"})
			return
		}
		s.handleCronJobAction(w, r, svc, id, parts[1])
	}
}

func (s *serviceServer) requireCronService(w http.ResponseWriter) *cron.Service {
	if s.cronSvc == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cron service unavailable", "enabled": s.config.Cron.Enabled})
		return nil
	}
	return s.cronSvc
}

func (s *serviceServer) handleCronJob(w http.ResponseWriter, r *http.Request, svc *cron.Service, id string) {
	switch r.Method {
	case http.MethodGet:
		jobs, err := svc.List()
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "cron jobs unavailable", err)
			return
		}
		job := findServiceCronJob(jobs, id)
		if job == nil {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"job": job})
	case http.MethodPatch, http.MethodPut:
		limitServiceRequestBody(w, r, serviceCronBodyLimit)
		job, err := decodeServiceCronJobRequest(r.Body, false)
		if err != nil {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		updated, err := svc.Update(id, job)
		if err != nil {
			if errors.Is(err, cron.ErrNotFound) {
				writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
				return
			}
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"job": updated})
	case http.MethodDelete:
		found, err := svc.Remove(id)
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "cron job delete failed", err)
			return
		}
		if !found {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": id, "status": "deleted"})
	default:
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *serviceServer) handleCronJobAction(w http.ResponseWriter, r *http.Request, svc *cron.Service, id string, action string) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	switch action {
	case "run":
		limitServiceRequestBody(w, r, serviceCronBodyLimit)
		force := serviceCronRunForce(r.Body)
		job, err := svc.RunNow(r.Context(), id, force)
		if err != nil {
			if errors.Is(err, cron.ErrNotFound) {
				writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
				return
			}
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if !force && !job.Enabled {
			writeServiceJSON(w, http.StatusOK, map[string]any{"id": id, "status": "skipped", "ran": false})
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"id": id, "status": "ran"})
	case "pause":
		s.writeCronEnabledState(w, svc, id, false)
	case "resume":
		s.writeCronEnabledState(w, svc, id, true)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron action not found"})
	}
}

func (s *serviceServer) writeCronEnabledState(w http.ResponseWriter, svc *cron.Service, id string, enabled bool) {
	job, err := svc.SetEnabled(id, enabled)
	if err != nil {
		if errors.Is(err, cron.ErrNotFound) {
			writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "cron job not found"})
			return
		}
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"job": job})
}

func decodeServiceCronJobRequest(body io.Reader, defaultEnabled bool) (cron.CronJob, error) {
	var raw map[string]json.RawMessage
	if err := decodeServiceRequestBody(body, &raw); err != nil {
		return cron.CronJob{}, err
	}
	if len(raw) == 0 {
		return cron.CronJob{}, fmt.Errorf("job is required")
	}
	jobRaw, ok := raw["job"]
	if !ok {
		b, err := json.Marshal(raw)
		if err != nil {
			return cron.CronJob{}, err
		}
		jobRaw = b
	}
	var job cron.CronJob
	decoder := json.NewDecoder(bytes.NewReader(jobRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&job); err != nil {
		return cron.CronJob{}, err
	}
	if defaultEnabled {
		var jobMap map[string]json.RawMessage
		if err := json.Unmarshal(jobRaw, &jobMap); err == nil {
			if _, hasEnabled := jobMap["enabled"]; !hasEnabled {
				job.Enabled = true
			}
		}
	}
	if job.Payload.Kind == "" {
		job.Payload.Kind = "agent_turn"
	}
	job.Payload = cron.NormalizePayload(job.Payload)
	return job, nil
}

func serviceCronRunForce(body io.Reader) bool {
	force := true
	var raw map[string]json.RawMessage
	if err := decodeServiceRequestBody(body, &raw); err != nil || len(raw) == 0 {
		return force
	}
	if value, ok := raw["force"]; ok {
		_ = json.Unmarshal(value, &force)
	}
	return force
}

func findServiceCronJob(jobs []cron.CronJob, id string) *cron.CronJob {
	for i := range jobs {
		if jobs[i].ID == id {
			return &jobs[i]
		}
	}
	return nil
}
