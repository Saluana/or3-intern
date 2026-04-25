package agent

import (
	"strings"
	"testing"
)

func TestStablePrefixIsByteStableAcrossTurns(t *testing.T) {
	b := &Builder{
		Soul:              "test soul",
		AgentInstructions: "test agent instructions",
		ToolNotes:         "test tool notes",
	}
	args := [7]string{"test soul", "identity text", "test agent instructions", "test tool notes", "static memory", "pinned content", "skills text"}
	first := b.renderStablePrefix(args[0], args[1], args[2], args[3], args[4], args[5], args[6], 1000)
	second := b.renderStablePrefix(args[0], args[1], args[2], args[3], args[4], args[5], args[6], 1000)
	if first != second {
		t.Errorf("renderStablePrefix not byte-stable across calls:\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestStablePrefixExcludesHeartbeatAndTriggerMetadata(t *testing.T) {
	b := &Builder{}
	result := b.renderStablePrefix("soul", "identity", "agents", "tools", "static", "pinned", "skills", 1000)
	if strings.Contains(result, "Heartbeat") {
		t.Error("stable prefix should not contain Heartbeat section")
	}
	if strings.Contains(result, "Structured Trigger Context") {
		t.Error("stable prefix should not contain Structured Trigger Context section")
	}
	if strings.Contains(result, "Memory Digest") {
		t.Error("stable prefix should not contain Memory Digest section")
	}
}

func TestVolatileSuffixContainsHeartbeatAndTriggerMetadata(t *testing.T) {
	b := &Builder{}
	heartbeatText := "heartbeat text content"
	triggerText := "trigger metadata content"
	result := b.renderVolatileSuffix(heartbeatText, triggerText, "", "(none)", "", "", 1000)
	if !strings.Contains(result, "Heartbeat") {
		t.Error("volatile suffix should contain Heartbeat section")
	}
	if !strings.Contains(result, heartbeatText) {
		t.Error("volatile suffix should contain heartbeat text")
	}
	if !strings.Contains(result, "Structured Trigger Context") {
		t.Error("volatile suffix should contain Structured Trigger Context section")
	}
	if !strings.Contains(result, triggerText) {
		t.Error("volatile suffix should contain trigger text")
	}
}
