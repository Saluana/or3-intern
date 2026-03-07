package scope

import "testing"

func TestIsGlobalScopeRequest(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "alias exact", input: "global", want: true},
		{name: "alias case insensitive", input: "Global", want: true},
		{name: "alias trimmed", input: "  global  ", want: true},
		{name: "memory scope constant", input: GlobalMemoryScope, want: true},
		{name: "unknown scope", input: "project-123", want: false},
		{name: "empty", input: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGlobalScopeRequest(tt.input); got != tt.want {
				t.Fatalf("IsGlobalScopeRequest(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}