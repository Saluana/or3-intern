package main

import (
	"strings"
	"testing"

	"or3-intern/internal/doctor"
)

func TestBuildDoctorAdminBrainEnvelopeUsesDedicatedPromptTemplate(t *testing.T) {
	report := doctor.Report{
		Summary: doctor.Summary{
			BlockCount: 1,
			ErrorCount: 2,
			WarnCount:  3,
		},
		Findings: []doctor.Finding{
			{Summary: "Block finding", Detail: "Block detail"},
			{Summary: "Error finding", Detail: "Error detail"},
			{Summary: "Warning finding", Detail: "Warning detail"},
			{Summary: "Ignored finding", Detail: "Should not appear"},
		},
	}

	prompt := buildDoctorAdminBrainEnvelope(report, "please help")

	if !strings.HasPrefix(prompt, doctorAdminBrainSystemPrompt) {
		t.Fatalf("expected prompt to start with system prompt, got %q", prompt)
	}
	for _, want := range []string{
		"Current doctor summary:",
		"- Blocking findings: 1",
		"- Error findings: 2",
		"- Warning findings: 3",
		"- Block finding: Block detail",
		"- Error finding: Error detail",
		"- Warning finding: Warning detail",
		"User message:\nplease help",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in prompt, got %q", want, prompt)
		}
	}
	if strings.Contains(prompt, "Ignored finding") {
		t.Fatalf("expected prompt to limit findings, got %q", prompt)
	}
}

func TestBuildDoctorAdminBrainContextExcludesUserMessage(t *testing.T) {
	report := doctor.Report{
		Summary: doctor.Summary{WarnCount: 1},
		Findings: []doctor.Finding{
			{Summary: "Warning finding", Detail: "Warning detail"},
		},
	}

	prompt := buildDoctorAdminBrainContext(report)

	if !strings.HasPrefix(prompt, doctorAdminBrainSystemPrompt) {
		t.Fatalf("expected context to start with system prompt, got %q", prompt)
	}
	if strings.Contains(prompt, "User message:") {
		t.Fatalf("expected context to exclude user message section, got %q", prompt)
	}
	if !strings.Contains(prompt, "- Warning findings: 1") {
		t.Fatalf("expected summary in context, got %q", prompt)
	}
}
