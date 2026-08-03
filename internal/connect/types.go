package connect

import "time"

const (
	DefaultCloudURL     = "https://or3.chat"
	DefaultPollInterval = 3 * time.Second
	DefaultTimeout      = 10 * time.Minute
	StateVersion        = 2
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
	// Token is retained only to read credentials issued by pre-local-config
	// OR3 deployments. New connections use the tunnel-scoped fields below.
	Token        string `json:"token,omitempty"`
	AccountTag   string `json:"accountTag,omitempty"`
	TunnelID     string `json:"tunnelId,omitempty"`
	TunnelSecret string `json:"tunnelSecret,omitempty"`
	Hostname     string `json:"hostname"`
}

type DeviceCredential struct {
	AccountID       string           `json:"accountId"`
	WorkspaceID     string           `json:"workspaceId"`
	EnvironmentID   string           `json:"environmentId"`
	EnvironmentName string           `json:"environmentName"`
	Namespace       string           `json:"namespace"`
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
	Version         int    `json:"version"`
	CloudURL        string `json:"cloudUrl"`
	AccountID       string `json:"accountId"`
	WorkspaceID     string `json:"workspaceId"`
	Namespace       string `json:"namespace,omitempty"`
	EnvironmentID   string `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	Hostname        string `json:"hostname"`
	ControlToken    string `json:"controlToken"`
	// Driver/Runtime identify the local service behind the tunnel. Empty values
	// are the legacy OR3 Intern connection and intentionally remain readable.
	Driver                string                 `json:"driver,omitempty"`
	Runtime               string                 `json:"runtime,omitempty"`
	RuntimeVersion        string                 `json:"runtimeVersion,omitempty"`
	LocalOrigin           string                 `json:"localOrigin,omitempty"`
	BasePath              string                 `json:"basePath,omitempty"`
	TunnelTokenFile       string                 `json:"tunnelTokenFile"`
	TunnelConfigFile      string                 `json:"tunnelConfigFile,omitempty"`
	TunnelCredentialsFile string                 `json:"tunnelCredentialsFile,omitempty"`
	CloudflaredPath       string                 `json:"cloudflaredPath"`
	ConfigPath            string                 `json:"configPath"`
	PreviousService       *ServiceConfigSnapshot `json:"previousService,omitempty"`
	AppliedService        *ServiceConfigSnapshot `json:"appliedService,omitempty"`
	Stage                 string                 `json:"stage,omitempty"`
	// RuntimeConfigRestored and CloudRevoked make external-runtime cleanup
	// resumable. They are written only after the corresponding cleanup step
	// succeeds, so disconnect can safely retry after an interruption.
	RuntimeConfigRestored bool      `json:"runtimeConfigRestored,omitempty"`
	CloudRevoked          bool      `json:"cloudRevoked,omitempty"`
	TerminalOnly          bool      `json:"terminalOnly,omitempty"`
	Installed             bool      `json:"installed"`
	ConnectedAt           time.Time `json:"connectedAt"`
}

type ServiceConfigSnapshot struct {
	Enabled               bool     `json:"enabled"`
	Listen                string   `json:"listen"`
	TrustedBrowserOrigins []string `json:"trustedBrowserOrigins,omitempty"`
}

type HostMetadata struct {
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	Architecture     string `json:"architecture"`
	InternVersion    string `json:"internVersion,omitempty"`
	Runtime          string `json:"runtime,omitempty"`
	RuntimeVersion   string `json:"runtimeVersion,omitempty"`
	Driver           string `json:"driver,omitempty"`
	BasePath         string `json:"basePath,omitempty"`
	HostID           string `json:"hostId,omitempty"`
	SigningPublicKey string `json:"signingPublicKey,omitempty"`
	NoisePublicKey   string `json:"noisePublicKey,omitempty"`
}
