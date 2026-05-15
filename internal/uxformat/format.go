package uxformat

import (
	"fmt"
	"io"
	"strings"
)

type ColorPolicy string

const (
	ColorAuto   ColorPolicy = "auto"
	ColorAlways ColorPolicy = "always"
	ColorNever  ColorPolicy = "never"
)

type ErrorBlock struct {
	Title   string
	Body    string
	Details []string
}

type LoadingState struct {
	Label  string
	Active bool
}

func RenderError(block ErrorBlock, policy ColorPolicy) string {
	title := strings.TrimSpace(block.Title)
	body := strings.TrimSpace(block.Body)
	lines := make([]string, 0, 2+len(block.Details))
	if title != "" {
		lines = append(lines, maybeColor(title, policy, "\033[31m"))
	}
	if body != "" {
		lines = append(lines, body)
	}
	for _, detail := range block.Details {
		if trimmed := strings.TrimSpace(detail); trimmed != "" {
			lines = append(lines, "- "+trimmed)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func WriteError(w io.Writer, block ErrorBlock, policy ColorPolicy) {
	_, _ = io.WriteString(w, RenderError(block, policy))
}

func RenderLoading(state LoadingState, policy ColorPolicy) string {
	label := strings.TrimSpace(state.Label)
	if label == "" {
		label = "Working"
	}
	if !state.Active {
		return label + "\n"
	}
	return maybeColor("… "+label, policy, "\033[36m") + "\n"
}

func WriteEmptyState(w io.Writer, title, body string, hints []string) {
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		fmt.Fprintln(w, trimmed)
	}
	if trimmed := strings.TrimSpace(body); trimmed != "" {
		fmt.Fprintln(w, trimmed)
	}
	for _, hint := range hints {
		if trimmed := strings.TrimSpace(hint); trimmed != "" {
			fmt.Fprintf(w, "  %s\n", trimmed)
		}
	}
}

func maybeColor(text string, policy ColorPolicy, code string) string {
	if policy != ColorAlways {
		return text
	}
	return code + text + "\033[0m"
}
