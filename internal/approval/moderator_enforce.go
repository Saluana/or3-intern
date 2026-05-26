package approval

import (
	"strings"

	"or3-intern/internal/config"
)

func enforceModeratorDecision(cfg config.ApprovalModeratorConfig, result ModeratorReviewResult, subjectType SubjectType, subject any, userPolicy string) ModeratorReviewResult {
	if hard, ok := deterministicHardDeny(subjectType, subject); ok {
		return hard
	}
	if subjectType == SubjectExec {
		if sub, ok := subject.(ExecSubject); ok {
			command := strings.TrimSpace(sub.ExecutablePath + " " + strings.Join(sub.Argv, " "))
			if blocked, alt := userPolicyBlocksExec(userPolicy, command); blocked {
				result.Risk = RiskHigh
				result.Action = ModeratorDeny
				result.Reason = "blocked by user policy"
				if alt != "" {
					result.Alternative = alt
				}
				return result
			}
		}
	}
	actionMap := cfg.EffectiveActions()
	mapped := mapRiskToConfiguredAction(actionMap, result.Risk)
	if mapped != "" {
		result.Action = mapped
	}
	if result.Risk == RiskExtreme && actionMap.Extreme == config.ApprovalModeratorActionDeny {
		result.Action = ModeratorDeny
	}
	if result.Risk == RiskHigh && actionMap.High == config.ApprovalModeratorActionEscalate && result.Action == ModeratorApprove {
		result.Action = ModeratorEscalate
		result.Reason = firstNonEmpty(result.Reason, "high-risk action requires user authorization")
	}
	if cfg.RequireUserAuthHigh && (result.Risk == RiskHigh || result.Risk == RiskExtreme) && result.Action == ModeratorApprove {
		result.Action = ModeratorEscalate
		result.Reason = firstNonEmpty(result.Reason, "high-risk action requires user authorization")
	}
	result.Reason = trimModeratorReason(result.Reason)
	result.Alternative = trimModeratorReason(result.Alternative)
	return result
}

func mapRiskToConfiguredAction(actionMap config.ApprovalModeratorActionMap, risk ModeratorRisk) ModeratorAction {
	switch risk {
	case RiskLow:
		return ModeratorAction(actionMap.Low)
	case RiskMedium:
		return ModeratorAction(actionMap.Medium)
	case RiskHigh:
		return ModeratorAction(actionMap.High)
	case RiskExtreme:
		return ModeratorAction(actionMap.Extreme)
	default:
		return ""
	}
}

func trimModeratorReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 500 {
		return reason[:500]
	}
	return reason
}

func configFailureAction(cfg config.ApprovalModeratorConfig) ModeratorAction {
	switch cfg.FailureAction {
	case config.ApprovalModeratorActionDeny:
		return ModeratorDeny
	default:
		return ModeratorEscalate
	}
}
