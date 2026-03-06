package tools

import (
	"context"
	"testing"
)

func TestBase_SchemaFor(t *testing.T) {
	b := Base{}
	schema := b.SchemaFor("my_tool", "does stuff", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"arg": map[string]any{"type": "string"},
		},
	})
	if schema["type"] != "function" {
		t.Errorf("expected type 'function', got %v", schema["type"])
	}
	fn, ok := schema["function"].(map[string]any)
	if !ok {
		t.Fatal("expected 'function' key to be map[string]any")
	}
	if fn["name"] != "my_tool" {
		t.Errorf("expected name 'my_tool', got %v", fn["name"])
	}
	if fn["description"] != "does stuff" {
		t.Errorf("expected description 'does stuff', got %v", fn["description"])
	}
	if fn["parameters"] == nil {
		t.Error("expected parameters to be set")
	}
}

// --- Registry tests ---

type mockTool struct {
	Base
	name string
	desc string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.desc }
func (m *mockTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (m *mockTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	return "mock result", nil
}
func (m *mockTool) Schema() map[string]any {
	return m.SchemaFor(m.name, m.desc, m.Parameters())
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "test_tool", desc: "a test tool"}
	r.Register(tool)

	got := r.Get("test_tool")
	if got == nil {
		t.Fatal("expected to find registered tool")
	}
	if got.Name() != "test_tool" {
		t.Errorf("expected name 'test_tool', got %q", got.Name())
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	if r.Get("nonexistent") != nil {
		t.Error("expected nil for unregistered tool")
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "tool_a", desc: ""})
	r.Register(&mockTool{name: "tool_b", desc: ""})

	names := r.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestRegistry_Definitions(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "tool_a", desc: "desc a"})
	defs := r.Definitions()
	if len(defs) != 1 {
		t.Errorf("expected 1 definition, got %d", len(defs))
	}
}

func TestRegistry_Execute_Success(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "test_tool", desc: ""})

	out, err := r.Execute(context.Background(), "test_tool", `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "mock result" {
		t.Errorf("expected 'mock result', got %q", out)
	}
}

func TestRegistry_Execute_NotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "missing_tool", `{}`)
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestRegistry_Execute_InvalidJSON(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "test_tool", desc: ""})

	_, err := r.Execute(context.Background(), "test_tool", `{invalid`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegistry_Execute_EmptyArgs(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "test_tool", desc: ""})

	out, err := r.Execute(context.Background(), "test_tool", "")
	if err != nil {
		t.Fatalf("Execute with empty args: %v", err)
	}
	if out != "mock result" {
		t.Errorf("expected 'mock result', got %q", out)
	}
}
