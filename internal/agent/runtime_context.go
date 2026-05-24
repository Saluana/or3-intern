package agent

import (
	"os"
	"runtime"
	"strings"
	"time"
)

func metaStringValue(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	if raw, ok := meta[key]; ok {
		return strings.TrimSpace(payloadStringValue(raw))
	}
	return ""
}

func hostOSDescription() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS (Darwin)"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "freebsd":
		return "FreeBSD"
	default:
		if runtime.GOOS == "" {
			return "unknown"
		}
		return runtime.GOOS
	}
}

func hostCPUArch() string {
	arch := strings.TrimSpace(runtime.GOARCH)
	if arch == "" {
		return "unknown"
	}
	return arch
}

func hostNameLine() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return ""
	}
	return "Hostname: " + strings.TrimSpace(name)
}

func toolPolicyModeSummary(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ask":
		return "Ask mode: read-only and low-risk tools (files, memory, web, skills). No write_file, edit_file, delete_file, exec, cron, channel messaging, or privileged service tools. If a write or exec tool is unavailable, do not retry it in this turn; explain the limitation to the user or suggest switching to Work mode."
	case "work":
		return "Work mode: normal assistant — read/write files, exec, web, memory, skills, and plan tools. No privileged service, cron, or channel admin tools."
	case "admin":
		return "Admin mode: full exposed tool surface for this host, including service, cron, and channel tools when available."
	default:
		return ""
	}
}

func (b *Builder) renderRuntimeContext(toolPolicyMode string) string {
	workingDir := strings.TrimSpace(b.WorkspaceDir)
	if workingDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workingDir = strings.TrimSpace(cwd)
		}
	}
	if workingDir == "" {
		workingDir = "(unknown)"
	}
	now := time.Now()
	lines := []string{
		"Host OS: " + hostOSDescription(),
		"CPU arch: " + hostCPUArch(),
		"Local time: " + now.Format("2006-01-02 15:04:05 MST"),
		"Working directory: " + workingDir,
	}
	if host := hostNameLine(); host != "" {
		lines = append(lines, host)
	}
	toolPolicyMode = strings.TrimSpace(toolPolicyMode)
	if toolPolicyMode != "" {
		lines = append(lines, "Tool policy mode: "+toolPolicyMode)
		if summary := toolPolicyModeSummary(toolPolicyMode); summary != "" {
			lines = append(lines, summary)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
