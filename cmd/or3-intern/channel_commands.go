package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/runners"
)

type channelCommandHandler struct {
	Config        config.Config
	DB            *db.DB
	RunnerManager *runners.Manager
	Channels      *rootchannels.Manager
	CLI           *cli.Deliverer
}

func (h *channelCommandHandler) Handle(ctx context.Context, ev bus.Event) (bus.Event, bool, error) {
	if h == nil || ev.Type != bus.EventUserMessage {
		return ev, false, nil
	}
	cmd, args, ok := parseChannelCommand(ev.Message)
	if ok {
		switch cmd {
		case "help":
			h.deliver(ctx, ev, channelCommandHelp())
			return ev, true, nil
		case "settings":
			h.deliver(ctx, ev, h.settingsText(ctx, ev.SessionKey))
			return ev, true, nil
		case "runners":
			h.deliver(ctx, ev, h.runnersText())
			return ev, true, nil
		case "runner":
			h.handleRunnerCommand(ctx, ev, args)
			return ev, true, nil
		case "models":
			h.handleModelsCommand(ctx, ev, args)
			return ev, true, nil
		case "model":
			h.handleModelCommand(ctx, ev, args)
			return ev, true, nil
		case "reset":
			h.handleResetCommand(ctx, ev)
			return ev, true, nil
		}
	}
	return h.applyPreferences(ctx, ev)
}

func parseChannelCommand(message string) (string, []string, bool) {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "/") {
		return "", nil, false
	}
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return "", nil, false
	}
	name := strings.TrimPrefix(fields[0], "/")
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", nil, false
	}
	return name, fields[1:], true
}

func channelCommandHelp() string {
	return strings.Join([]string{
		"Channel commands:",
		"/settings — show current runner and model",
		"/runners — list selectable runners",
		"/runner <id> — choose a runner for this channel session",
		"/models [runner] [provider] — list model IDs you can copy",
		"/model <id> — choose a model by exact ID",
		"/reset — clear saved runner/model preferences",
		"/approve <id> and /deny <id> — respond to approval prompts",
	}, "\n")
}

func (h *channelCommandHandler) settingsText(ctx context.Context, sessionKey string) string {
	meta, _ := h.sessionMeta(ctx, sessionKey)
	runnerID := strings.TrimSpace(meta.RunnerID)
	if runnerID == "" {
		runnerID = string(runners.ResolveDefaultRunner(h.Config)) + " (default)"
	}
	model := strings.TrimSpace(meta.RunnerModel)
	if model == "" {
		defaultRunner := strings.TrimSuffix(runnerID, " (default)")
		model = strings.TrimSpace(h.Config.Runners.DefaultModels[defaultRunner])
		if model == "" {
			model = "runner default"
		} else {
			model += " (default)"
		}
	}
	return fmt.Sprintf("Current channel settings:\nRunner: %s\nModel: %s", runnerID, model)
}

func (h *channelCommandHandler) runnersText() string {
	specs := runners.SelectableRunners()
	if len(specs) == 0 {
		return "No selectable runners are registered."
	}
	defaultRunner := string(runners.ResolveDefaultRunner(h.Config))
	lines := []string{"Selectable runners:"}
	for _, spec := range specs {
		label := string(spec.ID)
		if spec.DisplayName != "" {
			label += " — " + spec.DisplayName
		}
		if string(spec.ID) == defaultRunner {
			label += " (default)"
		}
		if runners.IsRunnerDisabledByConfig(h.Config, spec.ID) {
			label += " (disabled)"
		}
		lines = append(lines, "- "+label)
	}
	lines = append(lines, "Use /runner <id> to choose one.")
	return strings.Join(lines, "\n")
}

func (h *channelCommandHandler) handleRunnerCommand(ctx context.Context, ev bus.Event, args []string) {
	if len(args) == 0 {
		h.deliver(ctx, ev, h.runnersText())
		return
	}
	runnerID := strings.ToLower(strings.TrimSpace(args[0]))
	if err := runners.ValidateSelectableRunner(h.Config, runners.RunnerID(runnerID)); err != nil {
		h.deliver(ctx, ev, "I couldn't use that runner: "+err.Error()+"\n\n"+h.runnersText())
		return
	}
	label := h.runnerLabel(runnerID)
	if h.DB != nil {
		if _, err := h.DB.SetChatSessionRunnerPreference(ctx, ev.SessionKey, runnerID, label, ""); err != nil {
			h.deliver(ctx, ev, "I couldn't save that runner preference: "+err.Error())
			return
		}
	}
	text := "Runner set to " + runnerID + "."
	if label != "" && label != runnerID {
		text = "Runner set to " + label + " (" + runnerID + ")."
	}
	text += " Model preference cleared; use /models to browse exact model IDs, then /model <id>."
	h.deliver(ctx, ev, text)
}

func (h *channelCommandHandler) handleModelsCommand(ctx context.Context, ev bus.Event, args []string) {
	runnerID, providerFilter := h.parseModelsArgs(ctx, ev.SessionKey, args)
	if err := runners.ValidateSelectableRunner(h.Config, runners.RunnerID(runnerID)); err != nil {
		h.deliver(ctx, ev, "I couldn't list models for that runner: "+err.Error())
		return
	}
	models := h.runnerModels(ctx, runnerID)
	if len(models) == 0 {
		h.deliver(ctx, ev, h.modelsUnavailableText(runnerID))
		return
	}
	providers := groupedModelProviders(models)
	if providerFilter == "" && len(providers) > 1 {
		h.deliverWithMeta(ctx, ev, h.modelProviderPickerText(runnerID, models, providers), telegramModelProviderReplyMarkup(runnerID, providers))
		return
	}
	filtered := filterModelsByProvider(models, providerFilter)
	if len(filtered) == 0 {
		h.deliverWithMeta(ctx, ev, h.unknownProviderText(runnerID, providerFilter, providers), telegramModelProviderReplyMarkup(runnerID, providers))
		return
	}
	h.deliverWithMeta(ctx, ev, h.modelListText(runnerID, providerFilter, filtered, len(models)), telegramModelProviderReplyMarkup(runnerID, providers))
}

func (h *channelCommandHandler) handleModelCommand(ctx context.Context, ev bus.Event, args []string) {
	if len(args) == 0 {
		h.handleModelsCommand(ctx, ev, nil)
		return
	}
	model := strings.TrimSpace(strings.Join(args, " "))
	meta, _ := h.sessionMeta(ctx, ev.SessionKey)
	runnerID := strings.TrimSpace(meta.RunnerID)
	if runnerID == "" {
		runnerID = string(runners.ResolveDefaultRunner(h.Config))
	}
	if err := runners.ValidateSelectableRunner(h.Config, runners.RunnerID(runnerID)); err != nil {
		h.deliver(ctx, ev, "Choose a runner before setting a model: "+err.Error())
		return
	}
	canonicalModel, err := h.normalizeModel(ctx, runnerID, model)
	if err != nil {
		h.deliver(ctx, ev, err.Error()+"\n\n"+h.modelsHint(ctx, runnerID))
		return
	}
	label := h.runnerLabel(runnerID)
	if h.DB != nil {
		if _, err := h.DB.SetChatSessionRunnerPreference(ctx, ev.SessionKey, runnerID, label, canonicalModel); err != nil {
			h.deliver(ctx, ev, "I couldn't save that model preference: "+err.Error())
			return
		}
	}
	h.deliver(ctx, ev, fmt.Sprintf("Model set to %s for runner %s.", canonicalModel, runnerID))
}

func (h *channelCommandHandler) handleResetCommand(ctx context.Context, ev bus.Event) {
	if h.DB != nil {
		if _, err := h.DB.SetChatSessionRunnerPreference(ctx, ev.SessionKey, "", "", ""); err != nil {
			h.deliver(ctx, ev, "I couldn't reset channel preferences: "+err.Error())
			return
		}
	}
	h.deliver(ctx, ev, "Channel runner/model preferences reset. Future turns will use the service defaults.")
}

func (h *channelCommandHandler) applyPreferences(ctx context.Context, ev bus.Event) (bus.Event, bool, error) {
	meta, ok := h.sessionMeta(ctx, ev.SessionKey)
	if !ok {
		return ev, false, nil
	}
	savedRunnerID := strings.TrimSpace(meta.RunnerID)
	model := strings.TrimSpace(meta.RunnerModel)
	if savedRunnerID != "" {
		if err := runners.ValidateSelectableRunner(h.Config, runners.RunnerID(savedRunnerID)); err != nil {
			h.deliver(ctx, ev, "The saved runner for this channel is no longer available: "+err.Error()+"\nUse /runner <id> to choose a new runner or /reset to use defaults.")
			return ev, true, err
		}
	}
	runnerID := savedRunnerID
	if runnerID == "" {
		runnerID = string(runners.ResolveDefaultRunner(h.Config))
	}
	if model != "" {
		canonicalModel, err := h.normalizeModel(ctx, runnerID, model)
		if err != nil {
			h.deliver(ctx, ev, "The saved model for this channel is no longer available: "+err.Error()+"\nUse /models to browse exact IDs, /model <id> to choose one, or /reset to use defaults.")
			return ev, true, err
		}
		model = canonicalModel
	}
	if savedRunnerID == "" && model == "" {
		return ev, false, nil
	}
	if ev.Meta == nil {
		ev.Meta = map[string]any{}
	}
	if savedRunnerID != "" {
		ev.Meta["runner_id"] = savedRunnerID
	}
	if model != "" {
		ev.Meta["model"] = model
	}
	return ev, false, nil
}

func (h *channelCommandHandler) sessionMeta(ctx context.Context, sessionKey string) (db.ChatSessionMeta, bool) {
	if h == nil || h.DB == nil || strings.TrimSpace(sessionKey) == "" {
		return db.ChatSessionMeta{}, false
	}
	meta, err := h.DB.GetChatSessionMeta(ctx, sessionKey)
	if err != nil {
		if errors.Is(err, db.ErrChatSessionNotFound) {
			return db.ChatSessionMeta{}, false
		}
		log.Printf("channel command: load session meta for %q: %v", sessionKey, err)
		return db.ChatSessionMeta{}, false
	}
	return meta, true
}

func (h *channelCommandHandler) runnerLabel(runnerID string) string {
	for _, spec := range runners.SelectableRunners() {
		if strings.EqualFold(string(spec.ID), runnerID) {
			if strings.TrimSpace(spec.DisplayName) != "" {
				return strings.TrimSpace(spec.DisplayName)
			}
			return string(spec.ID)
		}
	}
	return runnerID
}

func (h *channelCommandHandler) runnerModels(ctx context.Context, runnerID string) []runners.RunnerModelInfo {
	if h == nil || h.RunnerManager == nil {
		return nil
	}
	runner := runners.RunnerID(strings.TrimSpace(runnerID))
	if h.RunnerManager.Registry != nil {
		if info, ok := h.RunnerManager.Registry.DetectCached(runner, 5*time.Minute); ok {
			if len(info.Runtime.Models) > 0 {
				return sortedRunnerModels(info.Runtime.Models)
			}
		}
	}
	if h.RunnerManager.Runtimes != nil {
		if runtime, ok := h.RunnerManager.Runtimes.Get(runner); ok {
			cfg := h.RunnerManager.Cfg
			if cfg.Default == "" && len(cfg.RuntimeMode) == 0 && len(cfg.DefaultModels) == 0 {
				cfg = h.Config.Runners
			}
			info := runtime.Info(ctx, cfg, h.RunnerManager.DetectOptions().Env)
			if len(info.Models) > 0 {
				return sortedRunnerModels(info.Models)
			}
		}
	}
	if h.RunnerManager.Registry == nil {
		return nil
	}
	info := runners.Detect(ctx, runnerSpecForID(runner), h.RunnerManager.DetectOptions())
	return sortedRunnerModels(info.Runtime.Models)
}

func runnerSpecForID(runnerID runners.RunnerID) runners.RunnerSpec {
	for _, spec := range runners.SelectableRunners() {
		if spec.ID == runnerID {
			return spec
		}
	}
	return runners.RunnerSpec{ID: runnerID}
}

func sortedRunnerModels(models []runners.RunnerModelInfo) []runners.RunnerModelInfo {
	out := append([]runners.RunnerModelInfo{}, models...)
	sort.Slice(out, func(left, right int) bool {
		if out[left].Default != out[right].Default {
			return out[left].Default
		}
		return strings.ToLower(out[left].ID) < strings.ToLower(out[right].ID)
	})
	return out
}

func (h *channelCommandHandler) parseModelsArgs(ctx context.Context, sessionKey string, args []string) (string, string) {
	runnerID := ""
	providerFilter := ""
	if len(args) > 0 {
		first := strings.ToLower(strings.TrimSpace(args[0]))
		if first != "" {
			if err := runners.ValidateSelectableRunner(h.Config, runners.RunnerID(first)); err == nil {
				runnerID = first
				if len(args) > 1 {
					providerFilter = strings.TrimSpace(strings.Join(args[1:], " "))
				}
			} else {
				providerFilter = strings.TrimSpace(strings.Join(args, " "))
			}
		}
	}
	if runnerID == "" {
		meta, _ := h.sessionMeta(ctx, sessionKey)
		runnerID = strings.TrimSpace(meta.RunnerID)
	}
	if runnerID == "" {
		runnerID = string(runners.ResolveDefaultRunner(h.Config))
	}
	return strings.ToLower(strings.TrimSpace(runnerID)), providerFilter
}

func (h *channelCommandHandler) modelsUnavailableText(runnerID string) string {
	lines := []string{
		"I couldn't fetch a live model list for " + runnerID + ".",
	}
	if defaultModel := strings.TrimSpace(h.Config.Runners.DefaultModels[runnerID]); defaultModel != "" {
		lines = append(lines, "Configured default: "+defaultModel)
	}
	if runnerID == string(runners.RunnerOpenCode) {
		lines = append(lines,
			"Use the plain model ID after /model. If a catalog or provider shows openrouter/kimi-k2.5, set /model kimi-k2.5.",
			"Provider/model input is accepted too, but OR3 stores the plain OpenCode model ID.",
		)
	} else {
		lines = append(lines, "Use /model <exact-id> if you already know a model this runner supports.")
	}
	lines = append(lines, "Try /models again after the runner is reachable.")
	return strings.Join(lines, "\n")
}

func (h *channelCommandHandler) modelProviderPickerText(runnerID string, models []runners.RunnerModelInfo, providers []modelProviderGroup) string {
	lines := []string{
		"Models for " + runnerID + " are grouped by provider.",
		"Tap a provider, or send /models " + runnerID + " <provider>.",
		"Then set one with /model <exact-id>.",
		"",
		"Providers:",
	}
	for _, provider := range providers {
		lines = append(lines, fmt.Sprintf("- %s (%d)", provider.Label, provider.Count))
	}
	if defaultModel := strings.TrimSpace(h.Config.Runners.DefaultModels[runnerID]); defaultModel != "" {
		lines = append(lines, "", "Configured default: "+defaultModel)
	}
	if runnerID == string(runners.RunnerOpenCode) && len(models) > 0 {
		lines = append(lines, "", "Tip: for OpenCode, use the model ID shown after the provider. Provider/model input also works, but OR3 stores the plain ID.")
	}
	return strings.Join(lines, "\n")
}

func (h *channelCommandHandler) unknownProviderText(runnerID, providerFilter string, providers []modelProviderGroup) string {
	lines := []string{
		"No models matched provider " + strings.TrimSpace(providerFilter) + " for " + runnerID + ".",
		"Available providers:",
	}
	for _, provider := range providers {
		lines = append(lines, "- "+provider.Label+" ("+provider.Key+")")
	}
	return strings.Join(lines, "\n")
}

func (h *channelCommandHandler) modelListText(runnerID, providerFilter string, models []runners.RunnerModelInfo, totalModels int) string {
	const maxModelsInChannelList = 30
	title := "Known model IDs for " + runnerID
	if providerFilter != "" && len(models) > 0 {
		title += " / " + modelProviderLabel(models[0])
	}
	lines := []string{title + ":"}
	limit := len(models)
	if limit > maxModelsInChannelList {
		limit = maxModelsInChannelList
	}
	for _, model := range models[:limit] {
		label := model.ID
		if model.DisplayName != "" && model.DisplayName != model.ID {
			label += " — " + model.DisplayName
		}
		if model.Default {
			label += " (default)"
		}
		lines = append(lines, "- "+label)
	}
	if len(models) > limit {
		lines = append(lines, fmt.Sprintf("Showing %d of %d for this provider. Narrow by provider or use /model <exact-id> if you already know it.", limit, len(models)))
	} else if providerFilter == "" && totalModels > len(models) {
		lines = append(lines, fmt.Sprintf("Showing %d of %d known models.", len(models), totalModels))
	}
	lines = append(lines, "Set one with /model <exact-id>.")
	if runnerID == string(runners.RunnerOpenCode) {
		lines = append(lines, "For OpenCode, the plain ID is the least confusing choice. Example: /model "+models[0].ID)
	}
	return strings.Join(lines, "\n")
}

func (h *channelCommandHandler) normalizeModel(ctx context.Context, runnerID, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errors.New("model is required")
	}
	models := h.runnerModels(ctx, runnerID)
	if len(models) == 0 {
		return canonicalOpenCodeModelWithoutCatalog(runnerID, model), nil
	}
	for _, item := range models {
		if modelMatchesRunnerModel(runnerID, item, model) {
			return item.ID, nil
		}
	}
	if runnerID == string(runners.RunnerOpenCode) {
		return canonicalOpenCodeModelWithoutCatalog(runnerID, model), nil
	}
	return "", fmt.Errorf("model %q is not in the known model list for %s", model, runnerID)
}

func (h *channelCommandHandler) modelsHint(ctx context.Context, runnerID string) string {
	models := h.runnerModels(ctx, runnerID)
	if len(models) == 0 {
		return "Use /models to inspect available models, or set a runner-supported model ID directly."
	}
	ids := make([]string, 0, len(models))
	for i, model := range models {
		if i >= 12 {
			ids = append(ids, "...")
			break
		}
		ids = append(ids, model.ID)
	}
	return "Known models: " + strings.Join(ids, ", ")
}

func (h *channelCommandHandler) deliver(ctx context.Context, ev bus.Event, text string) {
	h.deliverWithMeta(ctx, ev, text, nil)
}

func (h *channelCommandHandler) deliverWithMeta(ctx context.Context, ev bus.Event, text string, extraMeta map[string]any) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.EqualFold(ev.Channel, "cli") && h.CLI != nil {
		h.CLI.ShowNoticeForSession(ev.SessionKey, text)
		return
	}
	if h.Channels == nil {
		return
	}
	meta := rootchannels.ReplyMeta(ev.Meta)
	if len(extraMeta) > 0 {
		if meta == nil {
			meta = map[string]any{}
		}
		for key, value := range extraMeta {
			meta[key] = value
		}
	}
	_ = h.Channels.DeliverWithMeta(ctx, ev.Channel, channelEventTarget(ev), text, meta)
}

type modelProviderGroup struct {
	Key   string
	Label string
	Count int
}

func groupedModelProviders(models []runners.RunnerModelInfo) []modelProviderGroup {
	byKey := map[string]modelProviderGroup{}
	for _, model := range models {
		key := modelProviderKey(model)
		group := byKey[key]
		if group.Key == "" {
			group.Key = key
			group.Label = modelProviderLabel(model)
		}
		group.Count++
		byKey[key] = group
	}
	out := make([]modelProviderGroup, 0, len(byKey))
	for _, group := range byKey {
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

func filterModelsByProvider(models []runners.RunnerModelInfo, providerFilter string) []runners.RunnerModelInfo {
	filter := strings.ToLower(strings.TrimSpace(providerFilter))
	if filter == "" {
		return models
	}
	out := make([]runners.RunnerModelInfo, 0, len(models))
	for _, model := range models {
		if strings.EqualFold(modelProviderKey(model), filter) || strings.EqualFold(modelProviderLabel(model), providerFilter) || strings.Contains(strings.ToLower(modelProviderLabel(model)), filter) {
			out = append(out, model)
		}
	}
	return out
}

func modelProviderKey(model runners.RunnerModelInfo) string {
	key := strings.ToLower(strings.TrimSpace(model.Provider))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(model.ProviderName))
	}
	if key == "" {
		key = "default"
	}
	key = strings.NewReplacer(" ", "-", "_", "-").Replace(key)
	return key
}

func modelProviderLabel(model runners.RunnerModelInfo) string {
	if label := strings.TrimSpace(model.ProviderName); label != "" {
		return label
	}
	if label := strings.TrimSpace(model.Provider); label != "" {
		return label
	}
	return "Default"
}

func modelMatchesRunnerModel(runnerID string, item runners.RunnerModelInfo, requested string) bool {
	requested = strings.TrimSpace(requested)
	if strings.EqualFold(item.ID, requested) || strings.EqualFold(item.DisplayName, requested) {
		return true
	}
	providerPart, modelPart, split := strings.Cut(requested, "/")
	if !split || strings.TrimSpace(modelPart) == "" {
		return false
	}
	if strings.EqualFold(item.ID, modelPart) {
		return true
	}
	if strings.EqualFold(item.Provider+"/"+item.ID, providerPart+"/"+modelPart) {
		return true
	}
	if runnerID == string(runners.RunnerOpenCode) && !strings.Contains(modelPart, "/") {
		return strings.EqualFold(item.ID, modelPart)
	}
	return false
}

func canonicalOpenCodeModelWithoutCatalog(runnerID, requested string) string {
	requested = strings.TrimSpace(requested)
	if runnerID != string(runners.RunnerOpenCode) {
		return requested
	}
	_, modelPart, ok := strings.Cut(requested, "/")
	if ok && strings.TrimSpace(modelPart) != "" && !strings.Contains(modelPart, "/") {
		return strings.TrimSpace(modelPart)
	}
	return requested
}

func telegramModelProviderReplyMarkup(runnerID string, providers []modelProviderGroup) map[string]any {
	if strings.TrimSpace(runnerID) == "" || len(providers) == 0 {
		return nil
	}
	rows := make([][]map[string]string, 0, len(providers))
	for _, provider := range providers {
		if strings.TrimSpace(provider.Key) == "" {
			continue
		}
		callbackData := "or3:models:" + strings.TrimSpace(runnerID) + ":" + provider.Key
		if len(callbackData) > 64 {
			continue
		}
		rows = append(rows, []map[string]string{{
			"text":          provider.Label,
			"callback_data": callbackData,
		}})
	}
	if len(rows) == 0 {
		return nil
	}
	return map[string]any{"telegram_reply_markup": map[string]any{"inline_keyboard": rows}}
}
