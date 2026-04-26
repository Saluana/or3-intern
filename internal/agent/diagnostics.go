package agent

import (
	"fmt"
	"sort"
	"strings"
)

func FormatBudgetDiagnostics(report BudgetReport, maxRejected int) string {
	var out strings.Builder
	fmt.Fprintf(&out, "context pressure=%s used=%d max=%d reserve=%d pct=%.1f%%\n",
		redactDiagnostic(report.Pressure), report.EstimatedInputTokens, report.MaxInputTokens, report.OutputReserveTokens, report.BudgetUsedPercent)
	sections := append([]SectionUsage{}, report.Sections...)
	sort.SliceStable(sections, func(i, j int) bool { return sections[i].EstimatedTokens > sections[j].EstimatedTokens })
	limit := min(5, len(sections))
	for i := 0; i < limit; i++ {
		section := sections[i]
		fmt.Fprintf(&out, "section %s tokens=%d limit=%d protected=%v truncated=%v\n",
			redactDiagnostic(section.Name), section.EstimatedTokens, section.LimitTokens, section.Protected, section.Truncated)
	}
	for _, pruned := range report.Pruned {
		fmt.Fprintf(&out, "pruned section=%s reason=%s saved=%d\n", redactDiagnostic(pruned.Section), redactDiagnostic(pruned.Reason), pruned.TokensSaved)
	}
	if maxRejected <= 0 {
		maxRejected = 5
	}
	for i, rejected := range report.Rejected {
		if i >= maxRejected {
			fmt.Fprintf(&out, "rejected ... %d more\n", len(report.Rejected)-i)
			break
		}
		fmt.Fprintf(&out, "rejected %s\n", redactDiagnostic(rejected))
	}
	return strings.TrimSpace(out.String())
}

func redactDiagnostic(text string) string {
	words := strings.Fields(text)
	redactNext := false
	for i, word := range words {
		lower := strings.ToLower(word)
		if redactNext {
			words[i] = "[redacted]"
			redactNext = false
			continue
		}
		if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "password") || strings.Contains(lower, "bearer") {
			words[i] = "[redacted]"
			redactNext = true
		}
		if len(word) > 80 {
			words[i] = word[:80] + "..."
		}
	}
	return strings.Join(words, " ")
}
