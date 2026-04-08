# Dumb Issues - or3-intern Review

## 1) `breakdown.md` is fan fiction, not architecture documentation

**Where:** `breakdown.md:4-4`, `breakdown.md:20-24`, `README.md:3-3`

**Code snippet:**
```md
This is the single main process you run (e.g., `openclaw gateway`).
```

```md
Everything is file-based (super simple, no database hassle):
```

```md
`or3-intern` is a Go rewrite of nanobot with SQLite persistence, hybrid long-term memory retrieval, external channel integrations, autonomous triggers, and a hardened tool runtime.
```

**Why this is bad:**
This file is lying to the reader. It talks about `openclaw gateway` in a repo that ships `or3-intern`, and it claims the system is "file-based" with "no database hassle" while the actual project summary says SQLite persistence up front. That is not a harmless stale note. It poisons onboarding and makes every later design discussion noisier because people first have to figure out which document is real.

**Real-world consequences if left unfixed:**
New contributors will reason about the wrong architecture, cargo-cult the wrong mental model, and waste time chasing behavior that does not exist. Bad docs are not neutral. They actively generate bad code and bad review comments.

**Concrete fix:**
Either delete `breakdown.md` or rewrite it so it describes the current `or3-intern` runtime in repo-native terms.

```md
This is the single main process you run (for example `or3-intern serve`).

History and memory are persisted in SQLite. Workspace files and bootstrap docs are additional context sources, not the primary state store.
```

## 2) `web_fetch` resolves the same host multiple times because duplication apparently counts as defense now

**Where:** `internal/tools/web.go:57-62`, `internal/tools/web.go:81-106`, `internal/tools/web.go:127-139`, `internal/tools/web.go:274-283`, `internal/security/network.go:167-171`

**Code snippet:**
```go
if err := validateFetchURL(ctx, parsed); err != nil {
    return "", err
}
if err := validateURLAgainstPolicies(ctx, parsed, t.HostPolicy, profile); err != nil {
    return "", err
}
...
client = security.WrapHTTPClient(client, t.HostPolicy)
```

```go
addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
```

```go
plan, err := policy.resolveHost(req.Context(), req.URL.Hostname())
```

**Why this is bad:**
`web_fetch` does outbound host validation in at least two separate layers and usually resolves DNS more than once before the request even leaves the process. First `validateFetchURL` does its own lookup with `net.DefaultResolver`. Then policy validation can resolve again. Then `WrapHTTPClient` resolves again to pin the dial target. This is copy-pasted network policy with extra latency and extra failure modes.

Worse, the first lookup bypasses the injectable resolver hook that `internal/security/network.go` already exposes for tests. So you get the cost of duplicated logic and the pain of non-deterministic tests. That is the bad kind of clever: more code, less control.

**Real-world consequences if left unfixed:**
Every fetch is more DNS-sensitive than it needs to be. Flaky resolvers or restricted environments turn simple fetches into random failures. The duplicated logic will drift, and when it drifts you get policy behavior that depends on which copy of the rules happened to run.

**Concrete fix:**
Centralize host validation and resolution in `internal/security`, return a validated host plan once, and reuse it for both blocking rules and the actual dial target.

```go
plan, err := security.ResolveAndValidateURL(ctx, parsed, effectivePolicy)
if err != nil {
    return "", err
}

client = security.WrapHTTPClientWithPlan(client, effectivePolicy, plan)
```

At minimum, stop doing a separate `net.DefaultResolver.LookupIPAddr` inside `validateFetchURL`.

## 3) `TestWebFetch_MaxBytes` is not a test; it is a decorative no-op

**Where:** `internal/tools/web_test.go:93-109`

**Code snippet:**
```go
out, err := tool.Execute(context.Background(), map[string]any{
    "url":      "https://example.com/large",
    "maxBytes": float64(50),
})
if err != nil {
    t.Fatalf("WebFetch: %v", err)
}
// Body should be limited to 50 bytes
_ = out
```

**Why this is bad:**
The test claims to verify `maxBytes`, then throws the output in the trash and asserts nothing. That is fake coverage. The code path runs, the test turns green, and nobody actually learns whether truncation works.

**Real-world consequences if left unfixed:**
The suite gives false confidence around a size-limit control that is supposed to protect memory and output handling. Regressions slide through because the test never had teeth in the first place.

**Concrete fix:**
Assert on the returned body length or exact truncation behavior.

```go
if !strings.Contains(out, "status: 200") {
    t.Fatalf("expected status line, got %q", out)
}
parts := strings.SplitN(out, "\n\n", 2)
if len(parts) != 2 || len(parts[1]) != 50 {
    t.Fatalf("expected 50-byte body, got %d bytes in %q", len(parts[1]), out)
}
```

## 4) `TestHostPolicy_AllowsWildcardHost` is a unit test that still depends on live DNS like an amateur hour integration check

**Where:** `internal/security/network_test.go:21-26`, `internal/security/network.go:84-87`

**Code snippet:**
```go
func TestHostPolicy_AllowsWildcardHost(t *testing.T) {
    policy := HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: []string{"*.openai.com"}}
    target, _ := url.Parse("https://api.openai.com/v1/chat/completions")
    if err := policy.ValidateURL(context.Background(), target); err != nil {
        t.Fatalf("expected wildcard host allow, got %v", err)
    }
}
```

```go
addrs, err := lookupIPAddr(ctx, hostname)
if err != nil {
    return resolvedHostPlan{}, err
}
```

**Why this is bad:**
This is supposed to be a unit test for wildcard matching, but it also requires real DNS resolution for `api.openai.com`. The package already has an injectable `lookupIPAddr` hook and other tests in the same file use it correctly. This one just doesn’t bother. That is sloppy test design, not an unavoidable constraint.

**Real-world consequences if left unfixed:**
The suite fails in CI sandboxes, offline dev environments, and anywhere DNS is flaky or intentionally blocked. Engineers stop trusting the test suite because it reports network conditions instead of code correctness.

**Concrete fix:**
Stub `lookupIPAddr` in this test exactly like the other tests in the file already do.

```go
previousLookup := lookupIPAddr
defer func() { lookupIPAddr = previousLookup }()
lookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
    return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
}
```

## 5) The "disabled channels" test still hits the provider endpoint and lies about what it covers

**Where:** `cmd/or3-intern/security_setup_test.go:14-23`, `cmd/or3-intern/security_setup.go:70-74`

**Code snippet:**
```go
func TestValidateConfiguredOutboundEndpoints_IgnoresDisabledChannels(t *testing.T) {
    cfg := config.Default()
    policy := security.HostPolicy{
        Enabled:      true,
        DefaultDeny:  true,
        AllowedHosts: []string{"api.openai.com"},
    }
    if err := validateConfiguredOutboundEndpoints(context.Background(), cfg, policy); err != nil {
        t.Fatalf("expected disabled channel defaults to be ignored, got %v", err)
    }
}
```

```go
for _, endpoint := range []string{cfg.Provider.APIBase} {
    if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
        return err
    }
}
```

**Why this is bad:**
The test name says it is about disabled channel defaults. The function under test still validates the provider API base unconditionally, and `config.Default()` points that at a real host. So the test is not isolating disabled channels at all. It is accidentally a live-DNS test for the provider endpoint wearing a misleading name tag.

**Real-world consequences if left unfixed:**
When this test fails, the failure message sends people in the wrong direction. They go looking at Telegram/Slack/Discord defaults while the real problem is that the fixture still depends on provider DNS. That is how simple debugging turns into pointless thrashing.

**Concrete fix:**
Make the fixture match the test name. Clear or override `cfg.Provider.APIBase`, or explicitly stub resolution if provider validation is part of the intended coverage.

```go
cfg := config.Default()
cfg.Provider.APIBase = ""
```

Or rename the test so it says what it actually does.
