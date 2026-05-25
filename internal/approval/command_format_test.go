package approval

import (
	"strings"
	"testing"
)

func TestFormatExecCommandDisplayQuotesUnsafeArgv(t *testing.T) {
	got := FormatExecCommandDisplay("/bin/echo", []string{"/bin/echo", "hello world", "ok"})
	if got != `/bin/echo "hello world" ok` {
		t.Fatalf("unexpected command display %q", got)
	}
}

func TestSafeSubjectPreviewExec(t *testing.T) {
	preview := SafeSubjectPreview(string(SubjectExec), `{"type":"exec","executable_path":"/bin/rm","argv":["-rf","/tmp/secret"],"working_dir":"/tmp/secret","tool_name":"exec"}`)
	if preview == "" {
		t.Fatal("expected preview")
	}
	if !strings.Contains(preview, "-rf") {
		t.Fatalf("expected command-oriented preview, got %q", preview)
	}
}
