package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
)

type serviceConfigureChange struct {
	Section string               `json:"section"`
	Channel string               `json:"channel"`
	Field   string               `json:"field"`
	Op      string               `json:"op"`
	Value   configureChangeValue `json:"value"`
}

type configureChangeValue string

func (v *configureChangeValue) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*v = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*v = configureChangeValue(text)
		return nil
	}
	var flag bool
	if err := json.Unmarshal(data, &flag); err == nil {
		if flag {
			*v = "true"
		} else {
			*v = "false"
		}
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		*v = configureChangeValue(number.String())
		return nil
	}
	return fmt.Errorf("configure change value must be a string, boolean, number, or null")
}

func (v configureChangeValue) String() string {
	return string(v)
}

type serviceConfigureField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind"`
	Value       any      `json:"value,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	EmptyHint   string   `json:"emptyHint,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

func serviceConfigureFieldKind(kind configureFieldKind) string {
	switch kind {
	case configureFieldSecret:
		return "secret"
	case configureFieldToggle:
		return "toggle"
	case configureFieldChoice:
		return "choice"
	default:
		return "text"
	}
}

func serviceConfigureFieldValue(field configureField) any {
	if field.Kind == configureFieldToggle {
		return strings.EqualFold(strings.TrimSpace(field.Value), "on")
	}
	return field.Value
}

func serviceConfigureFieldDefinition(cfg config.Config, section, channel, fieldKey string) (configureField, bool) {
	var fields []configureField
	switch section {
	case "channels":
		fields = buildChannelFields(cfg, channel)
	case "mcp":
		fields = buildMCPFields(cfg, channel)
	default:
		fields = buildSectionFields(cfg, section, "")
	}
	for _, field := range fields {
		if field.Key == fieldKey {
			return field, true
		}
	}
	return configureField{}, false
}

func serviceConfigureBoolValue(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	default:
		return false, false
	}
}

func applyServiceConfigureSetValue(cfg *config.Config, section, channel, fieldKey, value string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	if field, ok := serviceConfigureFieldDefinition(*cfg, section, channel, fieldKey); ok && field.Kind == configureFieldToggle {
		if toggleValue, ok := serviceConfigureBoolValue(value); ok {
			if setToggleFieldValue(cfg, section, channel, fieldKey, toggleValue) {
				return true, nil
			}
		}
	}
	return applyFieldValue(cfg, section, channel, fieldKey, value)
}

func toServiceConfigureFields(fields []configureField) []serviceConfigureField {
	result := make([]serviceConfigureField, 0, len(fields))
	for _, field := range fields {
		result = append(result, serviceConfigureField{
			Key:         field.Key,
			Label:       field.Label,
			Description: field.Description,
			Kind:        serviceConfigureFieldKind(field.Kind),
			Value:       serviceConfigureFieldValue(field),
			Choices:     append([]string{}, field.Choices...),
			EmptyHint:   field.EmptyHint,
			Placeholder: field.EmptyHint,
		})
	}
	return result
}

func (s *serviceServer) handleConfigure(w http.ResponseWriter, r *http.Request) {
	if !requireServiceRole(w, r, approval.RoleOperator) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/v1/configure"), "/")
	switch {
	case path == "" || path == "sections":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		items := make([]map[string]any, 0, len(configureSections))
		for _, section := range configureSections {
			items = append(items, map[string]any{
				"key":         section.Key,
				"label":       section.Label,
				"description": section.Description,
				"status":      sectionStatus(s.config, section.Key),
			})
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"items": items})
	case path == "fields":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		section := normalizeConfigureSectionKey(serviceFirstNonEmpty(r.URL.Query().Get("section"), r.URL.Query().Get("sectionKey")))
		if section == "" {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "section is required"})
			return
		}
		channel := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("channel")))
		var fields []configureField
		if section == "channels" {
			if channel == "" {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "channel is required for channels section"})
				return
			}
			fields = buildChannelFields(s.config, channel)
		} else {
			fields = buildSectionFields(s.config, section, "")
		}
		writeServiceValue(w, http.StatusOK, map[string]any{
			"section": section,
			"channel": channel,
			"fields":  toServiceConfigureFields(fields),
		})
	case path == "channels/telegram/chats":
		s.handleConfigureTelegramChats(w, r)
	case path == "channels/discord/targets":
		s.handleConfigureDiscordTargets(w, r)
	case path == "providers":
		switch r.Method {
		case http.MethodGet:
			writeServiceValue(w, http.StatusOK, serviceProviderStatus(s.config))
		case http.MethodPost:
			s.handleConfigureProviderSave(w, r, "")
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case strings.HasPrefix(path, "providers/"):
		key := strings.Trim(strings.TrimPrefix(path, "providers/"), "/")
		switch r.Method {
		case http.MethodPut, http.MethodPatch:
			s.handleConfigureProviderSave(w, r, key)
		case http.MethodDelete:
			s.handleConfigureProviderDelete(w, r, key)
		default:
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		}
	case path == "models":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleConfigureModels(w, r)
	case path == "favorite-models":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleConfigureFavoriteModel(w, r)
	case path == "test":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.handleConfigureProviderTest(w, r)
	case path == "apply":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
		var body struct {
			Changes []serviceConfigureChange `json:"changes"`
		}
		if err := decodeServiceRequestBody(r.Body, &body); err != nil {
			writeServiceRequestDecodeError(w, err)
			return
		}
		if len(body.Changes) == 0 {
			writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "changes are required"})
			return
		}
		next := s.config
		for _, change := range body.Changes {
			section := normalizeConfigureSectionKey(change.Section)
			channel := strings.TrimSpace(change.Channel)
			field := strings.TrimSpace(change.Field)
			if section == "" || field == "" {
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "section and field are required for each change"})
				return
			}
			switch strings.ToLower(strings.TrimSpace(change.Op)) {
			case "", "set":
				changed, err := applyServiceConfigureSetValue(&next, section, channel, field, change.Value.String())
				if err != nil {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				if !changed {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported field update: " + section + "." + field})
					return
				}
			case "toggle":
				if !toggleFieldValue(&next, section, channel, field) {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported toggle field: " + section + "." + field})
					return
				}
			case "choose":
				changed, err := applyChoiceSelection(&next, section, channel, field, change.Value.String())
				if err != nil {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				if !changed {
					writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported choice field: " + section + "." + field})
					return
				}
			default:
				writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported op"})
				return
			}
		}
		normalizeDiscordInboundDefaults(&next)
		path, err := s.saveConfigureConfig(next)
		if err != nil {
			writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
			return
		}
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path, "live_reloaded": []string{"model_routing"}})
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "configure route not found"})
	}
}

func (s *serviceServer) saveConfigureConfig(next config.Config) (string, error) {
	path := s.configPath
	if strings.TrimSpace(path) == "" {
		path = cfgPathOrDefault("")
	}
	if err := config.Save(path, next); err != nil {
		return path, err
	}
	s.applyLiveConfig(next)
	return path, nil
}

func (s *serviceServer) applyLiveConfig(next config.Config) {
	if s == nil {
		return
	}
	s.config = next
	if s.runtime != nil {
		s.runtime.ApplyLiveModelConfig(runtimeModelConfigFromConfig(next))
	}
	if s.controlSvc != nil {
		s.controlSvc.Config = next
		s.controlSvc.Provider = newProviderClient(next)
	}
	if s.appSvc != nil {
		s.appSvc.SetConfig(next)
	}
	if s.modelCatalog != nil {
		s.modelCatalog.Clear()
	}
}

func serviceNormalizeProviderKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *serviceServer) handleConfigureProviderSave(w http.ResponseWriter, r *http.Request, pathKey string) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body struct {
		Key               string `json:"key"`
		Label             string `json:"label"`
		APIBase           string `json:"apiBase"`
		APIKey            string `json:"apiKey"`
		TimeoutSeconds    int    `json:"timeoutSeconds"`
		EnableVision      bool   `json:"enableVision"`
		DefaultChatModel  string `json:"defaultChatModel"`
		DefaultEmbedModel string `json:"defaultEmbedModel"`
		DefaultDimensions int    `json:"defaultDimensions"`
		ClearAPIKey       bool   `json:"clearApiKey"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	key := serviceNormalizeProviderKey(serviceFirstNonEmpty(pathKey, body.Key))
	if key == "" {
		key = serviceNormalizeProviderKey(body.Label)
	}
	if key == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider key or label is required"})
		return
	}
	if strings.TrimSpace(body.APIBase) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "apiBase is required"})
		return
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(body.APIBase)); err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "apiBase must be a valid URL"})
		return
	}
	next := s.config
	if next.Providers == nil {
		next.Providers = config.ProviderProfiles{}
	}
	profile := next.Providers[key]
	profile.Label = strings.TrimSpace(serviceFirstNonEmpty(body.Label, profile.Label, key))
	profile.APIBase = strings.TrimRight(strings.TrimSpace(body.APIBase), "/")
	if body.ClearAPIKey {
		profile.APIKey = ""
	} else if strings.TrimSpace(body.APIKey) != "" {
		profile.APIKey = strings.TrimSpace(body.APIKey)
	}
	if body.TimeoutSeconds > 0 {
		profile.TimeoutSeconds = body.TimeoutSeconds
	}
	profile.EnableVision = body.EnableVision
	profile.DefaultChatModel = strings.TrimSpace(body.DefaultChatModel)
	profile.DefaultEmbedModel = strings.TrimSpace(body.DefaultEmbedModel)
	if body.DefaultDimensions >= 0 {
		profile.DefaultDimensions = body.DefaultDimensions
	}
	next.Providers[key] = profile
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path, "provider": key})
}

func (s *serviceServer) handleConfigureProviderDelete(w http.ResponseWriter, r *http.Request, key string) {
	key = serviceNormalizeProviderKey(key)
	if key == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider key is required"})
		return
	}
	if key == "openai" || key == "openrouter" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "built-in providers cannot be deleted"})
		return
	}
	next := s.config
	if next.Providers != nil {
		delete(next.Providers, key)
	}
	if next.FavoriteModels != nil {
		delete(next.FavoriteModels, key)
	}
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path})
}

func serviceProviderStatus(cfg config.Config) map[string]any {
	providerItems := make([]map[string]any, 0, len(cfg.Providers))
	keys := make([]string, 0, len(cfg.Providers))
	for key := range cfg.Providers {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		profile := cfg.Providers[key]
		providerItems = append(providerItems, map[string]any{
			"key":               key,
			"label":             profile.Label,
			"apiBase":           profile.APIBase,
			"apiKeyConfigured":  strings.TrimSpace(profile.APIKey) != "",
			"timeoutSeconds":    profile.TimeoutSeconds,
			"enableVision":      profile.EnableVision,
			"defaultChatModel":  profile.DefaultChatModel,
			"defaultEmbedModel": profile.DefaultEmbedModel,
			"defaultDimensions": profile.DefaultDimensions,
			"favorites":         cfg.FavoriteModels[key],
		})
	}
	roleItems := map[string]any{}
	for _, roleName := range []string{config.ModelRoleChat, config.ModelRoleAgents, config.ModelRoleSubagents, config.ModelRoleSummarization, config.ModelRoleContextManager, config.ModelRoleEmbeddings} {
		role := cfg.ModelRole(roleName)
		roleItems[roleName] = map[string]any{
			"primary":         role.Primary,
			"fallbacks":       role.Fallbacks,
			"embedDimensions": role.EmbedDimensions,
			"warnings":        providerRoleWarnings(cfg, role),
		}
	}
	return map[string]any{"providers": providerItems, "roles": roleItems}
}

func providerRoleWarnings(cfg config.Config, role config.ModelRoleConfig) []string {
	var warnings []string
	check := func(ref config.ModelRef) {
		profile, ok := cfg.ProviderProfile(ref.Provider)
		if !ok {
			warnings = append(warnings, "provider not configured: "+ref.Provider)
			return
		}
		if strings.TrimSpace(profile.APIBase) == "" {
			warnings = append(warnings, "provider missing API base: "+ref.Provider)
		}
		if strings.TrimSpace(profile.APIKey) == "" {
			warnings = append(warnings, "provider missing API key: "+ref.Provider)
		}
	}
	check(role.Primary)
	seen := map[string]struct{}{}
	for _, fallback := range role.Fallbacks {
		key := fallback.Provider + "/" + fallback.Model
		if _, ok := seen[key]; ok {
			warnings = append(warnings, "duplicate fallback: "+key)
		}
		seen[key] = struct{}{}
		check(fallback)
	}
	return warnings
}

func (s *serviceServer) handleConfigureFavoriteModel(w http.ResponseWriter, r *http.Request) {
	limitServiceRequestBody(w, r, serviceConfigureBodyLimit)
	var body struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Label    string `json:"label"`
		Favorite *bool  `json:"favorite"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	provider := serviceNormalizeProviderKey(body.Provider)
	model := strings.TrimSpace(body.Model)
	if provider == "" || model == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "provider and model are required"})
		return
	}
	favorite := true
	if body.Favorite != nil {
		favorite = *body.Favorite
	}
	next := s.config
	if next.FavoriteModels == nil {
		next.FavoriteModels = config.FavoriteModelsConfig{}
	}
	current := next.FavoriteModels[provider]
	out := make([]config.FavoriteModelConfig, 0, len(current)+1)
	for _, item := range current {
		if strings.TrimSpace(item.Model) == model {
			continue
		}
		out = append(out, item)
	}
	if favorite {
		out = append([]config.FavoriteModelConfig{{Model: model, Label: strings.TrimSpace(body.Label)}}, out...)
	}
	next.FavoriteModels[provider] = out
	path, err := s.saveConfigureConfig(next)
	if err != nil {
		writeServiceError(w, r, http.StatusBadGateway, "config save failed", err)
		return
	}
	writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "config_path": path, "favorites": next.FavoriteModels[provider]})
}
