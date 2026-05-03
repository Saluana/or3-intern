package uxstate

import (
	"encoding/json"
	"strings"
	"testing"

	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	intdoctor "or3-intern/internal/doctor"
)

func TestBuildApprovalPromptRepresentativeSubjects(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		subject  any
		want     string
		risk     string
	}{
		{name: "npm install", typeName: string(approval.SubjectExec), subject: approval.ExecSubject{Argv: []string{"npm", "install"}}, want: "run a command", risk: "Medium"},
		{name: "shell", typeName: string(approval.SubjectExec), subject: approval.ExecSubject{Argv: []string{"sh", "-c", "rm -rf tmp"}}, want: "run a command", risk: "High"},
		{name: "skill", typeName: string(approval.SubjectSkillExec), subject: approval.SkillExecutionSubject{SkillID: "demo"}, want: "run a skill", risk: "High"},
		{name: "secret", typeName: string(approval.SubjectSecretAccess), subject: map[string]any{"secret_name": "provider.openai"}, want: "use a secret", risk: "High"},
		{name: "message", typeName: string(approval.SubjectMessageSend), subject: map[string]any{"channel": "slack"}, want: "send a message", risk: "Medium"},
		{name: "file", typeName: string(approval.SubjectFileTransfer), subject: map[string]any{"path": "/tmp/report.txt"}, want: "transfer a file", risk: "Medium"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.subject)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			view := BuildApprovalPrompt(db.ApprovalRequestRecord{ID: 1, Type: tc.typeName, SubjectJSON: string(data), PolicyMode: string(config.ApprovalModeAsk)})
			if !strings.Contains(view.Title, tc.want) || view.RiskLabel != tc.risk {
				t.Fatalf("unexpected view: %#v", view)
			}
		})
	}
}

func TestBuildAccessDashboardViewLocalAndHostedStates(t *testing.T) {
	local := config.Default()
	local.Tools.RestrictToWorkspace = true
	local.WorkspaceDir = t.TempDir()
	local.Service.Enabled = false
	view := BuildAccessDashboardView(local, intdoctor.Report{}, 0, 0)
	if len(view.Sections) != 7 {
		t.Fatalf("expected 7 dashboard sections, got %d", len(view.Sections))
	}
	if view.Sections[0].Name != "Files" || !strings.Contains(view.Sections[0].Status, "Only this folder") {
		t.Fatalf("unexpected files section: %#v", view.Sections[0])
	}
	if view.Sections[1].Name != "Commands" || view.Sections[1].Risk != "green" {
		t.Fatalf("expected disabled command execution to be green, got %#v", view.Sections[1])
	}
	local.Tools.EnableExec = true
	view = BuildAccessDashboardView(local, intdoctor.Report{}, 0, 0)
	if view.Sections[1].Risk != "red" {
		t.Fatalf("expected trusted available command execution to be red, got %#v", view.Sections[1])
	}
	hosted := config.Default()
	hosted.Tools.RestrictToWorkspace = false
	hosted.Service.Enabled = true
	hosted.Service.Secret = "secret"
	hosted.Security.Network.Enabled = true
	hosted.Security.Network.DefaultDeny = true
	view = BuildAccessDashboardView(hosted, intdoctor.Report{}, 2, 1)
	if view.Sections[0].Risk != "red" || view.Sections[2].Risk != "green" || !strings.Contains(view.Sections[4].Status, "2 connected") {
		t.Fatalf("unexpected hosted dashboard: %#v", view.Sections)
	}
}

func TestBuildDeviceViewsFormatsLastUsed(t *testing.T) {
	views := BuildDeviceViews([]db.PairedDeviceRecord{{
		DeviceID:    "device-1",
		DisplayName: "Phone",
		Role:        approval.RoleViewer,
		Status:      approval.StatusActive,
		LastSeenAt:  1712345678,
	}})
	if len(views) != 1 {
		t.Fatalf("expected one device view, got %#v", views)
	}
	if views[0].LastUsed != "2024-04-05 19:34 UTC" {
		t.Fatalf("unexpected last used label: %q", views[0].LastUsed)
	}
}

func TestBuildSettingsHomeView(t *testing.T) {
	cfg := config.Default()
	view := BuildSettingsHomeView(cfg)
	if len(view.Sections) != 9 {
		t.Fatalf("expected settings sections, got %#v", view.Sections)
	}
	if view.Sections[0].Title != "AI Provider" {
		t.Fatalf("expected provider first, got %#v", view.Sections[0])
	}
	if view.Sections[7].Title != "Context" || view.Sections[7].Advanced || !strings.Contains(view.Sections[7].Action, "--section context") {
		t.Fatalf("expected visible context settings section, got %#v", view.Sections[7])
	}
}
