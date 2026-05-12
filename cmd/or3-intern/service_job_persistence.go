package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/controlplane"
	"or3-intern/internal/db"
)

func (s *serviceServer) persistServiceJobSummary(ctx context.Context, jobID string) {
	if s == nil || s.runtime == nil || s.runtime.DB == nil {
		return
	}
	snapshot, ok := s.jobs.Snapshot(jobID)
	if !ok {
		return
	}
	events, err := json.Marshal(snapshot.Events)
	if err != nil {
		log.Printf("service_jobs: marshal failed for job %s: %v", jobID, err)
		return
	}
	summary := db.ServiceJobSummary{
		ID:         snapshot.ID,
		Kind:       snapshot.Kind,
		Status:     snapshot.Status,
		EventsJSON: string(events),
		CreatedAt:  snapshot.CreatedAt.Unix(),
		UpdatedAt:  snapshot.UpdatedAt.Unix(),
	}
	if err := s.runtime.DB.UpsertServiceJobSummary(ctx, summary); err != nil {
		log.Printf("service_jobs: persist failed for job %s: %v", jobID, err)
	}
}

func (s *serviceServer) writePersistedServiceJobSnapshot(w http.ResponseWriter, r *http.Request, jobID string) bool {
	if s == nil || s.runtime == nil || s.runtime.DB == nil {
		return false
	}
	summary, err := s.runtime.DB.GetServiceJobSummary(r.Context(), jobID)
	if err != nil {
		if !sqlIsNotFound(err) {
			log.Printf("service_jobs: lookup failed for job %s: %v", jobID, err)
		}
		return false
	}
	var events []agent.JobEvent
	if err := json.Unmarshal([]byte(summary.EventsJSON), &events); err != nil {
		log.Printf("service_jobs: decode failed for job %s: %v", jobID, err)
		return false
	}
	writeServiceValue(w, http.StatusOK, controlplane.BuildJobSnapshotResponse(agent.JobSnapshot{
		ID:        summary.ID,
		Kind:      summary.Kind,
		Status:    summary.Status,
		Events:    events,
		CreatedAt: time.Unix(summary.CreatedAt, 0),
		UpdatedAt: time.Unix(summary.UpdatedAt, 0),
	}))
	return true
}

func sqlIsNotFound(err error) bool {
	return err == sql.ErrNoRows
}
