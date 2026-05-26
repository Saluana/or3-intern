package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const promptBundleVersion = "context-envelope-v1"

type PromptCacheDiagnostics struct {
	PromptBundleVersion string
	StaticPromptHash    string
	SessionPromptHash   string
	TurnPromptHash      string
	StablePrefixTokens  int
	VolatileTokens      int
}

func hashPromptTier(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func buildPromptCacheDiagnostics(staticTier, sessionTier, turnTier string) PromptCacheDiagnostics {
	staticTier = strings.TrimSpace(staticTier)
	sessionTier = strings.TrimSpace(sessionTier)
	turnTier = strings.TrimSpace(turnTier)
	stable := strings.TrimSpace(staticTier + "\n\n" + sessionTier)
	return PromptCacheDiagnostics{
		PromptBundleVersion: promptBundleVersion,
		StaticPromptHash:    hashPromptTier(staticTier),
		SessionPromptHash:   hashPromptTier(sessionTier),
		TurnPromptHash:      hashPromptTier(turnTier),
		StablePrefixTokens:  estimateTextTokens(stable),
		VolatileTokens:      estimateTextTokens(turnTier),
	}
}
