package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestScanWithOptions_ParsesOpenClawFrontMatter(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "alpha", `---
name: alpha-skill
description: Handles explicit alpha tasks
homepage: https://example.com/alpha
user-invocable: true
disable-model-invocation: true
command-dispatch: tool
command-tool: exec
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: alpha
    primaryEnv: ALPHA_KEY
---
# Alpha
`)

	inv := ScanWithOptions(LoadOptions{
		Roots: []Root{{Path: root, Source: SourceWorkspace}},
	})
	skill, ok := inv.Get("alpha-skill")
	if !ok {
		t.Fatal("expected skill to be loaded")
	}
	if skill.Description != "Handles explicit alpha tasks" {
		t.Fatalf("unexpected description: %q", skill.Description)
	}
	if skill.Homepage != "https://example.com/alpha" {
		t.Fatalf("unexpected homepage: %q", skill.Homepage)
	}
	if !skill.UserInvocable {
		t.Fatal("expected user-invocable")
	}
	if !skill.Hidden {
		t.Fatal("expected disable-model-invocation to hide skill from model summary")
	}
	if skill.CommandDispatch != "tool" || skill.CommandTool != "exec" || skill.CommandArgMode != "raw" {
		t.Fatalf("unexpected command dispatch metadata: %#v", skill)
	}
	if skill.Key != "alpha" {
		t.Fatalf("expected metadata skill key, got %q", skill.Key)
	}
	if skill.Metadata.PrimaryEnv != "ALPHA_KEY" {
		t.Fatalf("expected primary env, got %q", skill.Metadata.PrimaryEnv)
	}
}

func TestScanWithOptions_NormalizesMetadataAliases(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "alias", `---
name: alias
description: alias
metadata:
  clawdbot:
    primaryEnv: DBOT_KEY
    requires:
      env: [DBOT_KEY]
---
# Alias
`)
	inv := ScanWithOptions(LoadOptions{
		Roots: []Root{{Path: root, Source: SourceManaged}},
		Entries: map[string]EntryConfig{
			"alias": {APIKey: "secret"},
		},
	})
	skill, ok := inv.Get("alias")
	if !ok {
		t.Fatal("expected skill")
	}
	if skill.Metadata.PrimaryEnv != "DBOT_KEY" {
		t.Fatalf("expected clawdbot metadata to normalize, got %q", skill.Metadata.PrimaryEnv)
	}
	if !skill.Eligible {
		t.Fatalf("expected apiKey to satisfy env requirement, got missing=%v", skill.Missing)
	}
}

func TestScanWithOptions_PreferenceWorkspaceManagedBundled(t *testing.T) {
	bundled := t.TempDir()
	managed := t.TempDir()
	workspace := t.TempDir()
	makeSkillBundle(t, bundled, "same", "---\nname: same\ndescription: bundled\n---\n# Bundled")
	makeSkillBundle(t, managed, "same", "---\nname: same\ndescription: managed\n---\n# Managed")
	makeSkillBundle(t, workspace, "same", "---\nname: same\ndescription: workspace\n---\n# Workspace")

	inv := ScanWithOptions(LoadOptions{
		Roots: []Root{
			{Path: bundled, Source: SourceBundled},
			{Path: managed, Source: SourceManaged},
			{Path: workspace, Source: SourceWorkspace},
		},
	})
	skill, ok := inv.Get("same")
	if !ok {
		t.Fatal("expected skill")
	}
	if skill.Source != SourceWorkspace {
		t.Fatalf("expected workspace precedence, got %s", skill.Source)
	}
	if skill.Description != "workspace" {
		t.Fatalf("expected workspace description, got %q", skill.Description)
	}
}

func TestScanWithOptions_EligibilityReasonsAndDisableFlags(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "gated", fmt.Sprintf(`---
name: gated
description: gated skill
metadata:
  clawdis:
    os: ["definitely-not-%s"]
    requires:
      bins: ["__missing_bin__"]
      anyBins: ["__missing_any_one__", "__missing_any_two__"]
      env: ["MISSING_ENV"]
      config: ["browser.enabled"]
---
# Gated
`, runtime.GOOS))
	inv := ScanWithOptions(LoadOptions{
		Roots: []Root{{Path: root, Source: SourceWorkspace}},
		Entries: map[string]EntryConfig{
			"gated": {Enabled: boolPtr(false)},
		},
		GlobalConfig: map[string]any{"browser": map[string]any{"enabled": false}},
		Env:          map[string]string{},
	})
	skill, ok := inv.Get("gated")
	if !ok {
		t.Fatal("expected skill")
	}
	if skill.Eligible {
		t.Fatal("expected skill to be ineligible")
	}
	reasons := strings.Join(skill.Missing, " | ")
	for _, want := range []string{
		"disabled in config",
		"os mismatch:",
		"missing binary:",
		"missing any-of binary:",
		"missing env:",
		"missing config:",
	} {
		if !strings.Contains(reasons, want) {
			t.Fatalf("expected reason %q in %q", want, reasons)
		}
	}
}

func TestScanWithOptions_ParseErrorDoesNotBreakInventory(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "broken", "---\nname: broken\nmetadata: [\n# Broken")
	makeSkillBundle(t, root, "healthy", "---\nname: healthy\ndescription: ok\n---\n# Healthy")

	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceWorkspace}}})
	broken, ok := inv.Get("broken")
	if !ok {
		t.Fatal("expected broken skill entry")
	}
	if broken.ParseError == "" {
		t.Fatal("expected parse error")
	}
	if healthy, ok := inv.Get("healthy"); !ok || healthy.ParseError != "" {
		t.Fatalf("expected unrelated skill to load, got %#v", healthy)
	}
}

func TestInventory_ModelSummary_EligibleVisibleOnly(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "visible", "---\nname: visible\ndescription: visible desc\n---\n# Visible")
	makeSkillBundle(t, root, "hidden", "---\nname: hidden\ndescription: hidden desc\ndisable-model-invocation: true\n---\n# Hidden")
	makeSkillBundle(t, root, "blocked", "---\nname: blocked\ndescription: blocked\nmetadata:\n  openclaw:\n    requires:\n      env: [BLOCKED_KEY]\n---\n# Blocked")

	inv := ScanWithOptions(LoadOptions{
		Roots: []Root{{Path: root, Source: SourceWorkspace}},
		Env:   map[string]string{},
	})
	summary := inv.ModelSummary(10)
	if !strings.Contains(summary, "visible | visible desc |") {
		t.Fatalf("expected visible skill in model summary, got %q", summary)
	}
	if strings.Contains(summary, "hidden") || strings.Contains(summary, "blocked") {
		t.Fatalf("expected hidden/ineligible skills to be omitted, got %q", summary)
	}
}

func TestInventory_ResolveBundlePath(t *testing.T) {
	root := t.TempDir()
	dir := makeSkillBundle(t, root, "bundle", "# Bundle")
	if err := os.WriteFile(filepath.Join(dir, "tool.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceWorkspace}}})
	resolved, err := inv.ResolveBundlePath("bundle", "tool.sh")
	if err != nil {
		t.Fatalf("ResolveBundlePath: %v", err)
	}
	if !strings.HasSuffix(resolved, "tool.sh") {
		t.Fatalf("unexpected resolved path: %s", resolved)
	}
	if _, err := inv.ResolveBundlePath("bundle", "../escape.txt"); err == nil {
		t.Fatal("expected path escape to fail")
	}
}

func TestScanWithOptions_DetectsUnsupportedCustomTools(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "custom-tool", `---
name: custom-tool
description: custom tool skill
tools:
  customThing:
    description: unsupported
---
# Custom
`)
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceWorkspace}}})
	skill, ok := inv.Get("custom-tool")
	if !ok {
		t.Fatal("expected skill")
	}
	if skill.Eligible {
		t.Fatal("expected custom tool declaration to make skill ineligible")
	}
	if !strings.Contains(strings.Join(skill.Unsupported, " | "), "frontmatter custom tools not supported") {
		t.Fatalf("unexpected unsupported reasons: %#v", skill.Unsupported)
	}
}
