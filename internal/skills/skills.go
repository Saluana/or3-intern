// Package skills discovers, evaluates, and loads skill metadata from multiple roots.
package skills

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"or3-intern/internal/clawhub"

	"gopkg.in/yaml.v3"
)

// SkillEntry describes a declared executable entrypoint from a skill manifest.
type SkillEntry struct {
	Name           string   `json:"name"`
	Command        []string `json:"command"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	AcceptsStdin   bool     `json:"acceptsStdin"`
}

type Source string

const (
	// SourceExtra marks skills loaded from explicitly supplied extra roots.
	SourceExtra Source = "extra"
	// SourceBundled marks skills that ship with the application.
	SourceBundled Source = "bundled"
	// SourceManaged marks skills installed from a remote registry.
	SourceManaged Source = "managed"
	// SourceWorkspace marks workspace-local skills.
	SourceWorkspace Source = "workspace"
)

// Root describes one filesystem root scanned for skills.
type Root struct {
	Path     string
	Source   Source
	Label    string
	Priority int
}

// EntryConfig applies per-skill runtime configuration overrides.
type EntryConfig struct {
	Enabled *bool
	APIKey  string
	Env     map[string]string
	Config  map[string]any
}

// LoadOptions controls how skill discovery and eligibility evaluation run.
type LoadOptions struct {
	Roots          []Root
	Entries        map[string]EntryConfig
	GlobalConfig   map[string]any
	Env            map[string]string
	AvailableTools map[string]struct{}
	ApprovalPolicy ApprovalPolicy
	OS             string
}

// ApprovalPolicy describes which managed skills are trusted or blocked.
type ApprovalPolicy struct {
	QuarantineByDefault bool
	ApprovedSkills      map[string]struct{}
	TrustedOwners       map[string]struct{}
	BlockedOwners       map[string]struct{}
	TrustedRegistries   map[string]struct{}
}

// SkillInstallSpec describes one suggested installation route for a dependency.
type SkillInstallSpec struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Bins    []string `json:"bins"`
	Formula string   `json:"formula"`
	Tap     string   `json:"tap"`
	Package string   `json:"package"`
	Module  string   `json:"module"`
	OS      []string `json:"os"`
	URL     string   `json:"url"`
}

// NixPluginSpec describes an optional Nix plugin dependency.
type NixPluginSpec struct {
	Plugin  string   `json:"plugin"`
	Systems []string `json:"systems"`
}

// SkillRequirements lists binary, env, and config prerequisites.
type SkillRequirements struct {
	Bins    []string `json:"bins"`
	AnyBins []string `json:"anyBins"`
	Env     []string `json:"env"`
	Config  []string `json:"config"`
}

// SkillRuntimeMeta captures runtime metadata parsed from skill front matter.
type SkillRuntimeMeta struct {
	Always     bool               `json:"always"`
	SkillKey   string             `json:"skillKey"`
	PrimaryEnv string             `json:"primaryEnv"`
	Emoji      string             `json:"emoji"`
	Homepage   string             `json:"homepage"`
	OS         []string           `json:"os"`
	Requires   SkillRequirements  `json:"requires"`
	Install    []SkillInstallSpec `json:"install"`
	Nix        *NixPluginSpec     `json:"nix"`
}

// SkillPermissions describes the requested shell, network, and write permissions.
type SkillPermissions struct {
	Shell        bool     `json:"shell" yaml:"shell"`
	Network      bool     `json:"network" yaml:"network"`
	Write        bool     `json:"write" yaml:"write"`
	AllowedPaths []string `json:"paths" yaml:"paths"`
	AllowedHosts []string `json:"hosts" yaml:"hosts"`
}

// SkillMeta is the fully merged metadata for one discovered skill.
type SkillMeta struct {
	Name        string
	Description string
	Homepage    string
	Path        string
	Dir         string
	Location    string
	Source      Source
	ModTime     time.Time
	Size        int64
	ID          string
	Summary     string
	Entrypoints []SkillEntry

	UserInvocable          bool
	DisableModelInvocation bool
	CommandDispatch        string
	CommandTool            string
	CommandArgMode         string

	Metadata         SkillRuntimeMeta
	Permissions      SkillPermissions
	AllowedTools     []string
	PermissionState  string
	PermissionNotes  []string
	Publisher        string
	Registry         string
	InstalledVersion string
	Modified         bool
	ScanStatus       string
	ScanFindings     []string
	Key              string
	Eligible         bool
	Disabled         bool
	Hidden           bool
	Missing          []string
	Unsupported      []string
	ParseError       string
	RuntimeEnv       map[string]string

	sourcePriority int
	rootOrder      int
}

// Inventory is the discovered skill set plus a name index.
type Inventory struct {
	Skills []SkillMeta
	byName map[string]SkillMeta
}

type skillManifest struct {
	Summary     string           `json:"summary"`
	Entrypoints []SkillEntry     `json:"entrypoints"`
	Tools       []string         `json:"tools"`
	Permissions SkillPermissions `json:"permissions"`
}

type skillFrontMatter struct {
	Name                   string           `yaml:"name"`
	Description            string           `yaml:"description"`
	Summary                string           `yaml:"summary"`
	Homepage               string           `yaml:"homepage"`
	UserInvocable          *bool            `yaml:"user-invocable"`
	DisableModelInvocation bool             `yaml:"disable-model-invocation"`
	CommandDispatch        string           `yaml:"command-dispatch"`
	CommandTool            string           `yaml:"command-tool"`
	CommandArgMode         string           `yaml:"command-arg-mode"`
	Permissions            SkillPermissions `yaml:"permissions"`
	Metadata               map[string]any   `yaml:"metadata"`
}

func defaultPriority(source Source) int {
	switch source {
	case SourceWorkspace:
		return 40
	case SourceManaged:
		return 30
	case SourceBundled:
		return 20
	default:
		return 10
	}
}

// Scan keeps the simple legacy API for callers that only provide directories.
func Scan(dirs []string) Inventory {
	roots := make([]Root, 0, len(dirs))
	for i, dir := range dirs {
		roots = append(roots, Root{
			Path:     dir,
			Source:   SourceExtra,
			Label:    dir,
			Priority: i + 1,
		})
	}
	return ScanWithOptions(LoadOptions{Roots: roots})
}

// ScanWithOptions discovers skills and evaluates their runtime eligibility.
func ScanWithOptions(opts LoadOptions) Inventory {
	if len(opts.Env) == 0 {
		opts.Env = envMap(os.Environ())
	}
	if strings.TrimSpace(opts.OS) == "" {
		opts.OS = runtime.GOOS
	}

	metaByName := map[string]SkillMeta{}
	for i, root := range opts.Roots {
		root = normalizeRoot(root)
		if strings.TrimSpace(root.Path) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root.Path)
		if err != nil {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			continue
		}
		scanSkillDir(metaByName, realRoot, root, i, opts)
		_ = filepath.WalkDir(realRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path == realRoot {
				return nil
			}
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(realRoot, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			if scanSkillDir(metaByName, realPath, root, i, opts) {
				return filepath.SkipDir
			}
			return nil
		})
	}

	skills := make([]SkillMeta, 0, len(metaByName))
	for _, s := range metaByName {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name == skills[j].Name {
			if skills[i].sourcePriority == skills[j].sourcePriority {
				return skills[i].Path < skills[j].Path
			}
			return skills[i].sourcePriority > skills[j].sourcePriority
		}
		return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
	})
	by := make(map[string]SkillMeta, len(skills))
	for _, s := range skills {
		by[s.Name] = s
		by[strings.ToLower(s.Name)] = s
	}
	return Inventory{Skills: skills, byName: by}
}

func normalizeRoot(root Root) Root {
	if strings.TrimSpace(root.Label) == "" {
		root.Label = string(root.Source)
	}
	if root.Priority == 0 {
		root.Priority = defaultPriority(root.Source)
	}
	return root
}

func scanSkillDir(metaByName map[string]SkillMeta, dir string, root Root, order int, opts LoadOptions) bool {
	skillPath, ok := skillFileInDir(dir)
	if !ok {
		return false
	}
	meta := loadSkill(dir, skillPath, root, order, opts)
	current, exists := metaByName[meta.Name]
	if !exists || shouldOverride(current, meta) {
		metaByName[meta.Name] = meta
	}
	return true
}

func shouldOverride(current SkillMeta, candidate SkillMeta) bool {
	if candidate.sourcePriority != current.sourcePriority {
		return candidate.sourcePriority > current.sourcePriority
	}
	if candidate.rootOrder != current.rootOrder {
		return candidate.rootOrder > current.rootOrder
	}
	return candidate.Path > current.Path
}

func skillFileInDir(dir string) (string, bool) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		return path, true
	}
	return "", false
}

func loadSkill(dir, path string, root Root, order int, opts LoadOptions) SkillMeta {
	info, _ := os.Stat(path)
	meta := SkillMeta{
		Name:           filepath.Base(dir),
		Path:           path,
		Dir:            dir,
		Location:       dir,
		Source:         root.Source,
		ID:             hash(path),
		UserInvocable:  true,
		CommandArgMode: "raw",
		sourcePriority: root.Priority,
		rootOrder:      order,
	}
	if info != nil {
		meta.ModTime = info.ModTime()
		meta.Size = info.Size()
	}

	body, err := LoadBody(path, 0)
	if err != nil {
		meta.ParseError = err.Error()
		meta.Hidden = true
		applyApprovalPolicy(&meta, opts.ApprovalPolicy)
		return meta
	}
	fm, rawTop, err := parseFrontMatter(body)
	if err != nil {
		meta.ParseError = err.Error()
		meta.Hidden = true
		applyApprovalPolicy(&meta, opts.ApprovalPolicy)
		return meta
	}
	if strings.TrimSpace(fm.Name) != "" {
		meta.Name = strings.TrimSpace(fm.Name)
	}
	meta.Description = strings.TrimSpace(firstNonEmpty(fm.Description, fm.Summary))
	meta.Summary = meta.Description
	meta.Homepage = strings.TrimSpace(fm.Homepage)
	if fm.UserInvocable != nil {
		meta.UserInvocable = *fm.UserInvocable
	}
	meta.DisableModelInvocation = fm.DisableModelInvocation
	meta.Hidden = meta.DisableModelInvocation
	meta.CommandDispatch = strings.TrimSpace(fm.CommandDispatch)
	meta.CommandTool = strings.TrimSpace(fm.CommandTool)
	if strings.TrimSpace(fm.CommandArgMode) != "" {
		meta.CommandArgMode = strings.TrimSpace(fm.CommandArgMode)
	}
	meta.Permissions = normalizeSkillPermissions(fm.Permissions)
	declaredTools, _ := parseDeclaredTools(rawTop["tools"])
	meta.AllowedTools = declaredTools

	manifest, err := loadManifest(dir)
	if err != nil {
		meta.ParseError = err.Error()
		meta.Hidden = true
	} else if len(manifest.Entrypoints) > 0 || len(manifest.Tools) > 0 || manifest.Permissions.Requested() || strings.TrimSpace(manifest.Summary) != "" {
		meta.Entrypoints = manifest.Entrypoints
		meta.AllowedTools = mergeStringLists(meta.AllowedTools, compactStrings(manifest.Tools))
		if requested := normalizeSkillPermissions(manifest.Permissions); requested.Requested() {
			meta.Permissions = requested
		}
		if strings.TrimSpace(manifest.Summary) != "" {
			meta.Summary = strings.TrimSpace(manifest.Summary)
		}
		if meta.Description == "" {
			meta.Summary = strings.TrimSpace(manifest.Summary)
			meta.Description = meta.Summary
		}
	}

	runtimeMeta, ok := normalizeRuntimeMetadata(fm.Metadata)
	if ok {
		meta.Metadata = runtimeMeta
	}
	if meta.Homepage == "" {
		meta.Homepage = strings.TrimSpace(meta.Metadata.Homepage)
	}
	if meta.Key == "" {
		meta.Key = strings.TrimSpace(firstNonEmpty(meta.Metadata.SkillKey, meta.Name))
	}
	entry := entryConfigForSkill(opts.Entries, meta)
	meta.RuntimeEnv = buildRuntimeEnv(meta, entry, opts.Env)
	applyOriginMetadata(&meta)
	applyEligibility(&meta, rawTop, body, entry, opts)
	applyApprovalPolicy(&meta, opts.ApprovalPolicy)
	return meta
}

func applyOriginMetadata(meta *SkillMeta) {
	if meta == nil || strings.TrimSpace(meta.Dir) == "" {
		return
	}
	origin, err := clawhub.ReadOrigin(meta.Dir)
	if err != nil {
		if meta.Source == SourceManaged {
			meta.PermissionNotes = append(meta.PermissionNotes, "managed skill missing origin metadata")
		}
		return
	}
	meta.Publisher = strings.TrimSpace(origin.Owner)
	meta.Registry = strings.TrimSpace(origin.Registry)
	meta.InstalledVersion = strings.TrimSpace(origin.InstalledVersion)
	meta.ScanStatus = normalizeSupplyChainValue(origin.ScanStatus)
	for _, finding := range origin.ScanFindings {
		if summary := strings.TrimSpace(finding.Summary()); summary != "" {
			meta.ScanFindings = append(meta.ScanFindings, summary)
		}
	}
	if modified, modErr := clawhub.LocalEdits(meta.Dir); modErr == nil {
		meta.Modified = modified
	}
}

func normalizeSkillPermissions(raw SkillPermissions) SkillPermissions {
	raw.AllowedPaths = compactStrings(raw.AllowedPaths)
	raw.AllowedHosts = compactStrings(raw.AllowedHosts)
	return raw
}

func (p SkillPermissions) Requested() bool {
	return p.Shell || p.Network || p.Write || len(p.AllowedPaths) > 0 || len(p.AllowedHosts) > 0
}

func parseDeclaredTools(raw any) ([]string, bool) {
	switch value := raw.(type) {
	case nil:
		return nil, true
	case []string:
		return compactStrings(value), true
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			name, ok := item.(string)
			if !ok {
				return nil, false
			}
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out = append(out, name)
		}
		return compactStrings(out), true
	default:
		return nil, false
	}
}

func (p SkillPermissions) Summary() string {
	parts := make([]string, 0, 5)
	if p.Shell {
		parts = append(parts, "shell")
	}
	if p.Network {
		parts = append(parts, "network")
	}
	if p.Write {
		parts = append(parts, "write")
	}
	if len(p.AllowedPaths) > 0 {
		parts = append(parts, "paths="+strings.Join(p.AllowedPaths, ","))
	}
	if len(p.AllowedHosts) > 0 {
		parts = append(parts, "hosts="+strings.Join(p.AllowedHosts, ","))
	}
	if len(parts) == 0 {
		return "(none declared)"
	}
	return strings.Join(parts, "; ")
}

func applyApprovalPolicy(meta *SkillMeta, policy ApprovalPolicy) {
	if meta == nil {
		return
	}
	if meta.ParseError != "" {
		meta.PermissionState = "blocked"
		meta.PermissionNotes = append(meta.PermissionNotes, "metadata parse failed")
		return
	}
	if ownerBlocked(meta, policy.BlockedOwners) {
		meta.PermissionState = "blocked"
		meta.PermissionNotes = append(meta.PermissionNotes, "publisher blocked by policy: "+meta.Publisher)
		return
	}
	if strings.EqualFold(meta.ScanStatus, "blocked") {
		meta.PermissionState = "blocked"
		meta.PermissionNotes = append(meta.PermissionNotes, "install-time scan blocked this bundle")
		meta.PermissionNotes = append(meta.PermissionNotes, meta.ScanFindings...)
		return
	}
	if meta.Modified {
		meta.PermissionState = "quarantined"
		meta.PermissionNotes = append(meta.PermissionNotes, "local modifications detected since install")
		return
	}
	if strings.EqualFold(meta.ScanStatus, "quarantined") {
		meta.PermissionState = "quarantined"
		meta.PermissionNotes = append(meta.PermissionNotes, "install-time scan flagged this bundle for review")
		meta.PermissionNotes = append(meta.PermissionNotes, meta.ScanFindings...)
		return
	}
	trustedPublisher := publisherTrusted(meta, policy)
	if skillApproved(meta, policy.ApprovedSkills) || trustedPublisher {
		meta.PermissionState = "approved"
		if trustedPublisher {
			meta.PermissionNotes = append(meta.PermissionNotes, "approved by trusted publisher policy")
		} else {
			meta.PermissionNotes = append(meta.PermissionNotes, "approved in config")
		}
		return
	}
	if meta.Source == SourceManaged && skillNeedsApproval(meta) {
		if strings.TrimSpace(meta.Registry) == "" || strings.TrimSpace(meta.Publisher) == "" {
			meta.PermissionState = "quarantined"
			meta.PermissionNotes = append(meta.PermissionNotes, "managed skill is missing trusted origin details")
			return
		}
		if len(policy.TrustedOwners) > 0 || len(policy.TrustedRegistries) > 0 {
			meta.PermissionState = "quarantined"
			meta.PermissionNotes = append(meta.PermissionNotes, "publisher or registry is not trusted by policy")
			return
		}
	}
	if skillNeedsApproval(meta) {
		meta.PermissionState = "quarantined"
		meta.PermissionNotes = append(meta.PermissionNotes, "operator approval required before script execution")
		return
	}
	meta.PermissionState = "approved"
}

func ownerBlocked(meta *SkillMeta, blocked map[string]struct{}) bool {
	if meta == nil || len(blocked) == 0 {
		return false
	}
	_, ok := blocked[normalizeSupplyChainValue(meta.Publisher)]
	return ok
}

func publisherTrusted(meta *SkillMeta, policy ApprovalPolicy) bool {
	if meta == nil {
		return false
	}
	anyPolicy := len(policy.TrustedOwners) > 0 || len(policy.TrustedRegistries) > 0
	if !anyPolicy {
		return false
	}
	if len(policy.TrustedOwners) > 0 {
		if _, ok := policy.TrustedOwners[normalizeSupplyChainValue(meta.Publisher)]; !ok {
			return false
		}
	}
	if len(policy.TrustedRegistries) > 0 {
		if _, ok := policy.TrustedRegistries[normalizeSupplyChainValue(meta.Registry)]; !ok {
			return false
		}
	}
	return true
}

func normalizeSupplyChainValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimRight(value, "/")
	return value
}

func skillNeedsApproval(meta *SkillMeta) bool {
	if meta == nil {
		return false
	}
	if meta.Permissions.Requested() || len(meta.Entrypoints) > 0 {
		return true
	}
	return skillHasRunnableContent(meta.Dir)
}

func skillApproved(meta *SkillMeta, approved map[string]struct{}) bool {
	if meta == nil || len(approved) == 0 {
		return false
	}
	for _, key := range []string{meta.Name, meta.Key, strings.ToLower(meta.Name), strings.ToLower(meta.Key)} {
		if _, ok := approved[key]; ok {
			return true
		}
	}
	return false
}

func loadManifest(dir string) (skillManifest, error) {
	manifestPath := filepath.Join(dir, "skill.json")
	info, err := os.Lstat(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return skillManifest{}, nil
		}
		return skillManifest{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return skillManifest{}, fs.ErrPermission
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return skillManifest{}, err
	}
	var m skillManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return skillManifest{}, fmt.Errorf("invalid skill manifest: %w", err)
	}
	return m, nil
}

func parseFrontMatter(content string) (skillFrontMatter, map[string]any, error) {
	block, ok, err := frontMatterBlock(content)
	if err != nil {
		return skillFrontMatter{}, nil, err
	}
	if !ok {
		return skillFrontMatter{}, map[string]any{}, nil
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(block), &raw); err != nil {
		return skillFrontMatter{}, nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	raw = toStringMap(raw)
	var fm skillFrontMatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return skillFrontMatter{}, nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	fm.Metadata = toStringMap(fm.Metadata)
	return fm, raw, nil
}

func frontMatterBlock(content string) (string, bool, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", false, nil
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false, fmt.Errorf("invalid frontmatter: missing closing delimiter")
	}
	block := rest[:end]
	return block, true, nil
}

func extractFrontMatterSummary(content string) string {
	fm, _, err := parseFrontMatter(content)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(fm.Summary, fm.Description))
}

func normalizeRuntimeMetadata(raw map[string]any) (SkillRuntimeMeta, bool) {
	if len(raw) == 0 {
		return SkillRuntimeMeta{}, false
	}
	var selected any
	for _, key := range []string{"openclaw", "clawdbot", "clawdis"} {
		if value, ok := raw[key]; ok {
			selected = value
			break
		}
	}
	if selected == nil {
		return SkillRuntimeMeta{}, false
	}
	buf, err := json.Marshal(toStringMap(selected))
	if err != nil {
		return SkillRuntimeMeta{}, false
	}
	var meta SkillRuntimeMeta
	if err := json.Unmarshal(buf, &meta); err != nil {
		return SkillRuntimeMeta{}, false
	}
	return meta, true
}

func applyEligibility(meta *SkillMeta, rawTop map[string]any, body string, entry EntryConfig, opts LoadOptions) {
	if meta == nil {
		return
	}
	if meta.ParseError != "" {
		meta.Eligible = false
		return
	}
	meta.Disabled = entry.Enabled != nil && !*entry.Enabled
	if meta.Disabled {
		meta.Missing = append(meta.Missing, "disabled in config")
	}
	meta.Unsupported = append(meta.Unsupported, detectUnsupported(*meta, rawTop, body, opts)...)
	if meta.Metadata.Always && !meta.Disabled && len(meta.Unsupported) == 0 {
		meta.Eligible = true
		return
	}
	if len(meta.Metadata.OS) > 0 && !containsFold(meta.Metadata.OS, opts.OS) {
		meta.Missing = append(meta.Missing, fmt.Sprintf("os mismatch: requires %s", strings.Join(meta.Metadata.OS, ", ")))
	}
	for _, bin := range meta.Metadata.Requires.Bins {
		if !hasBinary(bin) {
			meta.Missing = append(meta.Missing, "missing binary: "+bin)
		}
	}
	if len(meta.Metadata.Requires.AnyBins) > 0 {
		ok := false
		for _, bin := range meta.Metadata.Requires.AnyBins {
			if hasBinary(bin) {
				ok = true
				break
			}
		}
		if !ok {
			meta.Missing = append(meta.Missing, "missing any-of binary: "+strings.Join(meta.Metadata.Requires.AnyBins, ", "))
		}
	}
	for _, envName := range meta.Metadata.Requires.Env {
		if strings.TrimSpace(meta.RuntimeEnv[envName]) == "" {
			meta.Missing = append(meta.Missing, "missing env: "+envName)
		}
	}
	for _, key := range meta.Metadata.Requires.Config {
		if !configTruthy(opts.GlobalConfig, entry.Config, key) {
			meta.Missing = append(meta.Missing, "missing config: "+key)
		}
	}
	meta.Eligible = !meta.Disabled && len(meta.Missing) == 0 && len(meta.Unsupported) == 0
}

func detectUnsupported(meta SkillMeta, rawTop map[string]any, body string, opts LoadOptions) []string {
	var unsupported []string
	if _, ok := rawTop["tools"]; ok {
		if _, valid := parseDeclaredTools(rawTop["tools"]); !valid {
			unsupported = append(unsupported, "frontmatter tools must be a list of string tool names")
		}
	}
	if meta.Metadata.Nix != nil && strings.TrimSpace(meta.Metadata.Nix.Plugin) != "" {
		unsupported = append(unsupported, "requires nix plugin: "+meta.Metadata.Nix.Plugin)
	}
	for _, toolName := range meta.AllowedTools {
		if len(opts.AvailableTools) == 0 {
			continue
		}
		if _, ok := opts.AvailableTools[toolName]; !ok {
			unsupported = append(unsupported, "requires unsupported tool: "+toolName)
		}
	}
	if meta.CommandDispatch != "" && meta.CommandDispatch != "tool" {
		unsupported = append(unsupported, "unsupported command-dispatch: "+meta.CommandDispatch)
	}
	if meta.CommandDispatch == "tool" {
		if meta.CommandTool == "" {
			unsupported = append(unsupported, "command-dispatch tool requires command-tool")
		} else if len(opts.AvailableTools) > 0 {
			if _, ok := opts.AvailableTools[meta.CommandTool]; !ok {
				unsupported = append(unsupported, "requires unsupported tool: "+meta.CommandTool)
			}
		}
	}
	if strings.Contains(body, "nodes.run") {
		unsupported = append(unsupported, "requires unsupported tool: nodes.run")
	}
	return unsupported
}

func mergeStringLists(base []string, extra []string) []string {
	out := append([]string{}, compactStrings(base)...)
	seen := map[string]struct{}{}
	for _, item := range out {
		seen[item] = struct{}{}
	}
	for _, item := range compactStrings(extra) {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func buildRuntimeEnv(meta SkillMeta, entry EntryConfig, baseEnv map[string]string) map[string]string {
	out := copyMap(baseEnv)
	for k, v := range entry.Env {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(out[k]) == "" {
			out[k] = v
		}
	}
	if meta.Metadata.PrimaryEnv != "" && strings.TrimSpace(entry.APIKey) != "" && strings.TrimSpace(out[meta.Metadata.PrimaryEnv]) == "" {
		out[meta.Metadata.PrimaryEnv] = entry.APIKey
	}
	return out
}

func entryConfigForSkill(entries map[string]EntryConfig, meta SkillMeta) EntryConfig {
	if len(entries) == 0 {
		return EntryConfig{}
	}
	if entry, ok := entries[meta.Key]; ok {
		return entry
	}
	return entries[meta.Name]
}

func (inv Inventory) Get(name string) (SkillMeta, bool) {
	if strings.TrimSpace(name) == "" {
		return SkillMeta{}, false
	}
	if s, ok := inv.byName[name]; ok {
		return s, true
	}
	s, ok := inv.byName[strings.ToLower(name)]
	return s, ok
}

func (inv Inventory) Summary(max int) string {
	return summarize(inv.Skills, max)
}

func (inv Inventory) ModelSummary(max int) string {
	filtered := make([]SkillMeta, 0, len(inv.Skills))
	for _, skill := range inv.Skills {
		if !skill.Eligible || skill.Hidden {
			continue
		}
		filtered = append(filtered, skill)
	}
	if len(filtered) == 0 {
		return "(no eligible skills found)"
	}
	if max <= 0 {
		max = 50
	}
	lines := make([]string, 0, min(len(filtered), max)+1)
	for i, skill := range filtered {
		if i >= max {
			lines = append(lines, "…")
			break
		}
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = strings.TrimSpace(skill.Summary)
		}
		location := strings.TrimSpace(skill.Location)
		if location == "" {
			location = skill.Dir
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", skill.Name, oneLine(desc, 140), location))
	}
	return strings.Join(lines, "\n")
}

func summarize(skills []SkillMeta, max int) string {
	if max <= 0 {
		max = 50
	}
	lines := []string{}
	for i, s := range skills {
		if i >= max {
			lines = append(lines, "…")
			break
		}
		desc := strings.TrimSpace(firstNonEmpty(s.Description, s.Summary))
		if desc == "" {
			lines = append(lines, "- "+s.Name)
			continue
		}
		lines = append(lines, "- "+s.Name+": "+oneLine(desc, 140))
	}
	if len(lines) == 0 {
		return "(no skills found)"
	}
	return strings.Join(lines, "\n")
}

func (inv Inventory) RunEnv() map[string]string {
	out := map[string]string{}
	for _, skill := range inv.Skills {
		if !skill.Eligible {
			continue
		}
		for k, v := range filteredRuntimeEnv(skill.RuntimeEnv) {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	return out
}

func (inv Inventory) RunEnvForSkill(name string) map[string]string {
	skill, ok := inv.Get(name)
	if !ok || !skill.Eligible {
		return nil
	}
	return filteredRuntimeEnv(skill.RuntimeEnv)
}

func (inv Inventory) ResolveBundlePath(name, relPath string) (string, error) {
	skill, ok := inv.Get(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}
	root, err := filepath.EvalSymlinks(skill.Dir)
	if err != nil {
		return "", err
	}
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return root, nil
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("bundle path must be relative")
	}
	full := filepath.Join(root, relPath)
	clean := filepath.Clean(full)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		real = clean
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fs.ErrPermission
	}
	return real, nil
}

func LoadBody(path string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 200000
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fs.ErrPermission
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	return string(b), nil
}

func hash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}

func hasBinary(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func configTruthy(global map[string]any, skill map[string]any, path string) bool {
	if truthy(lookupPath(global, path)) {
		return true
	}
	return truthy(lookupPath(skill, path))
}

func lookupPath(root map[string]any, path string) any {
	if len(root) == 0 || strings.TrimSpace(path) == "" {
		return nil
	}
	var current any = root
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}
	return current
}

func truthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case string:
		return strings.TrimSpace(val) != ""
	case float64:
		return val != 0
	case int:
		return val != 0
	case []any:
		return len(val) > 0
	case map[string]any:
		return len(val) > 0
	default:
		return true
	}
}

func envMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, raw := range values {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}

func toStringMap(v any) map[string]any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = normalizeValue(child)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[fmt.Sprint(k)] = normalizeValue(child)
		}
		return out
	default:
		return map[string]any{}
	}
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any, map[any]any:
		return toStringMap(val)
	case []any:
		out := make([]any, 0, len(val))
		for _, item := range val {
			out = append(out, normalizeValue(item))
		}
		return out
	default:
		return v
	}
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func filteredRuntimeEnv(env map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(os.Getenv(k)) != "" {
			continue
		}
		out[k] = v
	}
	return out
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func skillHasRunnableContent(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return fs.SkipAll
		}
		if path == dir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(strings.TrimSpace(d.Name()))
		if name == "skill.md" || name == "skill.json" {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if info.Mode()&0o111 != 0 || ext == ".sh" || ext == ".py" {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
