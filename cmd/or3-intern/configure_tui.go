package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"or3-intern/internal/config"
)

type configureTUIOptions struct {
	Title           string
	Intro           []string
	Restricted      []string
	InitialSection  string
	CompletionAlias string
}

type configureScreen int

const (
	configureScreenSections configureScreen = iota
	configureScreenForm
	configureScreenChannels
	configureScreenReview
	configureScreenSuccess
	configureScreenQuitConfirm
)

type configureFieldKind int

const (
	configureFieldText configureFieldKind = iota
	configureFieldSecret
	configureFieldToggle
	configureFieldChoice
)

type configureField struct {
	Key         string
	Label       string
	Description string
	Kind        configureFieldKind
	Value       string
	Choices     []string
	ChoiceIndex int
	SecretHint  string
	EmptyHint   string
}

type configureListItem struct {
	key         string
	title       string
	description string
}

func (i configureListItem) FilterValue() string { return i.title + " " + i.description + " " + i.key }
func (i configureListItem) Title() string       { return i.title }
func (i configureListItem) Description() string { return i.description }

type configureKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Select key.Binding
	Back   key.Binding
	Save   key.Binding
	Quit   key.Binding
	Toggle key.Binding
	Next   key.Binding
	Prev   key.Binding
}

func newConfigureKeyMap() configureKeyMap {
	return configureKeyMap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "previous")),
		Right:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next")),
		Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select/edit")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Save:   key.NewBinding(key.WithKeys("s", "ctrl+s"), key.WithHelp("s", "review/save")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		Next:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		Prev:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous")),
	}
}

func (k configureKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Toggle, k.Save, k.Back, k.Quit}
}

func (k configureKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Left, k.Right}, {k.Select, k.Toggle, k.Next, k.Prev}, {k.Save, k.Back, k.Quit}}
}

type configureStyles struct {
	app         lipgloss.Style
	title       lipgloss.Style
	subtitle    lipgloss.Style
	panel       lipgloss.Style
	focused     lipgloss.Style
	label       lipgloss.Style
	value       lipgloss.Style
	muted       lipgloss.Style
	badgeOn     lipgloss.Style
	badgeOff    lipgloss.Style
	badgeWarn   lipgloss.Style
	section     lipgloss.Style
	error       lipgloss.Style
	success     lipgloss.Style
	help        lipgloss.Style
	highlight   lipgloss.Style
	placeholder lipgloss.Style
	button      lipgloss.Style
	buttonAlt   lipgloss.Style
}

func newConfigureStyles() configureStyles {
	baseBorder := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1, 2)
	return configureStyles{
		app:         lipgloss.NewStyle().Padding(1, 2),
		title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")),
		subtitle:    lipgloss.NewStyle().Foreground(lipgloss.Color("110")),
		panel:       baseBorder,
		focused:     baseBorder.Copy().BorderForeground(lipgloss.Color("212")),
		label:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")),
		value:       lipgloss.NewStyle().Foreground(lipgloss.Color("255")),
		muted:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		badgeOn:     lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("42")).Padding(0, 1),
		badgeOff:    lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("238")).Padding(0, 1),
		badgeWarn:   lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("220")).Padding(0, 1),
		section:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117")),
		error:       lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
		success:     lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true),
		help:        lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		highlight:   lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true),
		placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true),
		button:      lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("212")).Padding(0, 1).Bold(true),
		buttonAlt:   lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("63")).Padding(0, 1),
	}
}

type configureTUIModel struct {
	options         configureTUIOptions
	styles          configureStyles
	keys            configureKeyMap
	help            help.Model
	sectionList     list.Model
	channelList     list.Model
	textInput       textinput.Model
	width           int
	height          int
	screen          configureScreen
	cfgPath         string
	cwd             string
	cfg             config.Config
	original        config.Config
	existed         bool
	loadWarning     string
	currentSection  string
	currentChannel  string
	fieldCursor     int
	formScroll      int
	editingFieldKey string
	editing         bool
	dirty           bool
	errorMessage    string
	successMessage  string
	quitting        bool
	lastSection     string
}

func runConfigureWithTUI(cfgPath, cwd string, args []string, options configureTUIOptions) error {
	parsed, err := parseConfigureArgs(args)
	if err != nil {
		return err
	}
	if len(parsed.Sections) > 0 {
		options.Restricted = append([]string{}, parsed.Sections...)
	}
	cfg, existed, loadWarning, err := loadConfigureConfig(cfgPath, cwd)
	if err != nil {
		return err
	}
	model := newConfigureTUIModel(cfgPath, cwd, cfg, existed, loadWarning, options)
	p := tea.NewProgram(model, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return err
	}
	finalModel, ok := result.(configureTUIModel)
	if !ok {
		return nil
	}
	if finalModel.errorMessage != "" {
		return errors.New(finalModel.errorMessage)
	}
	return nil
}

func newConfigureTUIModel(cfgPath, cwd string, cfg config.Config, existed bool, loadWarning string, options configureTUIOptions) configureTUIModel {
	keys := newConfigureKeyMap()
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true
	delegate.SetHeight(2)
	items := buildConfigureSectionItems(cfg, options.Restricted)
	sectionList := list.New(items, delegate, 36, 16)
	sectionList.Title = "Sections"
	sectionList.SetShowStatusBar(false)
	sectionList.SetFilteringEnabled(false)
	sectionList.SetShowHelp(false)
	sectionList.SetShowPagination(false)

	channelList := list.New(buildChannelItems(cfg), delegate, 36, 16)
	channelList.Title = "Channels"
	channelList.SetShowStatusBar(false)
	channelList.SetFilteringEnabled(false)
	channelList.SetShowHelp(false)
	channelList.SetShowPagination(false)

	input := textinput.New()
	input.Prompt = "» "
	input.CharLimit = 512
	input.Width = 48

	model := configureTUIModel{
		options:     options,
		styles:      newConfigureStyles(),
		keys:        keys,
		help:        help.New(),
		sectionList: sectionList,
		channelList: channelList,
		textInput:   input,
		screen:      configureScreenSections,
		cfgPath:     cfgPath,
		cwd:         cwd,
		cfg:         cfg,
		original:    cfg,
		existed:     existed,
		loadWarning: loadWarning,
	}
	if options.Title == "" {
		model.options.Title = "or3-intern configure"
	}
	if len(model.options.Restricted) == 1 {
		model.currentSection = model.options.Restricted[0]
		model.screen = configureScreenForm
	}
	return model
}

func (m configureTUIModel) Init() tea.Cmd { return textinput.Blink }

func (m configureTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.sectionList.SetSize(maxInt(28, msg.Width/3), maxInt(10, msg.Height-10))
		m.channelList.SetSize(maxInt(28, msg.Width/3), maxInt(10, msg.Height-10))
		m.textInput.Width = maxInt(24, msg.Width/2)
		m.ensureFieldCursorVisible(len(m.activeFields()))
		return m, nil
	case tea.KeyMsg:
		if m.editing {
			return m.updateWhileEditing(msg)
		}
		if key.Matches(msg, m.keys.Quit) {
			if m.dirty {
				m.screen = configureScreenQuitConfirm
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Save) {
			m.screen = configureScreenReview
			return m, nil
		}
		switch m.screen {
		case configureScreenSections:
			return m.updateSectionPicker(msg)
		case configureScreenForm:
			return m.updateSectionForm(msg)
		case configureScreenChannels:
			return m.updateChannelPicker(msg)
		case configureScreenReview:
			return m.updateReview(msg)
		case configureScreenSuccess:
			if key.Matches(msg, m.keys.Select, m.keys.Quit, m.keys.Back) {
				return m, tea.Quit
			}
		case configureScreenQuitConfirm:
			return m.updateQuitConfirm(msg)
		}
	}

	var cmd tea.Cmd
	if m.screen == configureScreenSections {
		m.sectionList, cmd = m.sectionList.Update(msg)
		return m, cmd
	}
	if m.screen == configureScreenChannels {
		m.channelList, cmd = m.channelList.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m configureTUIModel) updateWhileEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.editing = false
		m.editingFieldKey = ""
		m.textInput.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Select):
		m.applyEditedValue(strings.TrimSpace(m.textInput.Value()))
		m.editing = false
		m.editingFieldKey = ""
		m.textInput.Blur()
		m.refreshLists()
		return m, nil
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m configureTUIModel) updateSectionPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Select) {
		if item, ok := m.sectionList.SelectedItem().(configureListItem); ok {
			m.currentSection = item.key
			if item.key == "channels" {
				m.screen = configureScreenChannels
				m.channelList.Select(0)
			} else {
				m.fieldCursor = 0
				m.formScroll = 0
				m.screen = configureScreenForm
			}
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.sectionList, cmd = m.sectionList.Update(msg)
	return m, cmd
}

func (m configureTUIModel) updateChannelPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		m.screen = configureScreenSections
		return m, nil
	}
	if key.Matches(msg, m.keys.Select) {
		if item, ok := m.channelList.SelectedItem().(configureListItem); ok {
			m.currentChannel = item.key
			m.currentSection = "channels"
			m.fieldCursor = 0
			m.formScroll = 0
			m.screen = configureScreenForm
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.channelList, cmd = m.channelList.Update(msg)
	return m, cmd
}

func (m configureTUIModel) updateSectionForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := m.activeFields()
	if len(fields) == 0 {
		m.screen = configureScreenSections
		return m, nil
	}
	if key.Matches(msg, m.keys.Back) {
		if m.currentSection == "channels" {
			m.screen = configureScreenChannels
			return m, nil
		}
		m.screen = configureScreenSections
		return m, nil
	}
	if key.Matches(msg, m.keys.Up, m.keys.Prev) {
		m.fieldCursor = maxInt(0, m.fieldCursor-1)
		m.ensureFieldCursorVisible(len(fields))
		return m, nil
	}
	if key.Matches(msg, m.keys.Down, m.keys.Next) {
		m.fieldCursor = minInt(m.fieldCursor+1, len(fields)-1)
		m.ensureFieldCursorVisible(len(fields))
		return m, nil
	}
	field := fields[m.fieldCursor]
	if field.Kind == configureFieldToggle && key.Matches(msg, m.keys.Toggle, m.keys.Select) {
		m.toggleField(field.Key)
		m.refreshLists()
		return m, nil
	}
	if field.Kind == configureFieldChoice {
		switch {
		case key.Matches(msg, m.keys.Left):
			m.cycleChoice(field.Key, -1)
			m.refreshLists()
			return m, nil
		case key.Matches(msg, m.keys.Right, m.keys.Select, m.keys.Toggle):
			m.cycleChoice(field.Key, 1)
			m.refreshLists()
			return m, nil
		}
	}
	if (field.Kind == configureFieldText || field.Kind == configureFieldSecret) && key.Matches(msg, m.keys.Select) {
		m.startEditingField(field)
		return m, nil
	}
	return m, nil
}

func (m configureTUIModel) updateReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		if m.lastSection == "channels" {
			m.screen = configureScreenChannels
			return m, nil
		}
		if m.currentSection != "" {
			m.screen = configureScreenForm
			return m, nil
		}
		m.screen = configureScreenSections
		return m, nil
	}
	if key.Matches(msg, m.keys.Select, m.keys.Save) {
		if err := config.Save(m.cfgPath, m.cfg); err != nil {
			m.errorMessage = err.Error()
			return m, nil
		}
		m.original = m.cfg
		m.dirty = false
		m.successMessage = "Configuration saved successfully."
		m.screen = configureScreenSuccess
		return m, nil
	}
	return m, nil
}

func (m configureTUIModel) updateQuitConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.quitting = true
		return m, tea.Quit
	case "n", "N", "esc":
		m.screen = configureScreenSections
	}
	return m, nil
}

func (m *configureTUIModel) refreshLists() {
	m.sectionList.SetItems(buildConfigureSectionItems(m.cfg, m.options.Restricted))
	m.channelList.SetItems(buildChannelItems(m.cfg))
	m.ensureFieldCursorVisible(len(m.activeFields()))
	if m.sectionList.Index() >= len(m.sectionList.Items()) && len(m.sectionList.Items()) > 0 {
		m.sectionList.Select(len(m.sectionList.Items()) - 1)
	}
	if m.channelList.Index() >= len(m.channelList.Items()) && len(m.channelList.Items()) > 0 {
		m.channelList.Select(len(m.channelList.Items()) - 1)
	}
}

func (m *configureTUIModel) ensureFieldCursorVisible(total int) {
	if total <= 0 {
		m.fieldCursor = 0
		m.formScroll = 0
		return
	}
	if m.fieldCursor < 0 {
		m.fieldCursor = 0
	}
	if m.fieldCursor >= total {
		m.fieldCursor = total - 1
	}
	visible := m.visibleFormFieldCount()
	maxScroll := maxInt(0, total-visible)
	if m.formScroll > maxScroll {
		m.formScroll = maxScroll
	}
	if m.fieldCursor < m.formScroll {
		m.formScroll = m.fieldCursor
	}
	if m.fieldCursor >= m.formScroll+visible {
		m.formScroll = m.fieldCursor - visible + 1
	}
	if m.formScroll < 0 {
		m.formScroll = 0
	}
}

func (m configureTUIModel) visibleFormFieldCount() int {
	if m.height <= 0 {
		return 6
	}
	count := (m.height - 12) / 4
	if count < 2 {
		return 2
	}
	return count
}

func (m *configureTUIModel) startEditingField(field configureField) {
	m.editingFieldKey = field.Key
	m.editing = true
	m.textInput.Reset()
	m.textInput.Focus()
	m.textInput.Prompt = "» "
	if field.Kind == configureFieldSecret {
		m.textInput.EchoMode = textinput.EchoPassword
		m.textInput.Placeholder = field.SecretHint
		m.textInput.SetValue("")
	} else {
		m.textInput.EchoMode = textinput.EchoNormal
		m.textInput.Placeholder = field.EmptyHint
		m.textInput.SetValue(field.Value)
	}
	m.textInput.CursorEnd()
}

func (m *configureTUIModel) applyEditedValue(value string) {
	if applyFieldValue(&m.cfg, m.currentSection, m.currentChannel, m.editingFieldKey, value) {
		m.dirty = true
		m.lastSection = m.currentSection
	}
	if m.currentSection == "channels" {
		m.lastSection = "channels"
	}
	if m.editingFieldKey == "provider_preset" {
		m.lastSection = "provider"
	}
	if m.currentSection == "channels" && strings.TrimSpace(m.currentChannel) != "" {
		m.lastSection = "channels"
	}
	if m.currentSection != "" {
		m.lastSection = m.currentSection
	}
}

func (m *configureTUIModel) toggleField(fieldKey string) {
	if toggleFieldValue(&m.cfg, m.currentSection, m.currentChannel, fieldKey) {
		m.dirty = true
		m.lastSection = m.currentSection
	}
}

func (m *configureTUIModel) cycleChoice(fieldKey string, delta int) {
	if cycleChoiceValue(&m.cfg, m.currentSection, m.currentChannel, fieldKey, delta) {
		m.dirty = true
		m.lastSection = m.currentSection
	}
}

func (m configureTUIModel) View() string {
	if m.quitting {
		return ""
	}
	header := m.styles.title.Render(m.options.Title)
	if len(m.options.Intro) == 0 {
		if m.existed {
			header += "\n" + m.styles.subtitle.Render("Loaded existing config. Use arrows, Enter, and Space to edit.")
		} else {
			header += "\n" + m.styles.subtitle.Render("Modern setup flow for local configuration.")
		}
	} else {
		header += "\n" + m.styles.subtitle.Render(strings.Join(m.options.Intro, " "))
	}
	if m.loadWarning != "" {
		header += "\n" + m.styles.badgeWarn.Render("repair mode") + " " + m.styles.muted.Render(m.loadWarning)
	}
	if m.errorMessage != "" {
		header += "\n" + m.styles.error.Render(m.errorMessage)
	}

	var body string
	switch m.screen {
	case configureScreenSections:
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.styles.focused.Width(maxInt(36, m.width/3)).Render(m.sectionList.View()),
			m.styles.panel.Width(maxInt(36, m.width-maxInt(42, m.width/3)-8)).Render(renderSummaryPanel(m.styles, m.cfg, "Pick a section on the left. Press s to review and save.")),
		)
	case configureScreenChannels:
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.styles.focused.Width(maxInt(36, m.width/3)).Render(m.channelList.View()),
			m.styles.panel.Width(maxInt(36, m.width-maxInt(42, m.width/3)-8)).Render(renderSummaryPanel(m.styles, m.cfg, "Select a channel to edit its toggles, access policy, and defaults.")),
		)
	case configureScreenForm:
		body = renderFormScreen(m)
	case configureScreenReview:
		body = m.styles.focused.Render(renderSummaryPanel(m.styles, m.cfg, "Review the snapshot below. Press Enter or s to save, esc to go back."))
	case configureScreenSuccess:
		body = m.styles.focused.Render(m.styles.success.Render(m.successMessage) + "\n\n" + renderSummaryPanel(m.styles, m.cfg, renderNextStepsText(m.cfg)))
	case configureScreenQuitConfirm:
		body = m.styles.focused.Render("You have unsaved changes. Quit anyway?\n\nPress y to discard changes, n or esc to continue editing.")
	}

	footer := m.styles.help.Render(m.help.View(m.keys))
	return m.styles.app.Render(header + "\n\n" + body + "\n\n" + footer)
}

func renderFormScreen(m configureTUIModel) string {
	fields := m.activeFields()
	if len(fields) == 0 {
		return m.styles.panel.Render("No editable fields for this screen.")
	}
	visibleCount := m.visibleFormFieldCount()
	start := clampInt(m.formScroll, 0, maxInt(0, len(fields)-1))
	if start > maxInt(0, len(fields)-visibleCount) {
		start = maxInt(0, len(fields)-visibleCount)
	}
	end := minInt(len(fields), start+visibleCount)
	visibleFields := fields[start:end]
	rows := make([]string, 0, len(visibleFields)+2)
	rows = append(rows, m.styles.section.Render(fmt.Sprintf("Fields %d-%d of %d", start+1, end, len(fields))))
	for offset, field := range visibleFields {
		i := start + offset
		selected := i == m.fieldCursor
		style := lipgloss.NewStyle().Padding(0, 1).BorderLeft(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238"))
		labelStyle := m.styles.label
		if selected && !m.editing {
			style = style.BorderForeground(lipgloss.Color("212")).Background(lipgloss.Color("236"))
			labelStyle = m.styles.highlight
		}
		value := field.Value
		if value == "" {
			value = m.styles.placeholder.Render(field.EmptyHint)
		}
		if field.Kind == configureFieldToggle {
			if field.Value == "on" {
				value = m.styles.badgeOn.Render(" ON ")
			} else {
				value = m.styles.badgeOff.Render(" OFF ")
			}
		}
		if field.Kind == configureFieldChoice && len(field.Choices) > 0 {
			value = m.styles.buttonAlt.Render(field.Value)
		}
		indicator := "  "
		if selected {
			indicator = m.styles.highlight.Render("▶ ")
		}
		rows = append(rows, style.Render(indicator+labelStyle.Render(field.Label)+"\n"+m.styles.value.Render(value)+"\n"+m.styles.muted.Render(field.Description)))
	}
	if start > 0 {
		rows = append([]string{m.styles.muted.Render("↑ more above")}, rows...)
	}
	if end < len(fields) {
		rows = append(rows, m.styles.muted.Render("↓ more below"))
	}
	left := m.styles.panel.Width(maxInt(40, m.width/2)).Render(strings.Join(rows, "\n\n"))
	rightContent := renderSummaryPanel(m.styles, m.cfg, formContextHint(m.currentSection, m.currentChannel, m.fieldCursor, len(fields)))
	if m.editing {
		rightContent += "\n\n" + m.styles.section.Render("Editing") + "\n" + m.textInput.View() + "\n" + m.styles.muted.Render("Enter to apply • esc to cancel")
	} else {
		rightContent += "\n\n" + m.styles.section.Render("Selected field") + "\n" + m.styles.highlight.Render(fields[m.fieldCursor].Label)
	}
	right := m.styles.panel.Width(maxInt(34, m.width-maxInt(46, m.width/2)-8)).Render(rightContent)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func formContextHint(section, channel string, cursor, total int) string {
	position := fmt.Sprintf("Field %d/%d. Use ↑/↓ or Tab/Shift+Tab to move between fields.", cursor+1, total)
	if section == "channels" && channel != "" {
		return position + " Editing the " + strings.Title(channel) + " channel. Use space for toggles, ←/→ for access presets, and Enter to edit text fields."
	}
	if section != "" {
		return position + " Editing the " + strings.Title(section) + " section. Press s to review and save when you’re happy with the current snapshot."
	}
	return position + " Use arrows to move around and Enter to edit the highlighted field."
}

func renderSummaryPanel(styles configureStyles, cfg config.Config, hint string) string {
	lines := []string{
		styles.section.Render("Current snapshot"),
		fmt.Sprintf("%s %s", styles.label.Render("Provider:"), styles.value.Render(configureProviderLabel(cfg.Provider.APIBase)+" · "+cfg.Provider.Model)),
		fmt.Sprintf("%s %s", styles.label.Render("Storage:"), styles.value.Render(cfg.DBPath+" · "+cfg.ArtifactsDir)),
		fmt.Sprintf("%s %s", styles.label.Render("Workspace:"), styles.value.Render(fmt.Sprintf("restrict=%t · %s", cfg.Tools.RestrictToWorkspace, emptyAsNone(cfg.WorkspaceDir)))),
		fmt.Sprintf("%s %s", styles.label.Render("Web:"), styles.value.Render(fmt.Sprintf("Brave=%t · proxy=%s", strings.TrimSpace(cfg.Tools.BraveAPIKey) != "", emptyAsNone(cfg.Tools.WebProxy)))),
		fmt.Sprintf("%s %s", styles.label.Render("Channels:"), styles.value.Render(strings.Join(enabledChannelNames(cfg), ", "))),
		fmt.Sprintf("%s %s", styles.label.Render("Service:"), styles.value.Render(serviceSummary(cfg))),
	}
	if len(enabledChannelNames(cfg)) == 0 {
		lines[5] = fmt.Sprintf("%s %s", styles.label.Render("Channels:"), styles.muted.Render("none enabled"))
	}
	if hint != "" {
		lines = append(lines, "", styles.section.Render("Hint"), styles.muted.Render(hint))
	}
	return strings.Join(lines, "\n")
}

func renderNextStepsText(cfg config.Config) string {
	b := &strings.Builder{}
	_ = printConfigureNextSteps(b, cfg)
	return strings.TrimSpace(b.String())
}

func buildConfigureSectionItems(cfg config.Config, restricted []string) []list.Item {
	keys := restricted
	if len(keys) == 0 {
		keys = make([]string, 0, len(configureSections))
		for _, section := range configureSections {
			keys = append(keys, section.Key)
		}
	}
	items := make([]list.Item, 0, len(keys))
	for _, key := range keys {
		meta := configureSectionMeta(key)
		items = append(items, configureListItem{key: key, title: meta.Label, description: sectionStatus(cfg, key)})
	}
	return items
}

func buildChannelItems(cfg config.Config) []list.Item {
	channels := []struct {
		Key         string
		Title       string
		Enabled     bool
		Description string
	}{
		{Key: "telegram", Title: "Telegram", Enabled: cfg.Channels.Telegram.Enabled, Description: channelStatus(cfg.Channels.Telegram.Enabled, cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, len(cfg.Channels.Telegram.AllowedChatIDs) > 0)},
		{Key: "slack", Title: "Slack", Enabled: cfg.Channels.Slack.Enabled, Description: channelStatus(cfg.Channels.Slack.Enabled, cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, len(cfg.Channels.Slack.AllowedUserIDs) > 0)},
		{Key: "discord", Title: "Discord", Enabled: cfg.Channels.Discord.Enabled, Description: channelStatus(cfg.Channels.Discord.Enabled, cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, len(cfg.Channels.Discord.AllowedUserIDs) > 0)},
		{Key: "whatsapp", Title: "WhatsApp", Enabled: cfg.Channels.WhatsApp.Enabled, Description: channelStatus(cfg.Channels.WhatsApp.Enabled, cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, len(cfg.Channels.WhatsApp.AllowedFrom) > 0)},
		{Key: "email", Title: "Email", Enabled: cfg.Channels.Email.Enabled, Description: channelStatus(cfg.Channels.Email.Enabled, cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, len(cfg.Channels.Email.AllowedSenders) > 0)},
	}
	items := make([]list.Item, 0, len(channels))
	for _, channel := range channels {
		items = append(items, configureListItem{key: channel.Key, title: channel.Title, description: channel.Description})
	}
	return items
}

func channelStatus(enabled bool, policy config.InboundPolicy, openAccess, hasAllowlist bool) string {
	if !enabled {
		return "disabled"
	}
	return "enabled · " + channelAccessSummary(policy, openAccess, hasAllowlist)
}

func sectionStatus(cfg config.Config, section string) string {
	switch section {
	case "provider":
		return configureProviderLabel(cfg.Provider.APIBase) + " · " + cfg.Provider.Model
	case "storage":
		return emptyAsNone(cfg.DBPath) + " · " + emptyAsNone(cfg.ArtifactsDir)
	case "workspace":
		return fmt.Sprintf("restrict=%t · %s", cfg.Tools.RestrictToWorkspace, emptyAsNone(cfg.WorkspaceDir))
	case "web":
		return fmt.Sprintf("Brave=%t · proxy=%s", strings.TrimSpace(cfg.Tools.BraveAPIKey) != "", emptyAsNone(cfg.Tools.WebProxy))
	case "channels":
		if len(enabledChannelNames(cfg)) == 0 {
			return "no channels enabled"
		}
		return strings.Join(enabledChannelNames(cfg), ", ")
	case "service":
		return serviceSummary(cfg)
	default:
		return ""
	}
}

func serviceSummary(cfg config.Config) string {
	if !cfg.Service.Enabled {
		return "disabled"
	}
	return "enabled · " + cfg.Service.Listen
}

func (m configureTUIModel) activeFields() []configureField {
	if m.currentSection == "channels" {
		return buildChannelFields(m.cfg, m.currentChannel)
	}
	return buildSectionFields(m.cfg, m.currentSection, m.cwd)
}

func buildSectionFields(cfg config.Config, section, cwd string) []configureField {
	switch section {
	case "provider":
		preset := providerPresetLabel(cfg.Provider.APIBase)
		return []configureField{
			{Key: "provider_preset", Label: "Provider preset", Description: "Cycle through the built-in presets.", Kind: configureFieldChoice, Value: preset, Choices: []string{"OpenAI", "OpenRouter", "Custom"}, ChoiceIndex: indexOfChoice([]string{"OpenAI", "OpenRouter", "Custom"}, preset)},
			{Key: "provider_api_base", Label: "API base", Description: "OpenAI-compatible base URL.", Kind: configureFieldText, Value: cfg.Provider.APIBase, EmptyHint: "https://api.openai.com/v1"},
			{Key: "provider_model", Label: "Chat model", Description: "Default chat model for turns.", Kind: configureFieldText, Value: cfg.Provider.Model, EmptyHint: "gpt-4.1-mini"},
			{Key: "provider_embed", Label: "Embedding model", Description: "Model used for embeddings and retrieval.", Kind: configureFieldText, Value: cfg.Provider.EmbedModel, EmptyHint: "text-embedding-3-small"},
			{Key: "provider_api_key", Label: "API key", Description: "Hidden secret. Leave blank while editing to keep the current value.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Provider.APIKey), SecretHint: "leave blank to keep current", EmptyHint: "not configured"},
		}
	case "storage":
		return []configureField{{Key: "storage_db", Label: "SQLite DB path", Description: "Main runtime database.", Kind: configureFieldText, Value: cfg.DBPath, EmptyHint: ".or3/or3-intern.sqlite"}, {Key: "storage_artifacts", Label: "Artifacts directory", Description: "Large output spillover and artifacts.", Kind: configureFieldText, Value: cfg.ArtifactsDir, EmptyHint: ".or3/artifacts"}}
	case "workspace":
		workspace := cfg.WorkspaceDir
		if strings.TrimSpace(workspace) == "" {
			workspace = cwd
		}
		return []configureField{{Key: "workspace_restrict", Label: "Restrict file tools", Description: "Keep file tools inside the selected workspace.", Kind: configureFieldToggle, Value: onOff(cfg.Tools.RestrictToWorkspace)}, {Key: "workspace_dir", Label: "Workspace directory", Description: "Project root for workspace-restricted file tools.", Kind: configureFieldText, Value: workspace, EmptyHint: cwd}}
	case "web":
		return []configureField{{Key: "web_brave", Label: "Brave Search key", Description: "Hidden secret used for Brave-backed web search.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Tools.BraveAPIKey), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "web_proxy", Label: "Web proxy", Description: "Optional outbound proxy URL.", Kind: configureFieldText, Value: cfg.Tools.WebProxy, EmptyHint: "http://proxy.internal:8080"}}
	case "service":
		return []configureField{{Key: "service_enabled", Label: "Enable service API", Description: "Expose the internal authenticated HTTP API.", Kind: configureFieldToggle, Value: onOff(cfg.Service.Enabled)}, {Key: "service_listen", Label: "Listen address", Description: "Bind address for the internal service.", Kind: configureFieldText, Value: cfg.Service.Listen, EmptyHint: "127.0.0.1:9100"}, {Key: "service_secret", Label: "Shared secret", Description: "Hidden secret required by service clients.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Service.Secret), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}}
	}
	return nil
}

func buildChannelFields(cfg config.Config, channel string) []configureField {
	accessChoices := []string{"pairing", "allowlist", "open", "deny"}
	switch channel {
	case "telegram":
		choice := channelAccessSummary(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, len(cfg.Channels.Telegram.AllowedChatIDs) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Telegram", Description: "Toggle the Telegram channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Telegram.Enabled)}, {Key: "token", Label: "Bot token", Description: "Hidden Telegram bot token.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Telegram.Token), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "default_id", Label: "Default chat ID", Description: "Used by outbound send defaults.", Kind: configureFieldText, Value: cfg.Channels.Telegram.DefaultChatID, EmptyHint: "123456789"}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound Telegram messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed chat IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(allowlistPromptDefault(cfg.Channels.Telegram.AllowedChatIDs, cfg.Channels.Telegram.DefaultChatID), ","), EmptyHint: "12345,67890"}}
	case "slack":
		choice := channelAccessSummary(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, len(cfg.Channels.Slack.AllowedUserIDs) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Slack", Description: "Toggle the Slack channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Slack.Enabled)}, {Key: "app_token", Label: "App token", Description: "Hidden Slack app token.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Slack.AppToken), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "bot_token", Label: "Bot token", Description: "Hidden Slack bot token.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Slack.BotToken), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "default_id", Label: "Default channel ID", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.Slack.DefaultChannelID, EmptyHint: "C123456"}, {Key: "require_mention", Label: "Require mention", Description: "Only react when the bot is mentioned.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Slack.RequireMention)}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound Slack messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed user IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.Slack.AllowedUserIDs, ","), EmptyHint: "U123,U456"}}
	case "discord":
		choice := channelAccessSummary(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, len(cfg.Channels.Discord.AllowedUserIDs) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Discord", Description: "Toggle the Discord channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Discord.Enabled)}, {Key: "token", Label: "Bot token", Description: "Hidden Discord bot token.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Discord.Token), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "default_id", Label: "Default channel ID", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.Discord.DefaultChannelID, EmptyHint: "123456"}, {Key: "require_mention", Label: "Require mention", Description: "Only react when the bot is mentioned.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Discord.RequireMention)}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound Discord messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed user IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.Discord.AllowedUserIDs, ","), EmptyHint: "123,456"}}
	case "whatsapp":
		choice := channelAccessSummary(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, len(cfg.Channels.WhatsApp.AllowedFrom) > 0)
		return []configureField{{Key: "enabled", Label: "Enable WhatsApp", Description: "Toggle the WhatsApp bridge.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.WhatsApp.Enabled)}, {Key: "bridge_url", Label: "Bridge URL", Description: "WebSocket bridge endpoint.", Kind: configureFieldText, Value: cfg.Channels.WhatsApp.BridgeURL, EmptyHint: "ws://127.0.0.1:3001/ws"}, {Key: "bridge_token", Label: "Bridge token", Description: "Hidden bridge token.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.WhatsApp.BridgeToken), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "default_to", Label: "Default recipient", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.WhatsApp.DefaultTo, EmptyHint: "+15555555555"}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound WhatsApp messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed sender IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.WhatsApp.AllowedFrom, ","), EmptyHint: "+1555,+1666"}}
	case "email":
		choice := channelAccessSummary(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, len(cfg.Channels.Email.AllowedSenders) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Email", Description: "Toggle the email channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Email.Enabled)}, {Key: "consent", Label: "Consent granted", Description: "Confirm consent for operating email automation.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Email.ConsentGranted)}, {Key: "imap_host", Label: "IMAP host", Description: "Inbound IMAP server.", Kind: configureFieldText, Value: cfg.Channels.Email.IMAPHost, EmptyHint: "imap.example.com"}, {Key: "imap_user", Label: "IMAP username", Description: "Inbound mailbox username.", Kind: configureFieldText, Value: cfg.Channels.Email.IMAPUsername, EmptyHint: "inbox@example.com"}, {Key: "imap_password", Label: "IMAP password", Description: "Hidden IMAP password.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Email.IMAPPassword), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "smtp_host", Label: "SMTP host", Description: "Outbound SMTP server.", Kind: configureFieldText, Value: cfg.Channels.Email.SMTPHost, EmptyHint: "smtp.example.com"}, {Key: "smtp_user", Label: "SMTP username", Description: "Outbound SMTP username.", Kind: configureFieldText, Value: cfg.Channels.Email.SMTPUsername, EmptyHint: "sender@example.com"}, {Key: "smtp_password", Label: "SMTP password", Description: "Hidden SMTP password.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Email.SMTPPassword), SecretHint: "leave blank to keep current", EmptyHint: "not configured"}, {Key: "from_address", Label: "From address", Description: "Outbound sender address.", Kind: configureFieldText, Value: cfg.Channels.Email.FromAddress, EmptyHint: "bot@example.com"}, {Key: "default_to", Label: "Default recipient", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.Email.DefaultTo, EmptyHint: "ops@example.com"}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound email is admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed senders", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.Email.AllowedSenders, ","), EmptyHint: "owner@example.com"}}
	}
	return nil
}

func toggleFieldValue(cfg *config.Config, section, channel, fieldKey string) bool {
	if cfg == nil {
		return false
	}
	if section == "channels" {
		switch channel {
		case "telegram":
			if fieldKey == "enabled" {
				cfg.Channels.Telegram.Enabled = !cfg.Channels.Telegram.Enabled
				return true
			}
		case "slack":
			if fieldKey == "enabled" {
				cfg.Channels.Slack.Enabled = !cfg.Channels.Slack.Enabled
				return true
			}
			if fieldKey == "require_mention" {
				cfg.Channels.Slack.RequireMention = !cfg.Channels.Slack.RequireMention
				return true
			}
		case "discord":
			if fieldKey == "enabled" {
				cfg.Channels.Discord.Enabled = !cfg.Channels.Discord.Enabled
				return true
			}
			if fieldKey == "require_mention" {
				cfg.Channels.Discord.RequireMention = !cfg.Channels.Discord.RequireMention
				return true
			}
		case "whatsapp":
			if fieldKey == "enabled" {
				cfg.Channels.WhatsApp.Enabled = !cfg.Channels.WhatsApp.Enabled
				return true
			}
		case "email":
			if fieldKey == "enabled" {
				cfg.Channels.Email.Enabled = !cfg.Channels.Email.Enabled
				return true
			}
			if fieldKey == "consent" {
				cfg.Channels.Email.ConsentGranted = !cfg.Channels.Email.ConsentGranted
				return true
			}
		}
		return false
	}
	switch fieldKey {
	case "workspace_restrict":
		cfg.Tools.RestrictToWorkspace = !cfg.Tools.RestrictToWorkspace
		return true
	case "service_enabled":
		cfg.Service.Enabled = !cfg.Service.Enabled
		return true
	default:
		return false
	}
}

func cycleChoiceValue(cfg *config.Config, section, channel, fieldKey string, delta int) bool {
	if cfg == nil {
		return false
	}
	if section == "provider" && fieldKey == "provider_preset" {
		choices := []string{"OpenAI", "OpenRouter", "Custom"}
		current := indexOfChoice(choices, providerPresetLabel(cfg.Provider.APIBase))
		next := wrapIndex(current+delta, len(choices))
		switch choices[next] {
		case "OpenAI":
			applyProviderPreset(cfg, "1")
		case "OpenRouter":
			applyProviderPreset(cfg, "2")
		default:
			applyProviderPreset(cfg, "3")
		}
		return true
	}
	if section == "channels" && fieldKey == "access" {
		choices := []string{"pairing", "allowlist", "open", "deny"}
		var current string
		switch channel {
		case "telegram":
			current = channelAccessSummary(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, len(cfg.Channels.Telegram.AllowedChatIDs) > 0)
		case "slack":
			current = channelAccessSummary(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, len(cfg.Channels.Slack.AllowedUserIDs) > 0)
		case "discord":
			current = channelAccessSummary(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, len(cfg.Channels.Discord.AllowedUserIDs) > 0)
		case "whatsapp":
			current = channelAccessSummary(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, len(cfg.Channels.WhatsApp.AllowedFrom) > 0)
		case "email":
			current = channelAccessSummary(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, len(cfg.Channels.Email.AllowedSenders) > 0)
		}
		next := choices[wrapIndex(indexOfChoice(choices, current)+delta, len(choices))]
		applyAccessChoice(cfg, channel, next)
		return true
	}
	return false
}

func applyFieldValue(cfg *config.Config, section, channel, fieldKey, value string) bool {
	if cfg == nil {
		return false
	}
	if section == "channels" {
		switch channel {
		case "telegram":
			switch fieldKey {
			case "token":
				if value != "" {
					cfg.Channels.Telegram.Token = value
				}
				return true
			case "default_id":
				cfg.Channels.Telegram.DefaultChatID = value
				return true
			case "allowlist":
				cfg.Channels.Telegram.AllowedChatIDs = splitAndCompact(value)
				return true
			}
		case "slack":
			switch fieldKey {
			case "app_token":
				if value != "" {
					cfg.Channels.Slack.AppToken = value
				}
				return true
			case "bot_token":
				if value != "" {
					cfg.Channels.Slack.BotToken = value
				}
				return true
			case "default_id":
				cfg.Channels.Slack.DefaultChannelID = value
				return true
			case "allowlist":
				cfg.Channels.Slack.AllowedUserIDs = splitAndCompact(value)
				return true
			}
		case "discord":
			switch fieldKey {
			case "token":
				if value != "" {
					cfg.Channels.Discord.Token = value
				}
				return true
			case "default_id":
				cfg.Channels.Discord.DefaultChannelID = value
				return true
			case "allowlist":
				cfg.Channels.Discord.AllowedUserIDs = splitAndCompact(value)
				return true
			}
		case "whatsapp":
			switch fieldKey {
			case "bridge_url":
				cfg.Channels.WhatsApp.BridgeURL = value
				return true
			case "bridge_token":
				if value != "" {
					cfg.Channels.WhatsApp.BridgeToken = value
				}
				return true
			case "default_to":
				cfg.Channels.WhatsApp.DefaultTo = value
				return true
			case "allowlist":
				cfg.Channels.WhatsApp.AllowedFrom = splitAndCompact(value)
				return true
			}
		case "email":
			switch fieldKey {
			case "imap_host":
				cfg.Channels.Email.IMAPHost = value
				return true
			case "imap_user":
				cfg.Channels.Email.IMAPUsername = value
				return true
			case "imap_password":
				if value != "" {
					cfg.Channels.Email.IMAPPassword = value
				}
				return true
			case "smtp_host":
				cfg.Channels.Email.SMTPHost = value
				return true
			case "smtp_user":
				cfg.Channels.Email.SMTPUsername = value
				return true
			case "smtp_password":
				if value != "" {
					cfg.Channels.Email.SMTPPassword = value
				}
				return true
			case "from_address":
				cfg.Channels.Email.FromAddress = value
				return true
			case "default_to":
				cfg.Channels.Email.DefaultTo = value
				return true
			case "allowlist":
				cfg.Channels.Email.AllowedSenders = splitAndCompact(value)
				return true
			}
		}
		return false
	}
	switch fieldKey {
	case "provider_api_base":
		cfg.Provider.APIBase = value
		return true
	case "provider_model":
		cfg.Provider.Model = value
		return true
	case "provider_embed":
		cfg.Provider.EmbedModel = value
		return true
	case "provider_api_key":
		if value != "" {
			cfg.Provider.APIKey = value
		}
		return true
	case "storage_db":
		cfg.DBPath = value
		return true
	case "storage_artifacts":
		cfg.ArtifactsDir = value
		return true
	case "workspace_dir":
		cfg.WorkspaceDir = value
		return true
	case "web_brave":
		if value != "" {
			cfg.Tools.BraveAPIKey = value
		}
		return true
	case "web_proxy":
		cfg.Tools.WebProxy = value
		return true
	case "service_listen":
		cfg.Service.Listen = value
		return true
	case "service_secret":
		if value != "" {
			cfg.Service.Secret = value
		}
		return true
	default:
		return false
	}
}

func applyAccessChoice(cfg *config.Config, channel, choice string) {
	switch channel {
	case "telegram":
		setInboundChoice(choice, &cfg.Channels.Telegram.InboundPolicy, &cfg.Channels.Telegram.OpenAccess)
	case "slack":
		setInboundChoice(choice, &cfg.Channels.Slack.InboundPolicy, &cfg.Channels.Slack.OpenAccess)
	case "discord":
		setInboundChoice(choice, &cfg.Channels.Discord.InboundPolicy, &cfg.Channels.Discord.OpenAccess)
	case "whatsapp":
		setInboundChoice(choice, &cfg.Channels.WhatsApp.InboundPolicy, &cfg.Channels.WhatsApp.OpenAccess)
	case "email":
		setInboundChoice(choice, &cfg.Channels.Email.InboundPolicy, &cfg.Channels.Email.OpenAccess)
	}
}

func setInboundChoice(choice string, policy *config.InboundPolicy, openAccess *bool) {
	if policy == nil || openAccess == nil {
		return
	}
	switch strings.TrimSpace(choice) {
	case "pairing":
		*policy = config.InboundPolicyPairing
		*openAccess = false
	case "allowlist":
		*policy = config.InboundPolicyAllowlist
		*openAccess = false
	case "open":
		*policy = ""
		*openAccess = true
	case "deny":
		*policy = config.InboundPolicyDeny
		*openAccess = false
	}
}

func configureSectionMeta(key string) struct{ Key, Label, Description string } {
	for _, section := range configureSections {
		if section.Key == key {
			return section
		}
	}
	return struct{ Key, Label, Description string }{Key: key, Label: strings.Title(key), Description: ""}
}

func providerPresetLabel(apiBase string) string {
	switch configureProviderLabel(apiBase) {
	case "OpenAI":
		return "OpenAI"
	case "OpenRouter":
		return "OpenRouter"
	default:
		return "Custom"
	}
}

func secretDisplay(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "configured"
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func indexOfChoice(choices []string, value string) int {
	for i, choice := range choices {
		if choice == value {
			return i
		}
	}
	return 0
}

func wrapIndex(index, length int) int {
	if length == 0 {
		return 0
	}
	for index < 0 {
		index += length
	}
	return index % length
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(value, lower, upper int) int {
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func configSnapshotsEqual(left, right config.Config) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
