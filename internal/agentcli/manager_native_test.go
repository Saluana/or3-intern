package agentcli

import (
	"encoding/json"
	"testing"

	"or3-intern/internal/db"
)

func TestEventToMapIncludesParsedPayload(t *testing.T) {
	payload := json.RawMessage(`{"type":"message.part.updated","sessionID":"session_42"}`)
	mapped := eventToMap(AgentRunEvent{Type: "structured", Payload: payload})
	raw, ok := mapped["payload"]
	if !ok {
		t.Fatalf("expected payload in mapped event: %#v", mapped)
	}
	payloadMap, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("expected payload map, got %#v", raw)
	}
	if payloadMap["sessionID"] != "session_42" {
		t.Fatalf("expected session_42, got %#v", payloadMap["sessionID"])
	}
}

func TestManagerBuildCommandSpecForRunnerChatNative(t *testing.T) {
	manager := &Manager{Registry: NewDefaultRegistry()}
	meta := map[string]any{
		"runner_chat_session_id":         "chat_sess_1",
		"runner_chat_turn_id":            "turn_1",
		"runner_chat_continuation_mode":  string(ContinuationNative),
		"runner_chat_user_message":       "pick up where you left off",
		"runner_chat_replay_prompt":      "ignore this replay prompt",
		"runner_chat_native_session_ref": "session_live_99",
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	cmd, err := manager.buildCommandSpecForRun(db.AgentCLIRun{
		RunnerID:  string(RunnerOpenCode),
		Task:      "fallback task",
		Model:     "gpt-5",
		Mode:      string(RunnerModeSandboxAuto),
		MetaJSON:  string(metaJSON),
	})
	if err != nil {
		t.Fatalf("buildCommandSpecForRun: %v", err)
	}
	want := []string{"run", "--format", "json", "--session", "session_live_99", "--model", "gpt-5", "--dangerously-skip-permissions", "pick up where you left off"}
	assertArgsEqual(t, want, cmd.Args)
}