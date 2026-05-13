package main

import (
	"or3-intern/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

type configureScreenAdapter interface {
	Init(model configureTUIModel) tea.Cmd
	Update(msg tea.Msg, model *configureTUIModel) (handled bool, cmd tea.Cmd)
	View(model configureTUIModel) string
	Save(model *configureTUIModel, cfg *config.Config) error
}

type configureNoopScreen struct{}

func (configureNoopScreen) Init(configureTUIModel) tea.Cmd { return nil }

func (configureNoopScreen) Update(tea.Msg, *configureTUIModel) (bool, tea.Cmd) { return false, nil }

func (configureNoopScreen) View(configureTUIModel) string { return "" }

func (configureNoopScreen) Save(model *configureTUIModel, cfg *config.Config) error {
	if model != nil && cfg != nil {
		*cfg = model.cfg
	}
	return nil
}

type configureSectionsScreen struct{ configureNoopScreen }

type configureFormScreen struct{ configureNoopScreen }

type configureQuitConfirmScreen struct{ configureNoopScreen }

func (m configureTUIModel) screenAdapter() configureScreenAdapter {
	switch m.screen {
	case configureScreenSections:
		return configureSectionsScreen{}
	case configureScreenChannels:
		return configureChannelsScreen{}
	case configureScreenMCPServerList, configureScreenMCPNameInput, configureScreenMCPForm, configureScreenMCPDeleteConfirm:
		return configureMCPScreen{}
	case configureScreenReview:
		return configureReviewScreen{}
	case configureScreenSuccess:
		return configureSuccessScreen{}
	case configureScreenQuitConfirm:
		return configureQuitConfirmScreen{}
	case configureScreenForm:
		return configureFormScreenForSection(m.currentSection)
	default:
		return configureFormScreen{}
	}
}

func configureFormScreenForSection(section string) configureScreenAdapter {
	switch section {
	case "provider":
		return configureProviderScreen{}
	case "workspace":
		return configureWorkspaceScreen{}
	case "channels":
		return configureChannelsScreen{}
	case "mcp":
		return configureMCPScreen{}
	case "context":
		return configureContextScreen{}
	case "hardening", "security", "session":
		return configureSafetyScreen{}
	case "service":
		return configureServiceScreen{}
	case "docindex":
		return configureDocIndexScreen{}
	default:
		return configureFormScreen{}
	}
}

func applyConfigureModelUpdate(model *configureTUIModel, next tea.Model, cmd tea.Cmd) (bool, tea.Cmd) {
	updated, ok := next.(configureTUIModel)
	if !ok {
		return true, cmd
	}
	*model = updated
	return true, cmd
}

func (configureSectionsScreen) Update(msg tea.Msg, model *configureTUIModel) (bool, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		next, cmd := model.updateSectionPicker(keyMsg)
		return applyConfigureModelUpdate(model, next, cmd)
	}
	var cmd tea.Cmd
	model.sectionList, cmd = model.sectionList.Update(msg)
	return true, cmd
}

func (configureSectionsScreen) View(model configureTUIModel) string {
	layout := deriveConfigureLayout(model.width, model.height)
	return renderConfigureSplitPanels(layout,
		model.styles.focused.Width(layout.navWidth).Render(model.sectionList.View()),
		model.styles.panel.Width(layout.detailWidth).Render(renderSummaryPanelMode(model.styles, model.cfg, "Pick a section on the left. Press s to review and save.", layout.compact)),
	)
}

func (configureFormScreen) Update(msg tea.Msg, model *configureTUIModel) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	next, cmd := model.updateSectionForm(keyMsg)
	return applyConfigureModelUpdate(model, next, cmd)
}

func (configureFormScreen) View(model configureTUIModel) string {
	return renderFormScreen(model)
}

func (configureQuitConfirmScreen) Update(msg tea.Msg, model *configureTUIModel) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	next, cmd := model.updateQuitConfirm(keyMsg)
	return applyConfigureModelUpdate(model, next, cmd)
}

func (configureQuitConfirmScreen) View(model configureTUIModel) string {
	layout := deriveConfigureLayout(model.width, model.height)
	return model.styles.focused.Width(layout.fullWidth).Render("You have unsaved changes. Quit anyway?\n\nPress y to discard changes, n or esc to continue editing.")
}
