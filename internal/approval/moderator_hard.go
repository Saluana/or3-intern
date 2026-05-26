package approval

import (
	"path/filepath"
	"regexp"
	"strings"
)

var securityWeakeningJSONPattern = regexp.MustCompile(`(?i)"enabled"\s*:\s*(false|0|null)`)

func deterministicHardDeny(subjectType SubjectType, subject any) (ModeratorReviewResult, bool) {
	switch subjectType {
	case SubjectExec:
		sub, ok := subject.(ExecSubject)
		if !ok {
			return ModeratorReviewResult{}, false
		}
		command := strings.ToLower(strings.TrimSpace(sub.ExecutablePath + " " + strings.Join(sub.Argv, " ")))
		if matchesDestructivePattern(command) {
			return ModeratorReviewResult{
				Risk: RiskExtreme, Action: ModeratorDeny,
				Reason: "destructive command blocked by hard policy",
			}, true
		}
		if matchesSecretExfilPattern(command) {
			return ModeratorReviewResult{
				Risk: RiskExtreme, Action: ModeratorDeny,
				Reason: "possible secret exfiltration blocked by hard policy",
			}, true
		}
		if matchesSecurityWeakeningPattern(command) {
			return ModeratorReviewResult{
				Risk: RiskExtreme, Action: ModeratorDeny,
				Reason: "security weakening blocked by hard policy",
			}, true
		}
	case SubjectSecretAccess:
		return ModeratorReviewResult{
			Risk: RiskHigh, Action: ModeratorEscalate,
			Reason: "secret access requires explicit review",
		}, false
	}
	return ModeratorReviewResult{}, false
}

func matchesDestructivePattern(command string) bool {
	patterns := []string{
		"rm -rf /", "rm -rf /*", "mkfs", "dd if=", ":(){:|:&};:",
		"git push --force", "git push -f origin main", "git push -f origin master",
		"drop database", "drop table", "truncate table",
	}
	for _, pattern := range patterns {
		if strings.Contains(command, pattern) {
			return true
		}
	}
	return false
}

func matchesSecurityWeakeningPattern(command string) bool {
	lower := strings.ToLower(command)
	securityKeys := []string{
		"security.approvals.enabled",
		"security.approvals.moderator.enabled",
		"security.audit.enabled",
		"security.audit.strict",
		"security.secretstore.enabled",
		"security.network.enabled",
		"security.network.defaultdeny",
		"hardening.sandbox.enabled",
		"hardening.guardedtools",
		"hardening.privilegedtools",
		"auth.enabled",
		"security_approvals_enabled",
		"security_audit_enabled",
		"security_network_enabled",
		"hardening_sandbox_enabled",
		"hardening_guarded_tools",
	}
	disableMarkers := []string{
		"=false", ":false", " false", "disabled", "disable",
		"=0", ":0", " 0", "null",
	}
	for _, key := range securityKeys {
		if !strings.Contains(lower, key) {
			continue
		}
		for _, marker := range disableMarkers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	if strings.Contains(lower, "security") && strings.Contains(lower, ".enabled") && strings.Contains(lower, "false") {
		return true
	}
	if strings.Contains(lower, "approval") && strings.Contains(lower, ".enabled") && strings.Contains(lower, "false") {
		return true
	}
	if securityWeakeningJSONPattern.MatchString(command) &&
		(strings.Contains(lower, "security") || strings.Contains(lower, "approval") || strings.Contains(lower, "audit") || strings.Contains(lower, "sandbox")) {
		return true
	}
	configTargets := []string{"config.json", "or3-intern.json", "settings.json", ".or3-intern"}
	for _, target := range configTargets {
		if strings.Contains(lower, target) && (strings.Contains(lower, "sed ") || strings.Contains(lower, "jq ") || strings.Contains(lower, "tee ") || strings.Contains(lower, ">")) {
			for _, key := range securityKeys {
				if strings.Contains(lower, key) {
					return true
				}
			}
		}
	}
	weakeningPhrases := []string{
		"disable approval", "disable audit", "disable sandbox", "disable auth",
		"turn off approval", "turn off audit", "turn off sandbox",
		"approvals.enabled=false", "audit.enabled=false", "sandbox.enabled=false",
	}
	for _, phrase := range weakeningPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func matchesSecretExfilPattern(command string) bool {
	lower := strings.ToLower(command)
	if hasNetworkSink(lower) && hasSecretSource(lower) {
		return true
	}
	exe := strings.ToLower(filepath.Base(strings.Fields(lower)[0]))
	if exe == "grep" && strings.Contains(lower, ".env") {
		return false
	}
	return false
}

func hasNetworkSink(command string) bool {
	sinks := []string{
		"curl ", "wget ", "http://", "https://",
		" nc ", " ncat ", " netcat ",
		"python -c", "python3 -c", "perl -e",
		"openssl s_client", "gh api ", "aws s3 cp ",
	}
	for _, sink := range sinks {
		if strings.Contains(command, sink) {
			return true
		}
	}
	if strings.HasPrefix(strings.TrimSpace(command), "curl ") || strings.HasPrefix(strings.TrimSpace(command), "wget ") {
		return true
	}
	return false
}

func hasSecretSource(command string) bool {
	sources := []string{
		"api_key", "api-key", "apikey", "authorization", "bearer ",
		"token=", "password=", "passwd=", "secret=", "private_key",
		".env", "id_rsa", "id_ed25519", ".aws/credentials",
		"printenv", "process.env", "os.environ", "gh secret",
		"$api", "$token", "$secret", "${api", "${token",
	}
	for _, source := range sources {
		if strings.Contains(command, source) {
			return true
		}
	}
	if strings.Contains(command, "|") && (strings.Contains(command, "cat ") || strings.Contains(command, "printenv") || strings.Contains(command, "env ")) {
		return true
	}
	return false
}

func userPolicyBlocksExec(userPolicy, command string) (bool, string) {
	policy := strings.ToLower(strings.TrimSpace(userPolicy))
	command = strings.ToLower(command)
	if policy == "" {
		return false, ""
	}
	if strings.Contains(policy, "never use grep") && strings.Contains(command, "grep") {
		return true, "try ripgrep (rg) for bounded workspace search"
	}
	return false, ""
}
