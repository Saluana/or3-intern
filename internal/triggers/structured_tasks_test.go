package triggers

import "testing"

func TestParseStructuredTasksText_AcceptsDocumentedShapes(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "tasks envelope",
			text: `{"tasks":[{"tool":"echo_tool","params":{"text":"hi"}}]}`,
		},
		{
			name: "structured_tasks wrapper",
			text: `{"version":1,"structured_tasks":[{"tool":"echo_tool","params":{"text":"hi"}}]}`,
		},
		{
			name: "fenced tasks",
			text: "```structured-tasks\n{\"tasks\":[{\"tool\":\"echo_tool\"}]}\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, ok := ParseStructuredTasksText(tt.text)
			if !ok {
				t.Fatalf("expected structured tasks to parse")
			}
			if len(env.Tasks) != 1 || env.Tasks[0].Tool != "echo_tool" {
				t.Fatalf("unexpected tasks: %#v", env.Tasks)
			}
		})
	}
}

func TestParseStructuredTasksText_RejectsTopLevelToolShortcut(t *testing.T) {
	env, ok := ParseStructuredTasksText(`{"tool":"echo_tool","params":{"text":"hi"}}`)
	if ok {
		t.Fatalf("expected top-level tool shortcut to be rejected, got %#v", env)
	}
}
