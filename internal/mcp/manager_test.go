package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"or3-intern/internal/config"
	"or3-intern/internal/tools"
)

type fakeSession struct {
	closeErr error
	listFn   func(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error)
	callFn   func(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error)
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

func (s *fakeSession) CallTool(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
	if s.callFn != nil {
		return s.callFn(ctx, params)
	}
	return &sdkmcp.CallToolResult{}, nil
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

func TestRemoteTool_SchemaPropagationAndExecution(t *testing.T) {
	var gotName string
	var gotArgs map[string]any
	tool := newRemoteToolSpec("alpha", config.MCPServerConfig{ToolTimeoutSeconds: 1}, &sdkmcp.Tool{
		Name:        "Echo!",
		Description: "Echoes input",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
	}, &fakeSession{
		callFn: func(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
			gotName = params.Name
			gotArgs = params.Arguments.(map[string]any)
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "hello back"}},
			}, nil
		},
	}).Tool()

	if tool.Name() != "mcp_alpha_echo" {
		t.Fatalf("unexpected tool name: %q", tool.Name())
	}
	schema := tool.Schema()
	function, ok := schema["function"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected schema: %#v", schema)
	}
	if function["description"] != "Echoes input" {
		t.Fatalf("expected remote description to survive, got %#v", function["description"])
	}

	out, err := tool.Execute(context.Background(), map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "hello back" {
		t.Fatalf("unexpected output: %q", out)
	}
	if gotName != "Echo!" {
		t.Fatalf("expected remote tool name to be preserved, got %q", gotName)
	}
	if gotArgs["text"] != "hi" {
		t.Fatalf("expected args to be forwarded, got %#v", gotArgs)
	}
}

func TestRemoteTool_ExecuteTimeout(t *testing.T) {
	tool := (&RemoteTool{
		localName:   "mcp_alpha_slow",
		serverName:  "alpha",
		remoteName:  "slow",
		description: "slow",
		parameters:  defaultParameters(),
		timeout:     10 * time.Millisecond,
		session: &fakeSession{
			callFn: func(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	})

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestRemoteTool_ExecuteRemoteError(t *testing.T) {
	tool := (&RemoteTool{
		localName:   "mcp_alpha_fail",
		serverName:  "alpha",
		remoteName:  "fail",
		description: "fail",
		parameters:  defaultParameters(),
		timeout:     time.Second,
		session: &fakeSession{
			callFn: func(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{
					IsError: true,
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "remote denied"}},
				}, nil
			},
		},
	})

	out, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected remote error")
	}
	if out != "" {
		t.Fatalf("expected empty output on remote error, got %q", out)
	}
	if !strings.Contains(err.Error(), "remote denied") {
		t.Fatalf("expected remote error text to survive, got %v", err)
	}
}

func TestResultToTextConvertsMixedContent(t *testing.T) {
	res := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: "plain text"},
			&sdkmcp.ImageContent{MIMEType: "image/png", Data: []byte("1234")},
			&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{URI: "file:///tmp/data.txt", Text: "resource text"}},
		},
		StructuredContent: map[string]any{"ok": true},
	}

	got := resultToText(res, maxResultChars)
	if !strings.Contains(got, "plain text") {
		t.Fatalf("expected text content, got %q", got)
	}
	if !strings.Contains(got, "[image content omitted mime=image/png bytes=4]") {
		t.Fatalf("expected image summary, got %q", got)
	}
	if !strings.Contains(got, "resource text") {
		t.Fatalf("expected embedded resource text, got %q", got)
	}
	if !strings.Contains(got, `{"ok":true}`) {
		t.Fatalf("expected structured content JSON, got %q", got)
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
				callFn: func(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
					return &sdkmcp.CallToolResult{
						Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
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

	reg := tools.NewRegistry()
	if got := manager.RegisterTools(reg); got != 1 {
		t.Fatalf("expected one registered tool, got %d", got)
	}
	defs := reg.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected one tool definition, got %#v", defs)
	}
	out, err := reg.ExecuteParams(context.Background(), "mcp_alpha_echo", map[string]any{})
	if err != nil {
		t.Fatalf("ExecuteParams: %v", err)
	}
	if out != "ok" {
		t.Fatalf("unexpected MCP tool output: %q", out)
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
