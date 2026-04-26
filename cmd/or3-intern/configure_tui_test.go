package main

import (
	"strings"
	"testing"
	"unicode/utf8"

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
	if !strings.Contains(view, "Current value:") || !strings.Contains(view, "main AI model") {
		t.Fatalf("expected selected field panel to show current value and plain-language help, got %q", view)
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
	for _, label := range []string{"Runtime", "Context", "Tools", "Skills", "Security", "Hardening", "Automation"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expected %q in section picker, got %q", label, view)
		}
	}
}

func TestConfigureTUIAppliesContextFields(t *testing.T) {
	cfg := config.Default()
	if changed, err := applyChoiceSelection(&cfg, "context", "", "context_mode", "balanced"); err != nil || !changed {
		t.Fatalf("apply context mode: changed=%v err=%v", changed, err)
	}
	if changed, err := applyFieldValue(&cfg, "context", "", "context_max_input_tokens", "12345"); err != nil || !changed {
		t.Fatalf("apply context max input: changed=%v err=%v", changed, err)
	}
	if changed := setToggleFieldValue(&cfg, "context", "", "context_manager_enabled", true); !changed {
		t.Fatal("expected context manager toggle to apply")
	}
	if changed, err := applyFieldValue(&cfg, "context", "", "context_manager_model", "mini-context"); err != nil || !changed {
		t.Fatalf("apply context manager model: changed=%v err=%v", changed, err)
	}
	if changed, err := applyFieldValue(&cfg, "context", "", "context_manager_idle_prune", "120"); err != nil || !changed {
		t.Fatalf("apply context manager idle prune: changed=%v err=%v", changed, err)
	}
	if cfg.Context.Mode != "balanced" || cfg.Context.MaxInputTokens != 12345 || !cfg.ContextManager.Enabled || cfg.ContextManager.Model != "mini-context" || cfg.ContextManager.IdlePruneSeconds != 120 {
		t.Fatalf("unexpected context config: %+v manager=%+v", cfg.Context, cfg.ContextManager)
	}
}

func TestConfigureTUIFieldDescriptionsAreHelpful(t *testing.T) {
	cfg := config.Default()
	sections := []string{"provider", "storage", "runtime", "context", "workspace", "tools", "docindex", "skills", "security", "hardening", "session", "automation", "service"}
	for _, section := range sections {
		for _, field := range buildSectionFields(cfg, section, "/workspace/project") {
			if len(strings.Fields(field.Description)) < 8 {
				t.Fatalf("expected helpful description for %s/%s, got %q", section, field.Key, field.Description)
			}
		}
	}
	for _, channel := range []string{"telegram", "slack", "discord", "whatsapp", "email"} {
		for _, field := range buildChannelFields(cfg, channel) {
			if len(strings.Fields(field.Description)) < 8 {
				t.Fatalf("expected helpful description for %s/%s, got %q", channel, field.Key, field.Description)
			}
		}
	}
}

func TestBuildSectionFields_ServiceIncludesLocalPairingToggle(t *testing.T) {
	fields := buildSectionFields(config.Default(), "service", "/workspace/project")
	for _, field := range fields {
		if field.Key == "service_allow_unauthenticated_pairing" {
			if field.Kind != configureFieldToggle {
				t.Fatalf("expected local pairing field to be a toggle, got %v", field.Kind)
			}
			if !strings.Contains(field.Description, "same computer") {
				t.Fatalf("expected plain-language explanation for local pairing field, got %q", field.Description)
			}
			return
		}
	}
	t.Fatal("expected service section to include local pairing toggle")
}

func TestSetToggleFieldValue_AppliesServiceLocalPairingToggle(t *testing.T) {
	cfg := config.Default()
	if cfg.Service.AllowUnauthenticatedPairing {
		t.Fatal("expected default local pairing bootstrap to be off")
	}
	if changed := setToggleFieldValue(&cfg, "service", "", "service_allow_unauthenticated_pairing", true); !changed {
		t.Fatal("expected local pairing bootstrap toggle to apply")
	}
	if !cfg.Service.AllowUnauthenticatedPairing {
		t.Fatal("expected local pairing bootstrap to be enabled")
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

func TestRenderSummaryPanelMode_NoChannelsDoesNotOverwriteAutomation(t *testing.T) {
	styles := newConfigureStyles()
	cfg := config.Default()
	panel := renderSummaryPanelMode(styles, cfg, "", false)
	if !strings.Contains(panel, "Automation:") {
		t.Fatalf("expected automation row to remain visible, got %q", panel)
	}
	if !strings.Contains(panel, "Channels:") || !strings.Contains(panel, "none enabled") {
		t.Fatalf("expected no-channels fallback in channels row, got %q", panel)
	}
}

func TestTruncateConfigureLine_PreservesUTF8(t *testing.T) {
	value := "日本語の設定値"
	truncated := truncateConfigureLine(value, 6)
	if !utf8.ValidString(truncated) {
		t.Fatalf("expected valid UTF-8 after truncation, got %q", truncated)
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Fatalf("expected ellipsis suffix after truncation, got %q", truncated)
	}
}
