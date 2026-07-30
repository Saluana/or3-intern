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
	if !strings.Contains(view, "Field 3/20") {
		t.Fatalf("expected field position hint in view, got %q", view)
	}
	if !strings.Contains(view, "Selected field") || !strings.Contains(view, "Embedding model") {
		t.Fatalf("expected selected field summary for embedding model, got %q", view)
	}
	if !strings.Contains(view, "Current value:") || !strings.Contains(view, "searchable memory") {
		t.Fatalf("expected selected field panel to show current value and field help, got %q", view)
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
	if !strings.Contains(view, "Field 9/13") {
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
	for _, label := range []string{"Runtime", "Context", "Skills", "Security", "Hardening", "Automation"} {
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
	if changed, err := applyFieldValue(&cfg, "context", "", "context_pressure_warning", "65"); err != nil || !changed {
		t.Fatalf("apply context pressure warning: changed=%v err=%v", changed, err)
	}
	if cfg.Context.Mode != "balanced" || cfg.Context.Pressure.WarningPercent != 65 {
		t.Fatalf("unexpected context config: %+v", cfg.Context)
	}
}

func TestConfigureTUIDiscordEnableDefaultsClosedInboundAccess(t *testing.T) {
	cfg := config.Default()
	if changed, err := applyFieldValue(&cfg, "channels", "discord", "token", "discord-token"); err != nil || !changed {
		t.Fatalf("apply discord token: changed=%v err=%v", changed, err)
	}
	if changed := setToggleFieldValue(&cfg, "channels", "discord", "enabled", true); !changed {
		t.Fatal("expected discord enabled toggle to apply")
	}
	if cfg.Channels.Discord.InboundPolicy != config.InboundPolicyDeny {
		t.Fatalf("expected discord inbound policy to default to deny, got %q", cfg.Channels.Discord.InboundPolicy)
	}

	allowlistCfg := config.Default()
	allowlistCfg.Channels.Discord.Enabled = true
	if changed, err := applyFieldValue(&allowlistCfg, "channels", "discord", "allowlist", "user-123"); err != nil || !changed {
		t.Fatalf("apply discord allowlist: changed=%v err=%v", changed, err)
	}
	if allowlistCfg.Channels.Discord.InboundPolicy != config.InboundPolicyAllowlist {
		t.Fatalf("expected blank discord inbound policy to switch to allowlist, got %q", allowlistCfg.Channels.Discord.InboundPolicy)
	}
}

func TestConfigureTUIFieldDescriptionsAreHelpful(t *testing.T) {
	cfg := config.Default()
	sections := []string{"provider", "storage", "runtime", "context", "workspace", "skills", "security", "hardening", "session", "automation", "service"}
	for _, section := range sections {
		for _, field := range buildSectionFields(cfg, section, "/workspace/project") {
			if len(strings.Fields(field.Description)) < 5 {
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

func TestConfigureTUIScreenAdaptersImplementInterface(t *testing.T) {
	screens := []configureScreenAdapter{
		configureProviderScreen{},
		configureWorkspaceScreen{},
		configureChannelsScreen{},
		configureContextScreen{},
		configureSafetyScreen{},
		configureServiceScreen{},
		configureReviewScreen{},
		configureSuccessScreen{},
	}
	model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", config.Default(), false, "", configureTUIOptions{})
	for _, screen := range screens {
		_ = screen.Init(model)
		if view := screen.View(model); strings.TrimSpace(view) == "" {
			t.Fatalf("expected screen view to render")
		}
		var saved config.Config
		if err := screen.Save(&model, &saved); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
}

func TestConfigureTUIScreenAdaptersHandleScreenUpdates(t *testing.T) {
	t.Run("section picker", func(t *testing.T) {
		model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", config.Default(), false, "", configureTUIOptions{})
		model.screen = configureScreenSections
		handled, _ := model.screenAdapter().Update(tea.KeyMsg{Type: tea.KeyEnter}, &model)
		if !handled || model.screen != configureScreenForm || model.currentSection == "" {
			t.Fatalf("expected section adapter to enter a form, handled=%v screen=%v section=%q", handled, model.screen, model.currentSection)
		}
	})

	t.Run("review back", func(t *testing.T) {
		model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", config.Default(), false, "", configureTUIOptions{Restricted: []string{"provider"}})
		model.currentSection = "provider"
		model.screen = configureScreenReview
		handled, _ := model.screenAdapter().Update(tea.KeyMsg{Type: tea.KeyEsc}, &model)
		if !handled || model.screen != configureScreenForm {
			t.Fatalf("expected review adapter to return to form, handled=%v screen=%v", handled, model.screen)
		}
	})
}

func TestConfigureTUISectionSmokeRendersWithoutPanic(t *testing.T) {
	cfg := config.Default()
	sections := []string{"provider", "storage", "runtime", "context", "workspace", "skills", "security", "hardening", "session", "automation", "service"}
	for _, section := range sections {
		t.Run(section, func(t *testing.T) {
			model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", cfg, false, "", configureTUIOptions{Restricted: []string{section}})
			model.height = 28
			model.currentSection = section
			model.screen = configureScreenForm
			view := model.View()
			if strings.TrimSpace(view) == "" {
				t.Fatalf("expected non-empty view for %s", section)
			}
		})
	}
	for _, channel := range []string{"telegram", "slack", "discord", "whatsapp", "email"} {
		t.Run("channel_"+channel, func(t *testing.T) {
			model := newConfigureTUIModel("/tmp/config.json", "/workspace/project", cfg, false, "", configureTUIOptions{Restricted: []string{"channels"}})
			model.height = 28
			model.currentSection = "channels"
			model.currentChannel = channel
			model.screen = configureScreenForm
			if strings.TrimSpace(model.View()) == "" {
				t.Fatalf("expected non-empty channel view for %s", channel)
			}
		})
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

func TestBuildSectionFields_SkillsSectionExcludesLegacyExecInRunnerOnly(t *testing.T) {
	// Runner-only mode drops the legacy built-in skills exec toggle.
	// Skills are now managed by the runner host instead of the built-in
	// or3-intern skill-exec broker.
	fields := buildSectionFields(config.Default(), "skills", "/workspace/project")
	for _, field := range fields {
		if field.Key == "skills_enable_exec" {
			t.Fatalf("expected skills section to exclude legacy exec toggle in runner-only mode")
		}
	}
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
