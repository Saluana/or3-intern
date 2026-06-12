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
