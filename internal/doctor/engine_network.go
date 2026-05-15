package doctor

import (
	"net"
	"net/url"
	"strings"

	"or3-intern/internal/config"
)

func networkFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if hostListContainsLiteralStar(cfg.Security.Network.AllowedHosts) {
		findings = append(findings, Finding{
			ID:       "network.literal_star",
			Area:     "network",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "security.network.allowedHosts contains *",
		})
	}
	if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
		findings = append(findings, Finding{
			ID:       "network.broad_allowlist",
			Area:     "network",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "security.network.allowedHosts is broad",
		})
	}
	if hasRemoteHTTPMCP(cfg) && (!cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny) {
		findings = append(findings, Finding{
			ID:       "network.remote_mcp_without_default_deny",
			Area:     "network",
			Severity: severityFor(opts.Mode, SeverityWarn, isHostedOrStartupMode(cfg, opts.Mode)),
			Summary:  "remote MCP transports are enabled without a meaningful deny-by-default network posture",
		})
	}
	return findings
}

func hostListContainsLiteralStar(hosts []string) bool {
	for _, host := range hosts {
		if strings.TrimSpace(host) == "*" {
			return true
		}
	}
	return false
}

func hostListTooBroad(hosts []string) bool {
	if len(hosts) > 10 {
		return true
	}
	for _, host := range hosts {
		host = strings.TrimSpace(strings.ToLower(host))
		if strings.Contains(host, "*") {
			return true
		}
	}
	return false
}

func isInsecureHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http")
}

func isLoopbackAddr(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func hasRemoteHTTPMCP(cfg config.Config) bool {
	for _, server := range cfg.Tools.MCPServers {
		if !server.Enabled {
			continue
		}
		transport := strings.ToLower(strings.TrimSpace(server.Transport))
		if transport != "sse" && transport != "streamablehttp" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(server.URL))
		if err != nil {
			return true
		}
		if !isLoopbackAddr(u.Hostname()) {
			return true
		}
	}
	return false
}
