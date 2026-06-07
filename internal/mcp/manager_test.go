package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"or3-intern/internal/config"
	"or3-intern/internal/security"
)

type fakeSession struct {
	closeErr error
	listFn   func(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error)
}

func (s *fakeSession) Close() error {
	return s.closeErr
}

func (s *fakeSession) ListTools(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
	if s.listFn != nil {
		return s.listFn(ctx, params)
	}
	return &sdkmcp.ListToolsResult{}, nil
}

func TestBuildTransportVariants(t *testing.T) {
	t.Setenv("PATH", "/test/bin")
	t.Setenv("HOME", "/test/home")
	t.Setenv("INHERITED_SECRET", "top-secret")
	stdio, err := buildTransport(config.MCPServerConfig{
		Transport: "stdio",
		Command:   "demo-server",
		Args:      []string{"--flag"},
		Env:       map[string]string{"API_KEY": "secret"},
	})
	if err != nil {
		t.Fatalf("buildTransport stdio: %v", err)
	}
	cmdTransport, ok := stdio.(*sdkmcp.CommandTransport)
	if !ok {
		t.Fatalf("expected CommandTransport, got %T", stdio)
	}
	if got := cmdTransport.Command.Args; len(got) != 2 || got[0] != "demo-server" || got[1] != "--flag" {
		t.Fatalf("unexpected stdio args: %#v", got)
	}
	if got := envSliceToMap(cmdTransport.Command.Env); got["API_KEY"] != "secret" || got["PATH"] != "/test/bin" || got["HOME"] != "/test/home" {
		t.Fatalf("expected merged env, got %#v", got)
	}
	if got := envSliceToMap(cmdTransport.Command.Env); got["INHERITED_SECRET"] != "" {
		t.Fatalf("expected inherited secret to be scrubbed, got %#v", got)
	}

	sse, err := buildTransport(config.MCPServerConfig{
		Transport:             "sse",
		URL:                   "https://example.com/sse",
		ConnectTimeoutSeconds: 5,
		Headers:               map[string]string{"Authorization": "Bearer token"},
	})
	if err != nil {
		t.Fatalf("buildTransport sse: %v", err)
	}
	if _, ok := sse.(*sdkmcp.SSEClientTransport); !ok {
		t.Fatalf("expected SSEClientTransport, got %T", sse)
	}

	streamable, err := buildTransport(config.MCPServerConfig{
		Transport:             "streamablehttp",
		URL:                   "https://example.com/mcp",
		ConnectTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("buildTransport streamablehttp: %v", err)
	}
	if _, ok := streamable.(*sdkmcp.StreamableClientTransport); !ok {
		t.Fatalf("expected StreamableClientTransport, got %T", streamable)
	}
}

func TestManagerConnect_PartialFailureAndRegistration(t *testing.T) {
	manager := NewManager(map[string]config.MCPServerConfig{
		"alpha": {Enabled: true, Transport: "stdio", Command: "alpha", ToolTimeoutSeconds: 5},
		"beta":  {Enabled: true, Transport: "stdio", Command: "beta", ToolTimeoutSeconds: 5},
	})
	var logs []string
	manager.SetLogger(func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	manager.connect = func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
		switch name {
		case "alpha":
			return &fakeSession{
				listFn: func(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
					return &sdkmcp.ListToolsResult{
						Tools: []*sdkmcp.Tool{{
							Name:        "echo",
							Description: "Echoes",
							InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
						}},
					}, nil
				},
			}, nil
		case "beta":
			return nil, errors.New("boom")
		default:
			return nil, errors.New("unexpected server")
		}
	}

	if err := manager.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(manager.ToolNames()) != 1 || manager.ToolNames()[0] != "mcp_alpha_echo" {
		t.Fatalf("unexpected MCP tool names: %#v", manager.ToolNames())
	}
	foundSuccess := false
	foundFailure := false
	for _, line := range logs {
		if strings.Contains(line, "mcp server connected: name=alpha transport=stdio tools=1") {
			foundSuccess = true
		}
		if strings.Contains(line, "mcp server unavailable: name=beta connect failed err=boom") {
			foundFailure = true
		}
	}
	if !foundSuccess || !foundFailure {
		t.Fatalf("expected success and failure startup logs, got %#v", logs)
	}
}

func TestManagerServerStatus(t *testing.T) {
	manager := NewManager(map[string]config.MCPServerConfig{
		"alpha":    {Enabled: true, Transport: "stdio", Command: "alpha", ToolTimeoutSeconds: 5},
		"beta":     {Enabled: true, Transport: "stdio", Command: "beta", ToolTimeoutSeconds: 5},
		"disabled": {Enabled: false, Transport: "stdio", Command: "disabled"},
	})
	manager.connect = func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
		switch name {
		case "alpha":
			return &fakeSession{
				listFn: func(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
					return &sdkmcp.ListToolsResult{
						Tools: []*sdkmcp.Tool{
							{Name: "write", InputSchema: map[string]any{"type": "object"}},
							{Name: "read", InputSchema: map[string]any{"type": "object"}},
						},
					}, nil
				},
			}, nil
		case "beta":
			return nil, errors.New("dial exploded")
		default:
			return nil, errors.New("unexpected server")
		}
	}

	if err := manager.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	status := manager.ServerStatus()
	if !status["alpha"].Connected {
		t.Fatalf("expected alpha connected, got %#v", status["alpha"])
	}
	if status["alpha"].State != "connected" {
		t.Fatalf("expected alpha connected state, got %#v", status["alpha"])
	}
	if status["alpha"].ToolCount != 2 || strings.Join(status["alpha"].Tools, ",") != "mcp_alpha_read,mcp_alpha_write" {
		t.Fatalf("unexpected alpha tools: %#v", status["alpha"])
	}
	if status["beta"].Connected || !strings.Contains(status["beta"].LastError, "dial exploded") {
		t.Fatalf("expected beta failure, got %#v", status["beta"])
	}
	if status["beta"].State != "degraded" {
		t.Fatalf("expected beta degraded state, got %#v", status["beta"])
	}
	if status["disabled"].Connected || status["disabled"].LastError != "disabled" {
		t.Fatalf("expected disabled status, got %#v", status["disabled"])
	}
	if status["disabled"].State != "off" {
		t.Fatalf("expected disabled off state, got %#v", status["disabled"])
	}
}

func TestManagerRefreshAndReconnectWithBackoff(t *testing.T) {
	manager := NewManager(map[string]config.MCPServerConfig{
		"alpha": {Enabled: true, Transport: "stdio", Command: "alpha", ToolTimeoutSeconds: 5},
	})
	attempts := 0
	manager.connect = func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("temporary dial failure")
		}
		return &fakeSession{
			listFn: func(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
				return &sdkmcp.ListToolsResult{Tools: []*sdkmcp.Tool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}, nil
			},
		}, nil
	}

	if err := manager.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if status := manager.ServerStatus()["alpha"]; status.State != "degraded" {
		t.Fatalf("expected initial degraded state, got %#v", status)
	}
	if err := manager.ReconnectWithBackoff(context.Background(), 2, 0); err != nil {
		t.Fatalf("ReconnectWithBackoff: %v", err)
	}
	if got := manager.ToolNames(); len(got) != 1 || got[0] != "mcp_alpha_echo" {
		t.Fatalf("expected tool after reconnect, got %#v", got)
	}

	if err := manager.Refresh(context.Background(), map[string]config.MCPServerConfig{
		"beta": {Enabled: false, Transport: "stdio", Command: "beta"},
	}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	status := manager.ServerStatus()
	if _, ok := status["alpha"]; ok {
		t.Fatalf("expected alpha removed after hot reload, got %#v", status)
	}
	if status["beta"].State != "off" {
		t.Fatalf("expected beta off after hot reload, got %#v", status["beta"])
	}
}

func TestManagerServerStatus_EmptyManager(t *testing.T) {
	status := NewManager(nil).ServerStatus()
	if len(status) != 0 {
		t.Fatalf("expected empty status, got %#v", status)
	}
	var manager *Manager
	if status := manager.ServerStatus(); len(status) != 0 {
		t.Fatalf("expected nil manager to return empty status, got %#v", status)
	}
}

func TestManagerConnect_SkipsMalformedRemoteTools(t *testing.T) {
	manager := NewManager(map[string]config.MCPServerConfig{
		"alpha": {Enabled: true, Transport: "stdio", Command: "alpha", ToolTimeoutSeconds: 5},
	})
	var logs []string
	manager.SetLogger(func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	manager.connect = func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
		return &fakeSession{
			listFn: func(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error) {
				return &sdkmcp.ListToolsResult{
					Tools: []*sdkmcp.Tool{
						nil,
						&sdkmcp.Tool{},
						{Name: "echo", InputSchema: map[string]any{"type": "object"}},
					},
				}, nil
			},
		}, nil
	}

	if err := manager.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := manager.ToolNames(); len(got) != 1 || got[0] != "mcp_alpha_echo" {
		t.Fatalf("expected only valid tool to be registered, got %#v", got)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "reason=nil") || !strings.Contains(joined, "reason=missing-name") {
		t.Fatalf("expected malformed-tool logs, got %#v", logs)
	}
}

func TestManagerConnect_HostPolicyBlocksRemoteHTTPBeforeDial(t *testing.T) {
	manager := NewManager(map[string]config.MCPServerConfig{
		"remote": {Enabled: true, Transport: "sse", URL: "https://blocked.example.com/mcp", ToolTimeoutSeconds: 5},
	})
	manager.SetHostPolicy(security.HostPolicy{
		Enabled:       true,
		DefaultDeny:   true,
		AllowedHosts:  []string{"allowed.example.com"},
		AllowLoopback: true,
	})
	called := false
	manager.connect = func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
		called = true
		return nil, errors.New("unexpected dial")
	}

	if err := manager.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if called {
		t.Fatal("expected host policy to block remote MCP before dial")
	}
	if got := manager.ToolNames(); len(got) != 0 {
		t.Fatalf("expected blocked remote MCP to register no tools, got %#v", got)
	}
}

func envSliceToMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, raw := range values {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}
