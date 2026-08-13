//go:build windows

package runners

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsProcessJob struct {
	handle windows.Handle
	stop   context.CancelFunc
}

var windowsProcessJobs sync.Map

func (p *ProcessManager) setProcessGroup(cmd *exec.Cmd) {
	applyProcessGroup(cmd)
}

func applyProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func attachProcessGroup(ctx context.Context, cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return err
	}
	err = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("assign process to job object: %w", err)
	}
	watchCtx, stop := context.WithCancel(context.Background())
	windowsProcessJobs.Store(cmd, windowsProcessJob{handle: job, stop: stop})
	go func() {
		select {
		case <-ctx.Done():
			_ = KillProcessGroup(cmd)
		case <-watchCtx.Done():
		}
	}()
	return nil
}

func releaseProcessGroup(cmd *exec.Cmd) {
	if raw, ok := windowsProcessJobs.LoadAndDelete(cmd); ok {
		job := raw.(windowsProcessJob)
		job.stop()
		_ = windows.CloseHandle(job.handle)
	}
}

// KillProcessGroup kills the direct process on Windows. Job Objects are
// documented as future hardening for proper child process cleanup.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if raw, ok := windowsProcessJobs.LoadAndDelete(cmd); ok {
		job := raw.(windowsProcessJob)
		job.stop()
		err := windows.TerminateJobObject(job.handle, 1)
		_ = windows.CloseHandle(job.handle)
		return err
	}
	return cmd.Process.Kill()
}
