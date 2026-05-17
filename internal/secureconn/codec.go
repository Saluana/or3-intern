package secureconn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
)

var cborEncMode = func() cbor.EncMode {
	mode, err := cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		panic(err)
	}
	return mode
}()

var cborDecMode = func() cbor.DecMode {
	mode, err := cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		TimeTag:          cbor.DecTagRequired,
		MaxArrayElements: 4096,
		MaxMapPairs:      4096,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	return mode
}()

func CanonicalBytes(value any) ([]byte, error) {
	return cborEncMode.Marshal(value)
}

func DecodeCanonical(data []byte, value any) error {
	return cborDecMode.Unmarshal(data, value)
}

func Base64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func DecodeBase64URL(raw string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
}

func RandomBytes(n int) ([]byte, error) {
	if n <= 0 {
		return nil, fmt.Errorf("random byte length must be positive")
	}
	out := make([]byte, n)
	if _, err := rand.Read(out); err != nil {
		return nil, err
	}
	return out, nil
}

func RandomBase64URL(n int) (string, error) {
	raw, err := RandomBytes(n)
	if err != nil {
		return "", err
	}
	return Base64URL(raw), nil
}

func HashBase64URL(parts ...[]byte) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write(part)
		_, _ = h.Write([]byte{0})
	}
	return Base64URL(h.Sum(nil))
}

func RendezvousCommitment(pairingSecret string) (string, error) {
	secret, err := DecodeBase64URL(pairingSecret)
	if err != nil {
		return "", fmt.Errorf("invalid pairing secret: %w", err)
	}
	if len(secret) < 32 {
		return "", fmt.Errorf("pairing secret must contain at least 256 bits")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("or3 relay rendezvous"))
	return Base64URL(mac.Sum(nil)), nil
}

func EncodePairingQR(payload PairingQRCodeV1) (string, error) {
	if payload.Version != ProtocolVersion {
		return "", fmt.Errorf("unsupported pairing QR version")
	}
	if strings.TrimSpace(payload.PairingSecret) == "" {
		return "", fmt.Errorf("pairing secret required")
	}
	secret, err := DecodeBase64URL(payload.PairingSecret)
	if err != nil {
		return "", fmt.Errorf("invalid pairing secret: %w", err)
	}
	if len(secret) < 32 {
		return "", fmt.Errorf("pairing secret must contain at least 256 bits")
	}
	if payload.ExpiresAtUnixMs <= time.Now().UTC().UnixMilli() {
		return "", fmt.Errorf("pairing QR expiry must be in the future")
	}
	encoded, err := CanonicalBytes(payload)
	if err != nil {
		return "", err
	}
	return QRPrefix + Base64URL(encoded), nil
}

func DecodePairingQR(raw string, now time.Time) (PairingQRCodeV1, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, QRPrefix) {
		return PairingQRCodeV1{}, fmt.Errorf("invalid OR3 pairing QR prefix")
	}
	body := strings.TrimPrefix(raw, QRPrefix)
	data, err := DecodeBase64URL(body)
	if err != nil {
		return PairingQRCodeV1{}, fmt.Errorf("invalid OR3 pairing QR encoding: %w", err)
	}
	var payload PairingQRCodeV1
	if err := DecodeCanonical(data, &payload); err != nil {
		return PairingQRCodeV1{}, fmt.Errorf("invalid OR3 pairing QR payload: %w", err)
	}
	if payload.Version != ProtocolVersion {
		return PairingQRCodeV1{}, fmt.Errorf("unsupported OR3 pairing QR version")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if payload.ExpiresAtUnixMs <= now.UTC().UnixMilli() {
		return PairingQRCodeV1{}, SecureConnectionError{Code: ErrorPairingExpired, SafeMessage: "This pairing code expired. Show a new one.", Retryable: true}
	}
	if _, err := RendezvousCommitment(payload.PairingSecret); err != nil {
		return PairingQRCodeV1{}, err
	}
	return payload, nil
}

func ValidateProtocolRange(minVersion, maxVersion int) error {
	if minVersion <= 0 || maxVersion <= 0 || minVersion > maxVersion {
		return SecureConnectionError{Code: ErrorProtocolUnsupported, SafeMessage: "This app and computer use incompatible connection versions.", Retryable: false}
	}
	if ProtocolVersion < minVersion || ProtocolVersion > maxVersion {
		return SecureConnectionError{Code: ErrorProtocolUnsupported, SafeMessage: "This app and computer use incompatible connection versions.", Retryable: false}
	}
	return nil
}
