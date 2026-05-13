package main

import "net/http"

func (s *serviceServer) rejectServiceAuthRateLimit(w http.ResponseWriter, r *http.Request, scope string) bool {
	if retryAfter := s.serviceAuthRetryAfter(r, scope); retryAfter > 0 {
		writeServiceAuthRateLimit(w, r, retryAfter)
		return true
	}
	return false
}
