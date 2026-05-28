package approval

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxModeratorReasonChars = 500

func parseModeratorResponse(raw string) (ModeratorReviewResult, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var result ModeratorReviewResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return ModeratorReviewResult{}, fmt.Errorf("moderator parse failure: %w", err)
	}
	if err := validateModeratorResult(result); err != nil {
		return ModeratorReviewResult{}, err
	}
	return result, nil
}

func validateModeratorResult(result ModeratorReviewResult) error {
	switch result.Risk {
	case RiskLow, RiskMedium, RiskHigh, RiskExtreme:
	default:
		return fmt.Errorf("moderator parse failure: unsupported risk %q", result.Risk)
	}
	switch result.Action {
	case ModeratorApprove, ModeratorEscalate, ModeratorDeny:
	default:
		return fmt.Errorf("moderator parse failure: unsupported action %q", result.Action)
	}
	if strings.TrimSpace(result.Reason) == "" {
		return fmt.Errorf("moderator parse failure: missing reason")
	}
	if len(strings.TrimSpace(result.Reason)) > maxModeratorReasonChars {
		return fmt.Errorf("moderator parse failure: reason too long")
	}
	if len(strings.TrimSpace(result.Alternative)) > maxModeratorReasonChars {
		return fmt.Errorf("moderator parse failure: alternative too long")
	}
	return nil
}
