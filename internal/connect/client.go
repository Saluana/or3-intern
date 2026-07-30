package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	configErr  error
}

type retryableRequestError struct {
	err error
}

func (e *retryableRequestError) Error() string {
	return e.err.Error()
}

func (e *retryableRequestError) Unwrap() error {
	return e.err
}

func (e *retryableRequestError) Retryable() bool {
	return true
}

type httpStatusError struct {
	message    string
	statusCode int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s (HTTP %d)", e.message, e.statusCode)
}

func (e *httpStatusError) Retryable() bool {
	return e.statusCode == http.StatusRequestTimeout ||
		e.statusCode == http.StatusTooEarly ||
		e.statusCode == http.StatusTooManyRequests ||
		e.statusCode >= http.StatusInternalServerError
}

// IsRetryablePollError reports whether the same device-code poll can be
// retried safely. Polling is idempotent and cloud credentials have a bounded
// redelivery lease, so transport failures and transient HTTP responses retry.
func IsRetryablePollError(err error) bool {
	var retryable interface {
		Retryable() bool
	}
	return errors.As(err, &retryable) && retryable.Retryable()
}

func NewClient(baseURL string) *Client {
	normalized, err := ValidateCloudURL(baseURL)
	return &Client{
		BaseURL: normalized,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		configErr: err,
	}
}

// ValidateCloudURL returns an origin-only Connect cloud URL. Public Connect
// endpoints must use HTTPS. Plain HTTP is accepted only for an exact loopback
// host so local development never turns a hostname typo into credential
// disclosure.
func ValidateCloudURL(value string) (string, error) {
	raw := strings.TrimSpace(value)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.Host == "" {
		return "", fmt.Errorf("OR3 Cloud URL is invalid")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("OR3 Cloud URL must not contain credentials, a query, or a fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("OR3 Cloud URL must be an origin without a path")
	}
	hostname := strings.ToLower(parsed.Hostname())
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		// Public deployments are always encrypted.
	case "http":
		if !isExactLoopbackHost(hostname) {
			return "", fmt.Errorf("OR3 Cloud URL must use HTTPS except for exact loopback development URLs")
		}
	default:
		return "", fmt.Errorf("OR3 Cloud URL must use HTTPS")
	}
	if hostname == "" {
		return "", fmt.Errorf("OR3 Cloud URL is missing a hostname")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isExactLoopbackHost(hostname string) bool {
	return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
}

func (c *Client) Start(ctx context.Context, host HostMetadata) (DeviceAuthorization, error) {
	if c.configErr != nil {
		return DeviceAuthorization{}, c.configErr
	}
	var result DeviceAuthorization
	err := c.post(ctx, "/api/connect/device/start", map[string]any{"host": host}, &result)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	if strings.TrimSpace(result.DeviceCode) == "" || strings.TrimSpace(result.UserCode) == "" {
		return DeviceAuthorization{}, fmt.Errorf("OR3 Cloud returned an incomplete sign-in request")
	}
	if err := c.validateVerificationURL(result.VerificationURI); err != nil {
		return DeviceAuthorization{}, fmt.Errorf("OR3 Cloud returned an invalid sign-in URL")
	}
	if err := c.validateVerificationURL(result.VerificationURIComplete); err != nil {
		return DeviceAuthorization{}, fmt.Errorf("OR3 Cloud returned an invalid complete sign-in URL")
	}
	return result, nil
}

func (c *Client) Poll(ctx context.Context, deviceCode string, host HostMetadata) (DeviceTokenResponse, error) {
	if c.configErr != nil {
		return DeviceTokenResponse{}, c.configErr
	}
	var result DeviceTokenResponse
	err := c.post(ctx, "/api/connect/device/token", map[string]any{
		"deviceCode": deviceCode,
		"host":       host,
	}, &result)
	if err != nil {
		return DeviceTokenResponse{}, err
	}
	return result, nil
}

func (c *Client) Revoke(ctx context.Context, state State) error {
	if c.configErr != nil {
		return c.configErr
	}
	if strings.TrimSpace(state.ControlToken) == "" {
		return nil
	}
	accountID := strings.TrimSpace(state.AccountID)
	workspaceID := strings.TrimSpace(state.WorkspaceID)
	if accountID == "" || workspaceID == "" {
		return fmt.Errorf("saved connection is missing its account or workspace scope")
	}
	body, err := json.Marshal(map[string]string{
		"accountId":   accountID,
		"workspaceId": workspaceID,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/connect/environments/revoke", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+state.ControlToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &retryableRequestError{
			err: fmt.Errorf("could not reach OR3 Cloud: %w", err),
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return safeHTTPError(resp)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, input any, output any) error {
	if c.configErr != nil {
		return c.configErr
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &retryableRequestError{
			err: fmt.Errorf("could not reach OR3 Cloud: %w", err),
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return safeHTTPError(resp)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(output); err != nil {
		return &retryableRequestError{
			err: fmt.Errorf("OR3 Cloud returned an unreadable response"),
		}
	}
	return nil
}

func (c *Client) client() *http.Client {
	base := c.HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	priorCheck := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 || !sameURLOrigin(req.URL, via[0].URL) {
			return fmt.Errorf("OR3 Cloud refused a cross-origin redirect")
		}
		if priorCheck != nil {
			return priorCheck(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}

func (c *Client) validateVerificationURL(value string) error {
	verification, err := url.Parse(strings.TrimSpace(value))
	if err != nil || verification.User != nil || verification.Fragment != "" {
		return fmt.Errorf("invalid verification URL")
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil || !sameURLOrigin(verification, base) {
		return fmt.Errorf("verification URL origin does not match OR3 Cloud")
	}
	return nil
}

func sameURLOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(canonicalURLHost(left), canonicalURLHost(right))
}

func canonicalURLHost(value *url.URL) string {
	host := strings.ToLower(value.Hostname())
	port := value.Port()
	if port == "" {
		switch strings.ToLower(value.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return host + ":" + port
}

func safeHTTPError(resp *http.Response) error {
	var payload struct {
		StatusMessage string `json:"statusMessage"`
		Message       string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)
	message := strings.TrimSpace(payload.StatusMessage)
	if message == "" {
		message = strings.TrimSpace(payload.Message)
	}
	if message == "" {
		message = "OR3 Cloud could not complete the request"
	}
	return &httpStatusError{
		message:    message,
		statusCode: resp.StatusCode,
	}
}
