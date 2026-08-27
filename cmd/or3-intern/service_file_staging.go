package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var externalAgentStagingDirPattern = regexp.MustCompile(`^\.or3-upload-[0-9]{10,}-[a-z0-9]{6}$`)

func isExternalAgentStagingDir(name string) bool {
	return externalAgentStagingDirPattern.MatchString(strings.TrimSpace(name))
}

// resolveExternalAgentStagingDir accepts only a direct child of the writable
// workspace root with the generated staging name. This is intentionally
// narrower than the general file resolver because release is recursive.
func (s *serviceServer) resolveExternalAgentStagingDir(rootID, relPath string) (serviceFileRoot, string, error) {
	if strings.TrimSpace(rootID) != "workspace" {
		return serviceFileRoot{}, "", fmt.Errorf("staging cleanup is limited to the workspace root")
	}
	root, absPath, rel, err := s.resolveServiceFilePath(rootID, relPath)
	if err != nil {
		return serviceFileRoot{}, "", err
	}
	if rel == "." || strings.Contains(rel, "/") || !isExternalAgentStagingDir(rel) {
		return serviceFileRoot{}, "", fmt.Errorf("invalid staging directory")
	}
	info, err := os.Lstat(absPath)
	if errors.Is(err, os.ErrNotExist) {
		return root, absPath, nil
	}
	if err != nil {
		return serviceFileRoot{}, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return serviceFileRoot{}, "", fmt.Errorf("staging target is not a directory")
	}
	return root, absPath, nil
}

func (s *serviceServer) handleStagingRelease(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceApprovalBodyLimit)
	var body struct {
		RootID string `json:"root_id"`
		Path   string `json:"path"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	_, absPath, err := s.resolveExternalAgentStagingDir(body.RootID, body.Path)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if info, statErr := os.Lstat(absPath); errors.Is(statErr, os.ErrNotExist) {
		writeServiceJSON(w, http.StatusOK, map[string]any{
			"root_id": body.RootID,
			"path":    filepath.ToSlash(filepath.Base(absPath)),
			"status":  "already_released",
		})
		return
	} else if statErr != nil {
		writeServiceError(w, r, http.StatusBadGateway, "staging directory unavailable", statErr)
		return
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "staging target is not a directory"})
		return
	}
	if err := os.RemoveAll(absPath); err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "staging cleanup failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{
		"root_id": body.RootID,
		"path":    filepath.ToSlash(filepath.Base(absPath)),
		"status":  "released",
	})
}
