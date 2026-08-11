package config

import "testing"

func TestCloneDoesNotShareNestedConfigState(t *testing.T) {
	original := Default()
	original.Runners.RuntimeMode["codex"] = "native"
	original.Providers["openai"] = ProviderProfileConfig{APIKey: "stored"}
	original.FavoriteModels["openai"] = []FavoriteModelConfig{{Model: "model-a"}}
	original.Skills.Entries["skill-a"] = SkillEntryConfig{Env: map[string]string{"TOKEN": "one"}, Config: map[string]any{"nested": "one"}}

	clone := Clone(original)
	clone.Runners.RuntimeMode["codex"] = "cli"
	profile := clone.Providers["openai"]
	profile.APIKey = "changed"
	clone.Providers["openai"] = profile
	clone.FavoriteModels["openai"][0].Model = "model-b"
	entry := clone.Skills.Entries["skill-a"]
	entry.Env["TOKEN"] = "two"
	entry.Config["nested"] = "two"
	clone.Skills.Entries["skill-a"] = entry

	if original.Runners.RuntimeMode["codex"] != "native" || original.Providers["openai"].APIKey != "stored" || original.FavoriteModels["openai"][0].Model != "model-a" {
		t.Fatalf("clone mutated original config: %#v", original)
	}
	originalEntry := original.Skills.Entries["skill-a"]
	if originalEntry.Env["TOKEN"] != "one" || originalEntry.Config["nested"] != "one" {
		t.Fatalf("clone mutated nested skill config: %#v", originalEntry)
	}
}
