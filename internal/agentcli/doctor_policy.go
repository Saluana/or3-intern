package agentcli

import (
	"fmt"
	"strings"
)

const doctorSessionMetaKey = "doctor_session"

// ValidateDoctorAgentRunRequest enforces Doctor/Admin chat safety bounds on
// external runner CLI runs. OR3-intern native turns use the internal tool
// registry instead; this guards the runner-chat delegation path.
func ValidateDoctorAgentRunRequest(req AgentRunRequest) error {
	if req.Meta == nil || !metaBool(req.Meta, doctorSessionMetaKey) {
		return nil
	}
	mode := RunnerMode(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = RunnerModeReview
	}
	if mode != RunnerModeReview {
		return fmt.Errorf("doctor sessions require review mode (got %q)", mode)
	}
	isolation := RunIsolation(strings.TrimSpace(req.Isolation))
	if isolation == "" {
		isolation = IsolationHostReadOnly
	}
	if isolation != IsolationHostReadOnly {
		return fmt.Errorf("doctor sessions require host-read-only isolation (got %q)", isolation)
	}
	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 4
	}
	if maxTurns > 4 {
		return fmt.Errorf("doctor sessions cap max turns at 4 (got %d)", maxTurns)
	}
	return nil
}

func metaBool(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	value, ok := meta[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func doctorAllowedToolsFromMeta(meta map[string]any) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta["doctor_allowed_tools"]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if name := strings.TrimSpace(fmt.Sprint(item)); name != "" {
				out = append(out, name)
			}
		}
		return out
	default:
		return nil
	}
}
