package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type configureMCPScreen struct{ configureNoopScreen }

func (configureMCPScreen) Update(msg tea.Msg, model *configureTUIModel) (bool, tea.Cmd) {
	if model.screen == configureScreenMCPForm || model.screen == configureScreenForm {
		return configureFormScreen{}.Update(msg, model)
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if ok {
		switch model.screen {
		case configureScreenMCPServerList:
			next, cmd := model.updateMCPServerList(keyMsg)
			return applyConfigureModelUpdate(model, next, cmd)
		case configureScreenMCPNameInput:
			next, cmd := model.updateMCPNameInput(keyMsg)
			return applyConfigureModelUpdate(model, next, cmd)
		case configureScreenMCPDeleteConfirm:
			next, cmd := model.updateMCPDeleteConfirm(keyMsg)
			return applyConfigureModelUpdate(model, next, cmd)
		}
	}
	if model.screen == configureScreenMCPServerList {
		var cmd tea.Cmd
		model.mcpList, cmd = model.mcpList.Update(msg)
		return true, cmd
	}
	return false, nil
}

func (configureMCPScreen) View(model configureTUIModel) string {
	layout := deriveConfigureLayout(model.width, model.height)
	switch model.screen {
	case configureScreenMCPServerList:
		return renderConfigureSplitPanels(layout,
			model.styles.focused.Width(layout.navWidth).Render(model.mcpList.View()),
			model.styles.panel.Width(layout.detailWidth).Render(renderMCPPanel(model)),
		)
	case configureScreenMCPNameInput:
		return model.styles.focused.Width(layout.fullWidth).Render(renderMCPNameInput(model))
	case configureScreenMCPForm, configureScreenForm:
		return renderFormScreen(model)
	case configureScreenMCPDeleteConfirm:
		return model.styles.focused.Width(layout.fullWidth).Render(fmt.Sprintf("Remove MCP server %q?\n\nPress y to delete it, n or esc to keep it.", model.currentMCPServerName))
	default:
		return renderConfigureSplitPanels(layout,
			model.styles.focused.Width(layout.navWidth).Render(model.mcpList.View()),
			model.styles.panel.Width(layout.detailWidth).Render(renderMCPPanel(model)),
		)
	}
}
