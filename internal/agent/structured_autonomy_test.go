package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"or3-intern/internal/bus"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

type staticStructuredTool struct {
	tools.Base
	name    string
	result  string
	err     error
	session string
}

func (t *staticStructuredTool) Name() string        { return t.name }
func (t *staticStructuredTool) Description() string { return t.name }
func (t *staticStructuredTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *staticStructuredTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	t.session = tools.SessionFromContext(ctx)
	return t.result, t.err
}
func (t *staticStructuredTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func structuredAutonomyEvent(eventType bus.EventType, sessionKey string, tasks []triggers.StructuredToolCall) bus.Event {
	return bus.Event{
		Type:       eventType,
		SessionKey: sessionKey,
		Channel:    "slack",
		From:       "tester",
		Meta: map[string]any{
			"channel_id":                    "channel-1",
			triggers.MetaKeyStructuredTasks: triggers.StructuredTasksMap(triggers.StructuredTaskEnvelope{Tasks: tasks}),
		},
	}
}

func rawSessionMessages(t *testing.T, d *db.DB, sessionKey string) []db.Message {
	t.Helper()

	rows, err := d.SQL.QueryContext(context.Background(),
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? ORDER BY id ASC`, sessionKey)
	if err != nil {
		t.Fatalf("query messages: %v", err)
	}
	defer rows.Close()

	var msgs []db.Message
	for rows.Next() {
		var msg db.Message
		if err := rows.Scan(&msg.ID, &msg.SessionKey, &msg.Role, &msg.Content, &msg.PayloadJSON, &msg.CreatedAt); err != nil {
			t.Fatalf("scan message: %v", err)
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate messages: %v", err)
	}
	return msgs
}

func TestHandleStructuredAutonomy_NonAutonomousEventNoOp(t *testing.T) {
	d := openRuntimeTestDB(t)
	reg := tools.NewRegistry()
	reg.Register(&staticStructuredTool{name: "noop", result: "ok"})
	rt := &Runtime{DB: d, Tools: reg}

	handled, err := rt.handleStructuredAutonomy(context.Background(), structuredAutonomyEvent(bus.EventUserMessage, "sess-user", []triggers.StructuredToolCall{{Tool: "noop"}}), 1)
	if err != nil {
		t.Fatalf("handleStructuredAutonomy: %v", err)
	}
	if handled {
		t.Fatal("expected non-autonomous event to be ignored")
	}

	msgs := rawSessionMessages(t, d, "sess-user")
	if len(msgs) != 0 {
		t.Fatalf("expected no persisted messages, got %#v", msgs)
	}
}

func TestHandleStructuredAutonomy_ReturnsFalseWhenNoToolsOrTasks(t *testing.T) {
	tests := []struct {
		name string
		rt   *Runtime
		ev   bus.Event
	}{
		{
			name: "nil tools",
			rt:   &Runtime{DB: openRuntimeTestDB(t)},
			ev:   structuredAutonomyEvent(bus.EventWebhook, "sess-nil-tools", []triggers.StructuredToolCall{{Tool: "noop"}}),
		},
		{
			name: "empty tasks",
			rt:   &Runtime{DB: openRuntimeTestDB(t), Tools: tools.NewRegistry()},
			ev:   structuredAutonomyEvent(bus.EventHeartbeat, "sess-empty-tasks", nil),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := tc.rt.handleStructuredAutonomy(context.Background(), tc.ev, 1)
			if err != nil {
				t.Fatalf("handleStructuredAutonomy: %v", err)
			}
			if handled {
				t.Fatal("expected structured autonomy to be skipped")
			}

			msgs := rawSessionMessages(t, tc.rt.DB, tc.ev.SessionKey)
			if len(msgs) != 0 {
				t.Fatalf("expected no persisted messages, got %#v", msgs)
			}
		})
	}
}

func TestHandleStructuredAutonomy_MixedTaskResults(t *testing.T) {
	d := openRuntimeTestDB(t)
	reg := tools.NewRegistry()
	reg.Register(&staticStructuredTool{name: "task_one", result: "first ok"})
	reg.Register(&staticStructuredTool{name: "task_two", result: "partial output", err: errors.New("boom")})
	reg.Register(&staticStructuredTool{name: "task_three", result: "third ok"})
	rt := &Runtime{DB: d, Tools: reg}

	handled, err := rt.handleStructuredAutonomy(context.Background(), structuredAutonomyEvent(bus.EventWebhook, "sess-mixed", []triggers.StructuredToolCall{
		{Tool: "task_one"},
		{Tool: "task_two"},
		{Tool: "task_three"},
	}), 7)
	if err != nil {
		t.Fatalf("handleStructuredAutonomy: %v", err)
	}
	if !handled {
		t.Fatal("expected structured autonomy tasks to execute")
	}

	msgs := rawSessionMessages(t, d, "sess-mixed")
	if len(msgs) != 4 {
		t.Fatalf("expected 3 tool messages and 1 assistant summary, got %#v", msgs)
	}
	if msgs[0].Role != "tool" || msgs[1].Role != "tool" || msgs[2].Role != "tool" || msgs[3].Role != "assistant" {
		t.Fatalf("unexpected message roles: %#v", msgs)
	}
	if got := msgs[3].Content; !strings.Contains(got, "2/3 succeeded") || !strings.Contains(got, "#2 task_two: boom") {
		t.Fatalf("expected mixed-result summary, got %q", got)
	}
}

func TestValidateStructuredValueDepth_TypeCoverage(t *testing.T) {
	tests := []struct {
		name           string
		schema         map[string]any
		value          any
		wantErrContain string
	}{
		{
			name: "object valid",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"enabled": map[string]any{"type": "boolean"},
				},
			},
			value: map[string]any{"enabled": true},
		},
		{
			name:           "object invalid",
			schema:         map[string]any{"type": "object"},
			value:          "bad",
			wantErrContain: "params must be an object",
		},
		{
			name: "array valid",
			schema: map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			value: []any{"a", "b"},
		},
		{
			name:           "array invalid",
			schema:         map[string]any{"type": "array"},
			value:          "bad",
			wantErrContain: "params must be an array",
		},
		{
			name:           "string invalid",
			schema:         map[string]any{"type": "string"},
			value:          1,
			wantErrContain: "params must be a string",
		},
		{
			name:           "boolean invalid",
			schema:         map[string]any{"type": "boolean"},
			value:          "true",
			wantErrContain: "params must be a boolean",
		},
		{
			name:   "integer valid",
			schema: map[string]any{"type": "integer"},
			value:  float64(2),
		},
		{
			name:           "integer invalid",
			schema:         map[string]any{"type": "integer"},
			value:          1.5,
			wantErrContain: "params must be an integer",
		},
		{
			name:   "number valid",
			schema: map[string]any{"type": "number"},
			value:  2.5,
		},
		{
			name:           "number invalid",
			schema:         map[string]any{"type": "number"},
			value:          "2.5",
			wantErrContain: "params must be a number",
		},
		{
			name:   "default type ignored",
			schema: map[string]any{"type": "mystery"},
			value:  map[string]any{"anything": true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStructuredValueDepth(tc.schema, tc.value, "params", 0, 32)
			if tc.wantErrContain == "" {
				if err != nil {
					t.Fatalf("validateStructuredValueDepth: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErrContain) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErrContain, err)
			}
		})
	}
}

func TestValidateStructuredValueDepth_MaxDepthExceeded(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	value := map[string]any{}
	currentSchema := schema
	currentValue := value
	for i := 0; i <= 32; i++ {
		nextSchema := map[string]any{"type": "object", "properties": map[string]any{}}
		currentSchema["properties"].(map[string]any)["next"] = nextSchema
		nextValue := map[string]any{}
		currentValue["next"] = nextValue
		currentSchema = nextSchema
		currentValue = nextValue
	}

	err := validateStructuredValueDepth(schema, value, "params", 0, 32)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum nesting depth (32)") {
		t.Fatalf("expected max-depth error, got %v", err)
	}
}

func TestValidateStructuredValueDepth_RejectsUnknownFieldsWhenAdditionalPropertiesFalse(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
	}

	err := validateStructuredValueDepth(schema, map[string]any{"text": "ok", "extra": true}, "params", 0, 32)
	if err == nil || !strings.Contains(err.Error(), "params.extra is not allowed") {
		t.Fatalf("expected additionalProperties error, got %v", err)
	}
}

func TestSliceItems_ReflectSlices(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  []any
	}{
		{name: "strings", value: []string{"a", "b"}, want: []any{"a", "b"}},
		{name: "ints", value: []int{1, 2}, want: []any{1, 2}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sliceItems(tc.value)
			if !ok {
				t.Fatal("expected reflected slice to be accepted")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d items, got %d (%#v)", len(tc.want), len(got), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("expected %#v, got %#v", tc.want, got)
				}
			}
		})
	}
}

func TestHandleStructuredAutonomy_FallsBackToSessionKeyWhenResolvedScopeEmpty(t *testing.T) {
	d := openRuntimeTestDB(t)
	if _, err := d.SQL.ExecContext(context.Background(),
		`INSERT INTO session_links(session_key, scope_key, linked_at, metadata_json) VALUES(?,?,?,?)`,
		"sess-fallback", "", 0, `{}`); err != nil {
		t.Fatalf("insert session link: %v", err)
	}

	tool := &staticStructuredTool{name: "capture_scope", result: "ok"}
	reg := tools.NewRegistry()
	reg.Register(tool)
	rt := &Runtime{DB: d, Tools: reg}

	handled, err := rt.handleStructuredAutonomy(context.Background(), structuredAutonomyEvent(bus.EventWebhook, "sess-fallback", []triggers.StructuredToolCall{{Tool: tool.Name()}}), 3)
	if err != nil {
		t.Fatalf("handleStructuredAutonomy: %v", err)
	}
	if !handled {
		t.Fatal("expected structured autonomy tasks to execute")
	}
	if tool.session != "sess-fallback" {
		t.Fatalf("expected fallback session scope, got %q", tool.session)
	}
}
