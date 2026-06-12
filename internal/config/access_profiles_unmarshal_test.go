package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAccessProfileConfig_UnmarshalJSON_AcceptsDeclaredTools(t *testing.T) {
	raw := []byte(`{"maxCapability":"guarded","declaredTools":["a","b"],"allowedHosts":["h"]}`)
	var p AccessProfileConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(p.DeclaredTools, []string{"a", "b"}) {
		t.Errorf("DeclaredTools = %v, want [a b]", p.DeclaredTools)
	}
	if p.MaxCapability != "guarded" {
		t.Errorf("MaxCapability = %q, want guarded", p.MaxCapability)
	}
	if !reflect.DeepEqual(p.AllowedHosts, []string{"h"}) {
		t.Errorf("AllowedHosts = %v, want [h]", p.AllowedHosts)
	}
}

func TestAccessProfileConfig_UnmarshalJSON_AcceptsLegacyAllowedTools(t *testing.T) {
	raw := []byte(`{"maxCapability":"guarded","allowedTools":["a","b"],"allowedHosts":["h"]}`)
	var p AccessProfileConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(p.DeclaredTools, []string{"a", "b"}) {
		t.Errorf("DeclaredTools after legacy unmarshal = %v, want [a b]", p.DeclaredTools)
	}
	if p.MaxCapability != "guarded" {
		t.Errorf("MaxCapability = %q, want guarded", p.MaxCapability)
	}
}

func TestAccessProfileConfig_UnmarshalJSON_DeclaredToolsWinsOverLegacy(t *testing.T) {
	raw := []byte(`{"declaredTools":["new"],"allowedTools":["old"]}`)
	var p AccessProfileConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(p.DeclaredTools, []string{"new"}) {
		t.Errorf("DeclaredTools = %v, want [new] (declaredTools should win)", p.DeclaredTools)
	}
}

func TestAccessProfileConfig_UnmarshalJSON_EmptyProfile(t *testing.T) {
	raw := []byte(`{}`)
	var p AccessProfileConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(p.DeclaredTools) != 0 {
		t.Errorf("DeclaredTools should be empty, got %v", p.DeclaredTools)
	}
	if p.MaxCapability != "" {
		t.Errorf("MaxCapability should be empty, got %q", p.MaxCapability)
	}
}
