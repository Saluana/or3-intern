package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/security"
)

const (
	defaultWebMarkdownMaxSourceBytes = 2 << 20
	defaultWebMarkdownPreviewBytes   = 2000
	maxWebMarkdownSourceBytes        = 10 << 20
)

type WebFetchMarkdown struct {
	Base
	HTTP           *http.Client
	Timeout        time.Duration
	HostPolicy     security.HostPolicy
	Store          *artifacts.Store
	Converter      *HTMLConverter
	MaxSourceBytes int
	DefaultPreview int
}

func (t *WebFetchMarkdown) Capability() CapabilityLevel { return CapabilityGuarded }

func (t *WebFetchMarkdown) Name() string { return "web_fetch_markdown" }

func (t *WebFetchMarkdown) Description() string {
	return "Fetch an HTML page, convert it to Markdown, save it as an artifact, and return a compact preview plus artifact_id for read_artifact. Use this when you want explicit HTML-to-Markdown controls; web_fetch already does this automatically for HTML by default."
}

func (t *WebFetchMarkdown) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"url":            map[string]any{"type": "string"},
		"maxSourceBytes": map[string]any{"type": "integer", "description": "Maximum HTML bytes to fetch and convert (default 2MiB, max 10MiB)"},
		"previewBytes":   map[string]any{"type": "integer", "description": "Markdown preview bytes returned directly (default 2000)"},
	}, "required": []string{"url"}}
}

func (t *WebFetchMarkdown) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *WebFetchMarkdown) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Store == nil {
		return "", fmt.Errorf("artifact store not set")
	}
	profile := ActiveProfileFromContext(ctx)
	rawURL := strings.TrimSpace(fmt.Sprint(params["url"]))
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", fmt.Errorf("invalid url")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if err := validateFetchURL(parsed); err != nil {
		return "", err
	}
	reqCtx, err := prepareWebFetchRequestContext(ctx, parsed, t.HostPolicy, profile)
	if err != nil {
		return "", err
	}
	client := t.httpClient()
	originalCheckRedirect := client.CheckRedirect
	client = security.WrapHTTPClient(client, t.HostPolicy)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= defaultWebFetchMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", defaultWebFetchMaxRedirects)
		}
		if originalCheckRedirect != nil {
			if err := originalCheckRedirect(req, via); err != nil {
				return err
			}
		}
		if err := validateFetchURL(req.URL); err != nil {
			return err
		}
		redirectCtx, err := prepareWebFetchRequestContext(req.Context(), req.URL, t.HostPolicy, profile)
		if err != nil {
			return err
		}
		*req = *req.WithContext(redirectCtx)
		return nil
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	maxSourceBytes := t.sourceLimit(params)
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxSourceBytes)+1))
	if err != nil {
		return "", err
	}
	sourceTruncated := len(body) > maxSourceBytes
	if sourceTruncated {
		body = body[:maxSourceBytes]
	}
	contentType := resp.Header.Get("Content-Type")
	info := StreamInfo{MIMEType: contentType, Extension: strings.ToLower(filepath.Ext(parsed.Path)), Filename: filepath.Base(parsed.Path), Charset: htmlCharsetFromContentType(contentType), URL: parsed.String()}
	converter := t.Converter
	if converter == nil {
		converter = NewHTMLConverter()
	}
	if !converter.Accepts(info) {
		return "", fmt.Errorf("web_fetch_markdown: unsupported content type %q for %s", contentType, parsed.String())
	}
	previewBytes := t.previewLimit(params)
	result, err := buildMarkdownFetchResult(ctx, t.Store, converter, parsed, resp.Status, resp.StatusCode, contentType, body, sourceTruncated, previewBytes, "web_fetch_markdown")
	if err != nil {
		return "", err
	}
	return EncodeToolResult(result), nil
}

func (t *WebFetchMarkdown) httpClient() *http.Client {
	if t.HTTP != nil {
		copyClient := *t.HTTP
		return &copyClient
	}
	return &http.Client{Timeout: t.effectiveTimeout()}
}

func (t *WebFetchMarkdown) effectiveTimeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return defaultWebTimeout
}

func (t *WebFetchMarkdown) sourceLimit(params map[string]any) int {
	limit := t.MaxSourceBytes
	if limit <= 0 {
		limit = defaultWebMarkdownMaxSourceBytes
	}
	if v, ok := params["maxSourceBytes"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	if limit > maxWebMarkdownSourceBytes {
		limit = maxWebMarkdownSourceBytes
	}
	return limit
}

func (t *WebFetchMarkdown) previewLimit(params map[string]any) int {
	limit := t.DefaultPreview
	if limit <= 0 {
		limit = defaultWebMarkdownPreviewBytes
	}
	if v, ok := params["previewBytes"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	return limit
}

func buildMarkdownFetchResult(ctx context.Context, store *artifacts.Store, converter *HTMLConverter, parsed *url.URL, status string, statusCode int, contentType string, body []byte, sourceTruncated bool, previewBytes int, resultKind string) (ToolResult, error) {
	if store == nil {
		return ToolResult{}, fmt.Errorf("artifact store not set")
	}
	if converter == nil {
		converter = NewHTMLConverter()
	}
	info := StreamInfo{MIMEType: contentType, Extension: strings.ToLower(filepath.Ext(parsed.Path)), Filename: filepath.Base(parsed.Path), Charset: htmlCharsetFromContentType(contentType), URL: parsed.String()}
	if !converter.Accepts(info) {
		return ToolResult{}, fmt.Errorf("web_fetch_markdown: unsupported content type %q for %s", contentType, parsed.String())
	}
	if statusCode >= http.StatusBadRequest {
		previewSource := CleanHTMLForLLM(string(body))
		if strings.TrimSpace(previewSource) == "" {
			previewSource = string(body)
		}
		preview, previewTruncated := PreviewString(previewSource, previewBytes)
		return ToolResult{
			Kind:    resultKind,
			OK:      false,
			Summary: fmt.Sprintf("Fetch failed for %s with HTTP status %s", parsed.String(), status),
			Preview: preview,
			Stats: map[string]any{
				"url":               parsed.String(),
				"mode":              "html_text",
				"status":            status,
				"status_code":       statusCode,
				"content_type":      contentType,
				"source_bytes":      len(body),
				"source_truncated":  sourceTruncated,
				"preview_bytes":     previewBytes,
				"preview_truncated": previewTruncated,
			},
		}, nil
	}
	converted, err := converter.Convert(bytes.NewReader(body), info)
	if err != nil {
		return ToolResult{}, err
	}
	if strings.TrimSpace(converted.Markdown) == "" {
		return ToolResult{}, fmt.Errorf("web_fetch_markdown: converted markdown is empty")
	}
	sessionKey := SessionFromContext(ctx)
	artifactID, err := store.Save(ctx, sessionKey, "text/markdown; charset=utf-8", []byte(converted.Markdown))
	if err != nil {
		return ToolResult{}, err
	}
	preview, previewTruncated := PreviewString(converted.Markdown, previewBytes)
	return ToolResult{
		Kind:       resultKind,
		OK:         statusCode < 400,
		Summary:    fmt.Sprintf("Fetched %s, converted HTML to Markdown, and saved it as artifact %s. Use read_artifact with this artifact_id to read only the parts needed.", parsed.String(), artifactID),
		Preview:    preview,
		ArtifactID: artifactID,
		Stats: map[string]any{
			"url":               parsed.String(),
			"mode":              "markdown",
			"status":            status,
			"status_code":       statusCode,
			"content_type":      contentType,
			"title":             converted.Title,
			"source_bytes":      len(body),
			"source_truncated":  sourceTruncated,
			"markdown_bytes":    len(converted.Markdown),
			"preview_bytes":     previewBytes,
			"preview_truncated": previewTruncated,
			"read_next":         fmt.Sprintf("read_artifact artifact_id=%s maxBytes=12000", artifactID),
		},
	}, nil
}
