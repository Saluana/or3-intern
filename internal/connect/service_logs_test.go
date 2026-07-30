package connect

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedServiceLogsStayBoundedAcrossRestarts(t *testing.T) {
	stateDir := t.TempDir()
	const (
		maxBytes = int64(128)
		backups  = 2
	)
	for _, name := range []string{"connect.log", "connect-error.log"} {
		if err := os.WriteFile(
			filepath.Join(stateDir, name),
			bytes.Repeat([]byte("legacy-unbounded-output"), 100),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	for restart := 0; restart < 4; restart++ {
		logs, err := openManagedServiceLogs(stateDir, maxBytes, backups)
		if err != nil {
			t.Fatalf("open logs on restart %d: %v", restart, err)
		}
		for line := 0; line < 20; line++ {
			_, _ = fmt.Fprintf(logs.Stdout, "restart=%d line=%d payload=%s\n", restart, line, strings.Repeat("x", 32))
			_, _ = fmt.Fprintf(logs.Stderr, "restart=%d error=%d payload=%s\n", restart, line, strings.Repeat("y", 32))
		}
		if err := logs.Close(); err != nil {
			t.Fatalf("close logs on restart %d: %v", restart, err)
		}
	}

	for _, base := range []string{"connect.log", "connect-error.log"} {
		var total int64
		for index := 0; index <= backups; index++ {
			name := base
			if index > 0 {
				name = fmt.Sprintf("%s.%d", base, index)
			}
			info, err := os.Stat(filepath.Join(stateDir, name))
			if err != nil {
				t.Fatalf("stat %s: %v", name, err)
			}
			if info.Size() > maxBytes {
				t.Fatalf("%s is %d bytes, want at most %d", name, info.Size(), maxBytes)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("%s permissions = %o, want 600", name, info.Mode().Perm())
			}
			total += info.Size()
		}
		if total > maxBytes*int64(backups+1) {
			t.Fatalf("%s family is %d bytes, exceeded bound", base, total)
		}
		if _, err := os.Stat(filepath.Join(stateDir, base+".3")); !os.IsNotExist(err) {
			t.Fatalf("%s retained too many backups", base)
		}
	}
}

func TestManagedServiceLogsRedactCredentialsSplitAcrossWrites(t *testing.T) {
	stateDir := t.TempDir()
	logs, err := openManagedServiceLogs(stateDir, 4096, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"Authorization: Bear",
		"er super-secret-token\n",
		"control_token=control-secret\n",
		"token=generic-token-secret\n",
		"--token tunnel-secret\n",
		"Authorization: Basic encoded-basic-secret\n",
		"jwt=eyJabcdefgh.ijklmnop.qrstuvwx\n",
	} {
		if _, err := logs.Stdout.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := logs.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(stateDir, "connect.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"super-secret-token", "control-secret", "generic-token-secret", "tunnel-secret", "encoded-basic-secret", "eyJabcdefgh.ijklmnop.qrstuvwx"} {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatalf("managed log leaked %q: %s", secret, body)
		}
	}
	if count := bytes.Count(body, []byte("<redacted>")); count < 5 {
		t.Fatalf("expected redaction markers, got %q", body)
	}
}

func TestRecentServiceDiagnosticsIsBoundedAndRedacted(t *testing.T) {
	stateDir := t.TempDir()
	secret := strings.Repeat("diagnostic-secret-", 1_000)
	if err := os.WriteFile(
		filepath.Join(stateDir, "connect-error.log"),
		[]byte(strings.Repeat("ordinary diagnostics\n", 50)+"Authorization: Bearer "+secret+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	got := RecentServiceDiagnostics(stateDir, 256)
	if len(got) > 256 {
		t.Fatalf("diagnostics length = %d, want at most 256", len(got))
	}
	if strings.Contains(got, "diagnostic-secret") || !strings.Contains(got, "<redacted>") {
		t.Fatalf("diagnostics were not redacted: %q", got)
	}
}
