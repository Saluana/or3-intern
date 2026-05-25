package approval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SafeSubjectPreview returns a redacted one-line summary for approval list responses.
func SafeSubjectPreview(subjectType, subjectJSON string) string {
	subjectType = strings.TrimSpace(subjectType)
	subjectJSON = strings.TrimSpace(subjectJSON)
	if subjectJSON == "" {
		return ""
	}
	switch SubjectType(subjectType) {
	case SubjectExec:
		var subject ExecSubject
		if err := json.Unmarshal([]byte(subjectJSON), &subject); err != nil {
			return "shell command"
		}
		command := FormatExecCommandDisplay(subject.ExecutablePath, subject.Argv)
		cwd := strings.TrimSpace(subject.WorkingDir)
		if command != "" && cwd != "" {
			return fmt.Sprintf("%s (cwd: %s)", command, cwd)
		}
		if command != "" {
			return command
		}
		if cwd != "" {
			return "cwd: " + cwd
		}
		return "shell command"
	case SubjectSkillExec:
		var subject SkillExecutionSubject
		if err := json.Unmarshal([]byte(subjectJSON), &subject); err != nil {
			return "skill execution"
		}
		if skill := strings.TrimSpace(subject.SkillID); skill != "" {
			return skill
		}
		return "skill execution"
	case SubjectRunnerPermission:
		var subject RunnerPermissionSubject
		if err := json.Unmarshal([]byte(subjectJSON), &subject); err != nil {
			return "runner permission"
		}
		parts := make([]string, 0, 3)
		if runner := strings.TrimSpace(subject.RunnerID); runner != "" {
			parts = append(parts, runner)
		}
		if access := strings.TrimSpace(subject.Access); access != "" {
			parts = append(parts, access)
		}
		if target := strings.TrimSpace(subject.TargetPath); target != "" {
			parts = append(parts, target)
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
		return "runner permission"
	case SubjectSecretAccess:
		return "secret access"
	case SubjectToolQuota:
		var subject ToolQuotaSubject
		if err := json.Unmarshal([]byte(subjectJSON), &subject); err != nil {
			return "tool quota"
		}
		target := strings.TrimSpace(firstNonEmpty(subject.Scope, subject.LimitName, subject.ToolName))
		if target != "" {
			return target
		}
		return "tool quota"
	default:
		return strings.TrimSpace(subjectType)
	}
}
