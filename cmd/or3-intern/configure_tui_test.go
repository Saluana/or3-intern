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
	if !strings.Contains(view, "Field 3/8") {
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
