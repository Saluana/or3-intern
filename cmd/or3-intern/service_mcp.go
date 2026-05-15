package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/mcp"
)

const serviceMCPBodyLimit int64 = 256 << 10
const serviceMCPRedactedValue = "configured"

type serviceMCPServerDetail struct {
	Name   string                 `json:"name"`
	Config config.MCPServerConfig `json:"config"`
	Status mcp.ServerStatus       `json:"status"`
}

type serviceMCPToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type serviceMCPTestResult struct {
	OK        bool                 `json:"ok"`
	ToolCount int                  `json:"toolCount,omitempty"`
	Tools     []serviceMCPToolInfo `json:"tools,omitempty"`
	Error     string               `json:"error,omitempty"`
}

type serviceMCPTestManager interface {
	Connect(context.Context) error
	Close() error
	ServerStatus() map[string]mcp.ServerStatus
}

type serviceMCPTestManagerFactory func(map[string]config.MCPServerConfig) serviceMCPTestManager

func (s *serviceServer) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/mcp/servers"), "/")
	switch {
	case path == "":
		switch r.Method {
		case http.MethodGet:
			s.handleMCPServersList(w, r)
		case http.MethodPost:
			s.handleMCPServersAdd(w, r)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case strings.HasSuffix(path, "/test"):
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		name := strings.TrimSuffix(path, "/test")
		s.handleMCPServersTest(w, r, name)
	default:
		if r.Method != http.MethodDelete {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleMCPServersDelete(w, r, path)
	}
}

func (s *serviceServer) handleMCPServersList(w http.ResponseWriter, r *http.Request) {
	statuses := map[string]mcp.ServerStatus{}
	if s != nil && s.mcpManager != nil {
		statuses = s.mcpManager.ServerStatus()
	}
	servers := s.config.Tools.MCPServers
	items := make([]serviceMCPServerDetail, 0, len(servers))
	for _, name := range sortedMCPServerNames(servers) {
		items = append(items, serviceMCPServerDetail{
			Name:   name,
			Config: redactServiceMCPConfig(servers[name]),
			Status: statuses[name],
		})
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"servers": items})
}

func (s *serviceServer) handleMCPServersAdd(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceMCPBodyLimit)
	var body struct {
		Name   string                 `json:"name"`
		Config config.MCPServerConfig `json:"config"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil && !errors.Is(err, io.EOF) {
		writeServiceRequestDecodeError(w, err)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "name is required"})
		return
	}
	next := s.config
	if next.Tools.MCPServers == nil {
		next.Tools.MCPServers = map[string]config.MCPServerConfig{}
	}
	previous := next.Tools.MCPServers[name]
	next.Tools.MCPServers[name] = preserveServiceMCPRedactedSecrets(normalizeServiceMCPConfig(body.Config, next.Hardening.ChildEnvAllowlist), previous)
	if err := config.ValidateMCPServers(next.Tools.MCPServers); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path, "restartRequired": true})
}

func (s *serviceServer) handleMCPServersDelete(w http.ResponseWriter, r *http.Request, rawName string) {
	name := decodeServiceMCPServerName(rawName)
	if name == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "server name is required"})
		return
	}
	next := s.config
	if next.Tools.MCPServers == nil {
		next.Tools.MCPServers = map[string]config.MCPServerConfig{}
	}
	if _, ok := next.Tools.MCPServers[name]; !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "mcp server not found"})
		return
	}
	delete(next.Tools.MCPServers, name)
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path, "restartRequired": true})
}

func (s *serviceServer) handleMCPServersTest(w http.ResponseWriter, r *http.Request, rawName string) {
	name := decodeServiceMCPServerName(rawName)
	if name == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "server name is required"})
		return
	}
	server, ok := s.config.Tools.MCPServers[name]
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "mcp server not found"})
		return
	}
	if !server.Enabled {
		writeServiceValue(w, http.StatusOK, serviceMCPTestResult{OK: false, Error: "server is disabled"})
		return
	}
	servers := map[string]config.MCPServerConfig{name: normalizeServiceMCPConfig(server, s.config.Hardening.ChildEnvAllowlist)}
	if err := config.ValidateMCPServers(servers); err != nil {
		writeServiceValue(w, http.StatusOK, serviceMCPTestResult{OK: false, Error: boundServiceMCPError(err)})
		return
	}
	manager := s.newServiceMCPTestManager(servers)
	if manager == nil {
		writeServiceValue(w, http.StatusOK, serviceMCPTestResult{OK: false, Error: "mcp test manager unavailable"})
		return
	}
	defer manager.Close()
	if err := manager.Connect(r.Context()); err != nil {
		writeServiceValue(w, http.StatusOK, serviceMCPTestResult{OK: false, Error: boundServiceMCPError(err)})
		return
	}
	status := manager.ServerStatus()[name]
	if !status.Connected {
		message := strings.TrimSpace(status.LastError)
		if message == "" {
			message = "server did not connect"
		}
		writeServiceValue(w, http.StatusOK, serviceMCPTestResult{OK: false, Error: boundServiceMCPError(fmt.Errorf("%s", message))})
		return
	}
	tools := make([]serviceMCPToolInfo, 0, len(status.Tools))
	for _, toolName := range status.Tools {
		tools = append(tools, serviceMCPToolInfo{Name: toolName})
	}
	writeServiceValue(w, http.StatusOK, serviceMCPTestResult{OK: true, ToolCount: status.ToolCount, Tools: tools})
}

func (s *serviceServer) newServiceMCPTestManager(servers map[string]config.MCPServerConfig) serviceMCPTestManager {
	if s != nil && s.mcpTestManagerFactory != nil {
		return s.mcpTestManagerFactory(servers)
	}
	manager := mcp.NewManager(servers)
	manager.SetHostPolicy(buildHostPolicy(s.config))
	return manager
}

func normalizeServiceMCPConfig(server config.MCPServerConfig, childEnvAllowlist []string) config.MCPServerConfig {
	server.Transport = strings.ToLower(strings.TrimSpace(server.Transport))
	if server.Transport == "" {
		server.Transport = config.DefaultMCPTransport
	}
	server.Command = strings.TrimSpace(server.Command)
	server.URL = strings.TrimSpace(server.URL)
	if server.Args == nil {
		server.Args = []string{}
	}
	if server.Env == nil {
		server.Env = map[string]string{}
	}
	if len(server.ChildEnvAllowlist) == 0 {
		server.ChildEnvAllowlist = append([]string{}, childEnvAllowlist...)
	}
	if server.Headers == nil {
		server.Headers = map[string]string{}
	}
	if server.ConnectTimeoutSeconds <= 0 {
		server.ConnectTimeoutSeconds = config.DefaultMCPConnectTimeoutSeconds
	}
	if server.ToolTimeoutSeconds <= 0 {
		server.ToolTimeoutSeconds = config.DefaultMCPToolTimeoutSeconds
	}
	return server
}

func redactServiceMCPConfig(server config.MCPServerConfig) config.MCPServerConfig {
	server.Env = redactServiceMCPSecretMap(server.Env)
	server.Headers = redactServiceMCPSecretMap(server.Headers)
	return server
}

func redactServiceMCPSecretMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	redacted := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			redacted[key] = ""
			continue
		}
		redacted[key] = serviceMCPRedactedValue
	}
	return redacted
}

func preserveServiceMCPRedactedSecrets(next, previous config.MCPServerConfig) config.MCPServerConfig {
	next.Env = preserveServiceMCPRedactedSecretMap(next.Env, previous.Env)
	next.Headers = preserveServiceMCPRedactedSecretMap(next.Headers, previous.Headers)
	return next
}

func preserveServiceMCPRedactedSecretMap(next, previous map[string]string) map[string]string {
	if next == nil {
		return nil
	}
	out := make(map[string]string, len(next))
	for key, value := range next {
		if value == serviceMCPRedactedValue {
			if previousValue, ok := previous[key]; ok {
				out[key] = previousValue
				continue
			}
		}
		out[key] = value
	}
	return out
}

func sortedMCPServerNames(servers map[string]config.MCPServerConfig) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func decodeServiceMCPServerName(raw string) string {
	raw = strings.Trim(raw, "/")
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	return strings.TrimSpace(raw)
}

func boundServiceMCPError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) > 240 {
		message = message[:240] + "...[truncated]"
	}
	if message == "" {
		return "mcp test failed"
	}
	return message
}
