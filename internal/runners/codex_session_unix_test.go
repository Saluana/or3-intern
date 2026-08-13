//go:build !windows

package runners

import (
	"context"
	"os"
	"syscall"
	"testing"
)

func TestStartCodexSessionIsolatesChildProcessGroup(t *testing.T) {
	dir := t.TempDir()
	binary := writeFakeBinary(t, dir, "codex", `
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*) printf '%s\n' '{"id":1,"result":{}}' ;;
  esac
done
`)

	sess, err := startCodexSession(context.Background(), binary, codexSessionConfig{
		Env: []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("startCodexSession: %v", err)
	}

	pgid, err := syscall.Getpgid(sess.cmd.Process.Pid)
	if err != nil {
		_ = sess.Close(context.Background())
		t.Fatalf("Getpgid: %v", err)
	}
	if pgid == syscall.Getpgrp() {
		// Avoid exercising unsafe group cleanup if this invariant regresses.
		releaseProcessGroup(sess.cmd)
		_ = sess.cmd.Process.Kill()
		_ = sess.cmd.Wait()
		t.Fatal("Codex child inherited the test runner process group")
	}
	if pgid != sess.cmd.Process.Pid {
		_ = sess.Close(context.Background())
		t.Fatalf("Codex child process group = %d, want child pid %d", pgid, sess.cmd.Process.Pid)
	}
	if err := sess.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
