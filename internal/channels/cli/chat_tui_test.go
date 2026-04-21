package cli

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
