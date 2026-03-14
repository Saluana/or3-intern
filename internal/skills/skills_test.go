package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/clawhub"
)

func makeSkillBundle(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if body == "" {
		body = "# " + name + "\nContent"
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScan_Empty(t *testing.T) {
	inv := Scan(nil)
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(inv.Skills))
	}
}

func TestScan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	inv := Scan([]string{dir})
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills in empty dir, got %d", len(inv.Skills))
	}
}

func TestScan_BlankDirSkipped(t *testing.T) {
	inv := Scan([]string{"   ", ""})
	if len(inv.Skills) != 0 {
		t.Errorf("expected 0 skills with blank dirs, got %d", len(inv.Skills))
	}
}

func TestScan_FiltersByExtension(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "skill-one", "")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Errorf("expected 1 bundle skill, got %d", len(inv.Skills))
	}
	if inv.Skills[0].Name != "skill-one" {
		t.Errorf("expected bundle name 'skill-one', got %q", inv.Skills[0].Name)
	}
}

func TestScan_SortedByName(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "beta", "")
	makeSkillBundle(t, dir, "alpha", "")
	inv := Scan([]string{dir})
	for i := 1; i < len(inv.Skills); i++ {
		if inv.Skills[i].Name < inv.Skills[i-1].Name {
			t.Errorf("expected sorted skills, got %q before %q", inv.Skills[i-1].Name, inv.Skills[i].Name)
		}
	}
}

func TestScan_SkillFields(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "skill-one", "")
	makeSkillBundle(t, dir, "skill-two", "")
	inv := Scan([]string{dir})

	for _, s := range inv.Skills {
		if s.Name == "" {
			t.Error("expected non-empty skill name")
		}
		if s.Path == "" {
			t.Error("expected non-empty skill path")
		}
		if s.ID == "" {
			t.Error("expected non-empty skill ID")
		}
		if s.Size <= 0 {
			t.Errorf("expected positive size for %q, got %d", s.Name, s.Size)
		}
	}
}

func TestScan_MultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	makeSkillBundle(t, dir1, "alpha", "")
	makeSkillBundle(t, dir2, "beta", "")

	inv := Scan([]string{dir1, dir2})
	if len(inv.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(inv.Skills))
	}
}

func TestScan_SkipsSymlinkedSkill(t *testing.T) {
	dir := t.TempDir()
	targetDir := t.TempDir()
	target := makeSkillBundle(t, targetDir, "outside", "")
	link := filepath.Join(dir, "outside")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	inv := Scan([]string{dir})
	if len(inv.Skills) != 0 {
		t.Fatalf("expected symlinked skill to be skipped, got %#v", inv.Skills)
	}
}

func TestInventory_Get_Found(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "skill-one", "")
	inv := Scan([]string{dir})

	s, ok := inv.Get("skill-one")
	if !ok {
		t.Fatal("expected to find 'skill-one'")
	}
	if s.Name != "skill-one" {
		t.Errorf("expected name 'skill-one', got %q", s.Name)
	}
}

func TestInventory_Get_NotFound(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "skill-one", "")
	inv := Scan([]string{dir})

	_, ok := inv.Get("nonexistent")
	if ok {
		t.Error("expected 'nonexistent' to not be found")
	}
}

func TestInventory_Summary_Empty(t *testing.T) {
	inv := Scan(nil)
	s := inv.Summary(50)
	if s != "(no skills found)" {
		t.Errorf("expected '(no skills found)', got %q", s)
	}
}

func TestInventory_Summary_WithItems(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "skill-one", "")
	makeSkillBundle(t, dir, "skill-two", "")
	inv := Scan([]string{dir})

	s := inv.Summary(10)
	if !strings.Contains(s, "skill-one") && !strings.Contains(s, "skill-two") {
		t.Errorf("expected summary to contain skill names, got %q", s)
	}
}

func TestInventory_Summary_MaxItems(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := []string{"aaa", "bbb", "ccc", "ddd", "eee"}[i]
		makeSkillBundle(t, dir, name, "")
	}

	inv := Scan([]string{dir})
	// Limit to 2
	s := inv.Summary(2)
	lines := strings.Split(strings.TrimSpace(s), "\n")
	// 2 items + "…" = 3 lines
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (2 items + ellipsis), got %d: %q", len(lines), s)
	}
	if lines[2] != "…" {
		t.Errorf("expected last line to be '…', got %q", lines[2])
	}
}

func TestInventory_Summary_DefaultMax(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "skill-one", "")
	makeSkillBundle(t, dir, "skill-two", "")
	inv := Scan([]string{dir})
	// passing 0 should use default of 50
	s := inv.Summary(0)
	if s == "" || s == "(no skills found)" {
		t.Errorf("expected summary with content, got %q", s)
	}
}

func TestLoadBody_Normal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := "# Skill\nSome content here"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := LoadBody(path, 0)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestLoadBody_Truncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := strings.Repeat("a", 100)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := LoadBody(path, 50)
	if err != nil {
		t.Fatalf("LoadBody: %v", err)
	}
	if len(got) != 50 {
		t.Errorf("expected 50 bytes, got %d", len(got))
	}
}

func TestLoadBody_FileNotFound(t *testing.T) {
	_, err := LoadBody("/nonexistent/path/skill.md", 0)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestHash_Deterministic(t *testing.T) {
	h1 := hash("some/path/file.md")
	h2 := hash("some/path/file.md")
	if h1 != h2 {
		t.Errorf("expected deterministic hash, got %q and %q", h1, h2)
	}
}

func TestHash_Different(t *testing.T) {
	h1 := hash("path/a.md")
	h2 := hash("path/b.md")
	if h1 == h2 {
		t.Error("expected different hashes for different paths")
	}
}

// ---- Manifest and front matter ----

func TestSkillManifestParsing(t *testing.T) {
	dir := t.TempDir()
	bundle := makeSkillBundle(t, dir, "myskill", "# My Skill\nDoes things.")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{
		"summary": "a really cool skill",
		"entrypoints": [
			{"name": "run", "command": ["python", "main.py"], "timeoutSeconds": 30, "acceptsStdin": false}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	s := inv.Skills[0]
	if s.Summary != "a really cool skill" {
		t.Errorf("expected summary 'a really cool skill', got %q", s.Summary)
	}
	if len(s.Entrypoints) != 1 {
		t.Fatalf("expected 1 entrypoint, got %d", len(s.Entrypoints))
	}
	ep := s.Entrypoints[0]
	if ep.Name != "run" {
		t.Errorf("expected entrypoint name 'run', got %q", ep.Name)
	}
	if len(ep.Command) != 2 || ep.Command[0] != "python" {
		t.Errorf("expected command [python main.py], got %v", ep.Command)
	}
	if ep.TimeoutSeconds != 30 {
		t.Errorf("expected timeout 30, got %d", ep.TimeoutSeconds)
	}
	if ep.AcceptsStdin {
		t.Error("expected acceptsStdin false")
	}
}

func TestSkillFrontMatterSummary(t *testing.T) {
	dir := t.TempDir()
	content := "---\nsummary: parses front matter correctly\n---\n# Skill\nBody text."
	makeSkillBundle(t, dir, "frontmatter", content)

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	s := inv.Skills[0]
	if s.Summary != "parses front matter correctly" {
		t.Errorf("expected summary 'parses front matter correctly', got %q", s.Summary)
	}
}

func TestSkillManifestOverridesFrontMatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\nsummary: from front matter\n---\n# Skill"
	bundle := makeSkillBundle(t, dir, "skill-bundle", content)
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"summary":"from manifest"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	// manifest takes precedence
	if inv.Skills[0].Summary != "from manifest" {
		t.Errorf("expected manifest summary to take precedence, got %q", inv.Skills[0].Summary)
	}
}

func TestSkillPermissionsAndApprovalPolicy(t *testing.T) {
	dir := t.TempDir()
	bundle := makeSkillBundle(t, dir, "approved-skill", "---\npermissions:\n  shell: true\n  hosts: [api.example.com]\n---\n# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{
		Roots: []Root{{Path: dir, Source: SourceWorkspace}},
		ApprovalPolicy: ApprovalPolicy{
			QuarantineByDefault: true,
			ApprovedSkills:      map[string]struct{}{"approved-skill": {}},
		},
	})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	skill := inv.Skills[0]
	if skill.PermissionState != "approved" {
		t.Fatalf("expected approved permission state, got %+v", skill)
	}
	if !skill.Permissions.Shell || len(skill.Permissions.AllowedHosts) != 1 || skill.Permissions.AllowedHosts[0] != "api.example.com" {
		t.Fatalf("unexpected permissions: %+v", skill.Permissions)
	}
	if !strings.Contains(skill.Permissions.Summary(), "shell") {
		t.Fatalf("expected permission summary, got %q", skill.Permissions.Summary())
	}
}

func TestSkillPermissionsDefaultToQuarantined(t *testing.T) {
	dir := t.TempDir()
	bundle := makeSkillBundle(t, dir, "quarantined-skill", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: dir, Source: SourceWorkspace}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true}})
	if inv.Skills[0].PermissionState != "quarantined" {
		t.Fatalf("expected quarantined permission state, got %+v", inv.Skills[0])
	}
}

func TestSkillWithoutExecutionSurfaceStaysApproved(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "tool-dispatch-skill", "---\ncommand-dispatch: tool\ncommand-tool: demo_echo\n---\n# Skill")
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: dir, Source: SourceWorkspace}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true}})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	if inv.Skills[0].PermissionState != "approved" {
		t.Fatalf("expected non-executable skill to remain approved, got %+v", inv.Skills[0])
	}
}

func TestSkillRunnableBundleWithoutEntrypointIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	bundle := makeSkillBundle(t, dir, "path-runner", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "tool.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: dir, Source: SourceWorkspace}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true}})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	if inv.Skills[0].PermissionState != "quarantined" {
		t.Fatalf("expected runnable bundle to be quarantined, got %+v", inv.Skills[0])
	}
}

func TestManagedSkillTrustedPublisherAutoApproves(t *testing.T) {
	root := t.TempDir()
	bundle := makeSkillBundle(t, root, "trusted-runner", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := clawhub.WriteOrigin(bundle, clawhub.SkillOrigin{Version: 2, Registry: "https://clawhub.ai", Owner: "trusted-owner", Slug: "trusted-runner", InstalledVersion: "1.0.0", InstalledAt: 1, Fingerprint: mustFingerprint(t, bundle), ScanStatus: "clean"}); err != nil {
		t.Fatalf("WriteOrigin: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceManaged}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true, TrustedOwners: map[string]struct{}{"trusted-owner": {}}, TrustedRegistries: map[string]struct{}{"https://clawhub.ai": {}}}})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	if inv.Skills[0].PermissionState != "approved" {
		t.Fatalf("expected trusted managed skill to be approved, got %+v", inv.Skills[0])
	}
	if !strings.Contains(strings.Join(inv.Skills[0].PermissionNotes, " | "), "trusted publisher policy") {
		t.Fatalf("expected trusted publisher note, got %#v", inv.Skills[0].PermissionNotes)
	}
}

func TestManagedSkillBlockedOwnerIsBlocked(t *testing.T) {
	root := t.TempDir()
	bundle := makeSkillBundle(t, root, "blocked-runner", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := clawhub.WriteOrigin(bundle, clawhub.SkillOrigin{Version: 2, Registry: "https://clawhub.ai", Owner: "blocked-owner", Slug: "blocked-runner", InstalledVersion: "1.0.0", InstalledAt: 1, Fingerprint: mustFingerprint(t, bundle), ScanStatus: "clean"}); err != nil {
		t.Fatalf("WriteOrigin: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceManaged}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true, BlockedOwners: map[string]struct{}{"blocked-owner": {}}}})
	if inv.Skills[0].PermissionState != "blocked" {
		t.Fatalf("expected blocked owner to block skill, got %+v", inv.Skills[0])
	}
}

func TestManagedSkillTrustedPublisherWithLocalEditsIsQuarantined(t *testing.T) {
	root := t.TempDir()
	bundle := makeSkillBundle(t, root, "trusted-modified-runner", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := clawhub.WriteOrigin(bundle, clawhub.SkillOrigin{Version: 2, Registry: "https://clawhub.ai", Owner: "trusted-owner", Slug: "trusted-modified-runner", InstalledVersion: "1.0.0", InstalledAt: 1, Fingerprint: mustFingerprint(t, bundle), ScanStatus: "clean"}); err != nil {
		t.Fatalf("WriteOrigin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho modified\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceManaged}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true, TrustedOwners: map[string]struct{}{"trusted-owner": {}}, TrustedRegistries: map[string]struct{}{"https://clawhub.ai": {}}}})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	if inv.Skills[0].PermissionState != "quarantined" {
		t.Fatalf("expected modified trusted managed skill to be quarantined, got %+v", inv.Skills[0])
	}
	if !strings.Contains(strings.Join(inv.Skills[0].PermissionNotes, " | "), "local modifications detected") {
		t.Fatalf("expected local modifications note, got %#v", inv.Skills[0].PermissionNotes)
	}
}

func TestManagedSkillScanFindingsQuarantine(t *testing.T) {
	root := t.TempDir()
	bundle := makeSkillBundle(t, root, "flagged-runner", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := clawhub.WriteOrigin(bundle, clawhub.SkillOrigin{Version: 2, Registry: "https://clawhub.ai", Owner: "review-owner", Slug: "flagged-runner", InstalledVersion: "1.0.0", InstalledAt: 1, Fingerprint: mustFingerprint(t, bundle), ScanStatus: "quarantined", ScanFindings: []clawhub.ScanFinding{{Severity: "medium", Path: "run.sh", Rule: "curl-pipe-shell", Message: "downloads remote content directly into a shell"}}}); err != nil {
		t.Fatalf("WriteOrigin: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceManaged}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true}})
	if inv.Skills[0].PermissionState != "quarantined" {
		t.Fatalf("expected scan finding to quarantine skill, got %+v", inv.Skills[0])
	}
	if !strings.Contains(strings.Join(inv.Skills[0].PermissionNotes, " | "), "install-time scan flagged") {
		t.Fatalf("expected scan note, got %#v", inv.Skills[0].PermissionNotes)
	}
}

func TestManagedSkillTrustedPublisherStillHonorsQuarantinedScan(t *testing.T) {
	root := t.TempDir()
	bundle := makeSkillBundle(t, root, "trusted-flagged-runner", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"entrypoints":[{"name":"run","command":["./run.sh"]}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := clawhub.WriteOrigin(bundle, clawhub.SkillOrigin{Version: 2, Registry: "https://clawhub.ai", Owner: "trusted-owner", Slug: "trusted-flagged-runner", InstalledVersion: "1.0.0", InstalledAt: 1, Fingerprint: mustFingerprint(t, bundle), ScanStatus: "quarantined", ScanFindings: []clawhub.ScanFinding{{Severity: "medium", Path: "run.sh", Rule: "curl-pipe-shell", Message: "downloads remote content directly into a shell"}}}); err != nil {
		t.Fatalf("WriteOrigin: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceManaged}}, ApprovalPolicy: ApprovalPolicy{QuarantineByDefault: true, TrustedOwners: map[string]struct{}{"trusted-owner": {}}, TrustedRegistries: map[string]struct{}{"https://clawhub.ai": {}}}})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(inv.Skills))
	}
	if inv.Skills[0].PermissionState != "quarantined" {
		t.Fatalf("expected quarantined scan to win over trusted publisher policy, got %+v", inv.Skills[0])
	}
	if !strings.Contains(strings.Join(inv.Skills[0].PermissionNotes, " | "), "install-time scan flagged") {
		t.Fatalf("expected install-time scan note, got %#v", inv.Skills[0].PermissionNotes)
	}
}

func TestSkillSummaryInInventory(t *testing.T) {
	dir := t.TempDir()
	makeSkillBundle(t, dir, "alpha", "---\nsummary: does alpha things\n---\n# Alpha")
	makeSkillBundle(t, dir, "beta", "# Beta\nNo front matter.")

	inv := Scan([]string{dir})
	s := inv.Summary(10)

	if !strings.Contains(s, "alpha: does alpha things") {
		t.Errorf("expected summary line 'alpha: does alpha things' in %q", s)
	}
	if !strings.Contains(s, "- beta\n") && !strings.HasSuffix(s, "- beta") {
		t.Errorf("expected plain '- beta' line (no summary) in %q", s)
	}
}

func TestExtractFrontMatterSummary_NoFrontMatter(t *testing.T) {
	got := extractFrontMatterSummary("# Title\nsome body")
	if got != "" {
		t.Errorf("expected empty string for no front matter, got %q", got)
	}
}

func TestExtractFrontMatterSummary_WithSummary(t *testing.T) {
	content := "---\nsummary: hello world\nauthor: test\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestExtractFrontMatterSummary_WithQuotedSummary(t *testing.T) {
	content := "---\nsummary: \"quoted value\"\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "quoted value" {
		t.Errorf("expected 'quoted value', got %q", got)
	}
}

func TestExtractFrontMatterSummary_MissingSummaryKey(t *testing.T) {
	content := "---\nauthor: someone\n---\n# Body"
	got := extractFrontMatterSummary(content)
	if got != "" {
		t.Errorf("expected empty string when no summary key, got %q", got)
	}
}

func TestSkillManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	bundle := makeSkillBundle(t, dir, "myskill", "# Skill")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`not valid json`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should not panic; skill loads without summary/entrypoints
	inv := Scan([]string{dir})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 skill even with invalid manifest, got %d", len(inv.Skills))
	}
	if inv.Skills[0].Summary != "" {
		t.Errorf("expected empty summary for invalid manifest, got %q", inv.Skills[0].Summary)
	}
}

func TestScan_DuplicateNameLaterDirWins(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	makeSkillBundle(t, dir1, "shared", "---\nsummary: from dir1\n---\n# Shared")
	makeSkillBundle(t, dir2, "shared", "---\nsummary: from dir2\n---\n# Shared")

	inv := Scan([]string{dir1, dir2})
	if len(inv.Skills) != 1 {
		t.Fatalf("expected 1 merged skill, got %d", len(inv.Skills))
	}
	s, ok := inv.Get("shared")
	if !ok {
		t.Fatal("expected to find shared")
	}
	if s.Summary != "from dir2" {
		t.Fatalf("expected later dir to win, got %q", s.Summary)
	}
}

func TestScanWithOptions_ParsesDeclaredToolsAllowlist(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "custom-tool", `---
name: custom-tool
description: custom tool skill
tools:
  - read_skill
  - exec
---
# Custom
`)

	inv := ScanWithOptions(LoadOptions{
		Roots:          []Root{{Path: root, Source: SourceWorkspace}},
		AvailableTools: map[string]struct{}{"read_skill": {}, "exec": {}},
	})
	skill, ok := inv.Get("custom-tool")
	if !ok {
		t.Fatal("expected skill")
	}
	if !skill.Eligible {
		t.Fatalf("expected declared tool list to be supported, got unsupported=%v", skill.Unsupported)
	}
	if len(skill.AllowedTools) != 2 || skill.AllowedTools[0] != "read_skill" || skill.AllowedTools[1] != "exec" {
		t.Fatalf("unexpected declared tools: %#v", skill.AllowedTools)
	}
}

func TestScanWithOptions_MergesManifestOnlyDeclaredTools(t *testing.T) {
	root := t.TempDir()
	bundle := makeSkillBundle(t, root, "manifest-tools", "---\nname: manifest-tools\ndescription: only manifest tools\n---\n# Manifest tools\n")
	if err := os.WriteFile(filepath.Join(bundle, "skill.json"), []byte(`{"tools":["read_skill"]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceWorkspace}}, AvailableTools: map[string]struct{}{"read_skill": {}}})
	skill, ok := inv.Get("manifest-tools")
	if !ok {
		t.Fatal("expected skill")
	}
	if len(skill.AllowedTools) != 1 || skill.AllowedTools[0] != "read_skill" {
		t.Fatalf("expected manifest-only tools to be merged, got %#v", skill.AllowedTools)
	}
	if !skill.Eligible {
		t.Fatalf("expected manifest-only tools skill to remain eligible, got unsupported=%v", skill.Unsupported)
	}
}

func TestScanWithOptions_RejectsMalformedDeclaredTools(t *testing.T) {
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
		t.Fatal("expected malformed tools declaration to make skill ineligible")
	}
	if !strings.Contains(strings.Join(skill.Unsupported, " | "), "frontmatter tools must be a list of string tool names") {
		t.Fatalf("unexpected unsupported reasons: %#v", skill.Unsupported)
	}
}

func TestScanWithOptions_RejectsNonStringDeclaredToolEntries(t *testing.T) {
	root := t.TempDir()
	makeSkillBundle(t, root, "custom-tool", `---
name: custom-tool
description: custom tool skill
tools:
  - read_skill
  - 123
---
# Custom
`)

	inv := ScanWithOptions(LoadOptions{Roots: []Root{{Path: root, Source: SourceWorkspace}}, AvailableTools: map[string]struct{}{"read_skill": {}}})
	skill, ok := inv.Get("custom-tool")
	if !ok {
		t.Fatal("expected skill")
	}
	if skill.Eligible {
		t.Fatal("expected non-string tools entry to make skill ineligible")
	}
	if !strings.Contains(strings.Join(skill.Unsupported, " | "), "frontmatter tools must be a list of string tool names") {
		t.Fatalf("unexpected unsupported reasons: %#v", skill.Unsupported)
	}
}

func mustFingerprint(t *testing.T, dir string) string {
	t.Helper()
	fingerprint, err := clawhub.FingerprintDir(dir)
	if err != nil {
		t.Fatalf("FingerprintDir: %v", err)
	}
	return fingerprint
}
