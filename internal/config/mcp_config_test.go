package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefault_MCPServersEmptyMap(t *testing.T) {
	clearConfigEnv(t)
	cfg := Default()

	if cfg.Tools.MCPServers == nil {
		t.Fatal("expected Tools.MCPServers to be initialized")
	}
	if len(cfg.Tools.MCPServers) != 0 {
		t.Fatalf("expected no default MCP servers, got %#v", cfg.Tools.MCPServers)
	}
}

func TestLoad_MCPServerDefaultsRemainBackwardCompatible(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := map[string]any{
		"tools": map[string]any{
			"mcpServers": map[string]any{
				"local": map[string]any{
					"enabled": true,
					"command": "demo-server",
				},
			},
		},
	}
	b, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	server, ok := cfg.Tools.MCPServers["local"]
	if !ok {
		t.Fatal("expected local MCP server config")
	}
	if server.Transport != DefaultMCPTransport {
		t.Fatalf("expected default transport %q, got %q", DefaultMCPTransport, server.Transport)
	}
	if server.ConnectTimeoutSeconds != DefaultMCPConnectTimeoutSeconds {
		t.Fatalf("expected default connect timeout %d, got %d", DefaultMCPConnectTimeoutSeconds, server.ConnectTimeoutSeconds)
	}
	if server.ToolTimeoutSeconds != DefaultMCPToolTimeoutSeconds {
		t.Fatalf("expected default tool timeout %d, got %d", DefaultMCPToolTimeoutSeconds, server.ToolTimeoutSeconds)
	}
	if server.Env == nil || server.Headers == nil {
		t.Fatalf("expected MCP env and headers maps to be initialized, got %#v", server)
	}
}

func TestLoad_MCPHTTPValidationRejectsUnsafeURLs(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := map[string]any{
		"tools": map[string]any{
			"mcpServers": map[string]any{
				"remote": map[string]any{
					"enabled":   true,
					"transport": "sse",
					"url":       "http://example.com/mcp",
				},
			},
		},
	}
	b, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected unsafe insecure HTTP MCP URL to be rejected")
	}
}

func TestLoad_MCPHTTPValidationAllowsLoopbackWithOptIn(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := map[string]any{
		"tools": map[string]any{
			"mcpServers": map[string]any{
				"local": map[string]any{
					"enabled":            true,
					"transport":          "streamableHttp",
					"url":                "http://127.0.0.1:8080/mcp",
					"allowInsecureHttp":  true,
					"toolTimeoutSeconds": 5,
				},
			},
		},
	}
	b, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	server := cfg.Tools.MCPServers["local"]
	if server.Transport != "streamablehttp" {
		t.Fatalf("expected normalized transport, got %q", server.Transport)
	}
	if !server.AllowInsecureHTTP {
		t.Fatal("expected allowInsecureHttp to remain true")
	}
	if server.ToolTimeoutSeconds != 5 {
		t.Fatalf("expected explicit tool timeout to survive normalization, got %d", server.ToolTimeoutSeconds)
	}
}
