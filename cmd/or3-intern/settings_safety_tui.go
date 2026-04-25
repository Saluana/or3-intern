package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"or3-intern/internal/config"
	"or3-intern/internal/safetymode"
	"or3-intern/internal/uxcopy"
)

type settingsSafetyScreen int

const (
	settingsSafetyChoose settingsSafetyScreen = iota
	settingsSafetyFallback
	settingsSafetySaved
)

type settingsSafetyModel struct {
	cfgPath  string
	cfg      config.Config
	styles   configureStyles
	width    int
	screen   settingsSafetyScreen
	cursor   int
	fallback int
	saved    bool
	err      error
}

func runSettingsSafetyWithTUI(cfgPath string, cfg config.Config) error {
	model := settingsSafetyModel{cfgPath: cfgPath, cfg: cfg, styles: newConfigureStyles()}
	inferred := safetymode.Infer(cfg)
	switch inferred.BaseMode {
	case safetymode.ModeRelaxed:
		model.cursor = 0
	case safetymode.ModeLockedDown:
		model.cursor = 2
	default:
		model.cursor = 1
	}
	p := tea.NewProgram(model, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return err
	}
	finalModel, ok := result.(settingsSafetyModel)
	if !ok {
		return nil
	}
	if finalModel.err != nil {
		return finalModel.err
	}
	return nil
}

func (m settingsSafetyModel) Init() tea.Cmd { return nil }

func (m settingsSafetyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.activeCursor() > 0 {
				m.setActiveCursor(m.activeCursor() - 1)
			}
		case "down", "j":
			max := 2
			if m.activeCursor() < max {
				m.setActiveCursor(m.activeCursor() + 1)
			}
		case "1", "2", "3":
			index := int(msg.String()[0] - '1')
			m.setActiveCursor(index)
			return m.activate()
		case "enter":
			return m.activate()
		}
	}
	return m, nil
}

func (m settingsSafetyModel) activeCursor() int {
	if m.screen == settingsSafetyFallback {
		return m.fallback
	}
	return m.cursor
}

func (m *settingsSafetyModel) setActiveCursor(value int) {
	if m.screen == settingsSafetyFallback {
		m.fallback = value
		return
	}
	m.cursor = value
}

func (m settingsSafetyModel) activate() (tea.Model, tea.Cmd) {
	if m.screen == settingsSafetyFallback {
		switch m.fallback {
		case 1:
			safetymode.Apply(&m.cfg, safetymode.ModeLockedDown)
		case 2:
			safetymode.Apply(&m.cfg, safetymode.ModeBalanced)
		default:
			applyLockedDownNoSandbox(&m.cfg)
		}
		return m.saveAndQuit()
	}
	mode := []safetymode.Mode{safetymode.ModeRelaxed, safetymode.ModeBalanced, safetymode.ModeLockedDown}[m.cursor]
	if mode == safetymode.ModeLockedDown {
		path := strings.TrimSpace(m.cfg.Hardening.Sandbox.BubblewrapPath)
		if path == "" {
			path = config.Default().Hardening.Sandbox.BubblewrapPath
		}
		if !sandboxToolAvailable(path) {
			m.screen = settingsSafetyFallback
			m.fallback = 0
			return m, nil
		}
	}
	safetymode.Apply(&m.cfg, mode)
	return m.saveAndQuit()
}

func (m settingsSafetyModel) saveAndQuit() (tea.Model, tea.Cmd) {
	if err := config.Save(m.cfgPath, m.cfg); err != nil {
		m.err = err
		return m, tea.Quit
	}
	m.saved = true
	m.screen = settingsSafetySaved
	return m, tea.Quit
}

func (m settingsSafetyModel) View() string {
	styles := m.styles
	width := m.width
	if width <= 0 {
		width = 96
	}
	contentWidth := maxInt(52, width-10)
	var b strings.Builder
	fmt.Fprintln(&b, styles.title.Render("Safety"))
	fmt.Fprintln(&b, styles.subtitle.Render("Choose how careful OR3 should be with commands, files, and network access."))
	fmt.Fprintln(&b)
	if m.screen == settingsSafetyFallback {
		fmt.Fprintln(&b, styles.badgeWarn.Render("Sandbox unavailable")+" Locked Down works best with command isolation.")
		fmt.Fprintln(&b, "This system does not appear to have the required sandbox tool.")
		fmt.Fprintln(&b)
		options := []string{
			"Block local commands instead",
			"Use sandboxing anyway",
			"Choose Balanced instead",
		}
		for index, option := range options {
			line := fmt.Sprintf("%d. %s", index+1, option)
			if index == m.fallback {
				line = styles.highlight.Render("> " + line)
			} else {
				line = "  " + line
			}
			fmt.Fprintln(&b, line)
		}
	} else {
		options := []struct {
			label string
			mode  safetymode.Mode
		}{
			{"Relaxed", safetymode.ModeRelaxed},
			{"Balanced", safetymode.ModeBalanced},
			{"Locked Down", safetymode.ModeLockedDown},
		}
		for index, option := range options {
			line := fmt.Sprintf("%d. %s", index+1, option.label)
			if index == m.cursor {
				line = styles.highlight.Render("> " + line)
			} else {
				line = "  " + line
			}
			fmt.Fprintln(&b, line)
			if index == m.cursor {
				fmt.Fprintln(&b, "   "+styles.muted.Render(uxcopy.SafetyModeSummary(option.mode)))
			}
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, styles.help.Render("↑/↓ or number keys move • enter saves • esc/q cancels"))
	return styles.app.Width(contentWidth).Render(styles.focused.Width(maxInt(40, contentWidth-8)).Render(strings.TrimRight(b.String(), "\n")))
}
