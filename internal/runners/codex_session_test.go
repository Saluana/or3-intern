package runners

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func stringsContains(s, sub string) bool { return strings.Contains(s, sub) }

func TestCodexRequestKindClassification(t *testing.T) {
	cases := []struct {
		method string
		want   NativeRequestKind
	}{
		{"item/commandExecution/approval", NativeRequestApproval},
		{"item/fileChange/approval", NativeRequestApproval},
		{"tool/approval", NativeRequestApproval},
		{"applyPatch/approval", NativeRequestApproval},
		{"item/tool/userInput", NativeRequestQuestion},
		{"question_request", NativeRequestQuestion},
		{"item/tool/user_input", NativeRequestInput},
		{"tool/input", NativeRequestInput},
		{"someUnknownMethod", NativeRequestUnknown},
		{"approval_extra", NativeRequestApproval},
		{"question_extra", NativeRequestQuestion},
	}
	for _, c := range cases {
		got := codexRequestKind(c.method)
		if got != c.want {
			t.Errorf("codexRequestKind(%q) = %q, want %q", c.method, got, c.want)
		}
	}
}

func TestParseInt64String(t *testing.T) {
	if v, err := parseInt64String("42"); err != nil || v != 42 {
		t.Fatalf("parseInt64String(42) = %d, %v", v, err)
	}
	if _, err := parseInt64String(""); err == nil {
		t.Fatal("expected error for empty input")
	}
	if _, err := parseInt64String("abc"); err == nil {
		t.Fatal("expected error for non-numeric input")
	}
}

func TestCodexSessionRegisterAndClearRequestRef(t *testing.T) {
	sess := &codexSession{
		threadID:    "thread_1",
		turnID:      "turn_1",
		requestRefs: map[string]NativeRequestRef{},
		startedAt:   time.Now().UTC(),
	}
	ref := sess.RegisterRequestRef(7, "item/commandExecution/approval", map[string]any{
		"message": "needs approval to run rm -rf",
	})
	if ref.RequestID != "7" {
		t.Fatalf("RequestID = %q, want 7", ref.RequestID)
	}
	if ref.Kind != NativeRequestApproval {
		t.Fatalf("Kind = %q, want approval", ref.Kind)
	}
	if ref.ThreadID != "thread_1" || ref.TurnID != "turn_1" {
		t.Fatalf("ref = %+v", ref)
	}
	if !stringsContains(ref.Summary, "needs approval") {
		t.Fatalf("summary = %q, want approval text", ref.Summary)
	}
	sess.mu.Lock()
	if _, ok := sess.requestRefs["7"]; !ok {
		t.Fatal("expected requestRefs to include 7")
	}
	sess.mu.Unlock()
}

func TestCodexAccountParsesEmail(t *testing.T) {
	resp := map[string]any{
		"account": map[string]any{"email": "user@example.com", "plan": "pro", "id": "acct_1"},
	}
	info := parseCodexAccount(resp)
	if info.Email != "user@example.com" {
		t.Fatalf("email = %q", info.Email)
	}
	if info.Plan != "pro" {
		t.Fatalf("plan = %q", info.Plan)
	}
	if info.AccountID != "acct_1" {
		t.Fatalf("account id = %q", info.AccountID)
	}
	if !info.LoggedIn {
		t.Fatal("expected LoggedIn true")
	}
}

func TestCodexSkillsParse(t *testing.T) {
	resp := map[string]any{
		"data": []any{
			map[string]any{"name": "code-review", "displayName": "Code Review", "description": "Reviews code"},
			map[string]any{"name": "test-writer"},
		},
	}
	skills := parseCodexSkills(resp)
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Name != "code-review" || skills[0].DisplayName != "Code Review" {
		t.Fatalf("unexpected first skill: %+v", skills[0])
	}
}

func TestCodexModelListFastAndEffort(t *testing.T) {
	resp := map[string]any{
		"data": []any{
			map[string]any{"model": "gpt-5", "fastMode": true, "supportedReasoningEfforts": []any{}},
		},
	}
	if !codexModelListHasFastKey(resp) {
		t.Fatal("expected fast key detection")
	}
	if !codexModelListHasEffort(resp) {
		t.Fatal("expected effort detection")
	}
	resp2 := map[string]any{"data": []any{map[string]any{"model": "gpt-4"}}}
	if codexModelListHasFastKey(resp2) {
		t.Fatal("expected no fast key")
	}
	if codexModelListHasEffort(resp2) {
		t.Fatal("expected no effort key")
	}
}

func TestCodexSessionCloseIsIdempotent(t *testing.T) {
	sess := &codexSession{
		processExit: make(chan struct{}),
		requestRefs: map[string]NativeRequestRef{},
		startedAt:   time.Now().UTC(),
	}
	if err := sess.Close(context.Background()); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := sess.Close(context.Background()); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestCodexSessionAbortNoActiveThread(t *testing.T) {
	sess := &codexSession{
		processExit: make(chan struct{}),
		requestRefs: map[string]NativeRequestRef{},
		startedAt:   time.Now().UTC(),
	}
	if err := sess.AbortTurn(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestCodexSessionRespondTracksRPCWrite(t *testing.T) {
	// Use a fakeWriteCloser to capture writes without spawning a process.
	writes := make(chan map[string]any, 4)
	rpc := &codexRPC{
		stdin:    &captureWriter{ch: writes},
		pending:  map[int64]chan rpcResponse{},
		done:     make(chan struct{}),
		turnDone: make(chan error, 1),
	}
	sess := &codexSession{
		rpc:         rpc,
		threadID:    "thread_1",
		turnID:      "turn_1",
		requestRefs: map[string]NativeRequestRef{},
	}
	ref := NativeRequestRef{
		RunnerID:  RunnerCodex,
		Kind:      NativeRequestApproval,
		RequestID: "9",
	}
	if err := sess.RespondToRequest(context.Background(), ref, NativeRequestDecision{Decision: "approve"}); err != nil {
		t.Fatalf("respond: %v", err)
	}
	// Drain two writes: requestResponse envelope, then id+result reply.
	var envelope map[string]any
	var reply map[string]any
	select {
	case envelope = <-writes:
	case <-time.After(time.Second):
		t.Fatal("did not see envelope write")
	}
	select {
	case reply = <-writes:
	case <-time.After(time.Second):
		t.Fatal("did not see reply write")
	}
	if envelope["method"] != "requestResponse" {
		t.Fatalf("envelope method = %v", envelope["method"])
	}
	idFloat, ok := reply["id"].(float64)
	if !ok || int64(idFloat) != 9 {
		t.Fatalf("reply id = %v", reply["id"])
	}
}

type captureWriter struct {
	ch chan map[string]any
}

func (c *captureWriter) Write(p []byte) (int, error) {
	line := trimTrailingNewline(p)
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		return len(p), nil
	}
	c.ch <- msg
	return len(p), nil
}

func (c *captureWriter) Close() error { return nil }

func trimTrailingNewline(p []byte) []byte {
	if len(p) > 0 && p[len(p)-1] == '\n' {
		return p[:len(p)-1]
	}
	return p
}

// make sure we don't import io for the test alone
var _ = io.Discard

// silence atomic warning if test compile flags change
var _ atomic.Bool
