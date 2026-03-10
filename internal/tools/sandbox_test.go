package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandWithSandbox_KeepsWritableCwdWritable(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cmd, err := commandWithSandbox(context.Background(), BubblewrapConfig{
		Enabled:        true,
		BubblewrapPath: writeFakeBubblewrap(t),
		WritablePaths:  []string{root},
	}, cwd, []string{"bash", "-lc", "pwd"})
	if err != nil {
		t.Fatalf("commandWithSandbox: %v", err)
	}
	if !hasArgSequence(cmd.Args, "--bind", root, root) {
		t.Fatalf("expected writable root bind in args: %#v", cmd.Args)
	}
	if hasArgSequence(cmd.Args, "--ro-bind", cwd, cwd) {
		t.Fatalf("did not expect cwd to be rebound read-only: %#v", cmd.Args)
	}
	if !hasArgSequence(cmd.Args, "--chdir", cwd) {
		t.Fatalf("expected cwd change in args: %#v", cmd.Args)
	}
}

func hasArgSequence(args []string, seq ...string) bool {
	if len(seq) == 0 || len(args) < len(seq) {
		return false
	}
	for i := 0; i <= len(args)-len(seq); i++ {
		match := true
		for j := range seq {
			if args[i+j] != seq[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
