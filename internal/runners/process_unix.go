//go:build !windows

package runners

import (
	"os/exec"
	"syscall"
	"time"
)

func (p *ProcessManager) setProcessGroup(cmd *exec.Cmd) {
	applyProcessGroup(cmd)
}

// applyProcessGroup enables process group tracking on the given command so
// the parent can SIGTERM/SIGKILL the entire process group at shutdown.
func applyProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillProcessGroup sends SIGTERM followed by SIGKILL to the process group.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return nil
	}
	// SIGTERM to the negative process group id
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	// SIGKILL after a grace period
	time.Sleep(2 * time.Second)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}
