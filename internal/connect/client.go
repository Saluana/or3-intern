package connect

import (
	"bytes"
	"context"
	"encoding/json"
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
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) Start(ctx context.Context, host HostMetadata) (DeviceAuthorization, error) {
	var result DeviceAuthorization
	err := c.post(ctx, "/api/connect/device/start", map[string]any{"host": host}, &result)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	if strings.TrimSpace(result.DeviceCode) == "" || strings.TrimSpace(result.UserCode) == "" {
		return DeviceAuthorization{}, fmt.Errorf("OR3 Cloud returned an incomplete sign-in request")
	}
	if _, err := url.ParseRequestURI(result.VerificationURI); err != nil {
		return DeviceAuthorization{}, fmt.Errorf("OR3 Cloud returned an invalid sign-in URL")
	}
	return result, nil
}

func (c *Client) Poll(ctx context.Context, deviceCode string, host HostMetadata) (DeviceTokenResponse, error) {
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
	if strings.TrimSpace(state.ControlToken) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/connect/environments/revoke", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+state.ControlToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("could not reach OR3 Cloud: %w", err)
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
		return fmt.Errorf("could not reach OR3 Cloud: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return safeHTTPError(resp)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(output); err != nil {
		return fmt.Errorf("OR3 Cloud returned an unreadable response")
	}
	return nil
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
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
	return fmt.Errorf("%s (HTTP %d)", message, resp.StatusCode)
}
