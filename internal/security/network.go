package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type HostPolicy struct {
	Enabled       bool
	DefaultDeny   bool
	AllowedHosts  []string
	AllowLoopback bool
	AllowPrivate  bool
}

func (p HostPolicy) EnabledPolicy() bool {
	return p.Enabled || p.DefaultDeny || len(p.AllowedHosts) > 0
}

func (p HostPolicy) ValidateURL(ctx context.Context, target *url.URL) error {
	if target == nil {
		return fmt.Errorf("invalid url")
	}
	hostname := strings.TrimSpace(strings.ToLower(target.Hostname()))
	if hostname == "" {
		return fmt.Errorf("missing host")
	}
	return p.ValidateHost(ctx, hostname)
}

func (p HostPolicy) ValidateEndpoint(ctx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid endpoint: %w", err)
		}
		return p.ValidateURL(ctx, u)
	}
	host, _, err := net.SplitHostPort(raw)
	if err == nil {
		return p.ValidateHost(ctx, strings.Trim(host, "[]"))
	}
	return p.ValidateHost(ctx, raw)
}

func (p HostPolicy) ValidateHost(ctx context.Context, hostname string) error {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), "[]")
	if hostname == "" {
		return fmt.Errorf("missing host")
	}
	if ip, err := netip.ParseAddr(hostname); err == nil {
		return p.validateAddr(ip.Unmap())
	}
	if p.EnabledPolicy() && !hostAllowed(hostname, p.AllowedHosts) && p.DefaultDeny {
		return fmt.Errorf("host denied by policy: %s", hostname)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host did not resolve")
	}
	for _, addr := range addrs {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok {
			return fmt.Errorf("host resolution failed")
		}
		if err := p.validateAddr(ip.Unmap()); err != nil {
			return err
		}
	}
	return nil
}

func (p HostPolicy) validateAddr(addr netip.Addr) error {
	if !addr.IsValid() {
		return fmt.Errorf("invalid host address")
	}
	if !p.AllowLoopback && addr.IsLoopback() {
		return fmt.Errorf("host denied by policy: loopback")
	}
	if !p.AllowPrivate && (addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified()) {
		return fmt.Errorf("host denied by policy: private address")
	}
	if addr.String() == "169.254.169.254" {
		return fmt.Errorf("host denied by policy: metadata endpoint")
	}
	return nil
}

func hostAllowed(host string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	host = strings.ToLower(strings.TrimSpace(host))
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if pattern == host {
			return true
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		}
	}
	return false
}

func WrapHTTPClient(client *http.Client, policy HostPolicy) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	cloned := *client
	prevCheckRedirect := cloned.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := policy.ValidateURL(req.Context(), req.URL); err != nil {
			return err
		}
		if prevCheckRedirect != nil {
			return prevCheckRedirect(req, via)
		}
		return nil
	}
	base := cloned.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cloned.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if err := policy.ValidateURL(req.Context(), req.URL); err != nil {
			return nil, err
		}
		return base.RoundTrip(req)
	})
	return &cloned
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
