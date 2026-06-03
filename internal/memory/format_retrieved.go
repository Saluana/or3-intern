package memory

import (
	"fmt"
	"strings"
)

const defaultRetrievedOneLineMax = 240

// FormatRetrievedBrief renders retrieved memory notes for runner prompt context.
func FormatRetrievedBrief(ms []Retrieved, maxChars int) string {
	if len(ms) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for i, m := range ms {
		kind := strings.TrimSpace(m.Kind)
		if kind == "" {
			kind = "note"
		}
		ref := strings.TrimSpace(m.Ref)
		if ref == "" && m.ID > 0 {
			ref = fmt.Sprintf("memory:%d", m.ID)
		}
		if ref == "" {
			ref = m.Source
		}
		line := fmt.Sprintf("%d) [%s score=%.3f ref=%s] %s\n", i+1, kind, m.Score, ref, oneLineBrief(m.Text, defaultRetrievedOneLineMax))
		if maxChars > 0 && b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "(none)"
	}
	return out
}

func oneLineBrief(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if max > 0 && len(s) > max {
		return s[:max] + "..."
	}
	return s
}
