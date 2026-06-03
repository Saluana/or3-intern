# Network Host Policy

The network policy controls which outbound hosts can be contacted by tools like web_fetch and web_search.

## HostPolicy

The `HostPolicy` struct has:
- `Enabled` - whether to apply the policy at all
- `DefaultDeny` - when true, only hosts in AllowedHosts can be contacted
- `AllowedHosts` - list of allowed hostnames (exact match or wildcard like `*.example.com`)
- `AllowLoopback` - whether loopback addresses (127.0.0.1, ::1) are allowed
- `AllowPrivate` - whether private/LAN addresses are allowed

Source: `internal/security/network.go:18-24`

## URL validation

`ValidateURL` checks a URL against the policy:
1. Checks if the hostname is denied by DefaultDeny + AllowedHosts
2. Resolves the hostname to IP addresses
3. Validates each resolved IP against address restrictions

Source: `internal/security/network.go:32-35`, `internal/security/network.go:89-112`

## Address restrictions

`validateAddr` blocks:
- Invalid addresses
- Loopback addresses (unless `AllowLoopback` is true)
- Private and link-local addresses (unless `AllowPrivate` is true)
- The cloud metadata endpoint `169.254.169.254` (always blocked)

Source: `internal/security/network.go:142-156`

## Hostname matching

`hostAllowed` checks a hostname against the allowed patterns:
- Exact match (case-insensitive)
- Wildcard prefix match: `*.example.com` matches `sub.example.com` but not `example.com`

Source: `internal/security/network.go:158-179`

## HTTP client wrapping

`WrapHTTPClient` creates a new HTTP client that enforces the policy:
- Sets `CheckRedirect` to validate redirect targets against the policy
- Wraps the transport to pin resolved IPs (prevents DNS rebinding)
- For `http.Transport`, clones the transport and injects a custom `DialContext` that uses the pre-resolved IP addresses from the context

Source: `internal/security/network.go:182-262`

## Endpoint validation

`ValidateEndpoint` supports both full URLs and `host:port` strings. It detects the format and delegates to `ValidateURL` or `ValidateHost` accordingly.

Source: `internal/security/network.go:38-55`

## Integration with web tools

The web_fetch and web_search tools call `validateURLAgainstPolicies` to check both the system network policy and the active access profile's allowed hosts. The request context is prepared with `PrepareURLRequestContext` to store pre-resolved DNS results.

Source: `internal/tools/web.go:476-497` (validateURLAgainstPolicies, prepareWebFetchRequestContext)

## Hardcoded blocks

Regardless of policy settings, web_fetch always blocks:
- `localhost` and `ip6-localhost`
- `metadata.google.internal`
- Any loopback, private, link-local, multicast, or unspecified IP

Source: `internal/tools/web.go:341-358` (isBlockedFetchHostname, isBlockedFetchAddr)
