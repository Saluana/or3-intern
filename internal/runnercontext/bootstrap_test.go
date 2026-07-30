package runnercontext

import (
	"strings"
	"testing"
)

func TestDefaultRunnerNotesDescribeRunnerFirstBehavior(t *testing.T) {
	for _, want := range []string{
		"external runner",
		"runner's native",
		"or3-intern memory",
	} {
		if !strings.Contains(DefaultRunnerNotes, want) {
			t.Fatalf("expected default runner notes to include %q", want)
		}
	}
	if strings.Contains(DefaultRunnerNotes, "spawn_subagent") {
		t.Fatal("runner notes must not document removed spawn_subagent tool")
	}
}
