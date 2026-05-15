package doctor

import (
	"fmt"
	"strings"

	"or3-intern/internal/config"
)

func mcpFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if len(cfg.Tools.MCPServers) == 0 {
		return findings
	}
	for name, server := range cfg.Tools.MCPServers {
		if !server.Enabled {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(server.Transport)) {
		case "stdio":
			if len(server.ChildEnvAllowlist) == 0 {
				findings = append(findings, Finding{
					ID:       "mcp.stdio_child_env_missing",
					Area:     "mcp",
					Severity: SeverityWarn,
					Summary:  fmt.Sprintf("server %q uses stdio without a server childEnvAllowlist", name),
				})
				if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
					findings = append(findings, Finding{
						ID:       "mcp.stdio_global_child_env_missing",
						Area:     "mcp",
						Severity: SeverityWarn,
						Summary:  fmt.Sprintf("server %q uses stdio with no server or global child environment allowlist", name),
					})
				}
			}
		case "sse", "streamablehttp":
			if server.AllowInsecureHTTP || isInsecureHTTPURL(server.URL) {
				findings = append(findings, Finding{
					ID:       "mcp.http_insecure",
					Area:     "mcp",
					Severity: SeverityWarn,
					Summary:  fmt.Sprintf("server %q uses insecure HTTP transport", name),
				})
			}
			if !cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny {
				findings = append(findings, Finding{
					ID:       "mcp.http_no_default_deny",
					Area:     "mcp",
					Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
					Summary:  fmt.Sprintf("server %q uses remote HTTP transport without deny-by-default network policy", name),
				})
			}
			if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
				findings = append(findings, Finding{
					ID:       "mcp.http_broad_allowlist",
					Area:     "mcp",
					Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
					Summary:  fmt.Sprintf("server %q relies on a broad network allowlist", name),
				})
			}
		}
	}
	return findings
}
