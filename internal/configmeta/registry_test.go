package configmeta

import (
	"testing"
)

func TestRiskRank(t *testing.T) {
	tests := []struct {
		name     string
		level    RiskLevel
		expected int
	}{
		{"safe", RiskSafe, 1},
		{"notice", RiskNotice, 2},
		{"warning", RiskWarning, 3},
		{"danger", RiskDanger, 4},
		{"unknown", RiskLevel("unknown"), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RiskRank(tt.level)
			if result != tt.expected {
				t.Errorf("RiskRank(%v) = %v, want %v", tt.level, result, tt.expected)
			}
		})
	}
}

func TestHigherRisk(t *testing.T) {
	tests := []struct {
		name     string
		a        RiskLevel
		b        RiskLevel
		expected RiskLevel
	}{
		{"safe vs notice", RiskSafe, RiskNotice, RiskNotice},
		{"notice vs safe", RiskNotice, RiskSafe, RiskNotice},
		{"warning vs danger", RiskWarning, RiskDanger, RiskDanger},
		{"danger vs warning", RiskDanger, RiskWarning, RiskDanger},
		{"safe vs safe", RiskSafe, RiskSafe, RiskSafe},
		{"warning vs warning", RiskWarning, RiskWarning, RiskWarning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HigherRisk(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("HigherRisk(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	// Clear registry before test
	Clear()

	// Register test metadata
	testMeta := ConfigFieldMetadata{
		Section:          "test",
		Key:              "field1",
		Path:             "test.field1",
		Label:            "Test Field",
		Description:      "A test field",
		Risk:             RiskSafe,
		RestartRequired:  false,
		RequiresApproval: false,
		RequiresStepUp:   false,
	}

	Register(testMeta)

	// Test Get
	t.Run("Get existing field", func(t *testing.T) {
		meta, ok := Get("test", "field1")
		if !ok {
			t.Error("Get(test, field1) returned false, want true")
		}
		if meta.Label != "Test Field" {
			t.Errorf("Get(test, field1).Label = %v, want 'Test Field'", meta.Label)
		}
	})

	t.Run("Get non-existing field", func(t *testing.T) {
		_, ok := Get("test", "nonexistent")
		if ok {
			t.Error("Get(test, nonexistent) returned true, want false")
		}
	})

	// Test GetByPath
	t.Run("GetByPath existing path", func(t *testing.T) {
		meta, ok := GetByPath("test.field1")
		if !ok {
			t.Error("GetByPath(test.field1) returned false, want true")
		}
		if meta.Label != "Test Field" {
			t.Errorf("GetByPath(test.field1).Label = %v, want 'Test Field'", meta.Label)
		}
	})

	t.Run("GetByPath non-existing path", func(t *testing.T) {
		_, ok := GetByPath("test.nonexistent")
		if ok {
			t.Error("GetByPath(test.nonexistent) returned true, want false")
		}
	})

	// Test List
	t.Run("List all fields", func(t *testing.T) {
		fields := List()
		if len(fields) != 1 {
			t.Errorf("List() length = %v, want 1", len(fields))
		}
	})

	// Test ListBySection
	t.Run("ListBySection existing section", func(t *testing.T) {
		fields := ListBySection("test")
		if len(fields) != 1 {
			t.Errorf("ListBySection(test) length = %v, want 1", len(fields))
		}
	})

	t.Run("ListBySection non-existing section", func(t *testing.T) {
		fields := ListBySection("nonexistent")
		if len(fields) != 0 {
			t.Errorf("ListBySection(nonexistent) length = %v, want 0", len(fields))
		}
	})

	// Test Clear
	t.Run("Clear registry", func(t *testing.T) {
		Clear()
		fields := List()
		if len(fields) != 0 {
			t.Errorf("After Clear(), List() length = %v, want 0", len(fields))
		}
	})
}

func TestRegistryWildcardLookup(t *testing.T) {
	Clear()
	Register(ConfigFieldMetadata{
		Section:         "skills_entry",
		Key:             "env.*",
		Path:            "skills.entries.*.env.*",
		Label:           "Skill Environment Override",
		Description:     "Wildcard metadata",
		Risk:            RiskWarning,
		RestartRequired: false,
	})
	meta, ok := Get("skills_entry", "env.API_TOKEN")
	if !ok {
		t.Fatal("expected wildcard section/key metadata to resolve")
	}
	if meta.Key != "env.*" {
		t.Fatalf("expected env.* metadata, got %#v", meta)
	}
	meta, ok = GetByPath("skills.entries.demo.env.API_TOKEN")
	if !ok {
		t.Fatal("expected wildcard path metadata to resolve")
	}
	if meta.Path != "skills.entries.*.env.*" {
		t.Fatalf("expected wildcard path metadata, got %#v", meta)
	}
	Register(ConfigFieldMetadata{
		Section:         "skills_entry",
		Key:             "config.managed_reference",
		Path:            "skills.entries.*.config.managed_reference",
		Label:           "Managed reference",
		Description:     "Specific metadata",
		Risk:            RiskWarning,
		RestartRequired: false,
	})
	meta, ok = Get("skills_entry", "config.managed_reference")
	if !ok || meta.Key != "config.managed_reference" {
		t.Fatalf("expected more specific metadata to win, got %#v ok=%t", meta, ok)
	}
	fields := List()
	if len(fields) != 2 {
		t.Fatalf("expected List to include wildcard metadata, got %#v", fields)
	}
	sectionFields := ListBySection("skills_entry")
	if len(sectionFields) != 2 {
		t.Fatalf("expected ListBySection to include wildcard metadata, got %#v", sectionFields)
	}
}

func TestRegisterFirstSliceFields(t *testing.T) {
	// Clear registry before test
	Clear()

	// Register first slice fields
	RegisterFirstSliceFields()

	// Verify some key fields are registered
	tests := []struct {
		section string
		key     string
		path    string
	}{
		{"provider", "api_base", "provider.apiBase"},
		{"provider", "api_key", "provider.apiKey"},
		{"provider", "openai_api_key", "providers.profiles.openai.apiKey"},
		{"tools", "enable_exec", "tools.enableExec"},
		{"service", "enabled", "service.enabled"},
		{"skills", "trust_policy", "skills.trustPolicy"},
		{"skills_entry", "enabled", "skills.entries.*.enabled"},
		{"service_action", "restart", "actions.restartService"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			// Test Get
			meta, ok := Get(tt.section, tt.key)
			if !ok {
				t.Errorf("Get(%v, %v) returned false, want true", tt.section, tt.key)
				return
			}

			// Verify path matches
			if meta.Path != tt.path {
				t.Errorf("Get(%v, %v).Path = %v, want %v", tt.section, tt.key, meta.Path, tt.path)
			}

			// Test GetByPath
			metaByPath, ok := GetByPath(tt.path)
			if !ok {
				t.Errorf("GetByPath(%v) returned false, want true", tt.path)
				return
			}

			// Verify section and key match
			if metaByPath.Section != tt.section {
				t.Errorf("GetByPath(%v).Section = %v, want %v", tt.path, metaByPath.Section, tt.section)
			}
			if metaByPath.Key != tt.key {
				t.Errorf("GetByPath(%v).Key = %v, want %v", tt.path, metaByPath.Key, tt.key)
			}
		})
	}

	// Verify risk levels are set correctly
	t.Run("API key is warning risk", func(t *testing.T) {
		meta, ok := Get("provider", "api_key")
		if !ok {
			t.Error("Get(provider, api_key) returned false")
			return
		}
		if meta.Risk != RiskWarning {
			t.Errorf("API key risk = %v, want %v", meta.Risk, RiskWarning)
		}
		if !meta.Secret {
			t.Error("API key should be marked as secret")
		}
	})

	t.Run("Enable exec is danger risk", func(t *testing.T) {
		meta, ok := Get("tools", "enable_exec")
		if !ok {
			t.Error("Get(tools, enable_exec) returned false")
			return
		}
		if meta.Risk != RiskDanger {
			t.Errorf("Enable exec risk = %v, want %v", meta.Risk, RiskDanger)
		}
		if !meta.RestartRequired {
			t.Error("Enable exec should require restart")
		}
	})

	t.Run("Model selection is safe", func(t *testing.T) {
		meta, ok := Get("provider", "model")
		if !ok {
			t.Error("Get(provider, model) returned false")
			return
		}
		if meta.Risk != RiskSafe {
			t.Errorf("Model risk = %v, want %v", meta.Risk, RiskSafe)
		}
	})

	t.Run("wildcard skill env metadata resolves concrete paths", func(t *testing.T) {
		meta, ok := Get("skills_entry", "env.API_TOKEN")
		if !ok {
			t.Fatal("expected wildcard env metadata")
		}
		if meta.Path != "skills.entries.*.env.*" {
			t.Fatalf("unexpected wildcard path: %#v", meta)
		}
		meta, ok = GetByPath("skills.entries.demo.config.credential_path")
		if !ok {
			t.Fatal("expected credential path metadata")
		}
		if meta.Key != "config.credential_path" {
			t.Fatalf("unexpected metadata: %#v", meta)
		}
	})
}
