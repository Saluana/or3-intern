package main

import tea "github.com/charmbracelet/bubbletea"

type configureChannelsScreen struct{ configureNoopScreen }

func (configureChannelsScreen) Update(msg tea.Msg, model *configureTUIModel) (bool, tea.Cmd) {
	if model.screen == configureScreenForm {
		return configureFormScreen{}.Update(msg, model)
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		next, cmd := model.updateChannelPicker(keyMsg)
		return applyConfigureModelUpdate(model, next, cmd)
	}
	var cmd tea.Cmd
	model.channelList, cmd = model.channelList.Update(msg)
	return true, cmd
}

func (configureChannelsScreen) View(model configureTUIModel) string {
	if model.screen == configureScreenForm {
		return renderFormScreen(model)
	}
	layout := deriveConfigureLayout(model.width, model.height)
	return renderConfigureSplitPanels(layout,
		model.styles.focused.Width(layout.navWidth).Render(model.channelList.View()),
		model.styles.panel.Width(layout.detailWidth).Render(renderSummaryPanelMode(model.styles, model.cfg, "Select a channel to edit its toggles, access policy, and defaults.", layout.compact)),
	)
}
