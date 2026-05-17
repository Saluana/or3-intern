package secureconn

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"
)

var enrollmentDomain = []byte("OR3-ENROLLMENT-CERTIFICATE-V1")
var enrollmentProposalDomain = []byte("OR3-ENROLLMENT-PROPOSAL-V1")

func NewEnrollmentCertificate(identity HostIdentity, proposal DeviceEnrollmentProposalV1, role string, capabilities []string, trustLevel, accountID string, epoch int64, expiresAt time.Time, now time.Time) (HostEnrollmentCertificateV1, error) {
	if err := identity.Validate(); err != nil {
		return HostEnrollmentCertificateV1{}, err
	}
	if err := ValidateEnrollmentProposal(proposal, now); err != nil {
		return HostEnrollmentCertificateV1{}, err
	}
	if role = NormalizeRole(role); role == "" {
		return HostEnrollmentCertificateV1{}, fmt.Errorf("invalid enrollment role")
	}
	if trustLevel = NormalizeTrustLevel(trustLevel, proposal.Platform); trustLevel == "" {
		return HostEnrollmentCertificateV1{}, fmt.Errorf("invalid trust level")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cert := HostEnrollmentCertificateV1{
		Version:                ProtocolVersion,
		HostID:                 identity.HostID,
		DeviceID:               strings.TrimSpace(proposal.DeviceID),
		DeviceSigningPublicKey: strings.TrimSpace(proposal.DeviceSigningPublicKey),
		DeviceNoisePublicKey:   strings.TrimSpace(proposal.DeviceNoisePublicKey),
		Role:                   role,
		Capabilities:           NormalizeCapabilities(capabilities),
		TrustLevel:             trustLevel,
		AccountID:              strings.TrimSpace(accountID),
		EnrollmentEpoch:        epoch,
		IssuedAtUnixMs:         now.UTC().UnixMilli(),
		HostSigningPublicKey:   identity.HostSigningPublicKey,
	}
	if !expiresAt.IsZero() {
		cert.ExpiresAtUnixMs = expiresAt.UTC().UnixMilli()
	}
	priv, err := identity.SigningPrivateKey()
	if err != nil {
		return HostEnrollmentCertificateV1{}, err
	}
	signed, err := enrollmentSigningBytes(cert)
	if err != nil {
		return HostEnrollmentCertificateV1{}, err
	}
	cert.Signature = Base64URL(ed25519.Sign(priv, signed))
	return cert, nil
}

func VerifyEnrollmentCertificate(cert HostEnrollmentCertificateV1, now time.Time) error {
	if err := validateEnrollmentCertificateShape(cert); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if cert.ExpiresAtUnixMs > 0 && cert.ExpiresAtUnixMs <= now.UTC().UnixMilli() {
		return SecureConnectionError{Code: ErrorSessionExpired, SafeMessage: "This device enrollment expired. Pair again from the desktop.", Retryable: false}
	}
	pubRaw, err := DecodeBase64URL(cert.HostSigningPublicKey)
	if err != nil {
		return fmt.Errorf("invalid host signing public key: %w", err)
	}
	if len(pubRaw) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid host signing public key length")
	}
	sig, err := DecodeBase64URL(cert.Signature)
	if err != nil {
		return fmt.Errorf("invalid enrollment signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid enrollment signature length")
	}
	signed, err := enrollmentSigningBytes(cert)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pubRaw), signed, sig) {
		return fmt.Errorf("invalid enrollment certificate signature")
	}
	return nil
}

func EnrollmentCertificateHash(cert HostEnrollmentCertificateV1) (string, error) {
	bytes, err := CanonicalBytes(cert)
	if err != nil {
		return "", err
	}
	return HashBase64URL([]byte("OR3-ENROLLMENT-CERTIFICATE-HASH-V1"), bytes), nil
}

func enrollmentSigningBytes(cert HostEnrollmentCertificateV1) ([]byte, error) {
	cert.Signature = ""
	encoded, err := CanonicalBytes(cert)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(enrollmentDomain)+len(encoded))
	out = append(out, enrollmentDomain...)
	out = append(out, encoded...)
	return out, nil
}

func ValidateEnrollmentProposal(proposal DeviceEnrollmentProposalV1, now time.Time) error {
	if proposal.Version != ProtocolVersion {
		return fmt.Errorf("unsupported enrollment proposal version")
	}
	if strings.TrimSpace(proposal.DeviceID) == "" {
		return fmt.Errorf("device ID required")
	}
	if NormalizeRole(proposal.RequestedRole) == "" {
		return fmt.Errorf("invalid requested role")
	}
	if NormalizePlatform(proposal.Platform) == "" {
		return fmt.Errorf("invalid platform")
	}
	if proposal.DeviceSigningPublicKey == "" || proposal.DeviceNoisePublicKey == "" {
		return fmt.Errorf("device public keys required")
	}
	if signing, err := DecodeBase64URL(proposal.DeviceSigningPublicKey); err != nil {
		return fmt.Errorf("invalid device signing public key: %w", err)
	} else if len(signing) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid device signing public key length")
	}
	if noise, err := DecodeBase64URL(proposal.DeviceNoisePublicKey); err != nil {
		return fmt.Errorf("invalid device noise public key: %w", err)
	} else if len(noise) != 32 {
		return fmt.Errorf("invalid device noise public key length")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if proposal.CreatedAtUnixMs <= 0 || proposal.CreatedAtUnixMs > now.Add(5*time.Minute).UTC().UnixMilli() {
		return fmt.Errorf("invalid enrollment proposal timestamp")
	}
	return nil
}

func VerifyEnrollmentProposalSignature(proposal DeviceEnrollmentProposalV1, now time.Time) error {
	if err := ValidateEnrollmentProposal(proposal, now); err != nil {
		return err
	}
	if strings.TrimSpace(proposal.Signature) == "" {
		return fmt.Errorf("enrollment proposal signature required")
	}
	pubRaw, err := DecodeBase64URL(proposal.DeviceSigningPublicKey)
	if err != nil {
		return fmt.Errorf("invalid device signing public key: %w", err)
	}
	if len(pubRaw) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid device signing public key length")
	}
	sig, err := DecodeBase64URL(proposal.Signature)
	if err != nil {
		return fmt.Errorf("invalid enrollment proposal signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid enrollment proposal signature length")
	}
	signed, err := EnrollmentProposalSigningBytes(proposal)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pubRaw), signed, sig) {
		return fmt.Errorf("invalid enrollment proposal signature")
	}
	return nil
}

func EnrollmentProposalSigningBytes(proposal DeviceEnrollmentProposalV1) ([]byte, error) {
	proposal.Signature = ""
	encoded, err := CanonicalBytes(proposal)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(enrollmentProposalDomain)+len(encoded))
	out = append(out, enrollmentProposalDomain...)
	out = append(out, encoded...)
	return out, nil
}

func validateEnrollmentCertificateShape(cert HostEnrollmentCertificateV1) error {
	if cert.Version != ProtocolVersion {
		return fmt.Errorf("unsupported enrollment certificate version")
	}
	if strings.TrimSpace(cert.HostID) == "" || strings.TrimSpace(cert.DeviceID) == "" {
		return fmt.Errorf("host ID and device ID are required")
	}
	if NormalizeRole(cert.Role) == "" {
		return fmt.Errorf("invalid enrollment role")
	}
	if NormalizeTrustLevel(cert.TrustLevel, "") == "" {
		return fmt.Errorf("invalid trust level")
	}
	if cert.EnrollmentEpoch <= 0 || cert.IssuedAtUnixMs <= 0 {
		return fmt.Errorf("invalid enrollment timestamps")
	}
	if strings.TrimSpace(cert.Signature) == "" {
		return fmt.Errorf("enrollment signature required")
	}
	if signing, err := DecodeBase64URL(cert.DeviceSigningPublicKey); err != nil {
		return fmt.Errorf("invalid device signing public key: %w", err)
	} else if len(signing) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid device signing public key length")
	}
	if noise, err := DecodeBase64URL(cert.DeviceNoisePublicKey); err != nil {
		return fmt.Errorf("invalid device noise public key: %w", err)
	} else if len(noise) != 32 {
		return fmt.Errorf("invalid device noise public key length")
	}
	return nil
}

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleViewer:
		return RoleViewer
	case "", RoleOperator:
		return RoleOperator
	case RoleAdmin:
		return RoleAdmin
	default:
		return ""
	}
}

func NormalizePlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformIOS:
		return PlatformIOS
	case PlatformAndroid:
		return PlatformAndroid
	case PlatformWeb:
		return PlatformWeb
	case PlatformDesktop:
		return PlatformDesktop
	default:
		return ""
	}
}

func NormalizeTrustLevel(trustLevel, platform string) string {
	switch strings.ToLower(strings.TrimSpace(trustLevel)) {
	case TrustNativeHardware:
		return TrustNativeHardware
	case TrustNativeSoftware:
		return TrustNativeSoftware
	case TrustWebLimited:
		return TrustWebLimited
	case TrustLegacy:
		return TrustLegacy
	}
	if NormalizePlatform(platform) == PlatformWeb {
		return TrustWebLimited
	}
	if NormalizePlatform(platform) != "" {
		return TrustNativeSoftware
	}
	return ""
}

func NormalizeCapabilities(capabilities []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" || seen[capability] {
			continue
		}
		seen[capability] = true
		out = append(out, capability)
	}
	return out
}
