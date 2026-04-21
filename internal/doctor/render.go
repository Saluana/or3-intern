package doctor

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RenderText(report Report) string {
	if len(report.Findings) == 0 {
		return "Status: ok\n\n[ok] configuration looks safe\n"
	}
	lines := []string{
		fmt.Sprintf("Status: %s", report.Summary.Status),
	}
	sections := []struct {
		title    string
		severity Severity
	}{
		{title: "Blockers:", severity: SeverityBlock},
		{title: "Errors:", severity: SeverityError},
		{title: "Warnings:", severity: SeverityWarn},
		{title: "Info:", severity: SeverityInfo},
	}
	for _, section := range sections {
		items := findingsWithSeverity(report.Findings, section.severity)
		if len(items) == 0 {
			continue
		}
		lines = append(lines, "", section.title)
		for _, finding := range items {
			lines = append(lines, renderFindingLine(finding))
		}
	}
	if len(report.FixesApplied) > 0 {
		lines = append(lines, "", "Fixes Applied:")
		for _, fix := range report.FixesApplied {
			lines = append(lines, fmt.Sprintf("- %s", fix.Summary))
		}
	}
	fixable := 0
	automatic := 0
	interactive := 0
	for _, finding := range report.Findings {
		if HasFix(finding.FixMode) {
			fixable++
		}
		if finding.FixMode == FixModeAutomatic {
			automatic++
		}
		if finding.FixMode == FixModeInteractive {
			interactive++
		}
	}
	if fixable > 0 {
		lines = append(lines, "", "Next Steps:")
		lines = append(lines, fmt.Sprintf("- %d finding(s) have available fixes.", fixable))
		if automatic > 0 {
			lines = append(lines, fmt.Sprintf("- %d finding(s) support safe automatic repair via `or3-intern doctor --fix`.", automatic))
		}
		if interactive > 0 {
			lines = append(lines, fmt.Sprintf("- %d finding(s) require guided repair via `or3-intern doctor --fix --interactive`.", interactive))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func RenderJSON(report Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func renderFindingLine(finding Finding) string {
	line := fmt.Sprintf("- %s: %s", finding.ID, finding.Summary)
	if finding.FixMode != "" && finding.FixMode != FixModeNone {
		line += fmt.Sprintf(" [fix=%s]", finding.FixMode)
	}
	return line
}

func findingsWithSeverity(findings []Finding, severity Severity) []Finding {
	items := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.Severity == severity {
			items = append(items, finding)
		}
	}
	return items
}
