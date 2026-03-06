package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
)

// fakeProvider wraps providers.Client-shaped Chat/Embed to avoid real network calls.
// We override behaviour using a hook approach by constructing a minimal server.
// Since providers.Client is concrete, we use a simple test helper that
// exercises only the Consolidator logic, not the network.

func openConsolidateTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// TestConsolidator_NilProvider is a no-op when provider is nil.
func TestConsolidator_NilProvider(t *testing.T) {
	d := openConsolidateTestDB(t)
	c := &Consolidator{DB: d}
	if err := c.MaybeConsolidate(context.Background(), "sess", 10); err != nil {
		t.Fatalf("expected nil error for nil provider, got: %v", err)
	}
}

// TestConsolidator_NoSession returns nil when session does not exist.
func TestConsolidator_NoSession(t *testing.T) {
	d := openConsolidateTestDB(t)
	// Provide a non-nil provider (won't be called because session not found).
	c := &Consolidator{DB: d, Provider: &providers.Client{}, WindowSize: 5}
	if err := c.MaybeConsolidate(context.Background(), "no-such-session", 10); err != nil {
		t.Fatalf("expected nil error for missing session, got: %v", err)
	}
}

// TestConsolidator_TooFewMessages is a no-op when message count < WindowSize.
func TestConsolidator_TooFewMessages(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()

	// 4 messages, historyMax=2 → 2 consolidatable, windowSize=5 → skip.
	for i := 0; i < 4; i++ {
		d.AppendMessage(ctx, "sess", "user", "msg", nil)
	}
	c := &Consolidator{DB: d, Provider: &providers.Client{}, WindowSize: 5}
	if err := c.MaybeConsolidate(ctx, "sess", 2); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// TestConsolidator_CursorAdvanced tests that SetLastConsolidatedID is called
// correctly when a real consolidation run succeeds.
// We verify the cursor moves by driving the DB directly without an LLM call.
func TestConsolidator_CursorAdvanced(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()

	// Insert 15 messages so that with historyMax=5, messages 1–10 are eligible.
	var msgIDs []int64
	for i := 0; i < 15; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		id, _ := d.AppendMessage(ctx, "sess", role, "content "+string(rune('A'+i)), nil)
		msgIDs = append(msgIDs, id)
	}

	// Verify initial cursor is 0.
	lastID, oldestActive, err := d.GetConsolidationRange(ctx, "sess", 5)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 {
		t.Fatalf("expected initial lastID=0, got %d", lastID)
	}
	if oldestActive == 0 {
		t.Fatalf("expected non-zero oldestActiveID with 15 messages and historyMax=5")
	}

	// Get the messages that would be consolidated.
	msgs, err := d.GetMessagesForConsolidation(ctx, "sess", 0, oldestActive)
	if err != nil {
		t.Fatalf("GetMessagesForConsolidation: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected consolidatable messages")
	}

	// Manually advance the cursor (simulates what MaybeConsolidate does after storing a note).
	lastMsgID := msgs[len(msgs)-1].ID
	d.InsertMemoryNote(ctx, "sess", "test summary", make([]byte, 0), sql.NullInt64{Int64: lastMsgID, Valid: true}, "consolidation")
	if err := d.SetLastConsolidatedID(ctx, "sess", lastMsgID); err != nil {
		t.Fatalf("SetLastConsolidatedID: %v", err)
	}

	// Now a second check should show the cursor has moved.
	newLastID, _, err := d.GetConsolidationRange(ctx, "sess", 5)
	if err != nil {
		t.Fatalf("GetConsolidationRange after advance: %v", err)
	}
	if newLastID != lastMsgID {
		t.Errorf("expected cursor=%d, got %d", lastMsgID, newLastID)
	}
}

// TestConsolidator_DefaultWindowSize ensures WindowSize defaults to 10.
func TestConsolidator_DefaultWindowSize(t *testing.T) {
	d := openConsolidateTestDB(t)
	ctx := context.Background()

	// 9 consolidatable messages (historyMax=5, total=14) — below default windowSize=10.
	for i := 0; i < 14; i++ {
		d.AppendMessage(ctx, "sess", "user", "msg", nil)
	}
	c := &Consolidator{DB: d, Provider: &providers.Client{}, WindowSize: 0} // 0 → default 10
	// Should not call Provider.Chat because count (9) < defaultWindowSize (10).
	if err := c.MaybeConsolidate(ctx, "sess", 5); err != nil {
		t.Fatalf("expected nil error (skipped), got: %v", err)
	}
}

// TestContentToStr covers the helper for various types.
func TestContentToStr_String(t *testing.T) {
	if got := contentToStr("hello"); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestContentToStr_Nil(t *testing.T) {
	if got := contentToStr(nil); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestContentToStr_Other(t *testing.T) {
	got := contentToStr(42)
	if !strings.Contains(got, "42") {
		t.Errorf("expected '42' in output, got %q", got)
	}
}
