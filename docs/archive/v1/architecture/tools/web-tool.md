# Web Fetch and Search Tools

## web_fetch

Name: `web_fetch` | Capability: `guarded` | Group: `web`

Fetches HTTP(S) URLs and returns content. HTML pages are automatically converted to Markdown when the artifact store is available.

Parameters:
- `url` (required) - full http/https URL
- `maxBytes` - max preview bytes (default: 12000)
- `raw` - bypass HTML-to-Markdown conversion
- `render` - use browser (Playwright) to execute JavaScript
- `waitUntil` - for render: domcontentloaded, load, or networkidle
- `waitMs` - extra wait time for render (max 15000ms)
- `selector` - CSS selector to wait for in render mode

Source: `internal/tools/web.go:22-33`

### Hardcoded security blocks

Always blocked regardless of policy:
- `localhost`, `ip6-localhost`, `metadata.google.internal`
- Loopback IPs (127.0.0.0/8, ::1)
- Private IPs (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
- Link-local, multicast, unspecified IPs

Source: `internal/tools/web.go:341-358`

### Network policy integration

Before fetching, the URL is validated against:
1. The global host policy (if enabled)
2. The active access profile's allowed hosts (if set)

The request context carries pre-resolved DNS results to prevent DNS rebinding attacks.

Source: `internal/tools/web.go:476-497`

### Redirect handling

Redirects are validated by the wrapped HTTP client's CheckRedirect function. Each redirect target is checked against blocked hosts and network policies. Max 10 redirects.

Source: `internal/tools/web.go:556-577`

### HTML to Markdown conversion

When the response is HTML and the artifact store is available, the content is converted to Markdown and saved as an artifact. The tool returns a summary with the artifact ID instead of raw content.

Source: `internal/tools/web.go:132-151`

### Browser rendering

When `render=true`, the tool uses Playwright (via Node.js) to load the page in a headless Chromium browser and extract text. The Node.js script is written to a temp file and executed with `node render.js url waitUntil waitMs selector`.

Source: `internal/tools/web.go:228-320` (PlaywrightRenderer and playwrightRenderScript)

## web_search

Name: `web_search` | Capability: `safe` | Group: `web`

Searches the web using the Brave Search API and returns result URLs with titles and snippets.

Parameters:
- `query` (required) - search terms
- `count` - max results (default: 5, max: 10)

Source: `internal/tools/web.go:360-454`

Requires `BRAVE_API_KEY` to be configured (or a secret reference pointing to one). Results are stripped to only title, URL, and description for a predictable response format.
