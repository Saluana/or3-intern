package secureconn

import (
	"crypto/hmac"
	"crypto/sha256"
	"io"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// NoiseHandshake defines the interface for Noise handshake implementations.
// This allows migrating from the current custom DH+KDF construction to a
// vetted Noise IK state machine without changing the session manager caller.
type NoiseHandshake interface {
	Accept(identity HostIdentity, init NoiseHandshakeInitV1, prologue SessionPrologueV1) (NoiseHandshakeResultV1, error)
}

// NoiseHandshakeIKV1 implements the current OR3 Noise IK handshake.
// It performs X25519 ephemeral-static and static-static DH, derives a
// transcript hash, and produces a session key via HKDF. Both the Go host
// and the TypeScript device must use identical derivation to agree on the
// session key.
type NoiseHandshakeIKV1 struct{}

func (NoiseHandshakeIKV1) Accept(identity HostIdentity, init NoiseHandshakeInitV1, prologue SessionPrologueV1) (NoiseHandshakeResultV1, error) {
	return hostAcceptNoiseIK(identity, init, prologue)
}

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

// HostAcceptNoiseIK is the default entry point that delegates to the v1 handshake.
func HostAcceptNoiseIK(identity HostIdentity, init NoiseHandshakeInitV1, prologue SessionPrologueV1) (NoiseHandshakeResultV1, error) {
	return hostAcceptNoiseIK(identity, init, prologue)
}

func hostAcceptNoiseIK(identity HostIdentity, init NoiseHandshakeInitV1, prologue SessionPrologueV1) (NoiseHandshakeResultV1, error) {
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
		return NoiseHandshakeResultV1{}, SecureConnectionError{Code: ErrorHandshakeFailed, SafeMessage: "Connection setup used invalid key material.", Retryable: false}
	}
	// Device static key proof: the host computes es and ss, which proves the
	// device possesses the static private key corresponding to deviceStaticPub
	// (the ss DH output is only correct if the initiator knows the private key
	// for deviceStaticPub). This is verified implicitly because the session key
	// derivation includes ss, so a relay attacker without the device private key
	// cannot produce a handshake that derives the correct session key.
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

// SealNoiseTransport encrypts plaintext with XChaCha20-Poly1305 using the
// negotiated session key. The nonce is prepended to the ciphertext. Callers
// must not use the returned bytes until Open has verified the AEAD tag on the
// receiving side — this is the atomic open/decode/validate boundary.
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

// OpenNoiseTransport decrypts ciphertext with XChaCha20-Poly1305 and verifies
// the AEAD tag before returning plaintext. Callers must treat any error as a
// hard failure — no decoded frame metadata should be trusted unless this
// function succeeds.
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

// deriveNoiseSessionKey derives a 32-byte XChaCha20-Poly1305 key from the
// handshake transcript. The derivation uses HMAC-SHA-256 with prologueHash as
// key over the DH shared secrets, then HKDF-SHA-256 with the transcript as
// salt and a fixed domain-separated info string. This MUST match the
// TypeScript buildMobileNoiseHandshake derivation exactly.
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

// VerifySessionKeyDerivation is a test helper that verifies a candidate
// session key matches the expected derivation for the given inputs. This
// enables cross-platform conformance tests between Go and TypeScript.
func VerifySessionKeyDerivation(prologueHash, transcript string, es, ss []byte, candidateKey []byte) bool {
	expected, err := deriveNoiseSessionKey(prologueHash, transcript, es, ss)
	if err != nil {
		return false
	}
	if len(expected) != len(candidateKey) {
		return false
	}
	var diff byte
	for i := range expected {
		diff |= expected[i] ^ candidateKey[i]
	}
	return diff == 0
}
