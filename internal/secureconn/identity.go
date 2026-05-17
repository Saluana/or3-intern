package secureconn

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/security"

	"golang.org/x/crypto/curve25519"
)

const hostIdentitySecretName = "secure-connections/host-identity-v1"

type HostIdentity struct {
	Version               int    `json:"version"`
	HostID                string `json:"host_id"`
	HostSigningPublicKey  string `json:"host_signing_public_key"`
	HostSigningPrivateKey string `json:"host_signing_private_key"`
	HostNoisePublicKey    string `json:"host_noise_public_key"`
	HostNoisePrivateKey   string `json:"host_noise_private_key"`
	Fingerprint           string `json:"fingerprint"`
	CreatedAtUnixMs       int64  `json:"created_at_unix_ms"`
}

type IdentityStore struct {
	Secrets *security.SecretManager
	Now     func() time.Time
}

func (s *IdentityStore) LoadOrCreate(ctx context.Context, displayName string) (HostIdentity, bool, error) {
	if s == nil || s.Secrets == nil {
		return HostIdentity{}, false, fmt.Errorf("secure secret store unavailable")
	}
	existing, ok, err := s.Secrets.Get(ctx, hostIdentitySecretName)
	if err != nil {
		return HostIdentity{}, false, err
	}
	if ok {
		var identity HostIdentity
		if err := json.Unmarshal([]byte(existing), &identity); err != nil {
			return HostIdentity{}, false, fmt.Errorf("decode host identity: %w", err)
		}
		if err := identity.Validate(); err != nil {
			return HostIdentity{}, false, err
		}
		return identity, false, nil
	}
	identity, err := NewHostIdentity(displayName, s.now())
	if err != nil {
		return HostIdentity{}, false, err
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return HostIdentity{}, false, err
	}
	if err := s.Secrets.Put(ctx, hostIdentitySecretName, string(encoded)); err != nil {
		return HostIdentity{}, false, err
	}
	return identity, true, nil
}

func (s *IdentityStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func NewHostIdentity(displayName string, now time.Time) (HostIdentity, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return HostIdentity{}, err
	}
	noisePriv, err := RandomBytes(32)
	if err != nil {
		return HostIdentity{}, err
	}
	clampX25519Scalar(noisePriv)
	noisePub, err := curve25519.X25519(noisePriv, curve25519.Basepoint)
	if err != nil {
		return HostIdentity{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	pubB64 := Base64URL(pub)
	noisePubB64 := Base64URL(noisePub)
	fingerprint := HostFingerprint(pubB64, noisePubB64)
	hostID := "host_" + fingerprint[:22]
	return HostIdentity{
		Version:               ProtocolVersion,
		HostID:                hostID,
		HostSigningPublicKey:  pubB64,
		HostSigningPrivateKey: Base64URL(priv),
		HostNoisePublicKey:    noisePubB64,
		HostNoisePrivateKey:   Base64URL(noisePriv),
		Fingerprint:           fingerprint,
		CreatedAtUnixMs:       now.UTC().UnixMilli(),
	}, nil
}

func HostFingerprint(signingPublicKey, noisePublicKey string) string {
	return HashBase64URL([]byte("OR3-HOST-IDENTITY-V1"), []byte(strings.TrimSpace(signingPublicKey)), []byte(strings.TrimSpace(noisePublicKey)))
}

func (i HostIdentity) Public() HostIdentityPublic {
	return HostIdentityPublic{
		Version:              i.Version,
		HostID:               i.HostID,
		HostSigningPublicKey: i.HostSigningPublicKey,
		HostNoisePublicKey:   i.HostNoisePublicKey,
		Fingerprint:          i.Fingerprint,
		CreatedAtUnixMs:      i.CreatedAtUnixMs,
	}
}

func (i HostIdentity) SigningPrivateKey() (ed25519.PrivateKey, error) {
	raw, err := DecodeBase64URL(i.HostSigningPrivateKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid host signing private key length")
	}
	return ed25519.PrivateKey(raw), nil
}

func (i HostIdentity) SigningPublicKey() (ed25519.PublicKey, error) {
	raw, err := DecodeBase64URL(i.HostSigningPublicKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid host signing public key length")
	}
	return ed25519.PublicKey(raw), nil
}

func (i HostIdentity) Validate() error {
	if i.Version != ProtocolVersion {
		return fmt.Errorf("unsupported host identity version")
	}
	if strings.TrimSpace(i.HostID) == "" {
		return fmt.Errorf("host ID required")
	}
	pub, err := i.SigningPublicKey()
	if err != nil {
		return err
	}
	priv, err := i.SigningPrivateKey()
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(pub, priv.Public().(ed25519.PublicKey)) != 1 {
		return fmt.Errorf("host signing keypair mismatch")
	}
	noisePub, err := DecodeBase64URL(i.HostNoisePublicKey)
	if err != nil {
		return err
	}
	noisePriv, err := DecodeBase64URL(i.HostNoisePrivateKey)
	if err != nil {
		return err
	}
	if len(noisePub) != 32 || len(noisePriv) != 32 {
		return fmt.Errorf("invalid host noise key length")
	}
	derived, err := curve25519.X25519(noisePriv, curve25519.Basepoint)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(noisePub, derived) != 1 {
		return fmt.Errorf("host noise keypair mismatch")
	}
	if want := HostFingerprint(i.HostSigningPublicKey, i.HostNoisePublicKey); i.Fingerprint != want {
		return fmt.Errorf("host identity fingerprint mismatch")
	}
	return nil
}

func clampX25519Scalar(k []byte) {
	if len(k) != 32 {
		return
	}
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
}

func DetectHostIdentityReplacement(expected, got HostIdentityPublic) error {
	if strings.TrimSpace(expected.HostID) == "" || strings.TrimSpace(got.HostID) == "" {
		return fmt.Errorf("host identity comparison requires both identities")
	}
	if expected.HostID != got.HostID {
		return SecureConnectionError{Code: ErrorHostIdentityChanged, SafeMessage: "This computer's identity changed. Reconnect from the desktop.", Retryable: false}
	}
	if expected.HostSigningPublicKey != got.HostSigningPublicKey || expected.HostNoisePublicKey != got.HostNoisePublicKey || expected.Fingerprint != got.Fingerprint {
		return SecureConnectionError{Code: ErrorHostIdentityChanged, SafeMessage: "This computer's identity changed. Reconnect from the desktop.", Retryable: false}
	}
	return nil
}
