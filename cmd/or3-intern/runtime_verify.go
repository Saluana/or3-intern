package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type hermesSSECorsError struct {
	Origin  string
	Runtime string
}

func (e *hermesSSECorsError) Error() string {
	runtimeName := e.Runtime
	if runtimeName == "" {
		runtimeName = "runtime"
	}
	return fmt.Sprintf("%s live SSE response did not include Access-Control-Allow-Origin %q; this is an upstream CORS defect", runtimeName, e.Origin)
}

type liveRunVerification struct {
	Streaming    string
	Cancellation string
}

func verifyOpenClawTarget(ctx context.Context, target *RuntimeConnectionTarget) (*Verification, error) {
	capabilities, err := probeRunsCapabilitiesHTTP(ctx, targetBaseURL(target), target.AccessToken)
	if err != nil {
		return nil, err
	}
	live, err := probeLiveRunEvents(ctx, targetBaseURL(target), target.AccessToken, target.plan.cloudOrigin)
	if err != nil {
		return nil, err
	}
	return verificationFromCapabilities(capabilities, live), nil
}

func verifyHermesTarget(ctx context.Context, target *RuntimeConnectionTarget) (*Verification, error) {
	capabilities, err := probeRunsCapabilitiesHTTP(ctx, targetBaseURL(target), target.AccessToken)
	if err != nil {
		return nil, err
	}
	live, err := probeLiveRunEvents(ctx, targetBaseURL(target), target.AccessToken, target.plan.cloudOrigin)
	if err != nil {
		return nil, err
	}
	return verificationFromCapabilities(capabilities, live), nil
}

func verificationFromCapabilities(capabilities map[string]any, live liveRunVerification) *Verification {
	features, _ := capabilities["features"].(map[string]any)
	commands := "not-advertised"
	if value, ok := features["commands"].(bool); ok && value {
		commands = "verified"
	}
	return &Verification{
		Capabilities: capabilities,
		Streaming:    live.Streaming,
		Commands:     commands,
		Cancellation: live.Cancellation,
	}
}

func targetBaseURL(target *RuntimeConnectionTarget) string {
	if target == nil {
		return ""
	}
	basePath := target.BasePath
	if basePath == "" {
		basePath = "/"
	}
	return strings.TrimRight(target.LocalOrigin, "/") + "/" + strings.Trim(basePath, "/")
}

func probeRunsCapabilitiesHTTP(ctx context.Context, baseURL, token string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/v1/capabilities",
		nil,
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("runtime API is not reachable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("runtime API returned HTTP %d", response.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 512*1024)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("runtime capabilities response is invalid: %w", err)
	}
	features, _ := payload["features"].(map[string]any)
	endpoints, _ := payload["endpoints"].(map[string]any)
	if features["session_resources"] != true && endpoints["sessions"] == nil {
		return nil, errors.New("runtime does not advertise Runs sessions")
	}
	if features["run_events_sse"] != true && endpoints["run_events"] == nil {
		return nil, errors.New("runtime does not advertise Runs streaming")
	}
	return payload, nil
}

func probeRunsHTTP(ctx context.Context, baseURL, token string) error {
	_, err := probeRunsCapabilitiesHTTP(ctx, baseURL, token)
	return err
}

func probeLiveRunEvents(ctx context.Context, baseURL, token, origin string) (liveRunVerification, error) {
	if strings.TrimSpace(origin) == "" {
		return liveRunVerification{}, errors.New("runtime live-stream verification requires the OR3 Cloud origin")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := probeCORSPreflight(probeCtx, baseURL, origin); err != nil {
		return liveRunVerification{}, err
	}

	payload, err := json.Marshal(map[string]any{
		"session_id": fmt.Sprintf("or3-connect-probe-%d", time.Now().UnixNano()),
		"input":      "Reply with exactly OK.",
	})
	if err != nil {
		return liveRunVerification{}, err
	}
	startURL := strings.TrimRight(baseURL, "/") + "/v1/runs"
	startRequest, err := http.NewRequestWithContext(probeCtx, http.MethodPost, startURL, bytes.NewReader(payload))
	if err != nil {
		return liveRunVerification{}, err
	}
	startRequest.Header.Set("Authorization", "Bearer "+token)
	startRequest.Header.Set("Origin", origin)
	startRequest.Header.Set("Content-Type", "application/json")
	startRequest.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	startResponse, err := client.Do(startRequest)
	if err != nil {
		return liveRunVerification{}, fmt.Errorf("start live-stream verification run: %w", err)
	}
	var startBody struct {
		RunID string `json:"run_id"`
		ID    string `json:"id"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(startResponse.Body, 128*1024)).Decode(&startBody)
	startResponse.Body.Close()
	if decodeErr != nil {
		return liveRunVerification{}, fmt.Errorf("runtime did not return a verification run: %w", decodeErr)
	}
	runID := strings.TrimSpace(startBody.RunID)
	if runID == "" {
		runID = strings.TrimSpace(startBody.ID)
	}
	if startResponse.StatusCode < 200 || startResponse.StatusCode >= 300 || runID == "" {
		return liveRunVerification{}, fmt.Errorf("runtime rejected live-stream verification run (HTTP %d)", startResponse.StatusCode)
	}

	stopped := false
	stop := func() bool {
		if stopped {
			return true
		}
		stopped = true
		stopRequest, requestErr := http.NewRequestWithContext(
			context.Background(),
			http.MethodPost,
			strings.TrimRight(baseURL, "/")+"/v1/runs/"+runID+"/stop",
			bytes.NewReader([]byte(`{}`)),
		)
		if requestErr != nil {
			return false
		}
		stopRequest.Header.Set("Authorization", "Bearer "+token)
		stopRequest.Header.Set("Origin", origin)
		stopRequest.Header.Set("Content-Type", "application/json")
		response, requestErr := (&http.Client{Timeout: 8 * time.Second}).Do(stopRequest)
		if requestErr != nil {
			return false
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16*1024))
		return response.StatusCode >= 200 && response.StatusCode < 300
	}
	defer stop()

	eventsRequest, err := http.NewRequestWithContext(
		probeCtx,
		http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/v1/runs/"+runID+"/events",
		nil,
	)
	if err != nil {
		return liveRunVerification{}, err
	}
	eventsRequest.Header.Set("Authorization", "Bearer "+token)
	eventsRequest.Header.Set("Origin", origin)
	eventsRequest.Header.Set("Accept", "text/event-stream")
	eventsResponse, err := (&http.Client{}).Do(eventsRequest)
	if err != nil {
		return liveRunVerification{}, fmt.Errorf("connect to live run events: %w", err)
	}
	streaming, corsErr := validateLiveSSEResponse(eventsResponse.StatusCode, eventsResponse.Header, origin)
	var evidenceErr error
	if corsErr == nil && streaming {
		evidenceErr = readLiveRunEvidence(probeCtx, eventsResponse.Body, runID)
	}
	eventsResponse.Body.Close()
	if corsErr != nil {
		return liveRunVerification{}, corsErr
	}
	if !streaming {
		return liveRunVerification{}, errors.New("runtime live run events did not return an SSE response")
	}
	if evidenceErr != nil {
		return liveRunVerification{}, fmt.Errorf("runtime live run events did not deliver assistant content followed by a terminal event: %w", evidenceErr)
	}
	cancelled := stop()
	result := liveRunVerification{Streaming: "verified", Cancellation: "not-tested"}
	if cancelled {
		result.Cancellation = "verified"
	}
	return result, nil
}

func readLiveRunEvidence(ctx context.Context, body io.Reader, runID string) error {
	if body == nil {
		return errors.New("SSE response body is empty")
	}
	result := make(chan error, 1)
	go func() {
		reader := bufio.NewReaderSize(body, 16*1024)
		var data []string
		assistantContent := false
		for {
			line, err := reader.ReadString('\n')
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
			if trimmed == "" && len(data) > 0 {
				var payload map[string]any
				if json.Unmarshal([]byte(strings.Join(data, "\n")), &payload) == nil && liveEventMatchesRun(payload, runID) {
					content, terminal := liveRunEventEvidence(payload)
					assistantContent = assistantContent || content
					if terminal {
						if assistantContent {
							result <- nil
						} else {
							result <- errors.New("terminal event arrived before assistant content")
						}
						return
					}
				}
				data = data[:0]
			}
			if err != nil {
				if err == io.EOF {
					result <- errors.New("SSE stream ended before terminal evidence")
				} else {
					result <- err
				}
				return
			}
		}
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func liveEventMatchesRun(payload map[string]any, runID string) bool {
	for _, key := range []string{"run_id", "runId", "turn_id", "turnId"} {
		if value, ok := payload[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != runID {
			return false
		}
	}
	return true
}

func liveRunEventEvidence(payload map[string]any) (content, terminal bool) {
	event := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["event"])))
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["status"])))
	for _, key := range []string{"delta", "text", "output", "content"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			content = true
			break
		}
	}
	switch event {
	case "run.completed", "turn.completed", "run.failed", "turn.failed", "run.cancelled", "run.canceled", "turn.cancelled", "turn.canceled":
		terminal = true
	}
	switch status {
	case "completed", "complete", "succeeded", "success", "failed", "error", "cancelled", "canceled":
		terminal = true
	}
	return content, terminal
}

// readSSEFrame waits for the first complete SSE frame. It deliberately does
// not consume the whole response because a live event stream remains open
// until the run finishes (or the caller cancels it).
func readSSEFrame(ctx context.Context, body io.Reader) (string, error) {
	if body == nil {
		return "", errors.New("SSE response body is empty")
	}
	reader := bufio.NewReaderSize(body, 16*1024)
	frames := make(chan struct {
		frame string
		err   error
	}, 1)
	go func() {
		var frame strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				frame.WriteString(line)
			}
			if strings.TrimSpace(line) == "" && frame.Len() > 0 {
				frames <- struct {
					frame string
					err   error
				}{frame: frame.String()}
				return
			}
			if err != nil {
				if frame.Len() > 0 {
					err = errors.New("SSE stream ended before a complete frame")
				}
				frames <- struct {
					frame string
					err   error
				}{err: err}
				return
			}
		}
	}()
	select {
	case result := <-frames:
		if result.err != nil {
			return "", result.err
		}
		return result.frame, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func probeCORSPreflight(ctx context.Context, baseURL, origin string) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodOptions,
		strings.TrimRight(baseURL, "/")+"/v1/runs",
		nil,
	)
	if err != nil {
		return err
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("CORS preflight check failed: %w", err)
	}
	defer response.Body.Close()
	if err := validateCORSPreflight(response.StatusCode, response.Header, origin); err != nil {
		return err
	}
	return nil
}

func validateLiveSSEResponse(status int, headers http.Header, origin string) (bool, error) {
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("live run events returned HTTP %d", status)
	}
	contentType := strings.ToLower(headers.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return false, fmt.Errorf("live run events returned content type %q, not text/event-stream", headers.Get("Content-Type"))
	}
	if got := headers.Get("Access-Control-Allow-Origin"); got != origin {
		return false, &hermesSSECorsError{Origin: origin}
	}
	return true, nil
}

func validateCORSPreflight(status int, headers http.Header, origin string) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("CORS preflight returned HTTP %d", status)
	}
	if got := headers.Get("Access-Control-Allow-Origin"); got != origin {
		return &hermesSSECorsError{Origin: origin}
	}
	if !headerIncludes(headers.Get("Access-Control-Allow-Methods"), http.MethodPost) {
		return errors.New("CORS preflight does not allow POST")
	}
	for _, header := range []string{"Authorization", "Content-Type"} {
		if !headerIncludes(headers.Get("Access-Control-Allow-Headers"), header) {
			return fmt.Errorf("CORS preflight does not allow %s", header)
		}
	}
	return nil
}

func headerIncludes(value, expected string) bool {
	for _, candidate := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), expected) {
			return true
		}
	}
	return false
}

func hermesUpdateCheck(binary string) string {
	output, err := runRuntime(binary, "update", "--check")
	if err != nil {
		return "Update check could not complete; see `hermes update --check`."
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return "Update check: " + line
		}
	}
	return "Hermes update check completed."
}

func recoverHermesSSECors(plan externalRuntimePlan, stateDir string, verifyErr error, stdout io.Writer, confirm func(string) (bool, error)) error {
	var corsErr *hermesSSECorsError
	if !errors.As(verifyErr, &corsErr) {
		return verifyErr
	}
	fmt.Fprintln(stdout, "Hermes reported a live SSE CORS compatibility problem.")
	if plan.runtimeBinary != "" {
		fmt.Fprintln(stdout, hermesUpdateCheck(plan.runtimeBinary))
	}
	if confirm == nil {
		confirm = func(action string) (bool, error) {
			return confirmRuntimeAction(stdout, action), nil
		}
	}
	confirmed, err := confirm("Apply the narrowly scoped local Hermes SSE CORS compatibility patch?")
	if err != nil {
		return err
	}
	if !confirmed {
		return corsErr
	}
	if err := applyHermesSSECorsPatch(plan.runtimeBinary, stateDir); err != nil {
		return err
	}
	if _, err := runRuntime(plan.runtimeBinary, "gateway", "restart"); err != nil {
		return fmt.Errorf("restart Hermes after SSE compatibility patch: %w", err)
	}
	return nil
}

// applyHermesSSECorsPatch only accepts the exact older source shape that was
// missing CORS headers on the already-200 SSE response. It refuses unknown
// source layouts rather than making a broad or speculative edit.
func applyHermesSSECorsPatch(binary, stateDir string) error {
	path, err := hermesAPIServerSourcePath(binary)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Hermes API server source: %w", err)
	}
	if bytes.Contains(body, []byte("cors = self._cors_headers_for_origin(origin)")) {
		return errors.New("Hermes source already contains the SSE CORS fix; restart Hermes and retry")
	}
	needle := []byte("        response = web.StreamResponse(status=200, headers=sse_headers)\n")
	replacement := []byte("        origin = request.headers.get(\"Origin\", \"\")\n        cors = self._cors_headers_for_origin(origin) if origin else None\n        if cors:\n            sse_headers.update(cors)\n        response = web.StreamResponse(status=200, headers=sse_headers)\n")
	if !bytes.Contains(body, needle) {
		return errors.New("Hermes source is outside the verified SSE patch compatibility shape")
	}
	if err := recordHermesSourcePatchBackup(stateDir, path); err != nil {
		return err
	}
	updated := bytes.Replace(body, needle, replacement, 1)
	if err := atomicReplaceFile(path, updated); err != nil {
		return fmt.Errorf("apply Hermes SSE CORS patch: %w", err)
	}
	return nil
}

func hermesAPIServerSourcePath(binary string) (string, error) {
	if root := strings.TrimSpace(os.Getenv("OR3_HERMES_SOURCE_ROOT")); root != "" {
		candidate := filepath.Join(root, "gateway", "platforms", "api_server.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if binary == "" {
		return "", errors.New("Hermes source path is unavailable; set OR3_HERMES_SOURCE_ROOT to the Hermes checkout")
	}
	launcher, err := os.ReadFile(binary)
	if err == nil {
		pattern := regexp.MustCompile(`"([^"]+/hermes-agent)/hermes"`)
		match := pattern.FindSubmatch(launcher)
		if len(match) == 2 {
			candidate := filepath.Join(string(match[1]), "gateway", "platforms", "api_server.py")
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
		}
	}
	return "", errors.New("Hermes source checkout was not found; set OR3_HERMES_SOURCE_ROOT to the checkout containing gateway/platforms/api_server.py")
}

func atomicReplaceFile(path string, body []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".or3-hermes-patch-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
