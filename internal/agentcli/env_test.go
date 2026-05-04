package agentcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAgentCLIEnv_ForcedNoColorAndTerm(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user", "TERM=xterm-256color", "NO_COLOR="}
	env := BuildAgentCLIEnv(base, []string{"PATH", "HOME", "TERM", "NO_COLOR"}, nil)

	if !hasEnv(env, "NO_COLOR", "1") {
		t.Errorf("expected NO_COLOR=1, got %v", env)
	}
	if !hasEnv(env, "TERM", "dumb") {
		t.Errorf("expected TERM=dumb, got %v", env)
	}
}

func TestBuildAgentCLIEnv_KeepsAllowedVars(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user", "USER=admin", "LANG=en_US.UTF-8"}
	env := BuildAgentCLIEnv(base, []string{"PATH", "HOME", "USER"}, nil)

	if path := envValue(env, "PATH"); path == "" || !strings.HasPrefix(path, "/usr/bin") {
		t.Errorf("expected PATH preserved, got %v", env)
	}
	if !hasEnv(env, "HOME", "/home/user") {
		t.Errorf("expected HOME preserved, got %v", env)
	}
	if !hasEnv(env, "USER", "admin") {
		t.Errorf("expected USER preserved, got %v", env)
	}
	if hasEnv(env, "LANG") {
		t.Errorf("unexpected LANG in filtered env: %v", env)
	}
}

func TestBuildAgentCLIEnv_StripsOR3Secrets(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"OR3_INTERNAL_TOKEN=secret123",
		"OR3_PAIRING_SECRET=secret456",
		"OR3_NODE_SECRET=secret789",
		"OR3_SERVICE_SECRET=abc",
		"OR3_API_KEY=xyz",
		"OPENAI_API_KEY=openai-key",
		"MY_APP_KEY=allowed",
	}
	env := BuildAgentCLIEnv(base, []string{"PATH", "HOME", "OR3_INTERNAL_TOKEN", "OR3_PAIRING_SECRET", "OR3_NODE_SECRET", "OR3_SERVICE_SECRET", "OR3_API_KEY", "OPENAI_API_KEY", "MY_APP_KEY"}, nil)

	if hasEnv(env, "OR3_INTERNAL_TOKEN") {
		t.Errorf("OR3_INTERNAL_TOKEN should be stripped: %v", env)
	}
	if hasEnv(env, "OR3_PAIRING_SECRET") {
		t.Errorf("OR3_PAIRING_SECRET should be stripped: %v", env)
	}
	if hasEnv(env, "OR3_NODE_SECRET") {
		t.Errorf("OR3_NODE_SECRET should be stripped: %v", env)
	}
	if hasEnv(env, "OR3_SERVICE_SECRET") {
		t.Errorf("OR3_SERVICE_SECRET should be stripped: %v", env)
	}
	if hasEnv(env, "OR3_API_KEY") {
		t.Errorf("OR3_API_KEY should be stripped: %v", env)
	}
	if hasEnv(env, "OPENAI_API_KEY") {
		t.Errorf("OPENAI_API_KEY should be stripped: %v", env)
	}
	if !hasEnv(env, "MY_APP_KEY", "allowed") {
		t.Errorf("MY_APP_KEY should be preserved: %v", env)
	}
}

func TestBuildAgentCLIEnv_UsesAllowlist(t *testing.T) {
	base := []string{"KEEP=me", "DROP=me", "PATH=/bin"}
	env := BuildAgentCLIEnv(base, []string{"KEEP", "PATH"}, nil)

	if !hasEnv(env, "KEEP", "me") {
		t.Errorf("expected KEEP preserved, got %v", env)
	}
	if path := envValue(env, "PATH"); path == "" || !strings.HasPrefix(path, "/bin") {
		t.Errorf("expected PATH preserved, got %v", env)
	}
	if hasEnv(env, "DROP") {
		t.Errorf("unexpected DROP in env: %v", env)
	}
}

func TestBuildAgentCLIEnv_DoesNotAddPathWhenDisallowed(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".opencode", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("HOME", home)

	env := BuildAgentCLIEnv([]string{"PATH=/bin", "HOME=" + home}, []string{"HOME"}, nil)
	if hasEnv(env, "PATH") {
		t.Fatalf("PATH should not be added when omitted from allowlist: %v", env)
	}
}

func TestBuildAgentCLIEnv_DefaultsWhenEmptyAllowlist(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/home"}
	env := BuildAgentCLIEnv(base, nil, nil)
	if !hasEnv(env, "PATH") || !hasEnv(env, "HOME") {
		t.Errorf("expected PATH and HOME with empty allowlist, got %v", env)
	}
}

func TestBuildAgentCLIEnv_AppendsCommonUserCLIDirs(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".opencode", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Setenv("HOME", home)

	env := BuildAgentCLIEnv([]string{"PATH=/usr/bin", "HOME=" + home}, []string{"PATH", "HOME"}, nil)
	path := envValue(env, "PATH")
	if !strings.Contains(path, bin) {
		t.Fatalf("expected PATH %q to include %q", path, bin)
	}
}

func TestBuildAgentCLIEnv_AdditionalEnv(t *testing.T) {
	base := []string{"PATH=/bin"}
	additional := map[string]string{"CUSTOM_VAR": "custom_val", "NO_COLOR": "should_not_override"}
	env := BuildAgentCLIEnv(base, []string{"PATH", "CUSTOM_VAR"}, additional)

	if !hasEnv(env, "CUSTOM_VAR", "custom_val") {
		t.Errorf("expected CUSTOM_VAR, got %v", env)
	}
	if !hasEnv(env, "NO_COLOR", "1") {
		t.Errorf("NO_COLOR must stay 1: %v", env)
	}
}

func TestSecretStrippedEnv(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("OR3_INTERNAL_TOKEN", "leaked")

	env := SecretStrippedEnv()
	if hasEnv(env, "OR3_INTERNAL_TOKEN") {
		t.Error("SecretStrippedEnv should strip OR3 secrets")
	}
}

func hasEnv(env []string, key string, values ...string) bool {
	upper := strings.ToUpper(key)
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(k, upper) || k == key {
			if len(values) == 0 {
				return true
			}
			for _, want := range values {
				if v == want {
					return true
				}
			}
		}
	}
	return false
}
