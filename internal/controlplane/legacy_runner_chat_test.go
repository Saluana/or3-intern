package controlplane

import (
	"testing"

	"or3-intern/internal/agentcli"
	"or3-intern/internal/db"
)

func TestBuildChatSessionMetaResponseLegacyRunner(t *testing.T) {
	resp := BuildChatSessionMetaResponse(db.ChatSessionMeta{
		SessionKey: "legacy:sess",
		RunnerID:   string(agentcli.RunnerOR3),
	})
	if resp["legacy_runner_id"] != string(agentcli.RunnerOR3) {
		t.Fatalf("expected legacy_runner_id, got %#v", resp)
	}
	if resp["runner_selectable"] != false {
		t.Fatalf("expected runner_selectable=false, got %#v", resp["runner_selectable"])
	}
}
