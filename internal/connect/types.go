package connect

import "time"

const (
	DefaultCloudURL     = "https://or3.chat"
	DefaultPollInterval = 3 * time.Second
	DefaultTimeout      = 10 * time.Minute
	StateVersion        = 1
)

type DeviceAuthorization struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

type TunnelCredential struct {
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
}

type DeviceCredential struct {
	AccountID       string           `json:"accountId"`
	EnvironmentID   string           `json:"environmentId"`
	EnvironmentName string           `json:"environmentName"`
	ControlToken    string           `json:"controlToken"`
	Tunnel          TunnelCredential `json:"tunnel"`
}

type DeviceTokenResponse struct {
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
	RetryAfter int               `json:"retryAfter,omitempty"`
	Credential *DeviceCredential `json:"credential,omitempty"`
}

type State struct {
	Version         int       `json:"version"`
	CloudURL        string    `json:"cloudUrl"`
	AccountID       string    `json:"accountId"`
	EnvironmentID   string    `json:"environmentId"`
	EnvironmentName string    `json:"environmentName"`
	Hostname        string    `json:"hostname"`
	ControlToken    string    `json:"controlToken"`
	TunnelTokenFile string    `json:"tunnelTokenFile"`
	CloudflaredPath string    `json:"cloudflaredPath"`
	ConfigPath      string    `json:"configPath"`
	Installed       bool      `json:"installed"`
	ConnectedAt     time.Time `json:"connectedAt"`
}

type HostMetadata struct {
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	Architecture     string `json:"architecture"`
	InternVersion    string `json:"internVersion"`
	HostID           string `json:"hostId,omitempty"`
	SigningPublicKey string `json:"signingPublicKey,omitempty"`
	NoisePublicKey   string `json:"noisePublicKey,omitempty"`
}
