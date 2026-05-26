package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// AllowlistMatchKeys holds normalized scope/matcher columns used for indexed lookups.
type AllowlistMatchKeys struct {
	ScopeHostID          string
	ScopeTool            string
	ScopeProfile         string
	ScopeAgent           string
	MatchExecutablePath  string
	MatchWorkingDir      string
	MatchScriptHash      string
	MatchSkillID         string
	MatchPlanHash        string
	MatchRunnerID        string
	MatchTargetPath      string
	MatchPathPrefix      string
	MatchFingerprint     string
}

func allowlistMatchKeys(domain string, scope AllowlistScope, matcher any) AllowlistMatchKeys {
	keys := AllowlistMatchKeys{
		ScopeHostID:  strings.TrimSpace(scope.HostID),
		ScopeTool:    strings.TrimSpace(scope.Tool),
		ScopeProfile: strings.TrimSpace(scope.Profile),
		ScopeAgent:   strings.TrimSpace(scope.Agent),
	}
	switch SubjectType(strings.TrimSpace(domain)) {
	case SubjectExec:
		item, _ := matcher.(ExecAllowlistMatcher)
		keys.MatchExecutablePath = strings.TrimSpace(item.ExecutablePath)
		keys.MatchWorkingDir = strings.TrimSpace(item.WorkingDir)
		keys.MatchScriptHash = strings.TrimSpace(item.ScriptHash)
	case SubjectSkillExec:
		item, _ := matcher.(SkillAllowlistMatcher)
		keys.MatchSkillID = strings.TrimSpace(item.SkillID)
		keys.MatchPlanHash = strings.TrimSpace(item.PlanHash)
		keys.MatchScriptHash = strings.TrimSpace(item.ScriptHash)
	case SubjectRunnerPermission:
		item, _ := matcher.(RunnerPermissionAllowlistMatcher)
		keys.MatchRunnerID = strings.TrimSpace(item.RunnerID)
		keys.MatchTargetPath = strings.TrimSpace(item.TargetPath)
		keys.MatchPathPrefix = strings.TrimSpace(item.PathPrefix)
	}
	scopeJSON, _ := marshalCanonical(scope)
	matcherJSON, _ := marshalCanonical(matcher)
	sum := sha256.Sum256([]byte(strings.TrimSpace(domain) + "\n" + scopeJSON + "\n" + matcherJSON))
	keys.MatchFingerprint = hex.EncodeToString(sum[:])
	return keys
}
