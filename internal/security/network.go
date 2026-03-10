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

var lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

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
	_, err := p.resolveHost(ctx, hostname)
	return err
}

func (p HostPolicy) resolveHost(ctx context.Context, hostname string) (resolvedHostPlan, error) {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), "[]")
	if hostname == "" {
		return resolvedHostPlan{}, fmt.Errorf("missing host")
	}
	if ip, err := netip.ParseAddr(hostname); err == nil {
		ip = ip.Unmap()
		if err := p.validateAddr(ip); err != nil {
			return resolvedHostPlan{}, err
		}
		return resolvedHostPlan{hostname: hostname, addrs: []netip.Addr{ip}}, nil
	}
	if p.EnabledPolicy() && !hostAllowed(hostname, p.AllowedHosts) && p.DefaultDeny {
		return resolvedHostPlan{}, fmt.Errorf("host denied by policy: %s", hostname)
	}
	addrs, err := lookupIPAddr(ctx, hostname)
	if err != nil {
		return resolvedHostPlan{}, err
	}
	if len(addrs) == 0 {
		return resolvedHostPlan{}, fmt.Errorf("host did not resolve")
	}
	approved := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok {
			return resolvedHostPlan{}, fmt.Errorf("host resolution failed")
		}
		ip = ip.Unmap()
		if err := p.validateAddr(ip); err != nil {
			return resolvedHostPlan{}, err
		}
		approved = append(approved, ip)
	}
	return resolvedHostPlan{hostname: hostname, addrs: approved}, nil
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
	wrappedBase := wrapTransportWithPolicy(base, policy)
	cloned.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		plan, err := policy.resolveHost(req.Context(), req.URL.Hostname())
		if err != nil {
			return nil, err
		}
		return wrappedBase.RoundTrip(req.Clone(withResolvedHostPlan(req.Context(), plan)))
	})
	return &cloned
}

type resolvedHostPlan struct {
	hostname string
	addrs    []netip.Addr
}

type resolvedHostPlanKey struct{}

func withResolvedHostPlan(ctx context.Context, plan resolvedHostPlan) context.Context {
	return context.WithValue(ctx, resolvedHostPlanKey{}, plan)
}

func resolvedHostPlanFromContext(ctx context.Context, host string) (resolvedHostPlan, bool) {
	plan, ok := ctx.Value(resolvedHostPlanKey{}).(resolvedHostPlan)
	if !ok {
		return resolvedHostPlan{}, false
	}
	if plan.hostname != strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]") {
		return resolvedHostPlan{}, false
	}
	return plan, len(plan.addrs) > 0
}

func wrapTransportWithPolicy(base http.RoundTripper, policy HostPolicy) http.RoundTripper {
	transport, ok := base.(*http.Transport)
	if !ok {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if err := policy.ValidateURL(req.Context(), req.URL); err != nil {
				return nil, err
			}
			return base.RoundTrip(req)
		})
	}
	cloned := transport.Clone()
	baseDial := cloned.DialContext
	if baseDial == nil {
		baseDial = (&net.Dialer{}).DialContext
	}
	cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialResolvedHost(ctx, network, addr, baseDial)
	}
	if cloned.DialTLSContext != nil {
		baseDialTLS := cloned.DialTLSContext
		cloned.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialResolvedHost(ctx, network, addr, baseDialTLS)
		}
	}
	return cloned
}

func dialResolvedHost(ctx context.Context, network, addr string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = ""
	}
	plan, ok := resolvedHostPlanFromContext(ctx, host)
	if !ok {
		return dial(ctx, network, addr)
	}
	var lastErr error
	for _, ip := range plan.addrs {
		target := ip.String()
		if port != "" {
			target = net.JoinHostPort(target, port)
		}
		conn, err := dial(ctx, network, target)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("host did not resolve")
	}
	return nil, lastErr
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
