package main

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type configureSuccessScreen struct{ configureNoopScreen }

func (configureSuccessScreen) Update(msg tea.Msg, model *configureTUIModel) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if key.Matches(keyMsg, model.keys.Select, model.keys.Quit, model.keys.Back) {
		return true, tea.Quit
	}
	return true, nil
}

func (configureSuccessScreen) View(model configureTUIModel) string {
	layout := deriveConfigureLayout(model.width, model.height)
	return model.styles.focused.Width(layout.fullWidth).Render(model.styles.success.Render(model.successMessage) + "\n\n" + renderSummaryPanelMode(model.styles, model.cfg, renderNextStepsText(model.cfg), layout.compact))
}
