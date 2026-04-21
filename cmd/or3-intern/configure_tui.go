package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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

const configureSecretClearKeyword = "clear"

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
	changed, err := applyFieldValue(&m.cfg, m.currentSection, m.currentChannel, m.editingFieldKey, value)
	if err != nil {
		m.errorMessage = err.Error()
		return
	}
	m.errorMessage = ""
	if changed {
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
	label := configureSectionMeta(section).Label
	if label == "" {
		label = strings.Title(section)
	}
	position := fmt.Sprintf("Field %d/%d. Use ↑/↓ or Tab/Shift+Tab to move between fields.", cursor+1, total)
	if section == "channels" && channel != "" {
		return position + " Editing the " + strings.Title(channel) + " channel. Use space for toggles, ←/→ for access presets, and Enter to edit text fields."
	}
	if section != "" {
		return position + " Editing the " + label + " section. Press s to review and save when you’re happy with the current snapshot."
	}
	return position + " Use arrows to move around and Enter to edit the highlighted field."
}

func renderSummaryPanel(styles configureStyles, cfg config.Config, hint string) string {
	lines := []string{
		styles.section.Render("Current snapshot"),
		fmt.Sprintf("%s %s", styles.label.Render("Provider:"), styles.value.Render(configureProviderLabel(cfg.Provider.APIBase)+" · "+cfg.Provider.Model+" · embed="+emptyAsNone(cfg.Provider.EmbedModel))),
		fmt.Sprintf("%s %s", styles.label.Render("Storage:"), styles.value.Render(cfg.DBPath+" · "+cfg.ArtifactsDir)),
		fmt.Sprintf("%s %s", styles.label.Render("Runtime:"), styles.value.Render(fmt.Sprintf("session=%s · workers=%d · history=%d", cfg.DefaultSessionKey, cfg.WorkerCount, cfg.HistoryMax))),
		fmt.Sprintf("%s %s", styles.label.Render("Workspace:"), styles.value.Render(fmt.Sprintf("restrict=%t · %s", cfg.Tools.RestrictToWorkspace, emptyAsNone(cfg.WorkspaceDir)))),
		fmt.Sprintf("%s %s", styles.label.Render("Tools:"), styles.value.Render(fmt.Sprintf("Brave=%t · exec=%ds · proxy=%s", strings.TrimSpace(cfg.Tools.BraveAPIKey) != "", cfg.Tools.ExecTimeoutSeconds, emptyAsNone(cfg.Tools.WebProxy)))),
		fmt.Sprintf("%s %s", styles.label.Render("Security:"), styles.value.Render(fmt.Sprintf("approvals=%t · guarded=%t · network=%t", cfg.Security.Approvals.Enabled, cfg.Hardening.GuardedTools, cfg.Security.Network.Enabled))),
		fmt.Sprintf("%s %s", styles.label.Render("Skills:"), styles.value.Render(fmt.Sprintf("exec=%t · watch=%t · quarantine=%t", cfg.Skills.EnableExec, cfg.Skills.Load.Watch, cfg.Skills.Policy.QuarantineByDefault))),
		fmt.Sprintf("%s %s", styles.label.Render("Automation:"), styles.value.Render(fmt.Sprintf("cron=%t · heartbeat=%t · webhook=%t", cfg.Cron.Enabled, cfg.Heartbeat.Enabled, cfg.Triggers.Webhook.Enabled))),
		fmt.Sprintf("%s %s", styles.label.Render("Channels:"), styles.value.Render(strings.Join(enabledChannelNames(cfg), ", "))),
		fmt.Sprintf("%s %s", styles.label.Render("Service:"), styles.value.Render(serviceSummary(cfg))),
	}
	if len(enabledChannelNames(cfg)) == 0 {
		lines[8] = fmt.Sprintf("%s %s", styles.label.Render("Channels:"), styles.muted.Render("none enabled"))
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
		return configureProviderLabel(cfg.Provider.APIBase) + " · " + cfg.Provider.Model + " · embed=" + emptyAsNone(cfg.Provider.EmbedModel)
	case "storage":
		return emptyAsNone(cfg.DBPath) + " · " + emptyAsNone(cfg.ArtifactsDir)
	case "runtime":
		return fmt.Sprintf("session=%s · workers=%d · consolidation=%t", cfg.DefaultSessionKey, cfg.WorkerCount, cfg.ConsolidationEnabled)
	case "workspace":
		return fmt.Sprintf("restrict=%t · %s", cfg.Tools.RestrictToWorkspace, emptyAsNone(cfg.WorkspaceDir))
	case "tools":
		return fmt.Sprintf("Brave=%t · exec=%ds", strings.TrimSpace(cfg.Tools.BraveAPIKey) != "", cfg.Tools.ExecTimeoutSeconds)
	case "docindex":
		return fmt.Sprintf("enabled=%t · roots=%d · retrieve=%d", cfg.DocIndex.Enabled, len(cfg.DocIndex.Roots), cfg.DocIndex.RetrieveLimit)
	case "skills":
		return fmt.Sprintf("exec=%t · watch=%t · quarantine=%t", cfg.Skills.EnableExec, cfg.Skills.Load.Watch, cfg.Skills.Policy.QuarantineByDefault)
	case "security":
		return fmt.Sprintf("approvals=%t · audit=%t · network=%t", cfg.Security.Approvals.Enabled, cfg.Security.Audit.Enabled, cfg.Security.Network.Enabled)
	case "hardening":
		return fmt.Sprintf("guarded=%t · privileged=%t · sandbox=%t", cfg.Hardening.GuardedTools, cfg.Hardening.PrivilegedTools, cfg.Hardening.Sandbox.Enabled)
	case "session":
		return fmt.Sprintf("sharedDM=%t · links=%d", cfg.Session.DirectMessagesShareDefault, len(cfg.Session.IdentityLinks))
	case "automation":
		return fmt.Sprintf("cron=%t · heartbeat=%t · webhook=%t", cfg.Cron.Enabled, cfg.Heartbeat.Enabled, cfg.Triggers.Webhook.Enabled)
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
			{Key: "provider_temperature", Label: "Temperature", Description: "Sampling temperature for chat turns.", Kind: configureFieldText, Value: formatFloat(cfg.Provider.Temperature), EmptyHint: "0"},
			{Key: "provider_timeout", Label: "Timeout seconds", Description: "HTTP timeout for provider calls.", Kind: configureFieldText, Value: formatInt(cfg.Provider.TimeoutSeconds), EmptyHint: "60"},
			{Key: "provider_vision", Label: "Enable vision", Description: "Allow image inputs when the model and runtime support it.", Kind: configureFieldToggle, Value: onOff(cfg.Provider.EnableVision)},
			{Key: "provider_api_key", Label: "API key", Description: "Hidden secret. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Provider.APIKey), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"},
		}
	case "storage":
		return []configureField{
			{Key: "storage_db", Label: "SQLite DB path", Description: "Main runtime database.", Kind: configureFieldText, Value: cfg.DBPath, EmptyHint: ".or3/or3-intern.sqlite"},
			{Key: "storage_artifacts", Label: "Artifacts directory", Description: "Large output spillover and artifacts.", Kind: configureFieldText, Value: cfg.ArtifactsDir, EmptyHint: ".or3/artifacts"},
			{Key: "storage_soul", Label: "SOUL.md path", Description: "Primary runtime soul/bootstrap file.", Kind: configureFieldText, Value: cfg.SoulFile, EmptyHint: "~/.or3-intern/SOUL.md"},
			{Key: "storage_agents", Label: "AGENTS.md path", Description: "Agent instructions/bootstrap file.", Kind: configureFieldText, Value: cfg.AgentsFile, EmptyHint: "~/.or3-intern/AGENTS.md"},
			{Key: "storage_tools", Label: "TOOLS.md path", Description: "Tool notes/bootstrap file.", Kind: configureFieldText, Value: cfg.ToolsFile, EmptyHint: "~/.or3-intern/TOOLS.md"},
			{Key: "storage_identity", Label: "IDENTITY.md path", Description: "Static identity file injected into prompts.", Kind: configureFieldText, Value: cfg.IdentityFile, EmptyHint: "~/.or3-intern/IDENTITY.md"},
			{Key: "storage_memory", Label: "MEMORY.md path", Description: "Static durable memory file injected into prompts.", Kind: configureFieldText, Value: cfg.MemoryFile, EmptyHint: "~/.or3-intern/MEMORY.md"},
		}
	case "runtime":
		profileChoices := []string{"default", "local-dev", "single-user-hardened", "hosted-service", "hosted-no-exec", "hosted-remote-sandbox-only"}
		profileValue := string(cfg.RuntimeProfile)
		if strings.TrimSpace(profileValue) == "" {
			profileValue = "default"
		}
		return []configureField{
			{Key: "runtime_default_session", Label: "Default session key", Description: "Session key used by `or3-intern chat` and other local flows.", Kind: configureFieldText, Value: cfg.DefaultSessionKey, EmptyHint: "cli:default"},
			{Key: "runtime_profile", Label: "Runtime profile", Description: "Preset hardening posture for local or hosted deployments.", Kind: configureFieldChoice, Value: profileValue, Choices: profileChoices, ChoiceIndex: indexOfChoice(profileChoices, profileValue)},
			{Key: "runtime_bootstrap_max_chars", Label: "Bootstrap max chars", Description: "Max chars per bootstrap file included in prompts.", Kind: configureFieldText, Value: formatInt(cfg.BootstrapMaxChars), EmptyHint: "20000"},
			{Key: "runtime_bootstrap_total_chars", Label: "Bootstrap total max chars", Description: "Total bootstrap prompt budget across files.", Kind: configureFieldText, Value: formatInt(cfg.BootstrapTotalMaxChars), EmptyHint: "150000"},
			{Key: "runtime_session_cache", Label: "Session cache limit", Description: "Cached session count for runtime state.", Kind: configureFieldText, Value: formatInt(cfg.SessionCache), EmptyHint: "64"},
			{Key: "runtime_history_max", Label: "History max messages", Description: "Conversation messages retained in active prompt history.", Kind: configureFieldText, Value: formatInt(cfg.HistoryMax), EmptyHint: "40"},
			{Key: "runtime_max_tool_bytes", Label: "Max tool bytes", Description: "Max tool output bytes before truncation.", Kind: configureFieldText, Value: formatInt(cfg.MaxToolBytes), EmptyHint: "24576"},
			{Key: "runtime_max_media_bytes", Label: "Max media bytes", Description: "Largest media payload accepted by the runtime.", Kind: configureFieldText, Value: formatInt(cfg.MaxMediaBytes), EmptyHint: "20971520"},
			{Key: "runtime_max_tool_loops", Label: "Max tool loops", Description: "Maximum assistant tool-call rounds per turn.", Kind: configureFieldText, Value: formatInt(cfg.MaxToolLoops), EmptyHint: "6"},
			{Key: "runtime_memory_retrieve", Label: "Memory retrieve limit", Description: "How many long-term memory hits are injected into prompts.", Kind: configureFieldText, Value: formatInt(cfg.MemoryRetrieve), EmptyHint: "8"},
			{Key: "runtime_vector_k", Label: "Vector search K", Description: "Semantic memory candidate count before ranking.", Kind: configureFieldText, Value: formatInt(cfg.VectorK), EmptyHint: "8"},
			{Key: "runtime_fts_k", Label: "FTS search K", Description: "Keyword memory candidate count before ranking.", Kind: configureFieldText, Value: formatInt(cfg.FTSK), EmptyHint: "8"},
			{Key: "runtime_vector_scan_limit", Label: "Vector scan limit", Description: "Upper bound for vector scoring work during retrieval.", Kind: configureFieldText, Value: formatInt(cfg.VectorScanLimit), EmptyHint: "2000"},
			{Key: "runtime_worker_count", Label: "Worker count", Description: "Concurrent runtime workers processing queued events.", Kind: configureFieldText, Value: formatInt(cfg.WorkerCount), EmptyHint: "4"},
			{Key: "runtime_consolidation_enabled", Label: "Enable consolidation", Description: "Summarize older messages into durable memory notes.", Kind: configureFieldToggle, Value: onOff(cfg.ConsolidationEnabled)},
			{Key: "runtime_consolidation_window", Label: "Consolidation window size", Description: "Minimum message window before a consolidation run starts.", Kind: configureFieldText, Value: formatInt(cfg.ConsolidationWindowSize), EmptyHint: "10"},
			{Key: "runtime_consolidation_max_messages", Label: "Consolidation max messages", Description: "Max messages summarized in one consolidation pass.", Kind: configureFieldText, Value: formatInt(cfg.ConsolidationMaxMessages), EmptyHint: "50"},
			{Key: "runtime_consolidation_max_input_chars", Label: "Consolidation max input chars", Description: "Prompt budget for consolidation transcript input.", Kind: configureFieldText, Value: formatInt(cfg.ConsolidationMaxInputChars), EmptyHint: "12000"},
			{Key: "runtime_consolidation_async_timeout", Label: "Consolidation async timeout", Description: "Timeout for async consolidation passes, in seconds.", Kind: configureFieldText, Value: formatInt(cfg.ConsolidationAsyncTimeoutSeconds), EmptyHint: "30"},
			{Key: "runtime_subagents_enabled", Label: "Enable subagents", Description: "Allow internal subagent orchestration.", Kind: configureFieldToggle, Value: onOff(cfg.Subagents.Enabled)},
			{Key: "runtime_subagents_max_concurrent", Label: "Subagents max concurrent", Description: "Maximum concurrent subagents.", Kind: configureFieldText, Value: formatInt(cfg.Subagents.MaxConcurrent), EmptyHint: "1"},
			{Key: "runtime_subagents_max_queued", Label: "Subagents max queued", Description: "Maximum queued subagent tasks.", Kind: configureFieldText, Value: formatInt(cfg.Subagents.MaxQueued), EmptyHint: "32"},
			{Key: "runtime_subagents_timeout", Label: "Subagents timeout seconds", Description: "Timeout for each subagent task.", Kind: configureFieldText, Value: formatInt(cfg.Subagents.TaskTimeoutSeconds), EmptyHint: "300"},
		}
	case "workspace":
		workspace := cfg.WorkspaceDir
		if strings.TrimSpace(workspace) == "" {
			workspace = cwd
		}
		return []configureField{
			{Key: "workspace_restrict", Label: "Restrict file tools", Description: "Keep file tools inside the selected workspace.", Kind: configureFieldToggle, Value: onOff(cfg.Tools.RestrictToWorkspace)},
			{Key: "workspace_dir", Label: "Workspace directory", Description: "Project root for workspace-restricted file tools.", Kind: configureFieldText, Value: workspace, EmptyHint: cwd},
			{Key: "workspace_allowed_dir", Label: "Allowed directory", Description: "Optional additional allowed root used by some flows and integrations.", Kind: configureFieldText, Value: cfg.AllowedDir, EmptyHint: cwd},
		}
	case "tools":
		return []configureField{
			{Key: "tools_brave", Label: "Brave Search key", Description: "Hidden secret for Brave web search. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Tools.BraveAPIKey), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"},
			{Key: "tools_web_proxy", Label: "Web proxy", Description: "Optional outbound proxy URL for web access.", Kind: configureFieldText, Value: cfg.Tools.WebProxy, EmptyHint: "http://proxy.internal:8080"},
			{Key: "tools_exec_timeout", Label: "Exec timeout seconds", Description: "Default timeout for built-in exec-capable tools.", Kind: configureFieldText, Value: formatInt(cfg.Tools.ExecTimeoutSeconds), EmptyHint: "60"},
			{Key: "tools_path_append", Label: "PATH append", Description: "Extra PATH entries appended for child process execution.", Kind: configureFieldText, Value: cfg.Tools.PathAppend, EmptyHint: "/opt/homebrew/bin"},
		}
	case "docindex":
		return []configureField{
			{Key: "docindex_enabled", Label: "Enable doc index", Description: "Index workspace files for retrieval-augmented prompts.", Kind: configureFieldToggle, Value: onOff(cfg.DocIndex.Enabled)},
			{Key: "docindex_roots", Label: "Roots", Description: "Comma-separated directories to index.", Kind: configureFieldText, Value: strings.Join(cfg.DocIndex.Roots, ","), EmptyHint: "docs,src"},
			{Key: "docindex_max_files", Label: "Max files", Description: "Maximum files indexed in one scope.", Kind: configureFieldText, Value: formatInt(cfg.DocIndex.MaxFiles), EmptyHint: "100"},
			{Key: "docindex_max_file_bytes", Label: "Max file bytes", Description: "Largest file size to index.", Kind: configureFieldText, Value: formatInt(cfg.DocIndex.MaxFileBytes), EmptyHint: "65536"},
			{Key: "docindex_max_chunks", Label: "Max chunks", Description: "Upper bound on indexed chunks.", Kind: configureFieldText, Value: formatInt(cfg.DocIndex.MaxChunks), EmptyHint: "500"},
			{Key: "docindex_embed_max_bytes", Label: "Embed max bytes", Description: "Max content bytes embedded per indexed file.", Kind: configureFieldText, Value: formatInt(cfg.DocIndex.EmbedMaxBytes), EmptyHint: "8192"},
			{Key: "docindex_refresh_seconds", Label: "Refresh seconds", Description: "Periodic refresh cadence for indexed roots.", Kind: configureFieldText, Value: formatInt(cfg.DocIndex.RefreshSeconds), EmptyHint: "300"},
			{Key: "docindex_retrieve_limit", Label: "Retrieve limit", Description: "How many indexed doc hits are injected into prompts.", Kind: configureFieldText, Value: formatInt(cfg.DocIndex.RetrieveLimit), EmptyHint: "5"},
		}
	case "skills":
		return []configureField{
			{Key: "skills_enable_exec", Label: "Enable skill exec", Description: "Allow skills to run external commands when approved by policy.", Kind: configureFieldToggle, Value: onOff(cfg.Skills.EnableExec)},
			{Key: "skills_max_run_seconds", Label: "Max run seconds", Description: "Timeout for skill execution.", Kind: configureFieldText, Value: formatInt(cfg.Skills.MaxRunSeconds), EmptyHint: "30"},
			{Key: "skills_managed_dir", Label: "Managed skills directory", Description: "Where installed and local managed skills are stored.", Kind: configureFieldText, Value: cfg.Skills.ManagedDir, EmptyHint: "~/.or3-intern/skills"},
			{Key: "skills_quarantine", Label: "Quarantine by default", Description: "Require explicit approval before new external skills are trusted.", Kind: configureFieldToggle, Value: onOff(cfg.Skills.Policy.QuarantineByDefault)},
			{Key: "skills_approved", Label: "Approved skills", Description: "Comma-separated pre-approved skill IDs.", Kind: configureFieldText, Value: strings.Join(cfg.Skills.Policy.Approved, ","), EmptyHint: "owner/skill"},
			{Key: "skills_trusted_owners", Label: "Trusted owners", Description: "Comma-separated owners trusted by default.", Kind: configureFieldText, Value: strings.Join(cfg.Skills.Policy.TrustedOwners, ","), EmptyHint: "your-org"},
			{Key: "skills_blocked_owners", Label: "Blocked owners", Description: "Comma-separated owners blocked from install/use.", Kind: configureFieldText, Value: strings.Join(cfg.Skills.Policy.BlockedOwners, ","), EmptyHint: "untrusted-owner"},
			{Key: "skills_trusted_registries", Label: "Trusted registries", Description: "Comma-separated trusted skill registries.", Kind: configureFieldText, Value: strings.Join(cfg.Skills.Policy.TrustedRegistries, ","), EmptyHint: "https://clawhub.ai"},
			{Key: "skills_extra_dirs", Label: "Extra directories", Description: "Comma-separated directories scanned for skills.", Kind: configureFieldText, Value: strings.Join(cfg.Skills.Load.ExtraDirs, ","), EmptyHint: "vendor/skills"},
			{Key: "skills_watch", Label: "Watch skill directories", Description: "Reload skills automatically when files change.", Kind: configureFieldToggle, Value: onOff(cfg.Skills.Load.Watch)},
			{Key: "skills_watch_debounce", Label: "Watch debounce ms", Description: "Delay before reloading changed skill files.", Kind: configureFieldText, Value: formatInt(cfg.Skills.Load.WatchDebounceMS), EmptyHint: "250"},
			{Key: "skills_clawhub_site", Label: "ClawHub site URL", Description: "Human-facing ClawHub site URL.", Kind: configureFieldText, Value: cfg.Skills.ClawHub.SiteURL, EmptyHint: "https://clawhub.ai"},
			{Key: "skills_clawhub_registry", Label: "ClawHub registry URL", Description: "Registry base URL used for remote skill operations.", Kind: configureFieldText, Value: cfg.Skills.ClawHub.RegistryURL, EmptyHint: "https://clawhub.ai"},
			{Key: "skills_clawhub_install", Label: "ClawHub install dir", Description: "Install subdirectory used for fetched skills.", Kind: configureFieldText, Value: cfg.Skills.ClawHub.InstallDir, EmptyHint: "skills"},
		}
	case "security":
		approvalChoices := []string{"deny", "ask", "allowlist", "trusted"}
		return []configureField{
			{Key: "security_secret_store_enabled", Label: "Enable secret store", Description: "Encrypt secrets in the local database instead of plain config only.", Kind: configureFieldToggle, Value: onOff(cfg.Security.SecretStore.Enabled)},
			{Key: "security_secret_store_required", Label: "Require secret store", Description: "Refuse startup if the secret store cannot be used.", Kind: configureFieldToggle, Value: onOff(cfg.Security.SecretStore.Required)},
			{Key: "security_secret_store_key_file", Label: "Secret-store key file", Description: "Master key file for encrypted secret storage.", Kind: configureFieldText, Value: cfg.Security.SecretStore.KeyFile, EmptyHint: "~/.or3-intern/master.key"},
			{Key: "security_audit_enabled", Label: "Enable audit log", Description: "Record signed audit events for sensitive operations.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Audit.Enabled)},
			{Key: "security_audit_strict", Label: "Strict audit", Description: "Treat audit write failures as runtime errors.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Audit.Strict)},
			{Key: "security_audit_key_file", Label: "Audit key file", Description: "Signing key for audit records.", Kind: configureFieldText, Value: cfg.Security.Audit.KeyFile, EmptyHint: "~/.or3-intern/audit.key"},
			{Key: "security_audit_verify_on_start", Label: "Verify audit on start", Description: "Verify audit chain integrity during startup.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Audit.VerifyOnStart)},
			{Key: "security_approvals_enabled", Label: "Enable approvals", Description: "Require approval workflows for sensitive domains.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Approvals.Enabled)},
			{Key: "security_approvals_host_id", Label: "Approval host ID", Description: "Host identifier used for approval tokens and pairing.", Kind: configureFieldText, Value: cfg.Security.Approvals.HostID, EmptyHint: "local"},
			{Key: "security_approvals_key_file", Label: "Approval key file", Description: "Signing key for pairing codes and approval tokens.", Kind: configureFieldText, Value: cfg.Security.Approvals.KeyFile, EmptyHint: "~/.or3-intern/approvals.key"},
			{Key: "security_approvals_pairing_ttl", Label: "Pairing-code TTL seconds", Description: "Expiration for pairing codes.", Kind: configureFieldText, Value: formatInt(cfg.Security.Approvals.PairingCodeTTLSeconds), EmptyHint: "300"},
			{Key: "security_approvals_pending_ttl", Label: "Pending TTL seconds", Description: "Expiration for pending approval requests.", Kind: configureFieldText, Value: formatInt(cfg.Security.Approvals.PendingTTLSeconds), EmptyHint: "900"},
			{Key: "security_approvals_token_ttl", Label: "Approval-token TTL seconds", Description: "Expiration for one-shot approval tokens.", Kind: configureFieldText, Value: formatInt(cfg.Security.Approvals.ApprovalTokenTTLSeconds), EmptyHint: "300"},
			{Key: "security_approval_pairing_mode", Label: "Pairing mode", Description: "Approval rule for device/channel pairing.", Kind: configureFieldChoice, Value: string(cfg.Security.Approvals.Pairing.Mode), Choices: approvalChoices, ChoiceIndex: indexOfChoice(approvalChoices, string(cfg.Security.Approvals.Pairing.Mode))},
			{Key: "security_approval_exec_mode", Label: "Exec mode", Description: "Approval rule for command execution.", Kind: configureFieldChoice, Value: string(cfg.Security.Approvals.Exec.Mode), Choices: approvalChoices, ChoiceIndex: indexOfChoice(approvalChoices, string(cfg.Security.Approvals.Exec.Mode))},
			{Key: "security_approval_skill_mode", Label: "Skill execution mode", Description: "Approval rule for skill execution.", Kind: configureFieldChoice, Value: string(cfg.Security.Approvals.SkillExecution.Mode), Choices: approvalChoices, ChoiceIndex: indexOfChoice(approvalChoices, string(cfg.Security.Approvals.SkillExecution.Mode))},
			{Key: "security_approval_secret_mode", Label: "Secret access mode", Description: "Approval rule for reading stored secrets.", Kind: configureFieldChoice, Value: string(cfg.Security.Approvals.SecretAccess.Mode), Choices: approvalChoices, ChoiceIndex: indexOfChoice(approvalChoices, string(cfg.Security.Approvals.SecretAccess.Mode))},
			{Key: "security_approval_message_mode", Label: "Message-send mode", Description: "Approval rule for outbound send-message actions.", Kind: configureFieldChoice, Value: string(cfg.Security.Approvals.MessageSend.Mode), Choices: approvalChoices, ChoiceIndex: indexOfChoice(approvalChoices, string(cfg.Security.Approvals.MessageSend.Mode))},
			{Key: "security_profiles_enabled", Label: "Enable access profiles", Description: "Map channels and triggers to named runtime capability profiles.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Profiles.Enabled)},
			{Key: "security_profiles_default", Label: "Default profile", Description: "Fallback profile name applied when no channel/trigger mapping matches.", Kind: configureFieldText, Value: cfg.Security.Profiles.Default, EmptyHint: "guarded"},
			{Key: "security_profiles_channels", Label: "Channel profile mappings", Description: "Comma-separated `channel=profile` mappings, e.g. `telegram=ops,slack=guarded`.", Kind: configureFieldText, Value: formatStringMap(cfg.Security.Profiles.Channels), EmptyHint: "telegram=ops,slack=guarded"},
			{Key: "security_profiles_triggers", Label: "Trigger profile mappings", Description: "Comma-separated `trigger=profile` mappings, e.g. `webhook=guarded`.", Kind: configureFieldText, Value: formatStringMap(cfg.Security.Profiles.Triggers), EmptyHint: "webhook=guarded"},
			{Key: "security_network_enabled", Label: "Enable network policy", Description: "Apply outbound host restrictions to provider, web, and MCP traffic.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Network.Enabled)},
			{Key: "security_network_default_deny", Label: "Default deny", Description: "Block all outbound hosts unless they are explicitly allowed.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Network.DefaultDeny)},
			{Key: "security_network_allowed_hosts", Label: "Allowed hosts", Description: "Comma-separated hosts allowed by the outbound network policy.", Kind: configureFieldText, Value: strings.Join(cfg.Security.Network.AllowedHosts, ","), EmptyHint: "openrouter.ai,api.telegram.org"},
			{Key: "security_network_allow_loopback", Label: "Allow loopback", Description: "Permit access to localhost/127.0.0.1.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Network.AllowLoopback)},
			{Key: "security_network_allow_private", Label: "Allow private networks", Description: "Permit RFC1918/private IP ranges.", Kind: configureFieldToggle, Value: onOff(cfg.Security.Network.AllowPrivate)},
		}
	case "hardening":
		return []configureField{
			{Key: "hardening_guarded_tools", Label: "Enable guarded tools", Description: "Allow guarded-capability tools like file writes and web fetches.", Kind: configureFieldToggle, Value: onOff(cfg.Hardening.GuardedTools)},
			{Key: "hardening_privileged_tools", Label: "Enable privileged tools", Description: "Allow privileged-capability tools in addition to guarded tools.", Kind: configureFieldToggle, Value: onOff(cfg.Hardening.PrivilegedTools)},
			{Key: "hardening_exec_shell", Label: "Enable exec shell mode", Description: "Permit shell-style command execution when approvals and policy allow it.", Kind: configureFieldToggle, Value: onOff(cfg.Hardening.EnableExecShell)},
			{Key: "hardening_isolate_channel_peers", Label: "Isolate channel peers", Description: "Prevent one channel identity from sharing another channel’s capability context.", Kind: configureFieldToggle, Value: onOff(cfg.Hardening.IsolateChannelPeers)},
			{Key: "hardening_exec_allowed_programs", Label: "Exec allowed programs", Description: "Comma-separated allowlist of binaries available to exec-capable tools.", Kind: configureFieldText, Value: strings.Join(cfg.Hardening.ExecAllowedPrograms, ","), EmptyHint: "cat,echo,git"},
			{Key: "hardening_child_env_allowlist", Label: "Child env allowlist", Description: "Comma-separated environment variables passed to child processes.", Kind: configureFieldText, Value: strings.Join(cfg.Hardening.ChildEnvAllowlist, ","), EmptyHint: "PATH,HOME,TMPDIR"},
			{Key: "hardening_sandbox_enabled", Label: "Enable sandbox", Description: "Run exec-capable tools inside a restricted sandbox.", Kind: configureFieldToggle, Value: onOff(cfg.Hardening.Sandbox.Enabled)},
			{Key: "hardening_sandbox_bwrap", Label: "Bubblewrap path", Description: "Path to the bubblewrap executable.", Kind: configureFieldText, Value: cfg.Hardening.Sandbox.BubblewrapPath, EmptyHint: "bwrap"},
			{Key: "hardening_sandbox_allow_network", Label: "Sandbox allow network", Description: "Permit outbound networking from inside the sandbox.", Kind: configureFieldToggle, Value: onOff(cfg.Hardening.Sandbox.AllowNetwork)},
			{Key: "hardening_sandbox_writable_paths", Label: "Sandbox writable paths", Description: "Comma-separated writable paths made available inside the sandbox.", Kind: configureFieldText, Value: strings.Join(cfg.Hardening.Sandbox.WritablePaths, ","), EmptyHint: "/tmp,/var/tmp"},
			{Key: "hardening_quotas_enabled", Label: "Enable hardening quotas", Description: "Enforce per-turn quotas on sensitive tool categories.", Kind: configureFieldToggle, Value: onOff(cfg.Hardening.Quotas.Enabled)},
			{Key: "hardening_max_tool_calls", Label: "Max tool calls", Description: "Total tool-call quota per turn.", Kind: configureFieldText, Value: formatInt(cfg.Hardening.Quotas.MaxToolCalls), EmptyHint: "16"},
			{Key: "hardening_max_exec_calls", Label: "Max exec calls", Description: "Exec-call quota per turn.", Kind: configureFieldText, Value: formatInt(cfg.Hardening.Quotas.MaxExecCalls), EmptyHint: "2"},
			{Key: "hardening_max_web_calls", Label: "Max web calls", Description: "Web-call quota per turn.", Kind: configureFieldText, Value: formatInt(cfg.Hardening.Quotas.MaxWebCalls), EmptyHint: "4"},
			{Key: "hardening_max_subagent_calls", Label: "Max subagent calls", Description: "Subagent-call quota per turn.", Kind: configureFieldText, Value: formatInt(cfg.Hardening.Quotas.MaxSubagentCalls), EmptyHint: "2"},
		}
	case "session":
		return []configureField{
			{Key: "session_direct_messages_share_default", Label: "Share default session for DMs", Description: "Link direct-message channels into the default session scope.", Kind: configureFieldToggle, Value: onOff(cfg.Session.DirectMessagesShareDefault)},
			{Key: "session_identity_links", Label: "Identity links", Description: "Semicolon-separated `canonical=peer1|peer2` mappings.", Kind: configureFieldText, Value: formatIdentityLinks(cfg.Session.IdentityLinks), EmptyHint: "alice=telegram:alice|slack:U123"},
		}
	case "automation":
		return []configureField{
			{Key: "automation_cron_enabled", Label: "Enable cron store", Description: "Persist cron jobs and make the cron service available.", Kind: configureFieldToggle, Value: onOff(cfg.Cron.Enabled)},
			{Key: "automation_cron_store_path", Label: "Cron store path", Description: "JSON persistence path for scheduled jobs.", Kind: configureFieldText, Value: cfg.Cron.StorePath, EmptyHint: "~/.or3-intern/cron.json"},
			{Key: "automation_heartbeat_enabled", Label: "Enable heartbeat", Description: "Run periodic autonomous heartbeat turns.", Kind: configureFieldToggle, Value: onOff(cfg.Heartbeat.Enabled)},
			{Key: "automation_heartbeat_interval", Label: "Heartbeat interval minutes", Description: "How often heartbeat turns run.", Kind: configureFieldText, Value: formatInt(cfg.Heartbeat.IntervalMinutes), EmptyHint: "30"},
			{Key: "automation_heartbeat_tasks_file", Label: "Heartbeat tasks file", Description: "Markdown file containing recurring heartbeat tasks.", Kind: configureFieldText, Value: cfg.Heartbeat.TasksFile, EmptyHint: "~/.or3-intern/HEARTBEAT.md"},
			{Key: "automation_heartbeat_session", Label: "Heartbeat session key", Description: "Session key used for heartbeat turns.", Kind: configureFieldText, Value: cfg.Heartbeat.SessionKey, EmptyHint: "heartbeat:default"},
			{Key: "automation_webhook_enabled", Label: "Enable webhook trigger", Description: "Accept inbound webhook events and route them into the runtime.", Kind: configureFieldToggle, Value: onOff(cfg.Triggers.Webhook.Enabled)},
			{Key: "automation_webhook_addr", Label: "Webhook listen addr", Description: "Bind address for the webhook trigger server.", Kind: configureFieldText, Value: cfg.Triggers.Webhook.Addr, EmptyHint: "127.0.0.1:8765"},
			{Key: "automation_webhook_secret", Label: "Webhook secret", Description: "Hidden shared secret for webhook authentication.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Triggers.Webhook.Secret), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"},
			{Key: "automation_webhook_max_body_kb", Label: "Webhook max body KB", Description: "Maximum webhook request payload size.", Kind: configureFieldText, Value: formatInt(cfg.Triggers.Webhook.MaxBodyKB), EmptyHint: "64"},
			{Key: "automation_filewatch_enabled", Label: "Enable file watch trigger", Description: "Poll local paths and emit trigger events on changes.", Kind: configureFieldToggle, Value: onOff(cfg.Triggers.FileWatch.Enabled)},
			{Key: "automation_filewatch_paths", Label: "File-watch paths", Description: "Comma-separated paths watched by the file trigger.", Kind: configureFieldText, Value: strings.Join(cfg.Triggers.FileWatch.Paths, ","), EmptyHint: "planning,tasks.md"},
			{Key: "automation_filewatch_poll_seconds", Label: "File-watch poll seconds", Description: "Polling interval for watched paths.", Kind: configureFieldText, Value: formatInt(cfg.Triggers.FileWatch.PollSeconds), EmptyHint: "5"},
			{Key: "automation_filewatch_debounce", Label: "File-watch debounce seconds", Description: "Debounce window before emitting a trigger.", Kind: configureFieldText, Value: formatInt(cfg.Triggers.FileWatch.DebounceSeconds), EmptyHint: "2"},
		}
	case "service":
		return []configureField{{Key: "service_enabled", Label: "Enable service API", Description: "Expose the internal authenticated HTTP API.", Kind: configureFieldToggle, Value: onOff(cfg.Service.Enabled)}, {Key: "service_listen", Label: "Listen address", Description: "Bind address for the internal service.", Kind: configureFieldText, Value: cfg.Service.Listen, EmptyHint: "127.0.0.1:9100"}, {Key: "service_secret", Label: "Shared secret", Description: "Hidden secret. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Service.Secret), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}}
	}
	return nil
}

func buildChannelFields(cfg config.Config, channel string) []configureField {
	accessChoices := []string{"pairing", "allowlist", "open", "deny"}
	switch channel {
	case "telegram":
		choice := channelAccessSummary(cfg.Channels.Telegram.InboundPolicy, cfg.Channels.Telegram.OpenAccess, len(cfg.Channels.Telegram.AllowedChatIDs) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Telegram", Description: "Toggle the Telegram channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Telegram.Enabled)}, {Key: "token", Label: "Bot token", Description: "Hidden Telegram bot token. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Telegram.Token), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}, {Key: "default_id", Label: "Default chat ID", Description: "Used by outbound send defaults.", Kind: configureFieldText, Value: cfg.Channels.Telegram.DefaultChatID, EmptyHint: "123456789"}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound Telegram messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed chat IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(allowlistPromptDefault(cfg.Channels.Telegram.AllowedChatIDs, cfg.Channels.Telegram.DefaultChatID), ","), EmptyHint: "12345,67890"}}
	case "slack":
		choice := channelAccessSummary(cfg.Channels.Slack.InboundPolicy, cfg.Channels.Slack.OpenAccess, len(cfg.Channels.Slack.AllowedUserIDs) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Slack", Description: "Toggle the Slack channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Slack.Enabled)}, {Key: "app_token", Label: "App token", Description: "Hidden Slack app token. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Slack.AppToken), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}, {Key: "bot_token", Label: "Bot token", Description: "Hidden Slack bot token. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Slack.BotToken), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}, {Key: "default_id", Label: "Default channel ID", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.Slack.DefaultChannelID, EmptyHint: "C123456"}, {Key: "require_mention", Label: "Require mention", Description: "Only react when the bot is mentioned.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Slack.RequireMention)}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound Slack messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed user IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.Slack.AllowedUserIDs, ","), EmptyHint: "U123,U456"}}
	case "discord":
		choice := channelAccessSummary(cfg.Channels.Discord.InboundPolicy, cfg.Channels.Discord.OpenAccess, len(cfg.Channels.Discord.AllowedUserIDs) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Discord", Description: "Toggle the Discord channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Discord.Enabled)}, {Key: "token", Label: "Bot token", Description: "Hidden Discord bot token. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Discord.Token), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}, {Key: "default_id", Label: "Default channel ID", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.Discord.DefaultChannelID, EmptyHint: "123456"}, {Key: "require_mention", Label: "Require mention", Description: "Only react when the bot is mentioned.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Discord.RequireMention)}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound Discord messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed user IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.Discord.AllowedUserIDs, ","), EmptyHint: "123,456"}}
	case "whatsapp":
		choice := channelAccessSummary(cfg.Channels.WhatsApp.InboundPolicy, cfg.Channels.WhatsApp.OpenAccess, len(cfg.Channels.WhatsApp.AllowedFrom) > 0)
		return []configureField{{Key: "enabled", Label: "Enable WhatsApp", Description: "Toggle the WhatsApp bridge.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.WhatsApp.Enabled)}, {Key: "bridge_url", Label: "Bridge URL", Description: "WebSocket bridge endpoint.", Kind: configureFieldText, Value: cfg.Channels.WhatsApp.BridgeURL, EmptyHint: "ws://127.0.0.1:3001/ws"}, {Key: "bridge_token", Label: "Bridge token", Description: "Hidden bridge token. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.WhatsApp.BridgeToken), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}, {Key: "default_to", Label: "Default recipient", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.WhatsApp.DefaultTo, EmptyHint: "+15555555555"}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound WhatsApp messages are admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed sender IDs", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.WhatsApp.AllowedFrom, ","), EmptyHint: "+1555,+1666"}}
	case "email":
		choice := channelAccessSummary(cfg.Channels.Email.InboundPolicy, cfg.Channels.Email.OpenAccess, len(cfg.Channels.Email.AllowedSenders) > 0)
		return []configureField{{Key: "enabled", Label: "Enable Email", Description: "Toggle the email channel.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Email.Enabled)}, {Key: "consent", Label: "Consent granted", Description: "Confirm consent for operating email automation.", Kind: configureFieldToggle, Value: onOff(cfg.Channels.Email.ConsentGranted)}, {Key: "imap_host", Label: "IMAP host", Description: "Inbound IMAP server.", Kind: configureFieldText, Value: cfg.Channels.Email.IMAPHost, EmptyHint: "imap.example.com"}, {Key: "imap_user", Label: "IMAP username", Description: "Inbound mailbox username.", Kind: configureFieldText, Value: cfg.Channels.Email.IMAPUsername, EmptyHint: "inbox@example.com"}, {Key: "imap_password", Label: "IMAP password", Description: "Hidden IMAP password. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Email.IMAPPassword), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}, {Key: "smtp_host", Label: "SMTP host", Description: "Outbound SMTP server.", Kind: configureFieldText, Value: cfg.Channels.Email.SMTPHost, EmptyHint: "smtp.example.com"}, {Key: "smtp_user", Label: "SMTP username", Description: "Outbound SMTP username.", Kind: configureFieldText, Value: cfg.Channels.Email.SMTPUsername, EmptyHint: "sender@example.com"}, {Key: "smtp_password", Label: "SMTP password", Description: "Hidden SMTP password. Enter replaces it; type clear to remove it.", Kind: configureFieldSecret, Value: secretDisplay(cfg.Channels.Email.SMTPPassword), SecretHint: "blank keeps current • type clear to remove", EmptyHint: "not configured"}, {Key: "from_address", Label: "From address", Description: "Outbound sender address.", Kind: configureFieldText, Value: cfg.Channels.Email.FromAddress, EmptyHint: "bot@example.com"}, {Key: "default_to", Label: "Default recipient", Description: "Used by outbound sends.", Kind: configureFieldText, Value: cfg.Channels.Email.DefaultTo, EmptyHint: "ops@example.com"}, {Key: "access", Label: "Inbound access", Description: "Choose how inbound email is admitted.", Kind: configureFieldChoice, Value: choice, Choices: accessChoices, ChoiceIndex: indexOfChoice(accessChoices, choice)}, {Key: "allowlist", Label: "Allowed senders", Description: "Comma-separated allowlist used when access = allowlist.", Kind: configureFieldText, Value: strings.Join(cfg.Channels.Email.AllowedSenders, ","), EmptyHint: "owner@example.com"}}
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
	current := false
	for _, field := range buildSectionFields(*cfg, section, "") {
		if field.Key == fieldKey {
			current = field.Value == "on"
			break
		}
	}
	return setToggleFieldValue(cfg, section, channel, fieldKey, !current)
}

func cycleChoiceValue(cfg *config.Config, section, channel, fieldKey string, delta int) bool {
	if cfg == nil {
		return false
	}
	if section != "channels" {
		for _, field := range buildSectionFields(*cfg, section, "") {
			if field.Key == fieldKey && len(field.Choices) > 0 {
				next := field.Choices[wrapIndex(indexOfChoice(field.Choices, field.Value)+delta, len(field.Choices))]
				changed, err := applyChoiceSelection(cfg, section, channel, fieldKey, next)
				return err == nil && changed
			}
		}
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

func applyFieldValue(cfg *config.Config, section, channel, fieldKey, value string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	clearRequested := strings.EqualFold(strings.TrimSpace(value), configureSecretClearKeyword)
	if section == "channels" {
		switch channel {
		case "telegram":
			switch fieldKey {
			case "token":
				if clearRequested {
					cfg.Channels.Telegram.Token = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Telegram.Token = value
				}
				return true, nil
			case "default_id":
				cfg.Channels.Telegram.DefaultChatID = value
				return true, nil
			case "allowlist":
				cfg.Channels.Telegram.AllowedChatIDs = splitAndCompact(value)
				return true, nil
			}
		case "slack":
			switch fieldKey {
			case "app_token":
				if clearRequested {
					cfg.Channels.Slack.AppToken = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Slack.AppToken = value
				}
				return true, nil
			case "bot_token":
				if clearRequested {
					cfg.Channels.Slack.BotToken = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Slack.BotToken = value
				}
				return true, nil
			case "default_id":
				cfg.Channels.Slack.DefaultChannelID = value
				return true, nil
			case "allowlist":
				cfg.Channels.Slack.AllowedUserIDs = splitAndCompact(value)
				return true, nil
			}
		case "discord":
			switch fieldKey {
			case "token":
				if clearRequested {
					cfg.Channels.Discord.Token = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Discord.Token = value
				}
				return true, nil
			case "default_id":
				cfg.Channels.Discord.DefaultChannelID = value
				return true, nil
			case "allowlist":
				cfg.Channels.Discord.AllowedUserIDs = splitAndCompact(value)
				return true, nil
			}
		case "whatsapp":
			switch fieldKey {
			case "bridge_url":
				cfg.Channels.WhatsApp.BridgeURL = value
				return true, nil
			case "bridge_token":
				if clearRequested {
					cfg.Channels.WhatsApp.BridgeToken = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.WhatsApp.BridgeToken = value
				}
				return true, nil
			case "default_to":
				cfg.Channels.WhatsApp.DefaultTo = value
				return true, nil
			case "allowlist":
				cfg.Channels.WhatsApp.AllowedFrom = splitAndCompact(value)
				return true, nil
			}
		case "email":
			switch fieldKey {
			case "imap_host":
				cfg.Channels.Email.IMAPHost = value
				return true, nil
			case "imap_user":
				cfg.Channels.Email.IMAPUsername = value
				return true, nil
			case "imap_password":
				if clearRequested {
					cfg.Channels.Email.IMAPPassword = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Email.IMAPPassword = value
				}
				return true, nil
			case "smtp_host":
				cfg.Channels.Email.SMTPHost = value
				return true, nil
			case "smtp_user":
				cfg.Channels.Email.SMTPUsername = value
				return true, nil
			case "smtp_password":
				if clearRequested {
					cfg.Channels.Email.SMTPPassword = ""
					return true, nil
				}
				if value != "" {
					cfg.Channels.Email.SMTPPassword = value
				}
				return true, nil
			case "from_address":
				cfg.Channels.Email.FromAddress = value
				return true, nil
			case "default_to":
				cfg.Channels.Email.DefaultTo = value
				return true, nil
			case "allowlist":
				cfg.Channels.Email.AllowedSenders = splitAndCompact(value)
				return true, nil
			}
		}
		return false, nil
	}
	switch fieldKey {
	case "provider_api_base":
		cfg.Provider.APIBase = value
		return true, nil
	case "provider_model":
		cfg.Provider.Model = value
		return true, nil
	case "provider_embed":
		cfg.Provider.EmbedModel = value
		return true, nil
	case "provider_temperature":
		return setFloatValue(&cfg.Provider.Temperature, value, fieldKey)
	case "provider_timeout":
		return setIntValue(&cfg.Provider.TimeoutSeconds, value, fieldKey)
	case "provider_api_key":
		if clearRequested {
			cfg.Provider.APIKey = ""
			return true, nil
		}
		if value != "" {
			cfg.Provider.APIKey = value
		}
		return true, nil
	case "storage_db":
		cfg.DBPath = value
		return true, nil
	case "storage_artifacts":
		cfg.ArtifactsDir = value
		return true, nil
	case "storage_soul":
		cfg.SoulFile = value
		return true, nil
	case "storage_agents":
		cfg.AgentsFile = value
		return true, nil
	case "storage_tools":
		cfg.ToolsFile = value
		return true, nil
	case "storage_identity":
		cfg.IdentityFile = value
		return true, nil
	case "storage_memory":
		cfg.MemoryFile = value
		return true, nil
	case "runtime_default_session":
		cfg.DefaultSessionKey = value
		return true, nil
	case "runtime_bootstrap_max_chars":
		return setIntValue(&cfg.BootstrapMaxChars, value, fieldKey)
	case "runtime_bootstrap_total_chars":
		return setIntValue(&cfg.BootstrapTotalMaxChars, value, fieldKey)
	case "runtime_session_cache":
		return setIntValue(&cfg.SessionCache, value, fieldKey)
	case "runtime_history_max":
		return setIntValue(&cfg.HistoryMax, value, fieldKey)
	case "runtime_max_tool_bytes":
		return setIntValue(&cfg.MaxToolBytes, value, fieldKey)
	case "runtime_max_media_bytes":
		return setIntValue(&cfg.MaxMediaBytes, value, fieldKey)
	case "runtime_max_tool_loops":
		return setIntValue(&cfg.MaxToolLoops, value, fieldKey)
	case "runtime_memory_retrieve":
		return setIntValue(&cfg.MemoryRetrieve, value, fieldKey)
	case "runtime_vector_k":
		return setIntValue(&cfg.VectorK, value, fieldKey)
	case "runtime_fts_k":
		return setIntValue(&cfg.FTSK, value, fieldKey)
	case "runtime_vector_scan_limit":
		return setIntValue(&cfg.VectorScanLimit, value, fieldKey)
	case "runtime_worker_count":
		return setIntValue(&cfg.WorkerCount, value, fieldKey)
	case "runtime_consolidation_window":
		return setIntValue(&cfg.ConsolidationWindowSize, value, fieldKey)
	case "runtime_consolidation_max_messages":
		return setIntValue(&cfg.ConsolidationMaxMessages, value, fieldKey)
	case "runtime_consolidation_max_input_chars":
		return setIntValue(&cfg.ConsolidationMaxInputChars, value, fieldKey)
	case "runtime_consolidation_async_timeout":
		return setIntValue(&cfg.ConsolidationAsyncTimeoutSeconds, value, fieldKey)
	case "runtime_subagents_max_concurrent":
		return setIntValue(&cfg.Subagents.MaxConcurrent, value, fieldKey)
	case "runtime_subagents_max_queued":
		return setIntValue(&cfg.Subagents.MaxQueued, value, fieldKey)
	case "runtime_subagents_timeout":
		return setIntValue(&cfg.Subagents.TaskTimeoutSeconds, value, fieldKey)
	case "workspace_dir":
		cfg.WorkspaceDir = value
		return true, nil
	case "workspace_allowed_dir":
		cfg.AllowedDir = value
		return true, nil
	case "tools_brave":
		if clearRequested {
			cfg.Tools.BraveAPIKey = ""
			return true, nil
		}
		if value != "" {
			cfg.Tools.BraveAPIKey = value
		}
		return true, nil
	case "tools_web_proxy":
		cfg.Tools.WebProxy = value
		return true, nil
	case "tools_exec_timeout":
		return setIntValue(&cfg.Tools.ExecTimeoutSeconds, value, fieldKey)
	case "tools_path_append":
		cfg.Tools.PathAppend = value
		return true, nil
	case "docindex_roots":
		cfg.DocIndex.Roots = splitAndCompact(value)
		return true, nil
	case "docindex_max_files":
		return setIntValue(&cfg.DocIndex.MaxFiles, value, fieldKey)
	case "docindex_max_file_bytes":
		return setIntValue(&cfg.DocIndex.MaxFileBytes, value, fieldKey)
	case "docindex_max_chunks":
		return setIntValue(&cfg.DocIndex.MaxChunks, value, fieldKey)
	case "docindex_embed_max_bytes":
		return setIntValue(&cfg.DocIndex.EmbedMaxBytes, value, fieldKey)
	case "docindex_refresh_seconds":
		return setIntValue(&cfg.DocIndex.RefreshSeconds, value, fieldKey)
	case "docindex_retrieve_limit":
		return setIntValue(&cfg.DocIndex.RetrieveLimit, value, fieldKey)
	case "skills_max_run_seconds":
		return setIntValue(&cfg.Skills.MaxRunSeconds, value, fieldKey)
	case "skills_managed_dir":
		cfg.Skills.ManagedDir = value
		return true, nil
	case "skills_approved":
		cfg.Skills.Policy.Approved = splitAndCompact(value)
		return true, nil
	case "skills_trusted_owners":
		cfg.Skills.Policy.TrustedOwners = splitAndCompact(value)
		return true, nil
	case "skills_blocked_owners":
		cfg.Skills.Policy.BlockedOwners = splitAndCompact(value)
		return true, nil
	case "skills_trusted_registries":
		cfg.Skills.Policy.TrustedRegistries = splitAndCompact(value)
		return true, nil
	case "skills_extra_dirs":
		cfg.Skills.Load.ExtraDirs = splitAndCompact(value)
		return true, nil
	case "skills_watch_debounce":
		return setIntValue(&cfg.Skills.Load.WatchDebounceMS, value, fieldKey)
	case "skills_clawhub_site":
		cfg.Skills.ClawHub.SiteURL = value
		return true, nil
	case "skills_clawhub_registry":
		cfg.Skills.ClawHub.RegistryURL = value
		return true, nil
	case "skills_clawhub_install":
		cfg.Skills.ClawHub.InstallDir = value
		return true, nil
	case "security_secret_store_key_file":
		cfg.Security.SecretStore.KeyFile = value
		return true, nil
	case "security_audit_key_file":
		cfg.Security.Audit.KeyFile = value
		return true, nil
	case "security_approvals_host_id":
		cfg.Security.Approvals.HostID = value
		return true, nil
	case "security_approvals_key_file":
		cfg.Security.Approvals.KeyFile = value
		return true, nil
	case "security_approvals_pairing_ttl":
		return setIntValue(&cfg.Security.Approvals.PairingCodeTTLSeconds, value, fieldKey)
	case "security_approvals_pending_ttl":
		return setIntValue(&cfg.Security.Approvals.PendingTTLSeconds, value, fieldKey)
	case "security_approvals_token_ttl":
		return setIntValue(&cfg.Security.Approvals.ApprovalTokenTTLSeconds, value, fieldKey)
	case "security_profiles_default":
		cfg.Security.Profiles.Default = value
		return true, nil
	case "security_profiles_channels":
		mapping, err := parseStringMap(value)
		if err != nil {
			return false, err
		}
		cfg.Security.Profiles.Channels = mapping
		return true, nil
	case "security_profiles_triggers":
		mapping, err := parseStringMap(value)
		if err != nil {
			return false, err
		}
		cfg.Security.Profiles.Triggers = mapping
		return true, nil
	case "security_network_allowed_hosts":
		cfg.Security.Network.AllowedHosts = splitAndCompact(value)
		return true, nil
	case "hardening_exec_allowed_programs":
		cfg.Hardening.ExecAllowedPrograms = splitAndCompact(value)
		return true, nil
	case "hardening_child_env_allowlist":
		cfg.Hardening.ChildEnvAllowlist = splitAndCompact(value)
		return true, nil
	case "hardening_sandbox_bwrap":
		cfg.Hardening.Sandbox.BubblewrapPath = value
		return true, nil
	case "hardening_sandbox_writable_paths":
		cfg.Hardening.Sandbox.WritablePaths = splitAndCompact(value)
		return true, nil
	case "hardening_max_tool_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxToolCalls, value, fieldKey)
	case "hardening_max_exec_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxExecCalls, value, fieldKey)
	case "hardening_max_web_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxWebCalls, value, fieldKey)
	case "hardening_max_subagent_calls":
		return setIntValue(&cfg.Hardening.Quotas.MaxSubagentCalls, value, fieldKey)
	case "session_identity_links":
		links, err := parseIdentityLinks(value)
		if err != nil {
			return false, err
		}
		cfg.Session.IdentityLinks = links
		return true, nil
	case "automation_cron_store_path":
		cfg.Cron.StorePath = value
		return true, nil
	case "automation_heartbeat_interval":
		return setIntValue(&cfg.Heartbeat.IntervalMinutes, value, fieldKey)
	case "automation_heartbeat_tasks_file":
		cfg.Heartbeat.TasksFile = value
		return true, nil
	case "automation_heartbeat_session":
		cfg.Heartbeat.SessionKey = value
		return true, nil
	case "automation_webhook_addr":
		cfg.Triggers.Webhook.Addr = value
		return true, nil
	case "automation_webhook_secret":
		if clearRequested {
			cfg.Triggers.Webhook.Secret = ""
			return true, nil
		}
		if value != "" {
			cfg.Triggers.Webhook.Secret = value
		}
		return true, nil
	case "automation_webhook_max_body_kb":
		return setIntValue(&cfg.Triggers.Webhook.MaxBodyKB, value, fieldKey)
	case "automation_filewatch_paths":
		cfg.Triggers.FileWatch.Paths = splitAndCompact(value)
		return true, nil
	case "automation_filewatch_poll_seconds":
		return setIntValue(&cfg.Triggers.FileWatch.PollSeconds, value, fieldKey)
	case "automation_filewatch_debounce":
		return setIntValue(&cfg.Triggers.FileWatch.DebounceSeconds, value, fieldKey)
	case "service_listen":
		cfg.Service.Listen = value
		return true, nil
	case "service_secret":
		if clearRequested {
			cfg.Service.Secret = ""
			return true, nil
		}
		if value != "" {
			cfg.Service.Secret = value
		}
		return true, nil
	default:
		return false, nil
	}
}

func setToggleFieldValue(cfg *config.Config, section, channel, fieldKey string, value bool) bool {
	if cfg == nil {
		return false
	}
	if section == "channels" {
		return false
	}
	switch fieldKey {
	case "provider_vision":
		cfg.Provider.EnableVision = value
	case "runtime_consolidation_enabled":
		cfg.ConsolidationEnabled = value
	case "runtime_subagents_enabled":
		cfg.Subagents.Enabled = value
	case "workspace_restrict":
		cfg.Tools.RestrictToWorkspace = value
	case "docindex_enabled":
		cfg.DocIndex.Enabled = value
	case "skills_enable_exec":
		cfg.Skills.EnableExec = value
	case "skills_quarantine":
		cfg.Skills.Policy.QuarantineByDefault = value
	case "skills_watch":
		cfg.Skills.Load.Watch = value
	case "security_secret_store_enabled":
		cfg.Security.SecretStore.Enabled = value
	case "security_secret_store_required":
		cfg.Security.SecretStore.Required = value
	case "security_audit_enabled":
		cfg.Security.Audit.Enabled = value
	case "security_audit_strict":
		cfg.Security.Audit.Strict = value
	case "security_audit_verify_on_start":
		cfg.Security.Audit.VerifyOnStart = value
	case "security_approvals_enabled":
		cfg.Security.Approvals.Enabled = value
	case "security_profiles_enabled":
		cfg.Security.Profiles.Enabled = value
	case "security_network_enabled":
		cfg.Security.Network.Enabled = value
	case "security_network_default_deny":
		cfg.Security.Network.DefaultDeny = value
	case "security_network_allow_loopback":
		cfg.Security.Network.AllowLoopback = value
	case "security_network_allow_private":
		cfg.Security.Network.AllowPrivate = value
	case "hardening_guarded_tools":
		cfg.Hardening.GuardedTools = value
	case "hardening_privileged_tools":
		cfg.Hardening.PrivilegedTools = value
	case "hardening_exec_shell":
		cfg.Hardening.EnableExecShell = value
	case "hardening_isolate_channel_peers":
		cfg.Hardening.IsolateChannelPeers = value
	case "hardening_sandbox_enabled":
		cfg.Hardening.Sandbox.Enabled = value
	case "hardening_sandbox_allow_network":
		cfg.Hardening.Sandbox.AllowNetwork = value
	case "hardening_quotas_enabled":
		cfg.Hardening.Quotas.Enabled = value
	case "session_direct_messages_share_default":
		cfg.Session.DirectMessagesShareDefault = value
	case "automation_cron_enabled":
		cfg.Cron.Enabled = value
	case "automation_heartbeat_enabled":
		cfg.Heartbeat.Enabled = value
	case "automation_webhook_enabled":
		cfg.Triggers.Webhook.Enabled = value
	case "automation_filewatch_enabled":
		cfg.Triggers.FileWatch.Enabled = value
	case "service_enabled":
		cfg.Service.Enabled = value
	default:
		return false
	}
	return true
}

func applyChoiceSelection(cfg *config.Config, section, channel, fieldKey, choice string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	if section == "channels" && fieldKey == "access" {
		applyAccessChoice(cfg, channel, choice)
		return true, nil
	}
	switch fieldKey {
	case "provider_preset":
		switch choice {
		case "OpenAI":
			applyProviderPreset(cfg, "1")
		case "OpenRouter":
			applyProviderPreset(cfg, "2")
		default:
			applyProviderPreset(cfg, "3")
		}
		return true, nil
	case "runtime_profile":
		if choice == "default" {
			cfg.RuntimeProfile = ""
		} else {
			cfg.RuntimeProfile = config.RuntimeProfile(choice)
		}
		return true, nil
	case "security_approval_pairing_mode":
		cfg.Security.Approvals.Pairing.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_exec_mode":
		cfg.Security.Approvals.Exec.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_skill_mode":
		cfg.Security.Approvals.SkillExecution.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_secret_mode":
		cfg.Security.Approvals.SecretAccess.Mode = config.ApprovalMode(choice)
		return true, nil
	case "security_approval_message_mode":
		cfg.Security.Approvals.MessageSend.Mode = config.ApprovalMode(choice)
		return true, nil
	default:
		return false, nil
	}
}

func currentSecretValue(cfg config.Config, section, fieldKey string) string {
	switch fieldKey {
	case "provider_api_key":
		return cfg.Provider.APIKey
	case "tools_brave":
		return cfg.Tools.BraveAPIKey
	case "automation_webhook_secret":
		return cfg.Triggers.Webhook.Secret
	case "service_secret":
		return cfg.Service.Secret
	default:
		return ""
	}
}

func setIntValue(target *int, value string, field string) (bool, error) {
	if target == nil {
		return false, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("invalid integer for %s: %q", field, value)
	}
	*target = parsed
	return true, nil
}

func setFloatValue(target *float64, value string, field string) (bool, error) {
	if target == nil {
		return false, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return false, fmt.Errorf("invalid number for %s: %q", field, value)
	}
	*target = parsed
	return true, nil
}

func formatInt(value int) string { return strconv.Itoa(value) }

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatStringMap(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ",")
}

func parseStringMap(value string) (map[string]string, error) {
	items := splitAndCompact(value)
	result := map[string]string{}
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid mapping %q (expected key=value)", item)
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func formatIdentityLinks(links []config.SessionIdentityLink) string {
	if len(links) == 0 {
		return ""
	}
	parts := make([]string, 0, len(links))
	for _, link := range links {
		canonical := strings.TrimSpace(link.Canonical)
		if canonical == "" {
			continue
		}
		parts = append(parts, canonical+"="+strings.Join(compactStrings(link.Peers), "|"))
	}
	return strings.Join(parts, ";")
}

func parseIdentityLinks(value string) ([]config.SessionIdentityLink, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []config.SessionIdentityLink{}, nil
	}
	entries := strings.Split(value, ";")
	links := make([]config.SessionIdentityLink, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid identity link %q (expected canonical=peer1|peer2)", entry)
		}
		canonical := strings.TrimSpace(parts[0])
		if canonical == "" {
			return nil, fmt.Errorf("invalid identity link %q (missing canonical identity)", entry)
		}
		peers := compactStrings(strings.Split(parts[1], "|"))
		if len(peers) == 0 {
			return nil, fmt.Errorf("invalid identity link %q (missing peers)", entry)
		}
		links = append(links, config.SessionIdentityLink{Canonical: canonical, Peers: peers})
	}
	return links, nil
}

func compactStrings(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			items = append(items, value)
		}
	}
	return items
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
