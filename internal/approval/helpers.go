package approval

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

func resolutionKind(alwaysAllow bool) string {
	if alwaysAllow {
		return ResolutionKindApproveAndAllowlist
	}
	return ResolutionKindApproveOnce
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleViewer:
		return RoleViewer
	case RoleOperator:
		return RoleOperator
	case RoleServiceClient:
		return RoleServiceClient
	case RoleWebUI:
		return RoleWebUI
	case RoleNode:
		return RoleNode
	case RoleAdmin:
		return RoleAdmin
	default:
		return ""
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", nil
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomDigits(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(length)), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", length, n.Int64()), nil
}

func hashBytes(raw string) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return sum[:]
}

func (b *Broker) hashPairingCode(code string) []byte {
	code = strings.TrimSpace(strings.ReplaceAll(code, "-", ""))
	if code == "" {
		return nil
	}
	if b == nil || len(b.SignKey) == 0 {
		return hashBytes(code)
	}
	mac := hmac.New(sha256.New, b.SignKey)
	_, _ = mac.Write([]byte("or3.pairing-code.v1\x00"))
	_, _ = mac.Write([]byte(code))
	return mac.Sum(nil)
}
