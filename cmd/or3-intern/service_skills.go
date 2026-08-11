package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/skills"
)

type serviceInstallBundledRequest struct {
	Target string `json:"target"`
}

type serviceSkillItem struct {
	Name             string   `json:"name"`
	Key              string   `json:"key"`
	Description      string   `json:"description,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Homepage         string   `json:"homepage,omitempty"`
	Source           string   `json:"source"`
	Location         string   `json:"location"`
	Eligible         bool     `json:"eligible"`
	Disabled         bool     `json:"disabled"`
	Hidden           bool     `json:"hidden"`
	Status           string   `json:"status"`
	PermissionState  string   `json:"permission_state"`
	PermissionNotes  []string `json:"permission_notes,omitempty"`
	Missing          []string `json:"missing,omitempty"`
	Unsupported      []string `json:"unsupported,omitempty"`
	ParseError       string   `json:"parse_error,omitempty"`
	UserInvocable    bool     `json:"user_invocable"`
	PrimaryEnv       string   `json:"primary_env,omitempty"`
	RequiredEnv      []string `json:"required_env,omitempty"`
	ConfigFields     []string `json:"config_fields,omitempty"`
	APIKeyConfigured bool     `json:"api_key_configured"`
}

type serviceSkillRoot struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
}

type serviceSkillSettingsRequest struct {
	Enabled     *bool             `json:"enabled"`
	APIKey      *string           `json:"api_key"`
	APIKeyCamel *string           `json:"apiKey"`
	Env         map[string]string `json:"env"`
	Config      map[string]any    `json:"config"`
}

func (s *serviceServer) handleSkills(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/skills"), "/")
	if path == "" {
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		cfg := s.configSnapshot()
		inv := s.serviceSkillsInventory(r.Context(), cfg)
		writeServiceValue(w, http.StatusOK, map[string]any{
			"items":                 serviceSkillItems(inv, cfg.Skills),
			"roots":                 serviceSkillRoots(cfg),
			"global_dir":            cfg.Skills.Load.GlobalDir,
			"global_skills_enabled": !cfg.Skills.Load.DisableGlobalDir,
		})
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) == 1 && parts[0] == "install-bundled" {
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleSkillInstallBundled(w, r)
		return
	}
	if len(parts) != 2 || parts[1] != "settings" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "skills route not found"})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(name) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid skill name"})
		return
	}
	s.handleSkillSettingsUpdate(w, r, name)
}

func (s *serviceServer) handleSkillSettingsUpdate(w http.ResponseWriter, r *http.Request, name string) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body serviceSkillSettingsRequest
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	current := config.Clone(s.config)
	inv := s.serviceSkillsInventory(r.Context(), current)
	skill, ok := inv.Get(name)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "skill not found"})
		return
	}
	next := config.Clone(current)
	if next.Skills.Entries == nil {
		next.Skills.Entries = map[string]config.SkillEntryConfig{}
	}
	entryKey := serviceSkillEntryKey(skill)
	entry := next.Skills.Entries[entryKey]
	if body.Enabled != nil {
		enabled := *body.Enabled
		entry.Enabled = &enabled
	}
	if apiKey := firstStringPointer(body.APIKey, body.APIKeyCamel); apiKey != nil {
		entry.APIKey = *apiKey
	}
	if body.Env != nil {
		entry.Env = mergeServiceSkillEnv(entry.Env, body.Env)
	}
	if body.Config != nil {
		entry.Config = mergeServiceSkillConfig(entry.Config, body.Config)
	}
	next.Skills.Entries[entryKey] = entry
	path, err := s.saveConfigureConfigLocked(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "skill settings save failed", err)
		return
	}
	updated := s.serviceSkillsInventory(r.Context(), next)
	s.applyServiceSkillsInventory(updated)
	itemSkill, _ := updated.Get(skill.Name)
	writeServiceValue(w, http.StatusOK, map[string]any{
		"ok":          true,
		"config_path": path,
		"skill":       serviceSkillItemFromMeta(itemSkill, next.Skills),
	})
}

func (s *serviceServer) handleSkillInstallBundled(w http.ResponseWriter, r *http.Request) {
	var body serviceInstallBundledRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body", "detail": err.Error()})
			return
		}
		body.Target = "global"
	}
	target := strings.TrimSpace(body.Target)
	cfg := s.configSnapshot()
	var targetDir string
	switch target {
	case "global":
		targetDir = strings.TrimSpace(cfg.Skills.Load.GlobalDir)
	case "workspace":
		targetDir = filepath.Join(strings.TrimSpace(cfg.WorkspaceDir), "skills")
	default:
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("unknown install target: %q", target)})
		return
	}
	if targetDir == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "install target directory not configured"})
		return
	}
	installed, err := installBundledSkills(targetDir)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "install bundled skills failed", err)
		return
	}
	writeServiceValue(w, http.StatusOK, map[string]any{
		"ok":        true,
		"target":    target,
		"targetDir": targetDir,
		"skills":    installed,
	})
}

func (s *serviceServer) serviceSkillsInventory(ctx context.Context, cfg config.Config) skills.Inventory {
	return buildSkillsInventory(cfg, s.serviceBundledSkillsDir(), s.serviceAvailableToolNames(ctx, cfg))
}

func (s *serviceServer) serviceAvailableToolNames(ctx context.Context, cfg config.Config) map[string]struct{} {
	return filterAdvertisedToolNames(cfg, availableToolNames(cfg.Cron.Enabled))
}

func (s *serviceServer) serviceBundledSkillsDir() string {
	cfgPath := strings.TrimSpace(s.configPath)
	if cfgPath == "" {
		cfgPath = cfgPathOrDefault("")
	}
	dir, err := resolveBundledSkillsDir(cfgPath)
	if err != nil {
		return filepath.Join(filepath.Dir(cfgPath), "builtin_skills")
	}
	return dir
}

func (s *serviceServer) applyServiceSkillsInventory(_ skills.Inventory) {}

func serviceSkillRoots(cfg config.Config) []serviceSkillRoot {
	roots := buildSkillRoots(cfg, "")
	out := make([]serviceSkillRoot, 0, len(roots))
	for _, root := range roots {
		out = append(out, serviceSkillRoot{
			Path:    root.Path,
			Source:  string(root.Source),
			Enabled: strings.TrimSpace(root.Path) != "",
		})
	}
	return out
}

func serviceSkillItems(inv skills.Inventory, cfg config.SkillsConfig) []serviceSkillItem {
	items := make([]serviceSkillItem, 0, len(inv.Skills))
	for _, skill := range inv.Skills {
		items = append(items, serviceSkillItemFromMeta(skill, cfg))
	}
	return items
}

func serviceSkillItemFromMeta(skill skills.SkillMeta, cfg config.SkillsConfig) serviceSkillItem {
	entry := cfg.Entries[serviceSkillEntryKey(skill)]
	permissionState := strings.TrimSpace(skill.PermissionState)
	if permissionState == "" {
		permissionState = "approved"
	}
	return serviceSkillItem{
		Name:             skill.Name,
		Key:              serviceSkillEntryKey(skill),
		Description:      skill.Description,
		Summary:          skill.Summary,
		Homepage:         skill.Homepage,
		Source:           string(skill.Source),
		Location:         skill.Dir,
		Eligible:         skill.Eligible,
		Disabled:         skill.Disabled,
		Hidden:           skill.Hidden,
		Status:           serviceSkillStatus(skill),
		PermissionState:  permissionState,
		PermissionNotes:  append([]string{}, skill.PermissionNotes...),
		Missing:          append([]string{}, skill.Missing...),
		Unsupported:      append([]string{}, skill.Unsupported...),
		ParseError:       skill.ParseError,
		UserInvocable:    skill.UserInvocable,
		PrimaryEnv:       skill.Metadata.PrimaryEnv,
		RequiredEnv:      append([]string{}, skill.Metadata.Requires.Env...),
		ConfigFields:     append([]string{}, skill.Metadata.Requires.Config...),
		APIKeyConfigured: strings.TrimSpace(entry.APIKey) != "",
	}
}

func serviceSkillEntryKey(skill skills.SkillMeta) string {
	if strings.TrimSpace(skill.Key) != "" {
		return strings.TrimSpace(skill.Key)
	}
	return strings.TrimSpace(skill.Name)
}

func serviceSkillStatus(skill skills.SkillMeta) string {
	switch {
	case strings.TrimSpace(skill.ParseError) != "":
		return "parse-error"
	case skill.Disabled:
		return "disabled"
	case skill.Hidden:
		return "hidden"
	case !skill.Eligible:
		return "ineligible"
	default:
		return "eligible"
	}
}

func firstStringPointer(values ...*string) *string {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func mergeServiceSkillEnv(current map[string]string, updates map[string]string) map[string]string {
	if current == nil {
		current = map[string]string{}
	}
	for key, value := range updates {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if strings.TrimSpace(value) == "" {
			delete(current, key)
			continue
		}
		current[key] = value
	}
	return current
}

func mergeServiceSkillConfig(current map[string]any, updates map[string]any) map[string]any {
	if current == nil {
		current = map[string]any{}
	}
	for key, value := range updates {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value == nil {
			delete(current, key)
			continue
		}
		current[key] = value
	}
	return current
}
