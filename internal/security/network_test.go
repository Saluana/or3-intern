package security

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHostPolicy_DefaultDenyBlocksUnknownHost(t *testing.T) {
	policy := HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"example.com"}}
	target, _ := url.Parse("https://api.openai.com/v1/chat/completions")
	if err := policy.ValidateURL(context.Background(), target); err == nil {
		t.Fatal("expected host policy denial")
	}
}

func TestHostPolicy_AllowsWildcardHost(t *testing.T) {
	previousLookup := lookupIPAddr
	defer func() { lookupIPAddr = previousLookup }()
	lookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}

	policy := HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"*.openai.com"}}
	target, _ := url.Parse("https://api.openai.com/v1/chat/completions")
	if err := policy.ValidateURL(context.Background(), target); err != nil {
		t.Fatalf("expected wildcard host allow, got %v", err)
	}
}

func TestWrapHTTPClient_PinsValidatedIPIntoDial(t *testing.T) {
	previousLookup := lookupIPAddr
	defer func() { lookupIPAddr = previousLookup }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	_, port, err := net.SplitHostPort(serverURL.Host)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}

	lookupCalls := 0
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		lookupCalls++
		if host != "skill-registry.test" {
			t.Fatalf("unexpected host lookup: %s", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}

	var dialedAddr string
	base := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialedAddr = addr
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	client := WrapHTTPClient(&http.Client{Transport: base}, HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"skill-registry.test"}, AllowLoopback: true})
	resp, err := client.Get("http://skill-registry.test:" + port + "/")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = resp.Body.Close()
	if lookupCalls != 1 {
		t.Fatalf("expected single DNS lookup, got %d", lookupCalls)
	}
	if strings.HasPrefix(dialedAddr, "skill-registry.test:") {
		t.Fatalf("expected dial target to be a pinned IP, got %q", dialedAddr)
	}
}

func TestWrapHTTPClient_AllowsProxyDialWithoutRevalidatingProxyHost(t *testing.T) {
	previousLookup := lookupIPAddr
	defer func() { lookupIPAddr = previousLookup }()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host := r.URL.Hostname(); host != "allowed.test" {
			t.Fatalf("expected proxied request for allowed.test, got %q", r.URL.Host)
		}
		_, _ = w.Write([]byte("proxied"))
	}))
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("url.Parse(proxy): %v", err)
	}
	_, proxyPort, err := net.SplitHostPort(proxyURL.Host)
	if err != nil {
		t.Fatalf("SplitHostPort(proxy): %v", err)
	}
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if host != "allowed.test" {
			t.Fatalf("unexpected DNS lookup for %q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}

	base := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) {
			return url.Parse("http://proxy.test:" + proxyPort)
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(addr, "proxy.test:") {
				addr = net.JoinHostPort("127.0.0.1", proxyPort)
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	client := WrapHTTPClient(&http.Client{Transport: base}, HostPolicy{
		Enabled:       true,
		DefaultDeny:   true,
		AllowedHosts:  []string{"allowed.test"},
		AllowLoopback: true,
	})
	resp, err := client.Get("http://allowed.test/resource")
	if err != nil {
		t.Fatalf("Get via proxy: %v", err)
	}
	_ = resp.Body.Close()
}
