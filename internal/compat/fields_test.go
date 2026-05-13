package compat

import "testing"

func TestFirstStringCanonicalWins(t *testing.T) {
	if got := FirstString(" snake ", "camel"); got != "snake" {
		t.Fatalf("expected canonical value, got %q", got)
	}
}

func TestFirstStringFallsBackToAliases(t *testing.T) {
	if got := FirstString("", " camel "); got != "camel" {
		t.Fatalf("expected alias value, got %q", got)
	}
}

func TestFirstStringSliceCopiesCanonical(t *testing.T) {
	source := []string{"one"}
	got := FirstStringSlice(source, []string{"two"})
	got[0] = "changed"
	if source[0] != "one" {
		t.Fatal("expected FirstStringSlice to copy selected slice")
	}
}
