package secureconn

import "time"

const (
	ProtocolVersion = 1
	QRPrefix        = "or3pair:v1:"

	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleAdmin    = "admin"

	PlatformIOS     = "ios"
	PlatformAndroid = "android"
	PlatformWeb     = "web"
	PlatformDesktop = "desktop"

	TrustNativeHardware = "native-hardware"
	TrustNativeSoftware = "native-software"
	TrustWebLimited     = "web-limited"
	TrustLegacy         = "legacy"

	StatusCreated         = "created"
	StatusDisplayed       = "displayed"
	StatusJoined          = "joined"
	StatusPendingApproval = "pending_approval"
	StatusApproved        = "approved"
	StatusRejected        = "rejected"
	StatusConsumed        = "consumed"
	StatusExpired         = "expired"
	StatusFailed          = "failed"
	StatusActive          = "active"
	StatusRevoked         = "revoked"

	FrameNoiseHandshake = "noiseHandshake"
	FrameNoiseTransport = "noiseTransport"
	FrameControl        = "control"

	MaxSecureFrameBodyBytes = 64 * 1024

	ErrorPairingExpired            = "PAIRING_EXPIRED"
	ErrorPairingConsumed           = "PAIRING_CONSUMED"
	ErrorPairingRejected           = "PAIRING_REJECTED"
	ErrorRelayUnavailable          = "RELAY_UNAVAILABLE"
	ErrorHandshakeFailed           = "HANDSHAKE_FAILED"
	ErrorHostIdentityChanged       = "HOST_IDENTITY_CHANGED"
	ErrorDeviceRevoked             = "DEVICE_REVOKED"
	ErrorSessionExpired            = "SESSION_EXPIRED"
	ErrorStepUpRequired            = "STEP_UP_REQUIRED"
	ErrorCapabilityDenied          = "CAPABILITY_DENIED"
	ErrorApprovalRequired          = "APPROVAL_REQUIRED"
	ErrorProtocolUnsupported       = "PROTOCOL_VERSION_UNSUPPORTED"
	ErrorLowAssuranceStorage       = "LOW_ASSURANCE_STORAGE"
	ErrorReplayDetected            = "REPLAY_DETECTED"
	ErrorMalformedPayload          = "MALFORMED_PAYLOAD"
	ErrorNoPlaintextRelayViolation = "NO_PLAINTEXT_RELAY_VIOLATION"
)

type HostIdentityPublic struct {
	Version              int    `json:"version" cbor:"version"`
	HostID               string `json:"host_id" cbor:"host_id"`
	HostSigningPublicKey string `json:"host_signing_public_key" cbor:"host_signing_public_key"`
	HostNoisePublicKey   string `json:"host_noise_public_key" cbor:"host_noise_public_key"`
	Fingerprint          string `json:"fingerprint" cbor:"fingerprint"`
	CreatedAtUnixMs      int64  `json:"created_at_unix_ms" cbor:"created_at_unix_ms"`
}

type PairingQRCodeV1 struct {
	Version              int      `json:"version" cbor:"version"`
	RelayOrigin          string   `json:"relayOrigin" cbor:"relayOrigin"`
	RendezvousID         string   `json:"rendezvousId" cbor:"rendezvousId"`
	HostID               string   `json:"hostId" cbor:"hostId"`
	HostDisplayName      string   `json:"hostDisplayName" cbor:"hostDisplayName"`
	HostSigningPublicKey string   `json:"hostSigningPublicKey" cbor:"hostSigningPublicKey"`
	HostNoisePublicKey   string   `json:"hostNoisePublicKey" cbor:"hostNoisePublicKey"`
	PairingSecret        string   `json:"pairingSecret" cbor:"pairingSecret"`
	ExpiresAtUnixMs      int64    `json:"expiresAtUnixMs" cbor:"expiresAtUnixMs"`
	RequestedAccountID   string   `json:"requestedAccountId,omitempty" cbor:"requestedAccountId,omitempty"`
	Capabilities         []string `json:"capabilities" cbor:"capabilities"`
	QRNonce              string   `json:"qrNonce" cbor:"qrNonce"`
}

type PairingIntent struct {
	RelayOrigin        string
	HostDisplayName    string
	RequestedRole      string
	Capabilities       []string
	RequestedAccountID string
	ExpiresAt          time.Time
	TTL                time.Duration
}

type PairingIntentResult struct {
	Payload          PairingQRCodeV1 `json:"payload"`
	Encoded          string          `json:"encoded"`
	SecretCommitment string          `json:"secret_commitment"`
}

type DeviceEnrollmentProposalV1 struct {
	Version                int            `json:"version" cbor:"version"`
	DeviceID               string         `json:"deviceId" cbor:"deviceId"`
	DeviceDisplayName      string         `json:"deviceDisplayName" cbor:"deviceDisplayName"`
	Platform               string         `json:"platform" cbor:"platform"`
	DeviceSigningPublicKey string         `json:"deviceSigningPublicKey" cbor:"deviceSigningPublicKey"`
	DeviceNoisePublicKey   string         `json:"deviceNoisePublicKey" cbor:"deviceNoisePublicKey"`
	RequestedRole          string         `json:"requestedRole" cbor:"requestedRole"`
	RequestedCapabilities  []string       `json:"requestedCapabilities" cbor:"requestedCapabilities"`
	AccountBinding         map[string]any `json:"accountBinding,omitempty" cbor:"accountBinding,omitempty"`
	SecureStorageEvidence  map[string]any `json:"secureStorageEvidence,omitempty" cbor:"secureStorageEvidence,omitempty"`
	CreatedAtUnixMs        int64          `json:"createdAtUnixMs" cbor:"createdAtUnixMs"`
}

type HostEnrollmentCertificateV1 struct {
	Version                int      `json:"version" cbor:"version"`
	HostID                 string   `json:"hostId" cbor:"hostId"`
	DeviceID               string   `json:"deviceId" cbor:"deviceId"`
	DeviceSigningPublicKey string   `json:"deviceSigningPublicKey" cbor:"deviceSigningPublicKey"`
	DeviceNoisePublicKey   string   `json:"deviceNoisePublicKey" cbor:"deviceNoisePublicKey"`
	Role                   string   `json:"role" cbor:"role"`
	Capabilities           []string `json:"capabilities" cbor:"capabilities"`
	TrustLevel             string   `json:"trustLevel" cbor:"trustLevel"`
	AccountID              string   `json:"accountId,omitempty" cbor:"accountId,omitempty"`
	EnrollmentEpoch        int64    `json:"enrollmentEpoch" cbor:"enrollmentEpoch"`
	IssuedAtUnixMs         int64    `json:"issuedAtUnixMs" cbor:"issuedAtUnixMs"`
	ExpiresAtUnixMs        int64    `json:"expiresAtUnixMs,omitempty" cbor:"expiresAtUnixMs,omitempty"`
	HostSigningPublicKey   string   `json:"hostSigningPublicKey" cbor:"hostSigningPublicKey"`
	Signature              string   `json:"signature" cbor:"signature"`
}

type SessionPrologueV1 struct {
	Protocol                  string `json:"protocol" cbor:"protocol"`
	Version                   int    `json:"version" cbor:"version"`
	RelayOrigin               string `json:"relayOrigin" cbor:"relayOrigin"`
	RouteID                   string `json:"routeId" cbor:"routeId"`
	HostID                    string `json:"hostId" cbor:"hostId"`
	DeviceIDHash              string `json:"deviceIdHash" cbor:"deviceIdHash"`
	EnrollmentCertificateHash string `json:"enrollmentCertificateHash" cbor:"enrollmentCertificateHash"`
	AccountID                 string `json:"accountId,omitempty" cbor:"accountId,omitempty"`
	MinProtocolVersion        int    `json:"minProtocolVersion" cbor:"minProtocolVersion"`
	MaxProtocolVersion        int    `json:"maxProtocolVersion" cbor:"maxProtocolVersion"`
}

type SecureFrameV1 struct {
	Version       int    `json:"version" cbor:"version"`
	Kind          string `json:"kind" cbor:"kind"`
	SessionID     string `json:"sessionId" cbor:"sessionId"`
	Sequence      uint64 `json:"sequence" cbor:"sequence"`
	CorrelationID string `json:"correlationId" cbor:"correlationId"`
	SentAtUnixMs  int64  `json:"sentAtUnixMs" cbor:"sentAtUnixMs"`
	Body          []byte `json:"body" cbor:"body"`
}

type SecureConnectionError struct {
	Code          string `json:"code"`
	SafeMessage   string `json:"safeMessage"`
	CorrelationID string `json:"correlationId"`
	Retryable     bool   `json:"retryable"`
}

func (e SecureConnectionError) Error() string {
	if e.SafeMessage != "" {
		return e.Code + ": " + e.SafeMessage
	}
	return e.Code
}
