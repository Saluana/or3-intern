package cli

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"or3-intern/internal/db"
)

type fakeHistoryStore struct {
	messages      []db.Message
	scopeKey      string
	scopeSessions []string
}

func (f fakeHistoryStore) GetLastMessagesScoped(ctx context.Context, sessionKey string, limit int) ([]db.Message, error) {
	_ = ctx
	_ = sessionKey
	_ = limit
	return f.messages, nil
}

func (f fakeHistoryStore) ResolveScopeKey(ctx context.Context, sessionKey string) (string, error) {
	_ = ctx
	if strings.TrimSpace(f.scopeKey) != "" {
		return f.scopeKey, nil
	}
	return sessionKey, nil
}

func (f fakeHistoryStore) ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error) {
	_ = ctx
	_ = scopeKey
	if len(f.scopeSessions) == 0 {
		return nil, nil
	}
	return f.scopeSessions, nil
}

func TestChatModelLocalCommandsRenderAndSwitchSessions(t *testing.T) {
	bridge := newBubbleChatBridge()
	store := fakeHistoryStore{
		messages:      []db.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi there"}},
		scopeKey:      "scope:ops",
		scopeSessions: []string{"cli:default", "ops:review"},
	}
	model := newChatModel(context.Background(), "cli:default", bridge, store, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	model.width = 120
	model.height = 36
	model.resize()

	updated, _ := model.Update(chatHistoryLoadedMsg{sessionKey: "cli:default", scopeKey: "scope:ops", scopeSessions: []string{"cli:default", "ops:review"}, items: []chatMessage{{role: "user", content: "hello"}, {role: "assistant", content: "hi there"}}})
	m := updated.(chatModel)
	m.input.SetValue("/commands")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(chatModel)
	view := m.View()
	if !strings.Contains(view, "Available local commands") {
		t.Fatalf("expected commands help in view, got %q", view)
	}

	m.input.SetValue("/session ops:review")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(chatModel)
	if m.sessionKey != "ops:review" {
		t.Fatalf("expected switched session, got %q", m.sessionKey)
	}
	if cmd == nil {
		t.Fatal("expected history reload command when switching session")
	}
}

func TestChatModelBridgeMessagesShowTranscriptAndActivity(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "cli:default", bridge, nil, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	model.width = 110
	model.height = 34
	model.resize()

	updated, _ := model.Update(chatAssistantDeltaMsg{streamID: 1, text: "Hello"})
	updated, _ = updated.(chatModel).Update(chatAssistantDeltaMsg{streamID: 1, text: " world"})
	updated, _ = updated.(chatModel).Update(chatToolCallMsg{name: "read_file", arguments: "README.md"})
	updated, _ = updated.(chatModel).Update(chatAssistantCloseMsg{streamID: 1, finalText: "Hello world", complete: true})
	m := updated.(chatModel)
	view := m.View()
	if !strings.Contains(view, "Hello world") {
		t.Fatalf("expected assistant transcript in view, got %q", view)
	}
	if !strings.Contains(view, "read_file README.md") {
		t.Fatalf("expected tool activity in sidebar, got %q", view)
	}
	if !strings.Contains(view, "or3-intern chat") {
		t.Fatalf("expected chat header, got %q", view)
	}
}

func TestChatModelSendDoesNotLeaveNewlinesInInput(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "cli:default", bridge, nil, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	model.input.SetValue("hello")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(chatModel)
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected cleared input after send, got %q", got)
	}

	updated, _ = m.Update(chatAssistantCloseMsg{streamID: 1, finalText: "hi", complete: true})
	m = updated.(chatModel)
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected input to stay empty after response, got %q", got)
	}

	m.input.SetValue("")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(chatModel)
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected blank enter to stay empty, got %q", got)
	}
	if strings.Contains(m.input.View(), "\n") {
		t.Fatalf("expected single-line input view, got %q", m.input.View())
	}
}

func TestDeriveChatLayoutStacksAndCompactsOnSmallTerminal(t *testing.T) {
	layout := deriveChatLayout(78, 20)
	if !layout.stacked {
		t.Fatal("expected stacked layout for narrow terminal")
	}
	if !layout.compact {
		t.Fatal("expected compact layout for narrow/short terminal")
	}
	if layout.viewportH < 6 {
		t.Fatalf("expected minimum viewport height, got %d", layout.viewportH)
	}
}

func TestChatModelNarrowViewKeepsTranscriptInputAndCompactStatus(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "cli:default", bridge, nil, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 78, Height: 22})
	updated, _ = updated.(chatModel).Update(chatAssistantCloseMsg{streamID: 1, finalText: "Hello world", complete: true})
	updated, _ = updated.(chatModel).Update(chatToolCallMsg{name: "read_file", arguments: "README.md"})
	m := updated.(chatModel)

	view := m.View()
	if !strings.Contains(view, "Conversation") || !strings.Contains(view, "Message") {
		t.Fatalf("expected transcript and input panels in narrow view, got %q", view)
	}
	if !strings.Contains(view, "Recent activity") {
		t.Fatalf("expected compact activity summary in narrow view, got %q", view)
	}
	if !strings.Contains(view, "Hello world") {
		t.Fatalf("expected transcript content in narrow view, got %q", view)
	}
}

func TestChatModelResizeKeepsTranscriptAcrossLayoutModes(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "cli:default", bridge, nil, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	updated, _ = updated.(chatModel).Update(chatAssistantCloseMsg{streamID: 1, finalText: "Persistent transcript", complete: true})
	updated, _ = updated.(chatModel).Update(tea.WindowSizeMsg{Width: 76, Height: 20})
	m := updated.(chatModel)

	view := m.View()
	if !strings.Contains(view, "Persistent transcript") {
		t.Fatalf("expected transcript content after resize, got %q", view)
	}
	if !strings.Contains(view, "Status") {
		t.Fatalf("expected status panel after resize, got %q", view)
	}
	if !deriveChatLayout(m.width, m.height).stacked {
		t.Fatal("expected stacked mode after narrow resize")
	}
}

func TestChatModelNoticeDoesNotCorruptInput(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "cli:default", bridge, nil, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m := updated.(chatModel)
	m.input.SetValue("still typing")

	updated, _ = m.Update(chatNoticeMsg{text: "consolidation failed: context deadline exceeded"})
	m = updated.(chatModel)

	if got := m.input.Value(); got != "still typing" {
		t.Fatalf("expected input to remain unchanged after notice, got %q", got)
	}
	if strings.Contains(m.input.View(), "\n") {
		t.Fatalf("expected single-line input view after notice, got %q", m.input.View())
	}
	view := m.View()
	if !strings.Contains(view, "Background") {
		t.Fatalf("expected background activity notice in view, got %q", view)
	}
	if !strings.Contains(view, "consolidation failed") {
		t.Fatalf("expected consolidation notice text in view, got %q", view)
	}
}

func TestChatModelNewSessionWaitsForBackendConfirmation(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "cli:default", bridge, nil, func(sessionKey, text string) bool {
		return sessionKey == "cli:default" && text == "/new"
	})
	model.width = 110
	model.height = 30
	model.resize()
	model.messages = []chatMessage{{role: "user", content: "old question"}, {role: "assistant", content: "old answer"}}
	model.input.SetValue("/new")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(chatModel)
	if len(m.messages) < 3 {
		t.Fatalf("expected existing transcript to remain until confirmation, got %#v", m.messages)
	}
	if m.messages[0].content != "old question" || m.messages[1].content != "old answer" {
		t.Fatalf("expected old transcript preserved before confirmation, got %#v", m.messages)
	}
	if m.statusText != "Starting new session…" {
		t.Fatalf("expected in-progress new-session status, got %q", m.statusText)
	}

	updated, _ = m.Update(chatSessionResetMsg{sessionKey: "cli:default", notice: "New session started."})
	m = updated.(chatModel)
	if len(m.messages) != 0 {
		t.Fatalf("expected transcript cleared after reset confirmation, got %#v", m.messages)
	}
	if m.pendingCount != 0 {
		t.Fatalf("expected no pending turns after reset, got %d", m.pendingCount)
	}
	if view := m.View(); strings.Contains(view, "old question") || strings.Contains(view, "old answer") {
		t.Fatalf("expected old transcript removed after reset, got %q", view)
	}
}

func TestChatModelIgnoresBridgeEventsFromOtherSessions(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "ops:review", bridge, nil, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	model.width = 100
	model.height = 28
	model.resize()

	updated, _ := model.Update(chatAssistantCloseMsg{sessionKey: "personal:notes", streamID: 1, finalText: "wrong session", complete: true})
	updated, _ = updated.(chatModel).Update(chatToolCallMsg{sessionKey: "personal:notes", name: "read_file", arguments: "secret.txt"})
	updated, _ = updated.(chatModel).Update(chatErrorMsg{sessionKey: "personal:notes", err: "old session failed"})
	m := updated.(chatModel)

	if len(m.messages) != 0 {
		t.Fatalf("expected no transcript updates from another session, got %#v", m.messages)
	}
	if len(m.activity) != 0 {
		t.Fatalf("expected no activity updates from another session, got %#v", m.activity)
	}
	if view := m.View(); strings.Contains(view, "wrong session") || strings.Contains(view, "secret.txt") || strings.Contains(view, "old session failed") {
		t.Fatalf("expected foreign-session events to be ignored, got %q", view)
	}
}

func TestChatModelFooterRemainsSingleLineInCompactLayout(t *testing.T) {
	bridge := newBubbleChatBridge()
	model := newChatModel(context.Background(), "cli:default", bridge, nil, func(sessionKey, text string) bool {
		_ = sessionKey
		_ = text
		return true
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 72, Height: 20})
	m := updated.(chatModel)

	footer := m.renderFooter(deriveChatLayout(m.width, m.height))
	if got := lipgloss.Height(footer); got != 1 {
		t.Fatalf("expected single-line footer, got height %d: %q", got, footer)
	}
	if strings.Contains(footer, "\n") {
		t.Fatalf("expected footer without wrapping, got %q", footer)
	}
}

func TestBubbleChatBridgeEmitReturnsFalseWhenFullOrClosed(t *testing.T) {
	bridge := newBubbleChatBridge()
	for i := 0; i < cap(bridge.events); i++ {
		if ok := bridge.emit(chatNoticeMsg{text: "notice"}); !ok {
			t.Fatalf("expected emit %d to fit in buffer", i)
		}
	}
	if ok := bridge.emit(chatNoticeMsg{text: "overflow"}); ok {
		t.Fatal("expected emit to fail when the bridge buffer is full")
	}
	bridge.close()
	if ok := bridge.emit(chatNoticeMsg{text: "after-close"}); ok {
		t.Fatal("expected emit to fail after bridge close")
	}
}

func TestBubbleChatBridgeWaitCmdReturnsClosedMessage(t *testing.T) {
	bridge := newBubbleChatBridge()
	cmd := bridge.waitCmd()
	bridge.close()
	if msg := cmd(); msg != (chatBridgeClosedMsg{}) {
		t.Fatalf("expected chatBridgeClosedMsg, got %T", msg)
	}
}
