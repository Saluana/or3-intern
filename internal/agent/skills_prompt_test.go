package agent

import (
	"strings"
	"testing"

	"or3-intern/internal/skills"
)

func TestComposeSystemPrompt_UsesCompactEligibleSkillList(t *testing.T) {
	b := &Builder{
		Skills: skills.Inventory{
			Skills: []skills.SkillMeta{
				{Name: "visible", Description: "visible desc", Dir: "/tmp/visible", Location: "/tmp/visible", Eligible: true},
				{Name: "secret-skill", Description: "hidden desc", Dir: "/tmp/hidden", Location: "/tmp/hidden", Eligible: true, Hidden: true},
				{Name: "blocked-skill", Description: "blocked desc", Dir: "/tmp/blocked", Location: "/tmp/blocked", Eligible: false},
			},
		},
	}
	got := b.composeSystemPrompt("(none)", "", "(none)", "", "", "", "", "", "")
	if !strings.Contains(got, "- visible | visible desc | /tmp/visible") {
		t.Fatalf("expected compact eligible skill line, got %q", got)
	}
	if strings.Contains(got, "secret-skill") || strings.Contains(got, "blocked-skill") {
		t.Fatalf("expected hidden/ineligible skills to be omitted, got %q", got)
	}
}
