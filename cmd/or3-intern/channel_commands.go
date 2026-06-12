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
	Config          config.Config
	DB              *db.DB
	AgentCLIManager *runners.Manager
	Channels        *rootchannels.Manager
	CLI             *cli.Deliverer
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
		"/models [runner] — list known models for a runner",
		"/model <name> — choose a model for this channel session",
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
	text += " Model preference cleared; use /model <name> if you want a specific model."
	h.deliver(ctx, ev, text)
}

func (h *channelCommandHandler) handleModelsCommand(ctx context.Context, ev bus.Event, args []string) {
	runnerID := ""
	if len(args) > 0 {
		runnerID = strings.ToLower(strings.TrimSpace(args[0]))
	}
	if runnerID == "" {
		meta, _ := h.sessionMeta(ctx, ev.SessionKey)
		runnerID = strings.TrimSpace(meta.RunnerID)
	}
	if runnerID == "" {
		runnerID = string(runners.ResolveDefaultRunner(h.Config))
	}
	if err := runners.ValidateSelectableRunner(h.Config, runners.RunnerID(runnerID)); err != nil {
		h.deliver(ctx, ev, "I couldn't list models for that runner: "+err.Error())
		return
	}
	models := h.runnerModels(ctx, runnerID)
	if len(models) == 0 {
		defaultModel := strings.TrimSpace(h.Config.Runners.DefaultModels[runnerID])
		if defaultModel != "" {
			h.deliver(ctx, ev, fmt.Sprintf("No live model list is available for %s. Current configured default: %s. Use /model <name> to set a known runner-supported model.", runnerID, defaultModel))
			return
		}
		h.deliver(ctx, ev, fmt.Sprintf("No live model list is available for %s. Use /model <name> to set a known runner-supported model.", runnerID))
		return
	}
	lines := []string{"Known models for " + runnerID + ":"}
	for _, model := range models {
		label := model.ID
		if model.DisplayName != "" && model.DisplayName != model.ID {
			label += " — " + model.DisplayName
		}
		if model.ProviderName != "" {
			label += " [" + model.ProviderName + "]"
		}
		if model.Default {
			label += " (default)"
		}
		lines = append(lines, "- "+label)
	}
	lines = append(lines, "Use /model <name> to choose one.")
	h.deliver(ctx, ev, strings.Join(lines, "\n"))
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
	if err := h.validateModel(ctx, runnerID, model); err != nil {
		h.deliver(ctx, ev, err.Error()+"\n\n"+h.modelsHint(ctx, runnerID))
		return
	}
	label := h.runnerLabel(runnerID)
	if h.DB != nil {
		if _, err := h.DB.SetChatSessionRunnerPreference(ctx, ev.SessionKey, runnerID, label, model); err != nil {
			h.deliver(ctx, ev, "I couldn't save that model preference: "+err.Error())
			return
		}
	}
	h.deliver(ctx, ev, fmt.Sprintf("Model set to %s for runner %s.", model, runnerID))
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
		if err := h.validateModel(ctx, runnerID, model); err != nil {
			h.deliver(ctx, ev, "The saved model for this channel is no longer available: "+err.Error()+"\nUse /model <name> to choose a new model or /reset to use defaults.")
			return ev, true, err
		}
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
	if h == nil || h.AgentCLIManager == nil || h.AgentCLIManager.Registry == nil {
		return nil
	}
	runner := runners.RunnerID(strings.TrimSpace(runnerID))
	if info, ok := h.AgentCLIManager.Registry.DetectCached(runner, 5*time.Minute); ok {
		return sortedRunnerModels(info.Runtime.Models)
	}
	info := runners.Detect(ctx, runnerSpecForID(runner), h.AgentCLIManager.DetectOptions())
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

func (h *channelCommandHandler) validateModel(ctx context.Context, runnerID, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("model is required")
	}
	models := h.runnerModels(ctx, runnerID)
	if len(models) == 0 {
		return nil
	}
	for _, item := range models {
		if strings.EqualFold(item.ID, model) || strings.EqualFold(item.DisplayName, model) {
			return nil
		}
	}
	return fmt.Errorf("model %q is not in the known model list for %s", model, runnerID)
}

func (h *channelCommandHandler) modelsHint(ctx context.Context, runnerID string) string {
	models := h.runnerModels(ctx, runnerID)
	if len(models) == 0 {
		return "Use /models to inspect available models, or set a runner-supported model name directly."
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return "Known models: " + strings.Join(ids, ", ")
}

func (h *channelCommandHandler) deliver(ctx context.Context, ev bus.Event, text string) {
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
	_ = h.Channels.DeliverWithMeta(ctx, ev.Channel, channelEventTarget(ev), text, rootchannels.ReplyMeta(ev.Meta))
}
