package agent

import (
	"strings"
	"testing"
)

func TestStaticPromptHashStableAcrossDifferentUserRequests(t *testing.T) {
	b := &Builder{
		Soul:              DefaultSoul,
		AgentInstructions: DefaultAgentInstructions,
		ToolNotes:         DefaultToolNotes,
	}
	first := b.buildContextPacket(turnPromptInput{currentUserMessage: "first request"})
	second := b.buildContextPacket(turnPromptInput{currentUserMessage: "second request"})
	renderProviderMessages(&first, b)
	renderProviderMessages(&second, b)
	if first.CacheDiagnostics.StaticPromptHash == "" {
		t.Fatal("expected static prompt hash")
	}
	if first.CacheDiagnostics.StaticPromptHash != second.CacheDiagnostics.StaticPromptHash {
		t.Fatalf("static hash changed: %s vs %s", first.CacheDiagnostics.StaticPromptHash, second.CacheDiagnostics.StaticPromptHash)
	}
	if first.CacheDiagnostics.TurnPromptHash == second.CacheDiagnostics.TurnPromptHash {
		t.Fatalf("expected turn hash to differ across requests")
	}
}

func TestRetrievedMemoryDoesNotChangeStaticPromptHash(t *testing.T) {
	b := &Builder{
		Soul:              DefaultSoul,
		AgentInstructions: DefaultAgentInstructions,
		ToolNotes:         DefaultToolNotes,
	}
	without := b.buildContextPacket(turnPromptInput{})
	with := b.buildContextPacket(turnPromptInput{memText: "1) [memory] stale fact"})
	renderProviderMessages(&without, b)
	renderProviderMessages(&with, b)
	if without.CacheDiagnostics.StaticPromptHash != with.CacheDiagnostics.StaticPromptHash {
		t.Fatalf("retrieved memory changed static hash: %s vs %s", without.CacheDiagnostics.StaticPromptHash, with.CacheDiagnostics.StaticPromptHash)
	}
	if without.CacheDiagnostics.SessionPromptHash != with.CacheDiagnostics.SessionPromptHash {
		t.Fatalf("retrieved memory changed session hash: %s vs %s", without.CacheDiagnostics.SessionPromptHash, with.CacheDiagnostics.SessionPromptHash)
	}
	if with.CacheDiagnostics.TurnPromptHash == without.CacheDiagnostics.TurnPromptHash {
		t.Fatalf("expected retrieved memory to affect only turn tier")
	}
}

func TestChatAttachmentDecodeAcceptsWorkspaceRef(t *testing.T) {
	atts := DecodeChatAttachments([]any{
		map[string]any{
			"id":     "att-1",
			"source": "workspace_ref",
			"kind":   "file",
			"name":   "main.go",
			"path":   "cmd/main.go",
			"root_id": "workspace",
		},
	})
	if len(atts) != 1 {
		t.Fatalf("expected one attachment, got %#v", atts)
	}
	if err := ValidateChatAttachments(atts); err != nil {
		t.Fatalf("ValidateChatAttachments: %v", err)
	}
	body := renderUserAttachmentsBody(atts)
	if !strings.Contains(body, `path="cmd/main.go"`) {
		t.Fatalf("expected workspace path in prompt body, got %q", body)
	}
}
