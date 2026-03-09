package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"or3-intern/internal/config"
	"or3-intern/internal/security"
	"or3-intern/internal/tools"
)

const maxResultChars = 64 * 1024

type session interface {
	Close() error
	ListTools(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error)
	CallTool(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error)
}

type connector func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error)

type Manager struct {
	servers    map[string]config.MCPServerConfig
	logf       func(string, ...any)
	connect    connector
	sessions   map[string]session
	tools      []remoteToolSpec
	hostPolicy security.HostPolicy
}

type remoteToolSpec struct {
	localName   string
	serverName  string
	remoteName  string
	description string
	parameters  map[string]any
	timeout     time.Duration
	session     session
}

type RemoteTool struct {
	tools.Base

	localName   string
	serverName  string
	remoteName  string
	description string
	parameters  map[string]any
	timeout     time.Duration
	session     session
}

func (t *RemoteTool) Capability() tools.CapabilityLevel { return tools.CapabilityGuarded }

func NewManager(servers map[string]config.MCPServerConfig) *Manager {
	cloned := make(map[string]config.MCPServerConfig, len(servers))
	for name, server := range servers {
		cloned[name] = server
	}
	mgr := &Manager{
		servers:  cloned,
		sessions: map[string]session{},
	}
	mgr.connect = func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
		return connectSessionWithPolicy(ctx, name, cfg, mgr.hostPolicy)
	}
	return mgr
}

func (m *Manager) SetLogger(logf func(string, ...any)) {
	if m == nil {
		return
	}
	m.logf = logf
}

func (m *Manager) SetHostPolicy(policy security.HostPolicy) {
	if m == nil {
		return
	}
	m.hostPolicy = policy
}

func (m *Manager) ToolNames() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.tools))
	for _, spec := range m.tools {
		out = append(out, spec.localName)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) Connect(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if len(m.tools) > 0 || len(m.sessions) > 0 {
		return nil
	}

	usedLocalNames := map[string]string{}
	for _, name := range enabledServerNames(m.servers) {
		cfg := m.servers[name]
		if m.hostPolicy.EnabledPolicy() && (cfg.Transport == "sse" || cfg.Transport == "streamablehttp") {
			if err := m.hostPolicy.ValidateEndpoint(ctx, cfg.URL); err != nil {
				m.logFailure(name, "host policy denied", err)
				continue
			}
		}
		sess, err := m.connect(ctx, name, cfg)
		if err != nil {
			m.logFailure(name, "connect failed", err)
			continue
		}

		remoteTools, err := listTools(ctx, sess, cfg)
		if err != nil {
			_ = sess.Close()
			m.logFailure(name, "tool discovery failed", err)
			continue
		}
		remoteTools = filterRemoteTools(name, remoteTools, m.logfSafe)
		sort.Slice(remoteTools, func(i, j int) bool {
			return strings.ToLower(remoteTools[i].Name) < strings.ToLower(remoteTools[j].Name)
		})

		added := 0
		for _, remote := range remoteTools {
			spec := newRemoteToolSpec(name, cfg, remote, sess)
			if previous, ok := usedLocalNames[spec.localName]; ok {
				m.logfSafe("mcp tool skipped: duplicate local name=%s remote=%s/%s previous=%s", spec.localName, name, remote.Name, previous)
				continue
			}
			usedLocalNames[spec.localName] = previousToolLabel(name, remote.Name)
			m.tools = append(m.tools, spec)
			added++
		}

		m.sessions[name] = sess
		m.logfSafe("mcp server connected: name=%s transport=%s tools=%d", name, cfg.Transport, added)
	}
	return nil
}

func (m *Manager) RegisterTools(reg *tools.Registry) int {
	if m == nil || reg == nil {
		return 0
	}
	for _, spec := range m.tools {
		reg.Register(spec.Tool())
	}
	return len(m.tools)
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var errs []error
	for name, sess := range m.sessions {
		if err := sess.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	m.sessions = map[string]session{}
	m.tools = nil
	return errors.Join(errs...)
}

func (m *Manager) logFailure(name, prefix string, err error) {
	if m == nil || m.logf == nil || err == nil {
		return
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 240 {
		msg = msg[:240] + "...[truncated]"
	}
	m.logf("mcp server unavailable: name=%s %s err=%s", name, prefix, msg)
}

func (m *Manager) logfSafe(format string, args ...any) {
	if m == nil || m.logf == nil {
		return
	}
	m.logf(format, args...)
}

func (s remoteToolSpec) Tool() tools.Tool {
	return &RemoteTool{
		localName:   s.localName,
		serverName:  s.serverName,
		remoteName:  s.remoteName,
		description: s.description,
		parameters:  cloneAnyMap(s.parameters),
		timeout:     s.timeout,
		session:     s.session,
	}
}

func (t *RemoteTool) Name() string { return t.localName }

func (t *RemoteTool) Description() string {
	if strings.TrimSpace(t.description) != "" {
		return t.description
	}
	return fmt.Sprintf("MCP tool %s from server %s.", t.remoteName, t.serverName)
}

func (t *RemoteTool) Parameters() map[string]any {
	return cloneAnyMap(t.parameters)
}

func (t *RemoteTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RemoteTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.session == nil {
		return "", fmt.Errorf("mcp %s/%s: session not connected", t.serverName, t.remoteName)
	}

	callCtx := ctx
	cancel := func() {}
	if t.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, t.timeout)
	}
	defer cancel()

	res, err := t.session.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      t.remoteName,
		Arguments: cloneAnyMap(params),
	})
	if err != nil {
		return "", fmt.Errorf("mcp %s/%s: %w", t.serverName, t.remoteName, err)
	}

	text := resultToText(res, maxResultChars)
	if res != nil && res.IsError {
		if strings.TrimSpace(text) == "" {
			text = "remote tool reported error"
		}
		return "", fmt.Errorf("mcp %s/%s: %s", t.serverName, t.remoteName, text)
	}
	return text, nil
}

func connectSession(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
	return connectSessionWithPolicy(ctx, name, cfg, security.HostPolicy{})
}

func connectSessionWithPolicy(ctx context.Context, _ string, cfg config.MCPServerConfig, policy security.HostPolicy) (session, error) {
	transport, err := buildTransportWithPolicy(cfg, policy)
	if err != nil {
		return nil, err
	}

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "or3-intern", Version: "v1"}, nil)
	connectCtx := ctx
	cancel := func() {}
	if cfg.ConnectTimeoutSeconds > 0 {
		connectCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds)*time.Second)
	}
	defer cancel()

	return client.Connect(connectCtx, transport, nil)
}

func buildTransport(cfg config.MCPServerConfig) (sdkmcp.Transport, error) {
	return buildTransportWithPolicy(cfg, security.HostPolicy{})
}

func buildTransportWithPolicy(cfg config.MCPServerConfig, policy security.HostPolicy) (sdkmcp.Transport, error) {
	switch cfg.Transport {
	case "stdio":
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = tools.BuildChildEnv(os.Environ(), cfg.ChildEnvAllowlist, cfg.Env, "")
		return &sdkmcp.CommandTransport{Command: cmd}, nil
	case "sse":
		return &sdkmcp.SSEClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: buildHTTPClient(cfg, policy),
		}, nil
	case "streamablehttp":
		return &sdkmcp.StreamableClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: buildHTTPClient(cfg, policy),
			MaxRetries: -1,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %s", cfg.Transport)
	}
}

func buildHTTPClient(cfg config.MCPServerConfig, policy security.HostPolicy) *http.Client {
	timeout := time.Duration(cfg.ConnectTimeoutSeconds) * time.Second
	base := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
	}
	client := &http.Client{
		Transport: &headerRoundTripper{base: base, headers: cfg.Headers},
	}
	if policy.EnabledPolicy() {
		return security.WrapHTTPClient(client, policy)
	}
	return client
}

func listTools(ctx context.Context, sess session, cfg config.MCPServerConfig) ([]*sdkmcp.Tool, error) {
	var out []*sdkmcp.Tool
	var cursor string
	for {
		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds)*time.Second)
		res, err := sess.ListTools(reqCtx, &sdkmcp.ListToolsParams{Cursor: cursor})
		cancel()
		if err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		cursor = strings.TrimSpace(res.NextCursor)
		if cursor == "" {
			break
		}
	}
	return out, nil
}

func enabledServerNames(servers map[string]config.MCPServerConfig) []string {
	names := make([]string, 0, len(servers))
	for name, server := range servers {
		if server.Enabled {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func newRemoteToolSpec(serverName string, cfg config.MCPServerConfig, remote *sdkmcp.Tool, sess session) remoteToolSpec {
	remoteName := ""
	description := ""
	var inputSchema any
	if remote != nil {
		remoteName = strings.TrimSpace(remote.Name)
		description = strings.TrimSpace(remote.Description)
		inputSchema = remote.InputSchema
	}
	return remoteToolSpec{
		localName:   localToolName(serverName, remoteName),
		serverName:  serverName,
		remoteName:  remoteName,
		description: description,
		parameters:  normalizeSchema(inputSchema),
		timeout:     time.Duration(cfg.ToolTimeoutSeconds) * time.Second,
		session:     sess,
	}
}

func filterRemoteTools(serverName string, remoteTools []*sdkmcp.Tool, logf func(string, ...any)) []*sdkmcp.Tool {
	filtered := make([]*sdkmcp.Tool, 0, len(remoteTools))
	for index, remote := range remoteTools {
		if remote == nil {
			if logf != nil {
				logf("mcp tool skipped: malformed entry server=%s index=%d reason=nil", serverName, index)
			}
			continue
		}
		if strings.TrimSpace(remote.Name) == "" {
			if logf != nil {
				logf("mcp tool skipped: malformed entry server=%s index=%d reason=missing-name", serverName, index)
			}
			continue
		}
		filtered = append(filtered, remote)
	}
	return filtered
}

func localToolName(serverName, remoteName string) string {
	return "mcp_" + sanitizeName(serverName) + "_" + sanitizeName(remoteName)
}

func sanitizeName(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "unnamed"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unnamed"
	}
	return out
}

func normalizeSchema(schema any) map[string]any {
	if schema == nil {
		return defaultParameters()
	}
	if m, ok := schema.(map[string]any); ok && len(m) > 0 {
		return cloneAnyMap(m)
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return defaultParameters()
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || len(m) == 0 {
		return defaultParameters()
	}
	return m
}

func resultToText(res *sdkmcp.CallToolResult, limit int) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, content := range res.Content {
		if part := contentToText(content, limit); strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	if structured := structuredToText(res.StructuredContent); structured != "" {
		if len(parts) == 0 || strings.TrimSpace(parts[len(parts)-1]) != strings.TrimSpace(structured) {
			parts = append(parts, structured)
		}
	}
	return truncateResult(strings.Join(parts, "\n\n"), limit)
}

func structuredToText(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func contentToText(content sdkmcp.Content, limit int) string {
	switch block := content.(type) {
	case *sdkmcp.TextContent:
		return truncateResult(block.Text, limit)
	case *sdkmcp.ImageContent:
		return fmt.Sprintf("[image content omitted mime=%s bytes=%d]", block.MIMEType, len(block.Data))
	case *sdkmcp.AudioContent:
		return fmt.Sprintf("[audio content omitted mime=%s bytes=%d]", block.MIMEType, len(block.Data))
	case *sdkmcp.ResourceLink:
		return fmt.Sprintf("[resource link uri=%s name=%s]", block.URI, strings.TrimSpace(block.Name))
	case *sdkmcp.EmbeddedResource:
		if block.Resource == nil {
			return "[embedded resource omitted]"
		}
		if strings.TrimSpace(block.Resource.Text) != "" {
			return truncateResult(block.Resource.Text, limit)
		}
		if len(block.Resource.Blob) > 0 {
			return fmt.Sprintf("[embedded resource omitted uri=%s mime=%s bytes=%d]", block.Resource.URI, block.Resource.MIMEType, len(block.Resource.Blob))
		}
		return fmt.Sprintf("[embedded resource uri=%s]", block.Resource.URI)
	default:
		b, err := json.Marshal(content)
		if err != nil {
			return fmt.Sprintf("[unsupported MCP content %T]", content)
		}
		return truncateResult(string(b), limit)
	}
}

func truncateResult(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return strings.TrimSpace(text[:limit]) + "...[truncated]"
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func defaultParameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, raw := range base {
		key, value, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		merged[key] = value
	}
	for key, value := range overrides {
		if strings.TrimSpace(key) == "" {
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+merged[key])
	}
	return out
}

func previousToolLabel(serverName, remoteName string) string {
	return serverName + "/" + remoteName
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	cloned := req.Clone(req.Context())
	for key, value := range rt.headers {
		cloned.Header.Set(key, value)
	}
	return base.RoundTrip(cloned)
}
