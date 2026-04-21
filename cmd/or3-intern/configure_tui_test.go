package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"or3-intern/internal/config"
)

func TestConfigureTUIFormNavigationHighlightsSelectedField(t *testing.T) {
	model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", config.Default(), false, "", configureTUIOptions{
		Restricted: []string{"provider"},
	})
	model.height = 28

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.(configureTUIModel).Update(tea.KeyMsg{Type: tea.KeyDown})
	finalModel := updated.(configureTUIModel)

	if finalModel.fieldCursor != 2 {
		t.Fatalf("expected field cursor 2, got %d", finalModel.fieldCursor)
	}
	view := finalModel.View()
	if !strings.Contains(view, "Field 3/9") {
		t.Fatalf("expected field position hint in view, got %q", view)
	}
	if !strings.Contains(view, "Selected field") || !strings.Contains(view, "Chat model") {
		t.Fatalf("expected selected field summary for Chat model, got %q", view)
	}
	if !strings.Contains(view, "▶ ") {
		t.Fatalf("expected visible selection indicator, got %q", view)
	}
}

func TestConfigureTUIFormNavigationScrollsLongSections(t *testing.T) {
	cfg := config.Default()
	model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", cfg, false, "", configureTUIOptions{})
	model.height = 20
	model.currentSection = "channels"
	model.currentChannel = "email"
	model.screen = configureScreenForm

	for i := 0; i < 8; i++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(configureTUIModel)
	}

	if model.fieldCursor != 8 {
		t.Fatalf("expected field cursor 8, got %d", model.fieldCursor)
	}
	if model.formScroll == 0 {
		t.Fatal("expected form scroll to advance for long section")
	}
	view := model.View()
	if !strings.Contains(view, "↑ more above") {
		t.Fatalf("expected upward scroll affordance, got %q", view)
	}
	if !strings.Contains(view, "Field 9/12") {
		t.Fatalf("expected updated field position hint, got %q", view)
	}
}

func TestConfigureTUISectionPickerShowsExpandedSections(t *testing.T) {
	items := buildConfigureSectionItems(config.Default(), nil)
	var titles []string
	for _, item := range items {
		entry := item.(configureListItem)
		titles = append(titles, entry.title)
	}
	view := strings.Join(titles, " | ")
	for _, label := range []string{"Runtime", "Tools", "Skills", "Security", "Hardening", "Automation"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expected %q in section picker, got %q", label, view)
		}
	}
}

func TestDeriveConfigureLayoutStacksAndCompactsOnSmallTerminal(t *testing.T) {
	layout := deriveConfigureLayout(78, 20)
	if !layout.stacked {
		t.Fatal("expected stacked layout for narrow terminal")
	}
	if !layout.compact {
		t.Fatal("expected compact layout for narrow/short terminal")
	}
	if layout.fieldRows < 2 {
		t.Fatalf("expected at least 2 visible rows, got %d", layout.fieldRows)
	}
}

func TestConfigureTUIFormStacksAndKeepsSelectedFieldVisible(t *testing.T) {
	model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", config.Default(), false, "", configureTUIOptions{
		Restricted: []string{"provider"},
	})
	model.currentSection = "provider"
	model.screen = configureScreenForm
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 78, Height: 22})
	model = updated.(configureTUIModel)

	view := model.View()
	if !strings.Contains(view, "Current snapshot") {
		t.Fatalf("expected snapshot panel in stacked form view, got %q", view)
	}
	if !strings.Contains(view, "Selected field") {
		t.Fatalf("expected selected field details in stacked form view, got %q", view)
	}
	if !strings.Contains(view, "Field 1/9") {
		t.Fatalf("expected field position hint in stacked form view, got %q", view)
	}
	if !deriveConfigureLayout(model.width, model.height).stacked {
		t.Fatal("expected responsive stacked mode")
	}
}

func TestConfigureTUICompactEditingKeepsEditorVisible(t *testing.T) {
	model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", config.Default(), false, "", configureTUIOptions{
		Restricted: []string{"provider"},
	})
	model.currentSection = "provider"
	model.screen = configureScreenForm
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 74, Height: 18})
	model = updated.(configureTUIModel)
	field := buildSectionFields(model.cfg, "provider", model.cwd)[1]
	model.startEditingField(field)

	view := model.View()
	if !strings.Contains(view, "Editing") {
		t.Fatalf("expected editing panel in compact mode, got %q", view)
	}
	if !strings.Contains(view, "Enter to apply") {
		t.Fatalf("expected apply/cancel help in compact mode, got %q", view)
	}
	if got := model.visibleFormFieldCount(); got > 4 {
		t.Fatalf("expected reduced visible rows in compact mode, got %d", got)
	}
}
