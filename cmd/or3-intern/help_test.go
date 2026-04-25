package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseRootCLIArgs_RootHelp(t *testing.T) {
	cfgPath, args, showHelp, unsafeDev, advanced, err := parseRootCLIArgs([]string{"--config", "/tmp/config.json", "--help", "approvals"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseRootCLIArgs: %v", err)
	}
	if cfgPath != "/tmp/config.json" {
		t.Fatalf("expected cfgPath to be preserved, got %q", cfgPath)
	}
	if !showHelp {
		t.Fatal("expected showHelp to be true")
	}
	if unsafeDev {
		t.Fatal("expected unsafeDev to default to false")
	}
	if advanced {
		t.Fatal("expected advanced help to default to false")
	}
	if len(args) != 1 || args[0] != "approvals" {
		t.Fatalf("expected approvals topic args, got %#v", args)
	}
}

func TestMaybeHandleHelpRequest_RootCommandHelp(t *testing.T) {
	var out bytes.Buffer
	handled, err := maybeHandleHelpRequest([]string{"help", "skills"}, &out)
	if err != nil {
		t.Fatalf("maybeHandleHelpRequest: %v", err)
	}
	if !handled {
		t.Fatal("expected help request to be handled")
	}
	got := out.String()
	if !strings.Contains(got, "Usage:\n  or3-intern skills <list|info|check|search|install|update|remove> ...") {
		t.Fatalf("expected skills usage in help output, got %q", got)
	}
	if !strings.Contains(got, "Subcommands:") {
		t.Fatalf("expected subcommands section, got %q", got)
	}
}

func TestMaybeHandleHelpRequest_TrailingHelpFlag(t *testing.T) {
	var out bytes.Buffer
	handled, err := maybeHandleHelpRequest([]string{"approvals", "approve", "-h"}, &out)
	if err != nil {
		t.Fatalf("maybeHandleHelpRequest: %v", err)
	}
	if !handled {
		t.Fatal("expected help request to be handled")
	}
	got := out.String()
	if !strings.Contains(got, "or3-intern approvals approve <id> [--allowlist] [--note text]") {
		t.Fatalf("expected approvals approve usage, got %q", got)
	}
	if !strings.Contains(got, "--allowlist") {
		t.Fatalf("expected approve flags in output, got %q", got)
	}
}

func TestPrintHelpTopic_Root(t *testing.T) {
	var out bytes.Buffer
	if err := printHelpTopic(&out, nil); err != nil {
		t.Fatalf("printHelpTopic: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Simple commands:") {
		t.Fatalf("expected simple commands section, got %q", got)
	}
	if !strings.Contains(got, "or3-intern help [command]") {
		t.Fatalf("expected root usage help, got %q", got)
	}
	if strings.Contains(got, "config-path") {
		t.Fatalf("did not expect advanced commands in default root help, got %q", got)
	}
	if !strings.Contains(got, "connect-device") {
		t.Fatalf("expected connect-device command in root help, got %q", got)
	}
}

func TestHelpTopicPath_IgnoresFlagsAfterCommandPath(t *testing.T) {
	path := helpTopicPath([]string{"pairing", "request", "--channel", "slack", "-h"})
	if len(path) != 2 || path[0] != "pairing" || path[1] != "request" {
		t.Fatalf("unexpected help topic path: %#v", path)
	}
}

func TestPrintHelpTopic_ConfigPath(t *testing.T) {
	var out bytes.Buffer
	if err := printHelpTopic(&out, []string{"config-path"}); err != nil {
		t.Fatalf("printHelpTopic: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Usage:\n  or3-intern config-path") {
		t.Fatalf("expected config-path usage, got %q", got)
	}
	if !strings.Contains(got, "Print the resolved path to config.json") {
		t.Fatalf("expected config-path summary, got %q", got)
	}
}

func TestPrintHelpTopic_Configure(t *testing.T) {
	var out bytes.Buffer
	if err := printHelpTopic(&out, []string{"configure"}); err != nil {
		t.Fatalf("printHelpTopic: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "or3-intern configure") {
		t.Fatalf("expected configure usage, got %q", got)
	}
	if !strings.Contains(got, "provider, storage, workspace, web, channels, service") {
		t.Fatalf("expected section list, got %q", got)
	}
	if !strings.Contains(got, "Bubble Tea setup UI") {
		t.Fatalf("expected configure help to mention interactive TUI mode, got %q", got)
	}
	if !strings.Contains(got, "plain text prompt flow") {
		t.Fatalf("expected configure help to mention non-interactive fallback, got %q", got)
	}
}

func TestCfgPathOrDefault(t *testing.T) {
	t.Run("explicit path", func(t *testing.T) {
		if got := cfgPathOrDefault("/tmp/custom-config.json"); got != "/tmp/custom-config.json" {
			t.Fatalf("expected explicit path, got %q", got)
		}
	})

	t.Run("default path", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		got := cfgPathOrDefault("")
		want := home + "/.or3-intern/config.json"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}
