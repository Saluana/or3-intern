//go:build !windows

package agentcli

import (
	"os/exec"
	"syscall"
)

func (p *ProcessManager) setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillProcessGroup sends SIGTERM followed by SIGKILL to the process group.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return err
	}
	// SIGTERM to the negative process group id
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	// SIGKILL after a grace period
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}
