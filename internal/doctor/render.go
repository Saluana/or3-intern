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
	blocks := report.BlockingFindings()
	if len(blocks) > 0 {
		lines = append(lines, "", "Blockers:")
		for _, finding := range blocks {
			lines = append(lines, renderFindingLine(finding))
		}
	}
	rest := make([]Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if finding.Severity != SeverityBlock {
			rest = append(rest, finding)
		}
	}
	if len(rest) > 0 {
		lines = append(lines, "", "Warnings:")
		for _, finding := range rest {
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
