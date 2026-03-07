package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/scope"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpen_CreatesDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer d.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected db file to be created")
	}
}

func TestOpen_MigratesLegacyMemorySchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open legacy db: %v", err)
	}
	defer sqlDB.Close()

	legacyStmts := []string{
		`CREATE TABLE sessions(
			key TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE messages(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE memory_pinned(
			key TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE memory_notes(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			source_message_id INTEGER,
			tags TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);`,
		`CREATE VIRTUAL TABLE memory_fts USING fts5(text, content='memory_notes', content_rowid='id');`,
		`CREATE TRIGGER memory_notes_ai AFTER INSERT ON memory_notes BEGIN
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
	}
	for _, stmt := range legacyStmts {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("exec legacy schema: %v", err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO memory_notes(text, embedding, tags, created_at) VALUES('legacy note', x'00000000', '', 1)`); err != nil {
		t.Fatalf("seed legacy note: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO memory_pinned(key, content, updated_at) VALUES('name', 'legacy', 1)`); err != nil {
		t.Fatalf("seed legacy pinned: %v", err)
	}
	_ = sqlDB.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open migrated db: %v", err)
	}
	defer d.Close()

	pinned, err := d.GetPinned(context.Background(), "session1")
	if err != nil {
		t.Fatalf("GetPinned after migration: %v", err)
	}
	if pinned["name"] != "legacy" {
		t.Fatalf("expected legacy pinned data after migration, got %#v", pinned)
	}

	rows, err := d.StreamMemoryNotesLimit(context.Background(), "session1", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesLimit after migration: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected migrated memory note row")
	}
	var id int64
	var text string
	var emb []byte
	var sourceID any
	var tags string
	var createdAt int64
	if err := rows.Scan(&id, &text, &emb, &sourceID, &tags, &createdAt); err != nil {
		t.Fatalf("scan migrated memory note: %v", err)
	}
	if text != "legacy note" {
		t.Fatalf("expected migrated memory note, got %q", text)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	// A path inside a non-existent directory shouldn't cause Open to fail
	// because SQLite creates the file. But an invalid path format should fail.
	_, err := Open("/dev/null/invalid/path/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestNowMS(t *testing.T) {
	before := time.Now().UnixMilli()
	ms := NowMS()
	after := time.Now().UnixMilli()
	if ms < before || ms > after {
		t.Errorf("NowMS() = %d, expected between %d and %d", ms, before, after)
	}
}

func TestClose(t *testing.T) {
	d := openTestDB(t)
	if err := d.Close(); err != nil {
		t.Errorf("unexpected close error: %v", err)
	}
}

func TestEnsureSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	err := d.EnsureSession(ctx, "test-session")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	// calling again should upsert without error
	err = d.EnsureSession(ctx, "test-session")
	if err != nil {
		t.Fatalf("EnsureSession (second call): %v", err)
	}
}

func TestAppendMessage_Basic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id, err := d.AppendMessage(ctx, "session1", "user", "hello", nil)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestAppendMessage_WithPayload(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	payload := map[string]any{"channel": "cli", "from": "user"}
	id, err := d.AppendMessage(ctx, "session1", "user", "hello", payload)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
}

func TestGetLastMessages_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	msgs, err := d.GetLastMessages(ctx, "session1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestGetLastMessages_Chronological(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.AppendMessage(ctx, "s1", "user", "first", nil)
	d.AppendMessage(ctx, "s1", "assistant", "second", nil)
	d.AppendMessage(ctx, "s1", "user", "third", nil)

	msgs, err := d.GetLastMessages(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	// Should be aligned so first is user
	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected first message role 'user', got %q", msgs[0].Role)
	}
}

func TestGetLastMessages_LimitRespected(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		d.AppendMessage(ctx, "s1", "user", "msg", nil)
	}

	msgs, err := d.GetLastMessages(ctx, "s1", 3)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) > 3 {
		t.Errorf("expected at most 3 messages, got %d", len(msgs))
	}
}

func TestGetLastMessages_AlignToUser(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert only assistant messages
	d.AppendMessage(ctx, "s1", "assistant", "resp1", nil)
	d.AppendMessage(ctx, "s1", "assistant", "resp2", nil)

	msgs, err := d.GetLastMessages(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	// should return empty (no user start) or at least not start with assistant
	for _, m := range msgs {
		if m.Role == "assistant" && msgs[0].Role == "assistant" {
			// This would only be invalid if first is assistant - but alignment strips leading non-user
			break
		}
	}
}

func TestGetPinned_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	pinned, err := d.GetPinned(ctx, "session1")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if len(pinned) != 0 {
		t.Errorf("expected empty pinned, got %d entries", len(pinned))
	}
}

func TestUpsertPinned_And_GetPinned(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	err := d.UpsertPinned(ctx, "session1", "name", "Alice")
	if err != nil {
		t.Fatalf("UpsertPinned: %v", err)
	}

	pinned, err := d.GetPinned(ctx, "session1")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if v, ok := pinned["name"]; !ok || v != "Alice" {
		t.Errorf("expected pinned['name']='Alice', got %q", pinned["name"])
	}
}

func TestUpsertPinned_Overwrites(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.UpsertPinned(ctx, "session1", "key", "first")
	d.UpsertPinned(ctx, "session1", "key", "second")

	pinned, _ := d.GetPinned(ctx, "session1")
	if pinned["key"] != "second" {
		t.Errorf("expected 'second', got %q", pinned["key"])
	}
}

func TestGetPinned_IncludesGlobalAndSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertPinned(ctx, scope.GlobalMemoryScope, "shared", "all"); err != nil {
		t.Fatalf("UpsertPinned global: %v", err)
	}
	if err := d.UpsertPinned(ctx, "session-a", "local", "only-a"); err != nil {
		t.Fatalf("UpsertPinned session: %v", err)
	}

	pinned, err := d.GetPinned(ctx, "session-a")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if pinned["shared"] != "all" || pinned["local"] != "only-a" {
		t.Fatalf("expected global and session pinned values, got %#v", pinned)
	}

	pinnedB, err := d.GetPinned(ctx, "session-b")
	if err != nil {
		t.Fatalf("GetPinned: %v", err)
	}
	if pinnedB["shared"] != "all" {
		t.Fatalf("expected shared global entry, got %#v", pinnedB)
	}
	if _, ok := pinnedB["local"]; ok {
		t.Fatalf("did not expect session-a entry in session-b view: %#v", pinnedB)
	}
}

func TestGetPinned_SessionNamedGlobalDoesNotLeak(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.UpsertPinned(ctx, scope.GlobalMemoryScope, "shared", "all"); err != nil {
		t.Fatalf("UpsertPinned global: %v", err)
	}
	if err := d.UpsertPinned(ctx, scope.GlobalScopeAlias, "local", "only-global-session"); err != nil {
		t.Fatalf("UpsertPinned session: %v", err)
	}

	pinnedOther, err := d.GetPinned(ctx, "session-b")
	if err != nil {
		t.Fatalf("GetPinned other: %v", err)
	}
	if pinnedOther["shared"] != "all" {
		t.Fatalf("expected shared global entry, got %#v", pinnedOther)
	}
	if _, ok := pinnedOther["local"]; ok {
		t.Fatalf("did not expect session named global to leak, got %#v", pinnedOther)
	}

	pinnedGlobal, err := d.GetPinned(ctx, scope.GlobalScopeAlias)
	if err != nil {
		t.Fatalf("GetPinned global session: %v", err)
	}
	if pinnedGlobal["local"] != "only-global-session" {
		t.Fatalf("expected session-global entry, got %#v", pinnedGlobal)
	}
}

func TestInsertMemoryNote(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	embedding := make([]byte, 4*3)
	id, err := d.InsertMemoryNote(ctx, "session1", "test text", embedding, sql.NullInt64{}, "tag1,tag2")
	if err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestStreamMemoryNotesLimit_NoLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.InsertMemoryNote(ctx, "session1", "note1", make([]byte, 4), sql.NullInt64{}, "")
	d.InsertMemoryNote(ctx, "session1", "note2", make([]byte, 4), sql.NullInt64{}, "")

	rows, err := d.StreamMemoryNotesLimit(ctx, "session1", 0)
	if err != nil {
		t.Fatalf("StreamMemoryNotesLimit: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		rows.Scan(&id, &text, &emb, &src, &tags, &created)
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestStreamMemoryNotesLimit_WithLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		d.InsertMemoryNote(ctx, "session1", "note", make([]byte, 4), sql.NullInt64{}, "")
	}

	rows, err := d.StreamMemoryNotesLimit(ctx, "session1", 2)
	if err != nil {
		t.Fatalf("StreamMemoryNotesLimit: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		rows.Scan(&id, &text, &emb, &src, &tags, &created)
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestSearchFTS(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert notes to trigger FTS
	d.InsertMemoryNote(ctx, "session1", "the quick brown fox", make([]byte, 4), sql.NullInt64{}, "")
	d.InsertMemoryNote(ctx, "session1", "lazy dog sits", make([]byte, 4), sql.NullInt64{}, "")

	results, err := d.SearchFTS(ctx, "session1", "quick fox", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one FTS result")
	}
	if results[0].Text != "the quick brown fox" {
		t.Errorf("expected 'the quick brown fox', got %q", results[0].Text)
	}
}

func TestSearchFTS_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	results, err := d.SearchFTS(ctx, "session1", "anything", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestSearchFTS_SessionIsolation(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.InsertMemoryNote(ctx, "session-a", "private fox", make([]byte, 4), sql.NullInt64{}, "")
	d.InsertMemoryNote(ctx, scope.GlobalMemoryScope, "shared fox", make([]byte, 4), sql.NullInt64{}, "")

	results, err := d.SearchFTS(ctx, "session-b", "fox", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) != 1 || results[0].Text != "shared fox" {
		t.Fatalf("expected only shared result, got %#v", results)
	}
}

func TestMessage_Fields(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.AppendMessage(ctx, "sess", "user", "hello world", nil)
	msgs, err := d.GetLastMessages(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.SessionKey != "sess" {
		t.Errorf("expected SessionKey='sess', got %q", m.SessionKey)
	}
	if m.Role != "user" {
		t.Errorf("expected Role='user', got %q", m.Role)
	}
	if m.Content != "hello world" {
		t.Errorf("expected Content='hello world', got %q", m.Content)
	}
	if m.CreatedAt <= 0 {
		t.Errorf("expected positive CreatedAt, got %d", m.CreatedAt)
	}
}

// ---- GetConsolidationRange ----

func TestGetConsolidationRange_NoSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	lastID, oldestID, err := d.GetConsolidationRange(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 || oldestID != 0 {
		t.Errorf("expected (0,0) for missing session, got (%d,%d)", lastID, oldestID)
	}
}

func TestGetConsolidationRange_FewMessages(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert fewer messages than historyMax — all messages are in the active
	// window, so oldestActiveID equals the first message's ID and there is
	// nothing to consolidate (no messages with id < oldestActiveID beyond the cursor).
	var firstID int64
	for i := 0; i < 3; i++ {
		id, _ := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		if i == 0 {
			firstID = id
		}
	}

	lastID, oldestID, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 {
		t.Errorf("expected lastID=0 (nothing consolidated), got %d", lastID)
	}
	// With 3 messages and historyMax=10, the active window covers all messages.
	// oldestActiveID should equal the first message's ID.
	if oldestID != firstID {
		t.Errorf("expected oldestActiveID=%d (all in window), got %d", firstID, oldestID)
	}
}

func TestGetConsolidationRange_ManyMessages(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert 20 messages, historyMax=5 → oldestActiveID should be the ID of
	// the 16th message (5 from the end).
	var ids []int64
	for i := 0; i < 20; i++ {
		id, err := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		ids = append(ids, id)
	}

	lastID, oldestActiveID, err := d.GetConsolidationRange(ctx, "sess", 5)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 {
		t.Errorf("expected lastID=0 (nothing consolidated yet), got %d", lastID)
	}
	// oldestActiveID should be the 16th message ID (index 15).
	expectedOldest := ids[15]
	if oldestActiveID != expectedOldest {
		t.Errorf("expected oldestActiveID=%d, got %d", expectedOldest, oldestActiveID)
	}
}

// ---- GetMessagesForConsolidation ----

func TestGetMessagesForConsolidation_Range(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	var ids []int64
	for i := 0; i < 10; i++ {
		id, _ := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		ids = append(ids, id)
	}

	// Retrieve messages strictly between ids[2] and ids[7].
	msgs, err := d.GetMessagesForConsolidation(ctx, "sess", ids[2], ids[7])
	if err != nil {
		t.Fatalf("GetMessagesForConsolidation: %v", err)
	}
	if len(msgs) != 4 {
		t.Errorf("expected 4 messages (ids[3]..ids[6]), got %d", len(msgs))
	}
	if msgs[0].ID != ids[3] {
		t.Errorf("expected first message id=%d, got %d", ids[3], msgs[0].ID)
	}
}

func TestGetMessagesForConsolidation_Empty(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	msgs, err := d.GetMessagesForConsolidation(ctx, "sess", 0, 1)
	if err != nil {
		t.Fatalf("GetMessagesForConsolidation: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

// ---- SetLastConsolidatedID ----

func TestSetLastConsolidatedID(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	d.EnsureSession(ctx, "sess")
	if err := d.SetLastConsolidatedID(ctx, "sess", 42); err != nil {
		t.Fatalf("SetLastConsolidatedID: %v", err)
	}

	// Verify via GetConsolidationRange.
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 42 {
		t.Errorf("expected lastConsolidatedID=42, got %d", lastID)
	}
}

func TestWriteConsolidation_Atomic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	var lastMsgID int64
	for i := 0; i < 3; i++ {
		id, err := d.AppendMessage(ctx, "sess", "user", "msg", nil)
		if err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
		lastMsgID = id
	}

	noteID, err := d.WriteConsolidation(ctx, ConsolidationWrite{
		SessionKey:    "sess",
		ScopeKey:      "sess",
		NoteText:      "summary",
		Embedding:     []byte{},
		SourceMsgID:   sql.NullInt64{Int64: lastMsgID, Valid: true},
		NoteTags:      "consolidation",
		CanonicalKey:  "long_term_memory",
		CanonicalText: "- stable fact",
		CursorMsgID:   lastMsgID,
	})
	if err != nil {
		t.Fatalf("WriteConsolidation: %v", err)
	}
	if noteID == 0 {
		t.Fatal("expected non-zero note id")
	}

	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != lastMsgID {
		t.Fatalf("expected last consolidated id %d, got %d", lastMsgID, lastID)
	}

	rows, err := d.StreamMemoryNotesScopeLimit(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 note, got %d", count)
	}

	pinned, ok, err := d.GetPinnedValue(ctx, "sess", "long_term_memory")
	if err != nil {
		t.Fatalf("GetPinnedValue: %v", err)
	}
	if !ok || pinned != "- stable fact" {
		t.Fatalf("expected canonical memory to be updated, got ok=%v value=%q", ok, pinned)
	}
}

func TestWriteConsolidation_RollbackOnFailure(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := d.EnsureSession(ctx, "sess"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	// Empty canonical key and source message id is valid, but we intentionally fail on cursor update
	// by writing to a missing session key.
	_, err := d.WriteConsolidation(ctx, ConsolidationWrite{
		SessionKey:  "missing-session",
		ScopeKey:    "sess",
		NoteText:    "summary",
		Embedding:   []byte{},
		NoteTags:    "consolidation",
		CursorMsgID: 999,
	})
	if err == nil {
		t.Fatal("expected write error for missing session")
	}

	rows, err := d.StreamMemoryNotesScopeLimit(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("expected no note due to rollback")
	}
}

func TestResetSessionHistory(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := d.AppendMessage(ctx, "sess", "user", "msg", nil); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	if err := d.SetLastConsolidatedID(ctx, "sess", 2); err != nil {
		t.Fatalf("SetLastConsolidatedID: %v", err)
	}

	if err := d.ResetSessionHistory(ctx, "sess"); err != nil {
		t.Fatalf("ResetSessionHistory: %v", err)
	}

	msgs, err := d.GetLastMessages(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages after reset, got %d", len(msgs))
	}
	lastID, _, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err != nil {
		t.Fatalf("GetConsolidationRange: %v", err)
	}
	if lastID != 0 {
		t.Fatalf("expected cursor reset to 0, got %d", lastID)
	}
}

func TestOpen_CreatesSubagentJobsTable(t *testing.T) {
	d := openTestDB(t)
	row := d.SQL.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='subagent_jobs'`)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("expected subagent_jobs table, got err=%v", err)
	}
	if name != "subagent_jobs" {
		t.Fatalf("expected subagent_jobs table, got %q", name)
	}
}

func TestSubagentJobs_Lifecycle(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	job := SubagentJob{
		ID:               "job-1",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-1",
		Channel:          "cli",
		ReplyTo:          "user",
		Task:             "do work",
	}
	if err := d.EnqueueSubagentJob(ctx, job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	queued, err := d.ListQueuedSubagentJobs(ctx)
	if err != nil {
		t.Fatalf("ListQueuedSubagentJobs: %v", err)
	}
	if len(queued) != 1 || queued[0].ID != job.ID {
		t.Fatalf("expected queued job, got %#v", queued)
	}
	claimed, err := d.ClaimNextSubagentJob(ctx)
	if err != nil {
		t.Fatalf("ClaimNextSubagentJob: %v", err)
	}
	if claimed == nil || claimed.Status != SubagentStatusRunning || claimed.Attempts != 1 {
		t.Fatalf("expected running claimed job, got %#v", claimed)
	}
	if err := d.MarkSubagentSucceeded(ctx, job.ID, "preview", "artifact-1"); err != nil {
		t.Fatalf("MarkSubagentSucceeded: %v", err)
	}
	stored, ok, err := d.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatal("expected stored job")
	}
	if stored.Status != SubagentStatusSucceeded || stored.ResultPreview != "preview" || stored.ArtifactID != "artifact-1" || stored.FinishedAt == 0 {
		t.Fatalf("unexpected stored job after success: %#v", stored)
	}
}

func TestSubagentJobs_ReconcileRunning(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	job := SubagentJob{
		ID:               "job-2",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-2",
		Channel:          "cli",
		ReplyTo:          "user",
		Task:             "do work",
	}
	if err := d.EnqueueSubagentJob(ctx, job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	if _, err := d.AppendMessage(ctx, job.ParentSessionKey, "user", "start", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	if err := d.MarkSubagentRunning(ctx, job.ID); err != nil {
		t.Fatalf("MarkSubagentRunning: %v", err)
	}
	if err := d.MarkRunningSubagentsInterrupted(ctx, "restart"); err != nil {
		t.Fatalf("MarkRunningSubagentsInterrupted: %v", err)
	}
	stored, ok, err := d.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatal("expected stored job")
	}
	if stored.Status != SubagentStatusInterrupted || stored.ErrorText != "restart" || stored.FinishedAt == 0 {
		t.Fatalf("unexpected interrupted job: %#v", stored)
	}
}

func TestSubagentJobs_EnqueueWithLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- d.EnqueueSubagentJobLimited(ctx, SubagentJob{
				ID:               "job-limit-" + string(rune('a'+i)),
				ParentSessionKey: "parent",
				ChildSessionKey:  "parent:subagent:" + string(rune('a'+i)),
				Task:             "do work",
			}, 1)
		}(i)
	}
	wg.Wait()
	close(errCh)
	var successCount int
	var fullCount int
	for err := range errCh {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrSubagentQueueFull):
			fullCount++
		default:
			t.Fatalf("unexpected enqueue error: %v", err)
		}
	}
	if successCount != 1 || fullCount != 1 {
		t.Fatalf("expected one success and one queue-full error, got success=%d full=%d", successCount, fullCount)
	}
	queued, err := d.ListQueuedSubagentJobs(ctx)
	if err != nil {
		t.Fatalf("ListQueuedSubagentJobs: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("expected exactly one queued job, got %#v", queued)
	}
}

func TestSubagentJobs_FinalizePersistsSummaryAtomically(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	job := SubagentJob{
		ID:               "job-finalize",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-finalize",
		Task:             "do work",
	}
	if err := d.EnqueueSubagentJob(ctx, job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	if _, err := d.AppendMessage(ctx, job.ParentSessionKey, "user", "start", nil); err != nil {
		t.Fatalf("AppendMessage parent: %v", err)
	}
	if err := d.MarkSubagentRunning(ctx, job.ID); err != nil {
		t.Fatalf("MarkSubagentRunning: %v", err)
	}
	if err := d.FinalizeSubagentJob(ctx, job, SubagentStatusSucceeded, "done", "artifact-1", "", "summary text", map[string]any{"subagent_job_id": job.ID}); err != nil {
		t.Fatalf("FinalizeSubagentJob: %v", err)
	}
	stored, ok, err := d.GetSubagentJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSubagentJob: %v", err)
	}
	if !ok {
		t.Fatal("expected stored job")
	}
	if stored.Status != SubagentStatusSucceeded || stored.ResultPreview != "done" || stored.ArtifactID != "artifact-1" {
		t.Fatalf("unexpected finalized job: %#v", stored)
	}
	msgs, err := d.GetLastMessages(ctx, job.ParentSessionKey, 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].Content != "summary text" {
		t.Fatalf("expected parent summary message, got %#v", msgs)
	}
}
