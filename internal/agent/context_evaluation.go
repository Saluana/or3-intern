package agent

import (
	"fmt"
	"strings"

	"or3-intern/internal/providers"
)

type ContextEvaluationFixture struct {
	Name              string
	UserMessage       string
	Soul              string
	Identity          string
	AgentInstructions string
	ToolNotes         string
	PinnedMemory      string
	MemoryDigest      string
	RetrievedMemory   string
	WorkspaceContext  string
	TaskCard          string
	HistoryTurns      int
}

type CachePrefixMeasurement struct {
	StablePrefixBytes int
	TotalInputBytes   int
	EligiblePercent   float64
}

func ContextEvaluationFixtures() []ContextEvaluationFixture {
	return []ContextEvaluationFixture{
		{Name: "coding", UserMessage: "implement the parser fix", WorkspaceContext: "File: internal/agent/prompt.go", TaskCard: "Goal: ship parser fix\nConstraint: keep tests green"},
		{Name: "planning", UserMessage: "make a rollout plan", TaskCard: "Goal: plan rollout\nDecision: use phased rollout"},
		{Name: "debugging", UserMessage: "debug failing tests", RetrievedMemory: "Fact: flaky test uses temp DB", TaskCard: "Goal: debug tests"},
		{Name: "long-running", UserMessage: "continue the migration", PinnedMemory: "- project: context packets", TaskCard: "Goal: migrate context packets\nOpen Question: rollout default"},
		{Name: "repeated-memories", UserMessage: "remember deploy details", MemoryDigest: "- Fact: deploy uses staging\n- Fact: deploy uses staging"},
		{Name: "stale-memory", UserMessage: "find current runbook", RetrievedMemory: "Warning: old runbook is stale"},
		{Name: "large-tool-log", UserMessage: "summarize the build log", RetrievedMemory: "Artifact: build failed | Ref: artifact:log-1"},
		{Name: "channel-session", UserMessage: "reply in channel", TaskCard: "Goal: answer slack thread\nRef: message:42"},
		{Name: "workspace-retrieval", UserMessage: "inspect workspace docs", WorkspaceContext: "README.md: setup and usage"},
	}
}

func packetForEvaluationFixture(f ContextEvaluationFixture, budgets ContextSectionBudgets, maxInput int) ContextPacket {
	b := &Builder{
		Soul:                  firstNonEmptyEval(f.Soul, DefaultSoul),
		IdentityText:          f.Identity,
		AgentInstructions:     firstNonEmptyEval(f.AgentInstructions, DefaultAgentInstructions),
		ToolNotes:             firstNonEmptyEval(f.ToolNotes, DefaultToolNotes),
		ContextMaxInputTokens: maxInput,
		ContextSectionBudgets: budgets,
		BootstrapMaxChars:     20000,
	}
	packet := b.buildContextPacket(f.PinnedMemory, f.MemoryDigest, f.RetrievedMemory, f.Identity, "", "", "active_task_card:\n"+f.TaskCard, "", f.WorkspaceContext)
	for i := 0; i < f.HistoryTurns; i++ {
		packet.RecentHistory = append(packet.RecentHistory, structChatMessage("user", fmt.Sprintf("turn %d", i)))
		packet.RecentHistory = append(packet.RecentHistory, structChatMessage("assistant", fmt.Sprintf("answer %d", i)))
	}
	packet.Budget = estimatePacketBudget(packet, b)
	return packet
}

func MeasureCachePrefix(packet ContextPacket, totalMessagesText string) CachePrefixMeasurement {
	stable := renderStablePrefix(packet)
	totalBytes := len(stable) + len(renderVolatileSuffix(packet)) + len(totalMessagesText)
	if totalBytes <= 0 {
		return CachePrefixMeasurement{}
	}
	return CachePrefixMeasurement{StablePrefixBytes: len(stable), TotalInputBytes: totalBytes, EligiblePercent: float64(len(stable)) / float64(totalBytes) * 100}
}

func firstNonEmptyEval(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func structChatMessage(role, content string) providers.ChatMessage {
	return providers.ChatMessage{Role: role, Content: content}
}
