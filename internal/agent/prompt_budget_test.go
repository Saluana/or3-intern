package agent

import (
	"testing"

	"or3-intern/internal/providers"
)

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"", 0},
		{"abc", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"hello world", 3},
	}
	for _, c := range cases {
		got := estimateTokens(c.text)
		if got != c.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}

func TestPressureLevel(t *testing.T) {
	cases := []struct {
		used, total int
		want        PressureLevel
	}{
		{0, 100, PressureNormal},
		{69, 100, PressureNormal},
		{70, 100, PressureWarning},
		{84, 100, PressureWarning},
		{85, 100, PressureHigh},
		{94, 100, PressureHigh},
		{95, 100, PressureEmergency},
		{100, 100, PressureEmergency},
		{0, 0, PressureNormal},
	}
	for _, c := range cases {
		got := pressureLevel(c.used, c.total)
		if got != c.want {
			t.Errorf("pressureLevel(%d, %d) = %v, want %v", c.used, c.total, got, c.want)
		}
	}
}

func TestBuildBudgetReport(t *testing.T) {
	sections := []ContextSection{
		{Name: "system", Text: "hello world system"},
		{Name: "memory", Text: "some memory content here"},
		{Name: "tools", Text: "t"},
	}
	report := buildBudgetReport(sections, 1000, 256)
	if report.TotalBudgetTokens != 1000 {
		t.Errorf("TotalBudgetTokens = %d, want 1000", report.TotalBudgetTokens)
	}
	if report.OutputReserve != 256 {
		t.Errorf("OutputReserve = %d, want 256", report.OutputReserve)
	}
	if len(report.Sections) != 3 {
		t.Errorf("Sections len = %d, want 3", len(report.Sections))
	}
	if report.EstimatedInputTokens <= 0 {
		t.Errorf("EstimatedInputTokens should be > 0")
	}
	if len(report.LargestSections) > 3 {
		t.Errorf("LargestSections len = %d, want <= 3", len(report.LargestSections))
	}
	// largest should be sorted descending
	if len(report.LargestSections) >= 2 {
		if report.LargestSections[0].EstimatedTokens < report.LargestSections[1].EstimatedTokens {
			t.Errorf("LargestSections not sorted descending")
		}
	}
}

func TestProtectedSectionsNotPruned(t *testing.T) {
	s := ContextSection{
		Name:      "pinned",
		Text:      "pinned content",
		Protected: true,
	}
	if !s.Protected {
		t.Error("Protected field should be preserved")
	}
}

func TestEstimateTokensMessages(t *testing.T) {
	msgs := []providers.ChatMessage{
		{Role: "system", Content: "hello world"},
		{Role: "user", Content: "test message"},
	}
	got := estimateTokensMessages(msgs)
	if got <= 0 {
		t.Errorf("estimateTokensMessages should return > 0, got %d", got)
	}
}
