package agentcli

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Detect detects a runner's readiness using binary lookup, version probe, and auth check.
func Detect(ctx context.Context, spec RunnerSpec, opts DetectOptions) RunnerInfo {
	info := RunnerInfo{
		ID:          string(spec.ID),
		DisplayName: spec.DisplayName,
		BinaryName:  spec.Binary,
		Status:      RunnerStatusAvailable,
		AuthStatus:  AuthUnknown,
		Supports:    spec.Supports,
	}

	if spec.ID == RunnerOR3 {
		info.AuthStatus = AuthReady
		return info
	}

	for _, d := range opts.DisabledRunners {
		if strings.EqualFold(d, string(spec.ID)) {
			info.Status = RunnerStatusDisabledByConfig
			return info
		}
	}

	path, err := exec.LookPath(spec.Binary)
	if err != nil {
		info.Status = RunnerStatusMissing
		return info
	}
	info.BinaryPath = path

	if len(spec.VersionArgs) > 0 {
		vCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(vCtx, path, spec.VersionArgs...)
		if opts.WorkDir != "" {
			cmd.Dir = opts.WorkDir
		}
		if len(opts.Env) > 0 {
			cmd.Env = opts.Env
		}
		out, err := cmd.Output()
		if err != nil {
			if spec.ID == RunnerGemini {
				vCtx2, cancel2 := context.WithTimeout(ctx, 2*time.Second)
				defer cancel2()
				helpCmd := exec.CommandContext(vCtx2, path, "--help")
				if opts.WorkDir != "" {
					helpCmd.Dir = opts.WorkDir
				}
				if len(opts.Env) > 0 {
					helpCmd.Env = opts.Env
				}
				if helpOut, helpErr := helpCmd.Output(); helpErr == nil {
					info.Version = firstLine(helpOut)
				} else {
					info.Status = RunnerStatusError
					return info
				}
			} else {
				info.Status = RunnerStatusError
				return info
			}
		} else {
			info.Version = firstLine(out)
		}
	}

	if spec.AuthCheck != nil && len(spec.AuthCheck.Args) > 0 {
		timeout := spec.AuthCheck.Timeout
		if timeout <= 0 {
			timeout = 3
		}
		aCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		cmd := exec.CommandContext(aCtx, path, spec.AuthCheck.Args...)
		if opts.WorkDir != "" {
			cmd.Dir = opts.WorkDir
		}
		if len(opts.Env) > 0 {
			cmd.Env = opts.Env
		}
		if err := cmd.Run(); err != nil {
			info.AuthStatus = AuthMissing
			info.Status = RunnerStatusAuthMissing
		} else {
			info.AuthStatus = AuthReady
		}
	}

	if spec.ID == RunnerGemini {
		info.AuthStatus = AuthUnknown
	}

	return info
}

func firstLine(out []byte) string {
	s := strings.TrimSpace(string(out))
	if nl := strings.IndexByte(s, '\n'); nl > 0 {
		return s[:nl]
	}
	return s
}
