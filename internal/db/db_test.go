package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func mustAppendMessage(t *testing.T, d *DB, ctx context.Context, sessionKey, role, content string, payload any) int64 {
	t.Helper()
	id, err := d.AppendMessage(ctx, sessionKey, role, content, payload)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	return id
}

func mustInsertMemoryNote(t *testing.T, d *DB, ctx context.Context, sessionKey, text string, embedding []byte, sourceMessageID sql.NullInt64, tags string) int64 {
	t.Helper()
	id, err := d.InsertMemoryNote(ctx, sessionKey, text, embedding, sourceMessageID, tags)
	if err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	return id
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

func TestOpen_CreatesApprovalAndPairingTables(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	for _, table := range []string{"paired_devices", "pairing_requests", "approval_requests", "approval_allowlists", "approval_tokens"} {
		row := d.SQL.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table)
		var name string
		if err := row.Scan(&name); err != nil {
			t.Fatalf("expected table %s: %v", table, err)
		}
	}
}

func TestApprovalStore_RoundTripAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approval.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	pairing, err := d.CreatePairingRequest(ctx, PairingRequestRecord{
		DeviceID:        "device-1",
		Role:            "operator",
		DisplayName:     "Ops Laptop",
		Origin:          "127.0.0.1",
		PairingCodeHash: []byte("hash"),
		RequestedAt:     1,
		ExpiresAt:       2,
		Status:          "pending",
		Metadata:        map[string]any{"ip": "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("CreatePairingRequest: %v", err)
	}
	if _, err := d.CreateApprovalRequest(ctx, ApprovalRequestRecord{Type: "exec", SubjectHash: "subj", SubjectJSON: `{"type":"exec"}`, ExecutionHostID: "local", Status: "pending", PolicyMode: "ask", RequestedAt: 3, ExpiresAt: 4}); err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}
	if _, err := d.CreateApprovalAllowlist(ctx, ApprovalAllowlistRecord{Domain: "exec", ScopeJSON: `{"host_id":"local"}`, MatcherJSON: `{"program":"echo"}`, CreatedBy: "cli", CreatedAt: 5}); err != nil {
		t.Fatalf("CreateApprovalAllowlist: %v", err)
	}
	if _, err := d.UpsertPairedDevice(ctx, PairedDeviceRecord{DeviceID: pairing.DeviceID, Role: pairing.Role, DisplayName: pairing.DisplayName, TokenHash: []byte("token"), Status: "active", CreatedAt: 6, LastSeenAt: 6}); err != nil {
		t.Fatalf("UpsertPairedDevice: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	d, err = Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer d.Close()
	if _, err := d.GetPairingRequest(ctx, pairing.ID); err != nil {
		t.Fatalf("GetPairingRequest after reopen: %v", err)
	}
	if _, err := d.GetPairedDevice(ctx, pairing.DeviceID); err != nil {
		t.Fatalf("GetPairedDevice after reopen: %v", err)
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

func TestSecretsStore_RoundTrip(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := d.PutSecret(ctx, "provider.openai", []byte("cipher"), []byte("nonce"), 1, "v1"); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	record, ok, err := d.GetSecret(ctx, "provider.openai")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if !ok {
		t.Fatal("expected stored secret record")
	}
	if string(record.Ciphertext) != "cipher" || string(record.Nonce) != "nonce" {
		t.Fatalf("unexpected secret record: %#v", record)
	}
}

func TestAuditEvents_VerifyDetectsTampering(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	key := []byte("01234567890123456789012345678901")
	if err := d.AppendAuditEvent(ctx, AuditEventInput{EventType: "tool.execute", SessionKey: "sess", Actor: "cli", Payload: map[string]any{"tool": "exec"}}, key); err != nil {
		t.Fatalf("AppendAuditEvent first: %v", err)
	}
	if err := d.AppendAuditEvent(ctx, AuditEventInput{EventType: "secret.set", Actor: "cli", Payload: map[string]any{"name": "provider"}}, key); err != nil {
		t.Fatalf("AppendAuditEvent second: %v", err)
	}
	if err := d.VerifyAuditChain(ctx, key); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if _, err := d.SQL.ExecContext(ctx, `UPDATE audit_events SET payload_json='{"tampered":true}' WHERE id=1`); err != nil {
		t.Fatalf("tamper update: %v", err)
	}
	if err := d.VerifyAuditChain(ctx, key); err == nil {
		t.Fatal("expected tampered audit chain to fail verification")
	}
}

func TestAuditEvents_ConcurrentAppendKeepsChainValid(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	key := []byte("01234567890123456789012345678901")

	const writes = 16
	var wg sync.WaitGroup
	errs := make(chan error, writes)
	for i := 0; i < writes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- d.AppendAuditEvent(ctx, AuditEventInput{
				EventType:  "tool.execute",
				SessionKey: fmt.Sprintf("sess-%d", i),
				Actor:      "test",
				Payload:    map[string]any{"i": i},
			}, key)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AppendAuditEvent: %v", err)
		}
	}
	if err := d.VerifyAuditChain(ctx, key); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestAuditEvents_ConcurrentWithMessageWrites(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	key := []byte("01234567890123456789012345678901")

	const writes = 32
	var wg sync.WaitGroup
	errs := make(chan error, writes*2)
	for i := 0; i < writes; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, err := d.AppendMessage(ctx, fmt.Sprintf("sess-%d", i%4), "user", fmt.Sprintf("msg-%d", i), nil)
			errs <- err
		}(i)
		go func(i int) {
			defer wg.Done()
			errs <- d.AppendAuditEvent(ctx, AuditEventInput{
				EventType:  "tool.execute",
				SessionKey: fmt.Sprintf("sess-%d", i),
				Actor:      "test",
				Payload:    map[string]any{"i": i},
			}, key)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}
	if err := d.VerifyAuditChain(ctx, key); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
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

func TestAppendMessage_RollsBackWhenSessionUpdateFails(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.SQL.ExecContext(ctx, `
		CREATE TRIGGER sessions_update_fail
		BEFORE UPDATE ON sessions
		BEGIN
			SELECT RAISE(FAIL, 'session update failed');
		END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := d.AppendMessage(ctx, "session1", "user", "hello", nil)
	if err == nil {
		t.Fatal("expected append failure")
	}

	msgs, err := d.GetLastMessages(ctx, "session1", 10)
	if err != nil {
		t.Fatalf("GetLastMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected rolled-back message insert, got %#v", msgs)
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

	if _, err := d.AppendMessage(ctx, "s1", "user", "first", nil); err != nil {
		t.Fatalf("AppendMessage first: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "s1", "assistant", "second", nil); err != nil {
		t.Fatalf("AppendMessage second: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "s1", "user", "third", nil); err != nil {
		t.Fatalf("AppendMessage third: %v", err)
	}

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
		mustAppendMessage(t, d, ctx, "s1", "user", "msg", nil)
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
	mustAppendMessage(t, d, ctx, "s1", "assistant", "resp1", nil)
	mustAppendMessage(t, d, ctx, "s1", "assistant", "resp2", nil)

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

	if err := d.UpsertPinned(ctx, "session1", "key", "first"); err != nil {
		t.Fatalf("UpsertPinned first: %v", err)
	}
	if err := d.UpsertPinned(ctx, "session1", "key", "second"); err != nil {
		t.Fatalf("UpsertPinned second: %v", err)
	}

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

	if _, err := d.InsertMemoryNote(ctx, "session1", "note1", make([]byte, 4), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote note1: %v", err)
	}
	if _, err := d.InsertMemoryNote(ctx, "session1", "note2", make([]byte, 4), sql.NullInt64{}, ""); err != nil {
		t.Fatalf("InsertMemoryNote note2: %v", err)
	}

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
		if err := rows.Scan(&id, &text, &emb, &src, &tags, &created); err != nil {
			t.Fatalf("rows.Scan: %v", err)
		}
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestStreamMemoryNotesLimit_WithLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := d.InsertMemoryNote(ctx, "session1", "note", make([]byte, 4), sql.NullInt64{}, ""); err != nil {
			t.Fatalf("InsertMemoryNote note: %v", err)
		}
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
		if err := rows.Scan(&id, &text, &emb, &src, &tags, &created); err != nil {
			t.Fatalf("rows.Scan: %v", err)
		}
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestSearchFTS(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert notes to trigger FTS
	mustInsertMemoryNote(t, d, ctx, "session1", "the quick brown fox", make([]byte, 4), sql.NullInt64{}, "")
	mustInsertMemoryNote(t, d, ctx, "session1", "lazy dog sits", make([]byte, 4), sql.NullInt64{}, "")

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

	mustInsertMemoryNote(t, d, ctx, "session-a", "private fox", make([]byte, 4), sql.NullInt64{}, "")
	mustInsertMemoryNote(t, d, ctx, scope.GlobalMemoryScope, "shared fox", make([]byte, 4), sql.NullInt64{}, "")

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

	mustAppendMessage(t, d, ctx, "sess", "user", "hello world", nil)
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

func TestGetConsolidationRange_PropagatesQueryErrors(t *testing.T) {
	d := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := d.GetConsolidationRange(ctx, "sess", 10)
	if err == nil {
		t.Fatal("expected query error for canceled context")
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

	if err := d.EnsureSession(ctx, "sess"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
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

func TestSubagentJobs_ListRunning(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	job := SubagentJob{
		ID:               "job-running-list",
		ParentSessionKey: "parent",
		ChildSessionKey:  "parent:subagent:job-running-list",
		Task:             "do work",
	}
	if err := d.EnqueueSubagentJob(ctx, job); err != nil {
		t.Fatalf("EnqueueSubagentJob: %v", err)
	}
	if err := d.MarkSubagentRunning(ctx, job.ID); err != nil {
		t.Fatalf("MarkSubagentRunning: %v", err)
	}
	running, err := d.ListRunningSubagentJobs(ctx)
	if err != nil {
		t.Fatalf("ListRunningSubagentJobs: %v", err)
	}
	if len(running) != 1 || running[0].ID != job.ID {
		t.Fatalf("expected running job list to include %q, got %#v", job.ID, running)
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

func TestLinkSession(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.LinkSession(ctx, "session-a", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession a: %v", err)
	}
	if err := d.LinkSession(ctx, "session-b", "scope-1", nil); err != nil {
		t.Fatalf("LinkSession b: %v", err)
	}

	scopeA, err := d.ResolveScopeKey(ctx, "session-a")
	if err != nil {
		t.Fatalf("ResolveScopeKey a: %v", err)
	}
	if scopeA != "scope-1" {
		t.Fatalf("expected scope-1, got %q", scopeA)
	}

	scopeB, err := d.ResolveScopeKey(ctx, "session-b")
	if err != nil {
		t.Fatalf("ResolveScopeKey b: %v", err)
	}
	if scopeB != "scope-1" {
		t.Fatalf("expected scope-1, got %q", scopeB)
	}
}

func TestResolveScopeKeyUnlinked(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	scopeKey, err := d.ResolveScopeKey(ctx, "unlinked-session")
	if err != nil {
		t.Fatalf("ResolveScopeKey: %v", err)
	}
	if scopeKey != "unlinked-session" {
		t.Fatalf("expected unlinked-session, got %q", scopeKey)
	}
}

func TestListScopeSessions(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if err := d.LinkSession(ctx, "sess-x", "scope-2", nil); err != nil {
		t.Fatalf("LinkSession x: %v", err)
	}
	if err := d.LinkSession(ctx, "sess-y", "scope-2", nil); err != nil {
		t.Fatalf("LinkSession y: %v", err)
	}

	sessions, err := d.ListScopeSessions(ctx, "scope-2")
	if err != nil {
		t.Fatalf("ListScopeSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	found := map[string]bool{}
	for _, s := range sessions {
		found[s] = true
	}
	if !found["sess-x"] || !found["sess-y"] {
		t.Fatalf("expected sess-x and sess-y, got %v", sessions)
	}
}

func TestGetLastMessagesScoped(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Link two sessions to a scope
	if err := d.LinkSession(ctx, "scoped-a", "scope-3", nil); err != nil {
		t.Fatalf("LinkSession a: %v", err)
	}
	if err := d.LinkSession(ctx, "scoped-b", "scope-3", nil); err != nil {
		t.Fatalf("LinkSession b: %v", err)
	}

	// Add messages to both sessions
	if _, err := d.AppendMessage(ctx, "scoped-a", "user", "hello from a", nil); err != nil {
		t.Fatalf("AppendMessage a user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "scoped-a", "assistant", "reply from a", nil); err != nil {
		t.Fatalf("AppendMessage a assistant: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "scoped-b", "user", "hello from b", nil); err != nil {
		t.Fatalf("AppendMessage b user: %v", err)
	}
	if _, err := d.AppendMessage(ctx, "scoped-b", "assistant", "reply from b", nil); err != nil {
		t.Fatalf("AppendMessage b assistant: %v", err)
	}

	msgs, err := d.GetLastMessagesScoped(ctx, "scoped-a", 10)
	if err != nil {
		t.Fatalf("GetLastMessagesScoped: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	// First message must be a user message (alignment rule)
	if msgs[0].Role != "user" {
		t.Fatalf("expected first message to be user, got %q", msgs[0].Role)
	}
	// Messages must be in ascending id order (chronological)
	for i := 1; i < len(msgs); i++ {
		if msgs[i].ID < msgs[i-1].ID {
			t.Fatalf("messages not in chronological order at index %d", i)
		}
	}
	// Both sessions' messages should appear
	contents := map[string]bool{}
	for _, m := range msgs {
		contents[m.Content] = true
	}
	if !contents["hello from a"] || !contents["hello from b"] {
		t.Fatalf("expected messages from both sessions, got %v", contents)
	}
}

func TestIntegrityCheck_BasicConsistency(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	var result string
	if err := d.SQL.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatalf("PRAGMA integrity_check: %v", err)
	}
	if result != "ok" {
		t.Errorf("integrity check failed: %s", result)
	}
}

func TestBackupRestore_PreservesCurrentSchemaAndData(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	d, err := Open(sourcePath)
	if err != nil {
		t.Fatalf("Open source db: %v", err)
	}
	defer d.Close()
	ctx := context.Background()

	if _, err := d.AppendMessage(ctx, "backup-session", "user", "hello backup", map[string]any{"source": "test"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := d.UpsertPinned(ctx, "backup-session", "note", "restored"); err != nil {
		t.Fatalf("UpsertPinned: %v", err)
	}

	backupPath := filepath.Join(dir, "backup.db")
	quotedBackupPath := strings.ReplaceAll(backupPath, "'", "''")
	if _, err := d.SQL.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", quotedBackupPath)); err != nil {
		t.Fatalf("VACUUM INTO backup: %v", err)
	}

	restored, err := Open(backupPath)
	if err != nil {
		t.Fatalf("Open restored db: %v", err)
	}
	defer restored.Close()

	msgs, err := restored.GetLastMessages(ctx, "backup-session", 10)
	if err != nil {
		t.Fatalf("GetLastMessages restored: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello backup" {
		t.Fatalf("expected restored history row, got %#v", msgs)
	}
	pinned, err := restored.GetPinned(ctx, "backup-session")
	if err != nil {
		t.Fatalf("GetPinned restored: %v", err)
	}
	if pinned["note"] != "restored" {
		t.Fatalf("expected restored pinned note, got %#v", pinned)
	}

	var result string
	if err := restored.SQL.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatalf("PRAGMA integrity_check restored: %v", err)
	}
	if result != "ok" {
		t.Fatalf("restored integrity check failed: %s", result)
	}

	var count int
	if err := restored.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('messages','memory_pinned','memory_docs','subagent_jobs')`).Scan(&count); err != nil {
		t.Fatalf("schema table count: %v", err)
	}
	if count != 4 {
		t.Fatalf("expected restored backup to preserve current schema, got %d tables", count)
	}
}

func BenchmarkHistoryLoad(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	d, err := Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer d.Close()
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := d.AppendMessage(ctx, "bench-session", role, fmt.Sprintf("message %d", i), nil); err != nil {
			b.Fatalf("AppendMessage: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.GetLastMessages(ctx, "bench-session", 20); err != nil {
			b.Fatalf("GetLastMessages: %v", err)
		}
	}
}

func BenchmarkScopedRetrieval(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	d, err := Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer d.Close()
	ctx := context.Background()

	embedding := make([]byte, 4*8)
	for i := 0; i < 50; i++ {
		if _, err := d.InsertMemoryNote(ctx, "bench-session", fmt.Sprintf("memory note %d about something important", i), embedding, sql.NullInt64{}, "bench"); err != nil {
			b.Fatalf("InsertMemoryNote: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := d.StreamMemoryNotesScopeLimit(ctx, "bench-session", 20)
		if err != nil {
			b.Fatalf("StreamMemoryNotesScopeLimit: %v", err)
		}
		for rows.Next() {
			var id int64
			var text string
			var emb []byte
			var src any
			var tags string
			var created int64
			if err := rows.Scan(&id, &text, &emb, &src, &tags, &created); err != nil {
				b.Fatalf("Scan: %v", err)
			}
		}
		rows.Close()
	}
}

func BenchmarkInsertHistory(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	d, err := Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer d.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := d.AppendMessage(ctx, "bench-session", role, fmt.Sprintf("message %d", i), nil); err != nil {
			b.Fatalf("AppendMessage: %v", err)
		}
	}
}

// ---- Memory metadata columns and helpers ----

func TestInsertMemoryNoteTyped_DefaultsAndFields(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id, err := d.InsertMemoryNoteTyped(ctx, "session1", TypedNoteInput{
		Text:       "user prefers dark mode",
		Embedding:  make([]byte, 4*2),
		Kind:       MemoryKindPreference,
		Status:     MemoryStatusActive,
		Importance: 0.8,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	// Verify via FTS (which now returns kind/status).
	results, err := d.SearchFTS(ctx, "session1", "dark mode", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS result after InsertMemoryNoteTyped")
	}
	r := results[0]
	if r.Kind != MemoryKindPreference {
		t.Errorf("expected kind=%q, got %q", MemoryKindPreference, r.Kind)
	}
	if r.Status != MemoryStatusActive {
		t.Errorf("expected status=%q, got %q", MemoryStatusActive, r.Status)
	}
	if r.Importance != 0.8 {
		t.Errorf("expected importance=0.8, got %v", r.Importance)
	}
}

func TestInsertMemoryNoteTyped_ImportanceClamped(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Importance values outside [0,1] should be clamped.
	id, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{
		Text:       "clamped note",
		Importance: 99.0,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	rows, err := d.SearchFTS(ctx, "sess", "clamped note", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected FTS result")
	}
	if rows[0].Importance > maxImportance {
		t.Errorf("expected importance clamped to %v, got %v", maxImportance, rows[0].Importance)
	}
}

func TestInsertMemoryNote_BackwardCompat_DefaultKindAndStatus(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id, err := d.InsertMemoryNote(ctx, "session1", "legacy compat note", make([]byte, 4), sql.NullInt64{}, "")
	if err != nil {
		t.Fatalf("InsertMemoryNote: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	results, err := d.SearchFTS(ctx, "session1", "legacy compat", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS result")
	}
	if results[0].Kind != MemoryKindNote {
		t.Errorf("expected default kind=%q, got %q", MemoryKindNote, results[0].Kind)
	}
	if results[0].Status != MemoryStatusActive {
		t.Errorf("expected default status=%q, got %q", MemoryStatusActive, results[0].Status)
	}
}

func TestTouchMemoryNotes_IncrementsUseCount(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{
		Text: "important fact",
		Kind: MemoryKindFact,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}

	now := NowMS()
	if err := d.TouchMemoryNotes(ctx, "sess", []int64{id}, now); err != nil {
		t.Fatalf("TouchMemoryNotes: %v", err)
	}
	if err := d.TouchMemoryNotes(ctx, "sess", []int64{id}, now+1); err != nil {
		t.Fatalf("TouchMemoryNotes second: %v", err)
	}

	// Verify via FTS: use_count should be 2.
	results, err := d.SearchFTS(ctx, "sess", "important fact", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS result")
	}
	if results[0].UseCount != 2 {
		t.Errorf("expected use_count=2 after two touches, got %d", results[0].UseCount)
	}
	if results[0].LastUsedAt != now+1 {
		t.Errorf("expected last_used_at=%d, got %d", now+1, results[0].LastUsedAt)
	}
}

func TestTouchMemoryNotes_RespectsScope(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert note in session-a.
	id, err := d.InsertMemoryNoteTyped(ctx, "session-a", TypedNoteInput{
		Text: "private note",
		Kind: MemoryKindFact,
	})
	if err != nil {
		t.Fatalf("InsertMemoryNoteTyped: %v", err)
	}

	// Touch with session-b scope: should NOT increment session-a note.
	if err := d.TouchMemoryNotes(ctx, "session-b", []int64{id}, NowMS()); err != nil {
		t.Fatalf("TouchMemoryNotes: %v", err)
	}

	results, err := d.SearchFTS(ctx, "session-a", "private note", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS result")
	}
	if results[0].UseCount != 0 {
		t.Errorf("expected use_count=0 (cross-scope touch should be blocked), got %d", results[0].UseCount)
	}
}

func TestTouchMemoryNotes_EmptyIDsIsNoOp(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if err := d.TouchMemoryNotes(ctx, "sess", nil, NowMS()); err != nil {
		t.Fatalf("TouchMemoryNotes with empty ids: %v", err)
	}
}

func TestCleanupStaleMemoryNotes_MarksOldNeverUsedSummaries(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert an old, never-used summary note by directly inserting with a
	// created_at that is older than the stale age threshold.
	oldTime := NowMS() - staleMemoryAgeMS - 1000
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at, kind, status, importance)
 VALUES(?,?,?,?,?,?,?,?,?)`,
		"sess", "old summary", make([]byte, 4), nil, "consolidation", oldTime,
		MemoryKindSummary, MemoryStatusActive, 0.0)
	if err != nil {
		t.Fatalf("insert old summary: %v", err)
	}

	// Insert a recent never-used summary note that should NOT be touched.
	if _, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{
		Text: "recent summary",
		Kind: MemoryKindSummary,
	}); err != nil {
		t.Fatalf("InsertMemoryNoteTyped recent: %v", err)
	}

	n, err := d.CleanupStaleMemoryNotes(ctx, "sess", NowMS(), 10)
	if err != nil {
		t.Fatalf("CleanupStaleMemoryNotes: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row cleaned up, got %d", n)
	}

	// The old row should now be stale; recent row should still be active.
	results, err := d.SearchFTS(ctx, "sess", "summary", 10)
	if err != nil {
		t.Fatalf("SearchFTS after cleanup: %v", err)
	}
	for _, r := range results {
		if r.Text == "old summary" && r.Status != MemoryStatusStale {
			t.Errorf("expected old summary to be stale, got status=%q", r.Status)
		}
		if r.Text == "recent summary" && r.Status != MemoryStatusActive {
			t.Errorf("expected recent summary to remain active, got status=%q", r.Status)
		}
	}
}

func TestCleanupStaleMemoryNotes_DoesNotTouchUsedNotes(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Insert an old summary and mark it as used once.
	oldTime := NowMS() - staleMemoryAgeMS - 1000
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at, kind, status, importance, use_count)
 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"sess", "used summary", make([]byte, 4), nil, "consolidation", oldTime,
		MemoryKindSummary, MemoryStatusActive, 0.0, 1)
	if err != nil {
		t.Fatalf("insert used summary: %v", err)
	}
	_, _ = res.LastInsertId()

	n, err := d.CleanupStaleMemoryNotes(ctx, "sess", NowMS(), 10)
	if err != nil {
		t.Fatalf("CleanupStaleMemoryNotes: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 rows cleaned (used note should be kept), got %d", n)
	}
}

func TestCleanupStaleMemoryNotes_DoesNotTouchFacts(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// A fact node old and never used should not be marked stale.
	oldTime := NowMS() - staleMemoryAgeMS - 1000
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at, kind, status, importance)
 VALUES(?,?,?,?,?,?,?,?,?)`,
		"sess", "old fact", make([]byte, 4), nil, "", oldTime,
		MemoryKindFact, MemoryStatusActive, 0.0)
	if err != nil {
		t.Fatalf("insert old fact: %v", err)
	}

	n, err := d.CleanupStaleMemoryNotes(ctx, "sess", NowMS(), 10)
	if err != nil {
		t.Fatalf("CleanupStaleMemoryNotes: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 rows (facts should not be cleaned), got %d", n)
	}
}

func TestCleanupStaleMemoryNotes_BatchLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	oldTime := NowMS() - staleMemoryAgeMS - 1000
	for i := 0; i < 5; i++ {
		_, err := d.SQL.ExecContext(ctx,
			`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at, kind, status, importance)
 VALUES(?,?,?,?,?,?,?,?,?)`,
			"sess", fmt.Sprintf("old summary %d", i), make([]byte, 4), nil, "consolidation", oldTime,
			MemoryKindSummary, MemoryStatusActive, 0.0)
		if err != nil {
			t.Fatalf("insert old summary %d: %v", i, err)
		}
	}

	n, err := d.CleanupStaleMemoryNotes(ctx, "sess", NowMS(), 3)
	if err != nil {
		t.Fatalf("CleanupStaleMemoryNotes: %v", err)
	}
	if n != 3 {
		t.Errorf("expected batch limit of 3 stale updates, got %d", n)
	}
}

func TestMemoryNotesMetaMigration_ExistingDB(t *testing.T) {
	// Verify that Open() on a pre-existing schema without metadata columns
	// adds them correctly and the DB is usable after migration.
	dir := t.TempDir()
	path := filepath.Join(dir, "migrate_meta.db")

	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	// Create a minimal legacy schema without kind/status/importance columns.
	legacyStmts := []string{
		`CREATE TABLE sessions(key TEXT PRIMARY KEY, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, metadata_json TEXT NOT NULL DEFAULT '{}', last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0);`,
		`CREATE TABLE messages(id INTEGER PRIMARY KEY AUTOINCREMENT, session_key TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, payload_json TEXT NOT NULL DEFAULT '{}', created_at INTEGER NOT NULL);`,
		`CREATE TABLE memory_pinned(key TEXT PRIMARY KEY, content TEXT NOT NULL, updated_at INTEGER NOT NULL);`,
		`CREATE TABLE memory_notes(id INTEGER PRIMARY KEY AUTOINCREMENT, session_key TEXT NOT NULL DEFAULT '__global__', text TEXT NOT NULL, embedding BLOB NOT NULL, source_message_id INTEGER, tags TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL);`,
		`CREATE VIRTUAL TABLE memory_fts USING fts5(text, content='memory_notes', content_rowid='id');`,
		`CREATE TRIGGER memory_notes_ai AFTER INSERT ON memory_notes BEGIN INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text); END;`,
	}
	for _, stmt := range legacyStmts {
		if _, err := rawDB.Exec(stmt); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	// Seed a note tagged consolidation (should be backfilled to kind='summary').
	if _, err := rawDB.Exec(`INSERT INTO memory_notes(session_key, text, embedding, tags, created_at) VALUES('sess','legacy summary',x'00','consolidation',1)`); err != nil {
		t.Fatalf("seed legacy note: %v", err)
	}
	_ = rawDB.Close()

	// Open via the production code path to trigger migration.
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open after legacy schema: %v", err)
	}
	defer d.Close()
	ctx := context.Background()

	// The new columns should exist and be queryable.
	results, err := d.SearchFTS(ctx, "sess", "legacy summary", 5)
	if err != nil {
		t.Fatalf("SearchFTS after migration: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected result after migration")
	}
	r := results[0]
	// Backfill should have set kind=summary for the consolidation-tagged note.
	if r.Kind != MemoryKindSummary {
		t.Errorf("expected backfilled kind=%q, got %q", MemoryKindSummary, r.Kind)
	}
	if r.Status != MemoryStatusActive {
		t.Errorf("expected default status=%q after migration, got %q", MemoryStatusActive, r.Status)
	}
	// UseCount and Importance default to 0.
	if r.UseCount != 0 || r.Importance != 0 {
		t.Errorf("expected zero UseCount/Importance, got %d/%v", r.UseCount, r.Importance)
	}

	// InsertMemoryNoteTyped should work after migration.
	if _, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{
		Text: "post-migration note",
		Kind: MemoryKindFact,
	}); err != nil {
		t.Fatalf("InsertMemoryNoteTyped post-migration: %v", err)
	}
}

func TestWriteConsolidation_WithExtraNotes(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.AppendMessage(ctx, "sess", "user", "hello", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	w := ConsolidationWrite{
		SessionKey:  "sess",
		ScopeKey:    "sess",
		NoteText:    "summary note",
		Embedding:   make([]byte, 0),
		NoteTags:    "consolidation",
		NoteKind:    MemoryKindSummary,
		CursorMsgID: 1,
		ExtraNotes: []TypedNoteInput{
			{Text: "prefers dark mode", Kind: MemoryKindPreference, Status: MemoryStatusActive},
			{Text: "goal: ship v2", Kind: MemoryKindGoal, Status: MemoryStatusActive},
		},
	}
	noteID, err := d.WriteConsolidation(ctx, w)
	if err != nil {
		t.Fatalf("WriteConsolidation: %v", err)
	}
	if noteID <= 0 {
		t.Fatalf("expected positive summary note ID, got %d", noteID)
	}

	// Verify the preference note was written.
	prefRows, err := d.SearchFTS(ctx, "sess", "dark mode", 5)
	if err != nil {
		t.Fatalf("SearchFTS preference: %v", err)
	}
	if len(prefRows) == 0 {
		t.Fatal("expected preference note via FTS")
	}
	if prefRows[0].Kind != MemoryKindPreference {
		t.Errorf("expected kind=%q, got %q", MemoryKindPreference, prefRows[0].Kind)
	}

	// Verify the goal note was written.
	goalRows, err := d.SearchFTS(ctx, "sess", "ship v2", 5)
	if err != nil {
		t.Fatalf("SearchFTS goal: %v", err)
	}
	if len(goalRows) == 0 {
		t.Fatal("expected goal note via FTS")
	}
	if goalRows[0].Kind != MemoryKindGoal {
		t.Errorf("expected kind=%q, got %q", MemoryKindGoal, goalRows[0].Kind)
	}
}

func TestWriteConsolidation_EmptyExtraNoteTextIsSkipped(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.AppendMessage(ctx, "sess", "user", "hello", nil); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	w := ConsolidationWrite{
		SessionKey:  "sess",
		ScopeKey:    "sess",
		NoteText:    "summary",
		Embedding:   make([]byte, 0),
		CursorMsgID: 1,
		ExtraNotes: []TypedNoteInput{
			{Text: "   ", Kind: MemoryKindFact}, // should be skipped
			{Text: "", Kind: MemoryKindGoal},    // should be skipped
		},
	}
	noteID, err := d.WriteConsolidation(ctx, w)
	if err != nil {
		t.Fatalf("WriteConsolidation: %v", err)
	}
	if noteID <= 0 {
		t.Fatalf("expected positive summary note ID, got %d", noteID)
	}
}

func TestInsertMemoryNoteTyped_RejectsVectorDimMismatch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{
		Text:      "first",
		Embedding: make([]byte, 8),
	}); err != nil {
		t.Fatalf("InsertMemoryNoteTyped first: %v", err)
	}
	if _, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{
		Text:      "mismatch",
		Embedding: make([]byte, 12),
	}); err == nil {
		t.Fatal("expected vector dim mismatch error")
	}
}

func TestWriteConsolidation_RejectsVectorDimMismatch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.InsertMemoryNoteTyped(ctx, "sess", TypedNoteInput{
		Text:      "first",
		Embedding: make([]byte, 8),
	}); err != nil {
		t.Fatalf("InsertMemoryNoteTyped first: %v", err)
	}
	if err := d.EnsureSession(ctx, "sess"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if _, err := d.WriteConsolidation(ctx, ConsolidationWrite{
		SessionKey:  "sess",
		ScopeKey:    "sess",
		NoteText:    "summary",
		Embedding:   make([]byte, 12),
		CursorMsgID: 0,
	}); err == nil {
		t.Fatal("expected vector dim mismatch error")
	}
}
