package main

import tea "github.com/charmbracelet/bubbletea"

type configureReviewScreen struct{ configureNoopScreen }

func (configureReviewScreen) Update(msg tea.Msg, model *configureTUIModel) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	next, cmd := model.updateReview(keyMsg)
	return applyConfigureModelUpdate(model, next, cmd)
}

func (configureReviewScreen) View(model configureTUIModel) string {
	layout := deriveConfigureLayout(model.width, model.height)
	return model.styles.focused.Width(layout.fullWidth).Render(renderSummaryPanelMode(model.styles, model.cfg, "Review the snapshot below. Press Enter or s to save, esc to go back.", layout.compact))
}
