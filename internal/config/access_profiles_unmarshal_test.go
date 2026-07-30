package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAccessProfileConfig_UnmarshalJSON_IgnoresUnknownToolMetadata(t *testing.T) {
	raw := []byte(`{"maxCapability":"guarded","declaredTools":["a","b"],"allowedHosts":["h"]}`)
	var p AccessProfileConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.MaxCapability != "guarded" {
		t.Errorf("MaxCapability = %q, want guarded", p.MaxCapability)
	}
	if !reflect.DeepEqual(p.AllowedHosts, []string{"h"}) {
		t.Errorf("AllowedHosts = %v, want [h]", p.AllowedHosts)
	}
}

func TestAccessProfileConfig_UnmarshalJSON_EmptyProfile(t *testing.T) {
	raw := []byte(`{}`)
	var p AccessProfileConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.MaxCapability != "" {
		t.Errorf("MaxCapability should be empty, got %q", p.MaxCapability)
	}
}
