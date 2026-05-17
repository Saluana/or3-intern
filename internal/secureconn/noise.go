package secureconn

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

type NoiseHandshakeInitV1 struct {
	Version                 int    `json:"version" cbor:"version"`
	PrologueHash            string `json:"prologueHash" cbor:"prologueHash"`
	DeviceID                string `json:"deviceId" cbor:"deviceId"`
	DeviceNoisePublicKey    string `json:"deviceNoisePublicKey" cbor:"deviceNoisePublicKey"`
	DeviceEphemeralKey      string `json:"deviceEphemeralKey" cbor:"deviceEphemeralKey"`
	EnrollmentCertHash      string `json:"enrollmentCertHash" cbor:"enrollmentCertHash"`
	EncryptedInitialPayload string `json:"encryptedInitialPayload,omitempty" cbor:"encryptedInitialPayload,omitempty"`
}

type NoiseHandshakeResultV1 struct {
	Version       int    `json:"version" cbor:"version"`
	SessionKey    []byte `json:"-"`
	Transcript    string `json:"transcript" cbor:"transcript"`
	ResponseNonce string `json:"responseNonce" cbor:"responseNonce"`
}

func HostAcceptNoiseIK(identity HostIdentity, init NoiseHandshakeInitV1, prologue SessionPrologueV1) (NoiseHandshakeResultV1, error) {
	if err := identity.Validate(); err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	if init.Version != ProtocolVersion {
		return NoiseHandshakeResultV1{}, SecureConnectionError{Code: ErrorProtocolUnsupported, SafeMessage: "This app and computer use incompatible connection versions.", Retryable: false}
	}
	if strings.TrimSpace(init.DeviceID) == "" || strings.TrimSpace(init.DeviceNoisePublicKey) == "" || strings.TrimSpace(init.DeviceEphemeralKey) == "" || strings.TrimSpace(init.EnrollmentCertHash) == "" {
		return NoiseHandshakeResultV1{}, SecureConnectionError{Code: ErrorHandshakeFailed, SafeMessage: "Connection setup was incomplete.", Retryable: true}
	}
	prologueBytes, err := CanonicalBytes(prologue)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	prologueHash := HashBase64URL([]byte("OR3-NOISE-PROLOGUE-V1"), prologueBytes)
	if init.PrologueHash != prologueHash {
		return NoiseHandshakeResultV1{}, SecureConnectionError{Code: ErrorHandshakeFailed, SafeMessage: "Connection setup was not bound to this route.", Retryable: true}
	}
	hostStaticPriv, err := DecodeBase64URL(identity.HostNoisePrivateKey)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	deviceStaticPub, err := DecodeBase64URL(init.DeviceNoisePublicKey)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	deviceEphemeralPub, err := DecodeBase64URL(init.DeviceEphemeralKey)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	if len(hostStaticPriv) != 32 || len(deviceStaticPub) != 32 || len(deviceEphemeralPub) != 32 {
		return NoiseHandshakeResultV1{}, fmt.Errorf("invalid Noise key length")
	}
	es, err := curve25519.X25519(hostStaticPriv, deviceEphemeralPub)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	ss, err := curve25519.X25519(hostStaticPriv, deviceStaticPub)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	transcript := HashBase64URL([]byte("OR3-NOISE-IK-TRANSCRIPT-V1"), []byte(prologueHash), deviceStaticPub, deviceEphemeralPub, es, ss)
	key, err := deriveNoiseSessionKey(prologueHash, transcript, es, ss)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	nonce, err := RandomBase64URL(24)
	if err != nil {
		return NoiseHandshakeResultV1{}, err
	}
	return NoiseHandshakeResultV1{Version: ProtocolVersion, SessionKey: key, Transcript: transcript, ResponseNonce: nonce}, nil
}

func SealNoiseTransport(key []byte, aad, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce, err := RandomBytes(chacha20poly1305.NonceSizeX)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, aad)
	return out, nil
}

func OpenNoiseTransport(key []byte, aad, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < chacha20poly1305.NonceSizeX {
		return nil, SecureConnectionError{Code: ErrorMalformedPayload, SafeMessage: "A malformed encrypted message was blocked.", Retryable: false}
	}
	nonce := ciphertext[:chacha20poly1305.NonceSizeX]
	body := ciphertext[chacha20poly1305.NonceSizeX:]
	return aead.Open(nil, nonce, body, aad)
}

func deriveNoiseSessionKey(prologueHash, transcript string, secrets ...[]byte) ([]byte, error) {
	h := hmac.New(sha256.New, []byte(prologueHash))
	for _, secret := range secrets {
		_, _ = h.Write(secret)
	}
	reader := hkdf.New(sha256.New, h.Sum(nil), []byte(transcript), []byte("OR3-NOISE-SESSION-KEY-V1"))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}
