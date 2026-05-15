//go:build windows

package agentcli

import (
	"os/exec"
)

func (p *ProcessManager) setProcessGroup(cmd *exec.Cmd) {
	// TODO: Use Windows Job Objects for proper process-group management in a future version.
	// For v1, direct process kill is the fallback.
}

// KillProcessGroup kills the direct process on Windows. Job Objects are
// documented as future hardening for proper child process cleanup.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
