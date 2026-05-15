package approval

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/db"
)

func (b *Broker) VerifyApprovalToken(ctx context.Context, token string, subjectHash string, hostID string) error {
	_, err := b.VerifyApprovalTokenClaims(ctx, token, subjectHash, hostID)
	return err
}

func (b *Broker) VerifyApprovalTokenClaims(ctx context.Context, token string, subjectHash string, hostID string) (ApprovalTokenClaims, error) {
	claims, err := b.parseApprovalToken(token)
	if err != nil {
		return ApprovalTokenClaims{}, err
	}
	now := b.now().Unix()
	if claims.ExpiresAt < now {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token expired")
	}
	if claims.ExecutionHost != strings.TrimSpace(hostID) {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token host mismatch")
	}
	if claims.SubjectHash != strings.TrimSpace(subjectHash) {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token subject mismatch")
	}
	if b.DB == nil {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token store unavailable")
	}
	record, err := b.DB.GetApprovalToken(ctx, claims.TokenID)
	if err != nil {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token record not found")
	}
	if record.RevokedAt > 0 {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token already used or revoked")
	}
	if record.SubjectHash != claims.SubjectHash {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token subject mismatch")
	}
	nowMS := b.now().UnixMilli()
	if record.ExpiresAt > 0 && record.ExpiresAt < nowMS {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token expired")
	}
	consumed, err := b.DB.ConsumeApprovalToken(ctx, claims.TokenID, nowMS)
	if err != nil {
		return ApprovalTokenClaims{}, err
	}
	if !consumed {
		return ApprovalTokenClaims{}, fmt.Errorf("approval token already used or revoked")
	}
	return claims, nil
}

func (b *Broker) issueTokenForRequest(ctx context.Context, req db.ApprovalRequestRecord, actor string) (string, error) {
	now := b.now()
	record, err := b.DB.CreateApprovalToken(ctx, db.ApprovalTokenRecord{ApprovalRequestID: req.ID, SubjectHash: req.SubjectHash, IssuedAt: now.UnixMilli(), ExpiresAt: now.Add(time.Duration(b.Config.ApprovalTokenTTLSeconds) * time.Second).UnixMilli(), Issuer: actor})
	if err != nil {
		return "", err
	}
	claims := ApprovalTokenClaims{TokenID: record.ID, RequestID: req.ID, SubjectHash: req.SubjectHash, ExecutionHost: req.ExecutionHostID, IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Duration(b.Config.ApprovalTokenTTLSeconds) * time.Second).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	signature := hex.EncodeToString(signToken(b.SignKey, payloadPart))
	_ = b.audit(ctx, "approval.token_issued", map[string]any{"request_id": req.ID, "token_id": record.ID, "subject_hash": req.SubjectHash, "host_id": req.ExecutionHostID, "actor": actor, "outcome": "issued"})
	return payloadPart + "." + signature, nil
}

func (b *Broker) parseApprovalToken(token string) (ApprovalTokenClaims, error) {
	payloadPart, signaturePart, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payloadPart == "" || signaturePart == "" {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token format")
	}
	signature, err := hex.DecodeString(signaturePart)
	if err != nil {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token signature")
	}
	expected := signToken(b.SignKey, payloadPart)
	if !hmac.Equal(signature, expected) {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token payload")
	}
	var claims ApprovalTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ApprovalTokenClaims{}, fmt.Errorf("invalid approval token payload")
	}
	return claims, nil
}

func signToken(key []byte, payload string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// CanonicalSubjectHash returns the JSON payload and hex SHA-256 hash for a
// subject. The JSON is produced by encoding/json (deterministic for Go-owned
// maps and structs with exported fields) and is suitable as the broker's
// local canonical form.
func CanonicalSubjectHash(subject any) (SubjectHash, error) {
	payload, err := marshalCanonical(subject)
	if err != nil {
		return SubjectHash{}, err
	}
	sum := sha256.Sum256([]byte(payload))
	return SubjectHash{JSON: payload, Hash: hex.EncodeToString(sum[:])}, nil
}

// marshalCanonical serializes a value with encoding/json.  The result is
// deterministic for maps and structs that Go's json package controls,
// but is not a cross-language canonical JSON format.
func marshalCanonical(value any) (string, error) {
	blob, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(blob), nil
}
