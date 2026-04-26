package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"or3-intern/internal/agent"
	"or3-intern/internal/bus"
	"or3-intern/internal/db"
)

const (
	chatHistoryLimit   = 80
	chatActivityLimit  = 10
	chatViewportMinW   = 40
	chatSidebarMinW    = 28
	chatInputMinW      = 24
	chatInputPanelH    = 3
	chatHeaderPanelH   = 4
	chatMessagePadding = 2
)

type historyStore interface {
	GetLastMessagesScoped(ctx context.Context, sessionKey string, limit int) ([]db.Message, error)
	ResolveScopeKey(ctx context.Context, sessionKey string) (string, error)
	ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error)
}

type bubbleChatBridge struct {
	events       chan tea.Msg
	done         chan struct{}
	closeOnce    sync.Once
	nextStreamID atomic.Int64
}

func newBubbleChatBridge() *bubbleChatBridge {
	return &bubbleChatBridge{events: make(chan tea.Msg, 256), done: make(chan struct{})}
}

type chatBridgeClosedMsg struct{}

func (b *bubbleChatBridge) waitCmd() tea.Cmd {
	return func() tea.Msg {
		if b == nil {
			return chatBridgeClosedMsg{}
		}
		select {
		case msg := <-b.events:
			return msg
		case <-b.done:
			return chatBridgeClosedMsg{}
		}
	}
}

func (b *bubbleChatBridge) emit(msg tea.Msg) bool {
	if b == nil || msg == nil {
		return false
	}
	select {
	case <-b.done:
		return false
	case b.events <- msg:
		return true
	default:
		return false
	}
}

func (b *bubbleChatBridge) close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		close(b.done)
	})
}

func (b *bubbleChatBridge) newStreamID() int {
	if b == nil {
		return 0
	}
	return int(b.nextStreamID.Add(1))
}

type chatHistoryLoadedMsg struct {
	sessionKey    string
	scopeKey      string
	scopeSessions []string
	items         []chatMessage
	err           error
}

type chatAssistantDeltaMsg struct {
	sessionKey string
	streamID   int
	text       string
}

type chatAssistantCloseMsg struct {
	sessionKey string
	streamID   int
	finalText  string
	complete   bool
}

type chatAssistantAbortMsg struct {
	sessionKey string
	streamID   int
}

type chatErrorMsg struct {
	sessionKey string
	err        string
}

type chatNoticeMsg struct {
	sessionKey string
	text       string
}

type chatRuntimeLogMsg struct {
	sessionKey string
	text       string
	kind       string
}

type chatSessionResetMsg struct {
	sessionKey string
	notice     string
}

type chatToolCallMsg struct {
	sessionKey string
	name       string
	arguments  string
}

type chatToolResultMsg struct {
	sessionKey string
	name       string
	result     string
	err        string
}

type chatMessage struct {
	role    string
	content string
	pending bool
}

type chatActivity struct {
	title  string
	detail string
	kind   string
}

type chatKeyMap struct {
	Send       key.Binding
	Quit       key.Binding
	ClearInput key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	ScrollTop  key.Binding
	ScrollEnd  key.Binding
	Commands   key.Binding
	Session    key.Binding
	ClearView  key.Binding
}

func newChatKeyMap() chatKeyMap {
	return chatKeyMap{
		Send:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		Quit:       key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		ClearInput: key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "clear input")),
		ScrollUp:   key.NewBinding(key.WithKeys("pgup", "shift+up", "up"), key.WithHelp("↑/pgup", "scroll up")),
		ScrollDown: key.NewBinding(key.WithKeys("pgdown", "shift+down", "down"), key.WithHelp("↓/pgdn", "scroll down")),
		ScrollTop:  key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "scroll to top")),
		ScrollEnd:  key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "scroll to end")),
		Commands:   key.NewBinding(key.WithKeys("ctrl+/"), key.WithHelp("ctrl+/", "commands")),
		Session:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "session info")),
		ClearView:  key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear view")),
	}
}

func (k chatKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.ScrollUp, k.ScrollDown, k.Commands, k.Session, k.ClearView, k.Quit}
}

func (k chatKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Send, k.ClearInput, k.Commands, k.Session}, {k.ScrollUp, k.ScrollDown, k.ScrollTop, k.ScrollEnd}, {k.ClearView, k.Quit}}
}

type chatStyles struct {
	app         lipgloss.Style
	header      lipgloss.Style
	title       lipgloss.Style
	subtitle    lipgloss.Style
	panel       lipgloss.Style
	panelTitle  lipgloss.Style
	muted       lipgloss.Style
	status      lipgloss.Style
	inputBox    lipgloss.Style
	userLabel   lipgloss.Style
	assistant   lipgloss.Style
	system      lipgloss.Style
	error       lipgloss.Style
	activity    lipgloss.Style
	badge       lipgloss.Style
	badgeWarm   lipgloss.Style
	badgeCool   lipgloss.Style
	help        lipgloss.Style
	placeholder lipgloss.Style
}

type chatLayout struct {
	panelWidth     int
	transcriptW    int
	sidebarW       int
	viewportH      int
	stacked        bool
	compact        bool
	compactSidebar bool
}

func newChatStyles() chatStyles {
	border := lipgloss.RoundedBorder()
	return chatStyles{
		app:         lipgloss.NewStyle().Padding(0, 1),
		header:      lipgloss.NewStyle().BorderStyle(border).BorderForeground(lipgloss.Color("63")).Padding(0, 1),
		title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")),
		subtitle:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		panel:       lipgloss.NewStyle().BorderStyle(border).BorderForeground(lipgloss.Color("240")).Padding(0, 1),
		panelTitle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111")),
		muted:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		status:      lipgloss.NewStyle().Foreground(lipgloss.Color("229")),
		inputBox:    lipgloss.NewStyle().BorderStyle(border).BorderForeground(lipgloss.Color("69")).Padding(0, 1),
		userLabel:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		assistant:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		system:      lipgloss.NewStyle().Foreground(lipgloss.Color("221")),
		error:       lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true),
		activity:    lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
		badge:       lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1),
		badgeWarm:   lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("166")).Padding(0, 1),
		badgeCool:   lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("32")).Padding(0, 1),
		help:        lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
}

type chatModel struct {
	ctx           context.Context
	bridge        *bubbleChatBridge
	store         historyStore
	publish       func(sessionKey, text string) bool
	width         int
	height        int
	sessionKey    string
	scopeKey      string
	scopeSessions []string
	viewport      viewport.Model
	input         textinput.Model
	keys          chatKeyMap
	styles        chatStyles
	messages      []chatMessage
	activity      []chatActivity
	streamIndex   map[int]int
	pendingCount  int
	statusText    string
	historyLimit  int
}

func newChatModel(ctx context.Context, sessionKey string, bridge *bubbleChatBridge, store historyStore, publish func(sessionKey, text string) bool) chatModel {
	input := textinput.New()
	input.Placeholder = "Ask anything, or type /commands"
	input.Prompt = ""
	input.CharLimit = 0
	input.Focus()
	input.Width = 80
	vp := viewport.New(80, 20)
	vp.YPosition = 0
	vp.MouseWheelEnabled = true
	return chatModel{
		ctx:          ctx,
		bridge:       bridge,
		store:        store,
		publish:      publish,
		sessionKey:   strings.TrimSpace(sessionKey),
		viewport:     vp,
		input:        input,
		keys:         newChatKeyMap(),
		styles:       newChatStyles(),
		streamIndex:  map[int]int{},
		statusText:   "Ready",
		historyLimit: chatHistoryLimit,
	}
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.bridge.waitCmd(), loadChatHistoryCmd(m.ctx, m.store, m.sessionKey, m.historyLimit))
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		m.refreshViewport(true)
	case tea.MouseMsg:
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
		return m, tea.Batch(cmds...)
	case chatBridgeClosedMsg:
		return m, nil
	case tea.KeyMsg:
		handledKey := false
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.ClearInput):
			handledKey = true
			m.input.SetValue("")
		case key.Matches(msg, m.keys.ScrollUp):
			handledKey = true
			m.viewport.LineUp(3)
		case key.Matches(msg, m.keys.ScrollDown):
			handledKey = true
			m.viewport.LineDown(3)
		case key.Matches(msg, m.keys.ScrollTop):
			handledKey = true
			m.viewport.GotoTop()
		case key.Matches(msg, m.keys.ScrollEnd):
			handledKey = true
			m.viewport.GotoBottom()
		case key.Matches(msg, m.keys.Commands):
			handledKey = true
			m.appendSystem(localCommandsHelp())
			m.refreshViewport(true)
		case key.Matches(msg, m.keys.Session):
			handledKey = true
			m.appendSystem(m.sessionSummary())
			m.refreshViewport(true)
		case key.Matches(msg, m.keys.ClearView):
			handledKey = true
			m.messages = nil
			m.activity = nil
			m.streamIndex = map[int]int{}
			m.statusText = "Transcript cleared"
			m.refreshViewport(true)
		case key.Matches(msg, m.keys.Send):
			handledKey = true
			line := strings.TrimSpace(m.input.Value())
			if line == "" {
				break
			}
			m.input.SetValue("")
			if handled, cmd := m.handleLocalCommand(line); handled {
				m.refreshViewport(true)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				break
			}
			if line == "/new" {
				m.statusText = "Starting new session…"
			} else if line == "/prune" {
				m.statusText = "Pruning context…"
			} else if line == "/status" {
				m.statusText = "Loading status…"
			}
			m.messages = append(m.messages, chatMessage{role: "user", content: line})
			if m.publish == nil || !m.publish(m.sessionKey, line) {
				m.appendError("queue full — message dropped")
			} else {
				m.pendingCount++
				if line != "/new" && line != "/status" && line != "/prune" {
					m.statusText = "Thinking…"
				}
			}
			m.refreshViewport(true)
		}
		if handledKey {
			return m, tea.Batch(cmds...)
		}
	case chatHistoryLoadedMsg:
		if msg.err != nil {
			m.appendError("load history: " + msg.err.Error())
		} else if msg.sessionKey == m.sessionKey {
			m.scopeKey = msg.scopeKey
			m.scopeSessions = msg.scopeSessions
			m.messages = msg.items
			m.streamIndex = map[int]int{}
			m.statusText = fmt.Sprintf("Loaded %d messages", len(msg.items))
		}
		m.refreshViewport(true)
	case chatAssistantDeltaMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		idx, ok := m.streamIndex[msg.streamID]
		if !ok {
			m.messages = append(m.messages, chatMessage{role: "assistant", pending: true})
			idx = len(m.messages) - 1
			m.streamIndex[msg.streamID] = idx
		}
		m.messages[idx].content += msg.text
		m.messages[idx].pending = true
		m.statusText = "Streaming response…"
		m.refreshViewport(true)
		cmds = append(cmds, m.bridge.waitCmd())
	case chatAssistantCloseMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		if idx, ok := m.streamIndex[msg.streamID]; ok {
			if strings.TrimSpace(msg.finalText) != "" && strings.TrimSpace(m.messages[idx].content) == "" {
				m.messages[idx].content = msg.finalText
			}
			m.messages[idx].pending = false
			delete(m.streamIndex, msg.streamID)
		} else if strings.TrimSpace(msg.finalText) != "" {
			m.messages = append(m.messages, chatMessage{role: "assistant", content: msg.finalText})
		}
		if msg.complete && m.pendingCount > 0 {
			m.pendingCount--
		}
		if m.pendingCount == 0 {
			m.statusText = "Ready"
		} else {
			m.statusText = "Working…"
		}
		m.refreshViewport(true)
		cmds = append(cmds, m.bridge.waitCmd())
	case chatAssistantAbortMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		m.addActivity("Tool loop", "assistant stream paused while tools run", "tool")
		m.statusText = "Using tools…"
		cmds = append(cmds, m.bridge.waitCmd())
	case chatErrorMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		m.appendError(msg.err)
		if m.pendingCount > 0 {
			m.pendingCount--
		}
		if m.pendingCount == 0 {
			m.statusText = "Error"
		} else {
			m.statusText = "Working…"
		}
		m.refreshViewport(true)
		cmds = append(cmds, m.bridge.waitCmd())
	case chatNoticeMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		m.appendSystem("Runtime\n" + strings.TrimSpace(msg.text))
		m.addActivity("Background", summarizeText(msg.text, 100), "notice")
		if m.pendingCount == 0 {
			m.statusText = "Background notice"
		}
		m.refreshViewport(true)
		cmds = append(cmds, m.bridge.waitCmd())
	case chatRuntimeLogMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		text := strings.TrimSpace(msg.text)
		if text == "" {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		title := "Runtime"
		if msg.kind == "error" {
			title = "Runtime error"
			m.messages = append(m.messages, chatMessage{role: "error", content: title + "\n" + text})
			if m.pendingCount == 0 {
				m.statusText = "Runtime error"
			}
		} else {
			m.appendSystem(title + "\n" + text)
			if m.pendingCount == 0 {
				m.statusText = "Runtime notice"
			}
		}
		m.addActivity(title, summarizeText(text, 100), msg.kind)
		m.refreshViewport(true)
		cmds = append(cmds, m.bridge.waitCmd())
	case chatSessionResetMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		m.messages = nil
		m.activity = nil
		m.streamIndex = map[int]int{}
		m.pendingCount = 0
		m.statusText = fallback(strings.TrimSpace(msg.notice), "New session started.")
		m.refreshViewport(true)
		cmds = append(cmds, m.bridge.waitCmd())
	case chatToolCallMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		m.addActivity("Tool", fmt.Sprintf("%s %s", msg.name, strings.TrimSpace(msg.arguments)), "tool")
		m.statusText = "Using tools…"
		cmds = append(cmds, m.bridge.waitCmd())
	case chatToolResultMsg:
		if !m.acceptsSessionEvent(msg.sessionKey) {
			cmds = append(cmds, m.bridge.waitCmd())
			break
		}
		detail := msg.result
		if strings.TrimSpace(msg.err) != "" {
			detail = msg.err
		}
		m.addActivity("Result", fmt.Sprintf("%s · %s", msg.name, summarizeText(detail, 80)), "result")
		if m.pendingCount == 0 {
			m.statusText = "Ready"
		}
		cmds = append(cmds, m.bridge.waitCmd())
	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)
	return m, tea.Batch(cmds...)
}

func (m chatModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading chat UI..."
	}
	layout := deriveChatLayout(m.width, m.height)
	header := m.renderHeader(layout)
	transcriptPanel := m.styles.panel.Width(layout.transcriptW).Height(maxInt(7, m.viewport.Height+2)).Render(m.styles.panelTitle.Render("Conversation") + "\n" + m.viewport.View())
	var content string
	if layout.stacked {
		sidebarContent := m.renderSidebar(layout.panelWidth)
		if layout.compactSidebar {
			sidebarContent = m.renderSidebarCompact(layout.panelWidth)
		}
		sidebar := m.styles.panel.Width(layout.panelWidth).Render(sidebarContent)
		content = lipgloss.JoinVertical(lipgloss.Left, transcriptPanel, sidebar)
	} else {
		sidebar := m.styles.panel.Width(layout.sidebarW).Height(maxInt(7, m.viewport.Height+2)).Render(m.renderSidebar(layout.sidebarW))
		content = lipgloss.JoinHorizontal(lipgloss.Top, transcriptPanel, "  ", sidebar)
	}
	inputTitle := m.styles.panelTitle.Render("Message") + "  " + m.styles.status.Render(m.statusLabel())
	inputPanel := m.styles.inputBox.Width(layout.panelWidth).Render(inputTitle + "\n" + m.input.View())
	footer := m.renderFooter(layout)
	return m.styles.app.Render(lipgloss.JoinVertical(lipgloss.Left, header, content, inputPanel, footer))
}

func (m *chatModel) resize() {
	layout := deriveChatLayout(m.width, m.height)
	m.viewport.Width = maxInt(20, layout.transcriptW-4)
	m.viewport.Height = layout.viewportH
	m.input.Width = maxInt(chatInputMinW, layout.panelWidth-6)
}

func (m *chatModel) renderHeader(layout chatLayout) string {
	title := m.styles.title.Render("or3-intern chat")
	session := m.styles.badge.Render("session " + safeLabel(m.sessionKey, "default"))
	scope := m.styles.badgeCool.Render("scope " + safeLabel(m.scopeKey, m.sessionKey))
	subtitleText := "Slash commands, live tool activity, scoped history, and a proper full-screen terminal UI."
	if layout.compact {
		subtitleText = "Slash commands, activity, and scoped history."
	}
	subtitle := m.styles.subtitle.Render(subtitleText)
	if layout.compact {
		return m.styles.header.Width(layout.panelWidth).Render(lipgloss.JoinVertical(lipgloss.Left, title, lipgloss.JoinHorizontal(lipgloss.Left, session, " ", scope), subtitle))
	}
	return m.styles.header.Width(layout.panelWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", session, " ", scope), subtitle))
}

func (m *chatModel) renderSidebar(width int) string {
	sections := []string{
		m.styles.panelTitle.Render("Status"),
		m.styles.muted.Render("Pending turns: ") + fmt.Sprintf("%d", m.pendingCount),
		m.styles.muted.Render("Scope sessions: ") + fmt.Sprintf("%d", len(m.scopeSessions)),
		"",
		m.styles.panelTitle.Render("Slash commands"),
		m.styles.activity.Render("/commands  /status  /session  /session <key>\n/scope  /clear  /new  /prune  /exit"),
		"",
		m.styles.panelTitle.Render("Recent activity"),
	}
	if len(m.activity) == 0 {
		sections = append(sections, m.styles.placeholder.Render("No tool activity yet."))
	} else {
		detailLimit := maxInt(20, width-8)
		for _, item := range m.activity {
			sections = append(sections, m.styles.activity.Render("• "+item.title), m.styles.muted.Render("  "+summarizeText(item.detail, detailLimit)))
		}
	}
	return lipgloss.NewStyle().Width(width - 4).Render(strings.Join(sections, "\n"))
}

func (m *chatModel) renderSidebarCompact(width int) string {
	sections := []string{
		m.styles.panelTitle.Render("Status"),
		m.styles.muted.Render("Pending: ") + fmt.Sprintf("%d", m.pendingCount),
		m.styles.muted.Render("Scope sessions: ") + fmt.Sprintf("%d", len(m.scopeSessions)),
		"",
		m.styles.panelTitle.Render("Recent activity"),
	}
	if len(m.activity) == 0 {
		sections = append(sections, m.styles.placeholder.Render("No recent tool activity."))
	} else {
		limit := minInt(3, len(m.activity))
		detailLimit := maxInt(24, width-12)
		for _, item := range m.activity[:limit] {
			sections = append(sections, m.styles.activity.Render("• "+item.title+" · "+summarizeText(item.detail, detailLimit)))
		}
	}
	sections = append(sections, "", m.styles.panelTitle.Render("Commands"), m.styles.activity.Render("/commands  /session  /scope  /clear"))
	return lipgloss.NewStyle().Width(width - 4).Render(strings.Join(sections, "\n"))
}

func (m *chatModel) renderFooter(layout chatLayout) string {
	line := strings.Join([]string{
		"ctrl+/ commands",
		"ctrl+s session",
		"ctrl+l clear",
		"ctrl+c quit",
	}, " • ")
	if layout.compact {
		line = strings.Join([]string{"/commands", "/session", "/clear", "ctrl+c quit"}, " • ")
	}
	return m.styles.help.Width(layout.panelWidth).Render(truncateChatLine(line, maxInt(12, layout.panelWidth-2)))
}

func deriveChatLayout(width, height int) chatLayout {
	panelWidth := maxInt(chatInputMinW, width-4)
	stacked := width > 0 && width < 104
	compact := (width > 0 && width < 82) || (height > 0 && height < 24)
	transcriptW := panelWidth
	sidebarW := panelWidth
	if !stacked {
		transcriptW = maxInt(chatViewportMinW, panelWidth-chatSidebarMinW-2)
		sidebarW = maxInt(chatSidebarMinW, panelWidth-transcriptW-2)
	}
	// header(4) + transcript chrome(panel border 2 + title 1 + viewport pad 1)
	// + input(4) + footer(1) ≈ 13. Stacked adds the sidebar block beneath
	// the transcript so we have to reserve more vertical room for it.
	reserved := 13
	if stacked {
		if compact {
			reserved += 6
		} else {
			reserved += 8
		}
	}
	viewportH := 6
	if height > 0 {
		viewportH = maxInt(6, height-reserved)
	}
	return chatLayout{
		panelWidth:     panelWidth,
		transcriptW:    transcriptW,
		sidebarW:       sidebarW,
		viewportH:      viewportH,
		stacked:        stacked,
		compact:        compact,
		compactSidebar: stacked && compact,
	}
}

func truncateChatLine(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > limit {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return "…"
	}
	return strings.TrimRight(string(runes), " ") + "…"
}

func (m *chatModel) refreshViewport(stickBottom bool) {
	atBottom := stickBottom || m.viewport.AtBottom()
	contentWidth := maxInt(20, m.viewport.Width-chatMessagePadding)
	if len(m.messages) == 0 {
		m.viewport.SetContent(m.styles.placeholder.Render("No messages yet. Try /commands, ask a question, or switch sessions with /session <key>."))
		return
	}
	blocks := make([]string, 0, len(m.messages))
	for _, item := range m.messages {
		blocks = append(blocks, renderChatMessage(m.styles, item, contentWidth))
	}
	m.viewport.SetContent(strings.Join(blocks, "\n\n"))
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *chatModel) acceptsSessionEvent(sessionKey string) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	return sessionKey == "" || sessionKey == m.sessionKey
}

func (m *chatModel) appendSystem(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m.messages = append(m.messages, chatMessage{role: "system", content: text})
}

func (m *chatModel) appendError(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m.messages = append(m.messages, chatMessage{role: "error", content: text})
	if m.pendingCount > 0 {
		m.pendingCount--
	}
	if strings.TrimSpace(m.statusText) == "" {
		m.statusText = "Error"
	}
}

func (m *chatModel) addActivity(title, detail, kind string) {
	m.activity = append([]chatActivity{{title: title, detail: strings.TrimSpace(detail), kind: kind}}, m.activity...)
	if len(m.activity) > chatActivityLimit {
		m.activity = m.activity[:chatActivityLimit]
	}
}

func (m *chatModel) handleLocalCommand(line string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(line)
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return true, nil
	}
	switch parts[0] {
	case "/exit", "/quit":
		return true, tea.Quit
	case "/commands", "/help":
		m.appendSystem(localCommandsHelp())
		return true, nil
	case "/clear":
		m.messages = nil
		m.activity = nil
		m.streamIndex = map[int]int{}
		m.statusText = "Transcript cleared"
		return true, nil
	case "/session":
		if len(parts) == 1 {
			m.appendSystem(m.sessionSummary())
			return true, nil
		}
		m.sessionKey = strings.TrimSpace(parts[1])
		m.scopeKey = ""
		m.scopeSessions = nil
		m.pendingCount = 0
		m.messages = nil
		m.activity = nil
		m.streamIndex = map[int]int{}
		m.statusText = "Switched session to " + m.sessionKey
		return true, loadChatHistoryCmd(m.ctx, m.store, m.sessionKey, m.historyLimit)
	case "/scope":
		m.appendSystem(scopeSummary(m.scopeKey, m.scopeSessions, m.sessionKey))
		return true, nil
	default:
		return false, nil
	}
}

func (m *chatModel) sessionSummary() string {
	return fmt.Sprintf("Current session: %s\nCurrent scope: %s\nScoped sessions: %s", safeLabel(m.sessionKey, "default"), safeLabel(m.scopeKey, m.sessionKey), strings.Join(nonEmptyOrDefault(m.scopeSessions, []string{m.sessionKey}), ", "))
}

func (m *chatModel) statusLabel() string {
	if m.pendingCount > 0 {
		return fmt.Sprintf("%s · %d request(s) in flight", fallback(m.statusText, "Working…"), m.pendingCount)
	}
	return fallback(m.statusText, "Ready")
}

func renderChatMessage(styles chatStyles, item chatMessage, width int) string {
	content := lipgloss.NewStyle().Width(width).Render(strings.TrimSpace(item.content))
	switch item.role {
	case "user":
		return styles.userLabel.Render("You") + "\n" + content
	case "assistant":
		label := "or3-intern"
		if item.pending {
			label += " · typing…"
		}
		return styles.panelTitle.Render(label) + "\n" + styles.assistant.Render(content)
	case "system":
		return styles.system.Render("System\n" + content)
	case "error":
		return styles.error.Render("Error\n" + content)
	default:
		return content
	}
}

func localCommandsHelp() string {
	return strings.Join([]string{
		"Available local commands:",
		"/commands or /help  Show this command list",
		"/session             Show current session and scope",
		"/session <key>       Switch to another session and load its history",
		"/scope               Show all sessions linked to the current scope",
		"/status              Show runtime context, memory, and token budget status",
		"/clear               Clear only the current on-screen transcript",
		"/new                 Clear backend history for the current session",
		"/prune               Archive recent chat into memory and clear live context",
		"/exit or /quit       Leave chat",
		"Any other slash command is forwarded to the runtime, including skill commands.",
	}, "\n")
}

func scopeSummary(scopeKey string, sessions []string, sessionKey string) string {
	return fmt.Sprintf("Scope: %s\nSessions: %s", safeLabel(scopeKey, sessionKey), strings.Join(nonEmptyOrDefault(sessions, []string{sessionKey}), ", "))
}

func loadChatHistoryCmd(ctx context.Context, store historyStore, sessionKey string, limit int) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return chatHistoryLoadedMsg{sessionKey: sessionKey, scopeKey: sessionKey}
		}
		messages, err := store.GetLastMessagesScoped(ctx, sessionKey, limit)
		if err != nil {
			return chatHistoryLoadedMsg{sessionKey: sessionKey, err: err}
		}
		scopeKey, err := store.ResolveScopeKey(ctx, sessionKey)
		if err != nil {
			scopeKey = sessionKey
		}
		scopeSessions, err := store.ListScopeSessions(ctx, scopeKey)
		if err != nil || len(scopeSessions) == 0 {
			scopeSessions = []string{sessionKey}
		}
		items := make([]chatMessage, 0, len(messages))
		for _, msg := range messages {
			role := msg.Role
			if role == "" {
				role = "system"
			}
			items = append(items, chatMessage{role: role, content: msg.Content})
		}
		return chatHistoryLoadedMsg{sessionKey: sessionKey, scopeKey: scopeKey, scopeSessions: scopeSessions, items: items}
	}
}

func (c *Channel) runBubbleTea(ctx context.Context) error {
	bridge := newBubbleChatBridge()
	defer bridge.close()
	if c.Deliverer != nil {
		c.Deliverer.SetBridge(bridge)
		defer c.Deliverer.SetBridge(nil)
	}
	restoreLogs := captureBubbleTeaLogs(bridge)
	defer restoreLogs()
	publish := func(sessionKey, text string) bool {
		return c.Bus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "cli", From: "local", Message: text})
	}
	model := newChatModel(ctx, c.SessionKey, bridge, c.History, publish)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func summarizeText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(text) <= limit || limit <= 3 {
		return text
	}
	return text[:limit-1] + "…"
}

func safeLabel(value, fallbackValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackValue
	}
	return value
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func nonEmptyOrDefault(values, fallbackValues []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			trimmed = append(trimmed, value)
		}
	}
	if len(trimmed) == 0 {
		return fallbackValues
	}
	return trimmed
}

type bridgeObserver struct{ bridge *bubbleChatBridge }

type bridgeLogWriter struct{ bridge *bubbleChatBridge }

func (w bridgeLogWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text == "" || w.bridge == nil {
		return len(p), nil
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		w.bridge.emit(chatRuntimeLogMsg{sessionKey: "", text: line, kind: classifyRuntimeLogLine(line)})
	}
	return len(p), nil
}

func captureBubbleTeaLogs(bridge *bubbleChatBridge) func() {
	if bridge == nil {
		return func() {}
	}
	prevWriter := log.Writer()
	log.SetOutput(io.MultiWriter(bridgeLogWriter{bridge: bridge}))
	return func() {
		log.SetOutput(prevWriter)
	}
}

func classifyRuntimeLogLine(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	switch {
	case text == "":
		return "notice"
	case strings.Contains(text, " panic") || strings.Contains(text, "panic:"):
		return "error"
	case strings.Contains(text, " fatal") || strings.Contains(text, "fatal:"):
		return "error"
	case strings.Contains(text, " error") || strings.Contains(text, "error:"):
		return "error"
	case strings.Contains(text, " failed") || strings.Contains(text, "failed:"):
		return "error"
	case strings.Contains(text, " warning") || strings.Contains(text, "warning:"):
		return "error"
	default:
		return "notice"
	}
}

func (o bridgeObserver) OnTextDelta(context.Context, string) {}

func (o bridgeObserver) OnToolCall(ctx context.Context, name string, arguments string) {
	o.bridge.emit(chatToolCallMsg{sessionKey: agent.ConversationSessionFromContext(ctx), name: name, arguments: arguments})
}

func (o bridgeObserver) OnToolResult(ctx context.Context, name string, result string, err error) {
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	o.bridge.emit(chatToolResultMsg{sessionKey: agent.ConversationSessionFromContext(ctx), name: name, result: result, err: errText})
}

func (o bridgeObserver) OnCompletion(context.Context, string, bool) {}

func (o bridgeObserver) OnError(ctx context.Context, err error) {
	if err != nil {
		o.bridge.emit(chatErrorMsg{sessionKey: agent.ConversationSessionFromContext(ctx), err: err.Error()})
	}
}

func (d *Deliverer) Observer() agent.ConversationObserver {
	if d == nil || d.bridge == nil {
		return nil
	}
	return bridgeObserver{bridge: d.bridge}
}
