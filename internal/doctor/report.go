package doctor

import (
	"encoding/json"
	"sort"
	"strings"
)

type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
	SeverityBlock Severity = "block"
)

type FixMode string

const (
	FixModeNone        FixMode = "none"
	FixModeAutomatic   FixMode = "automatic"
	FixModeInteractive FixMode = "interactive"
	FixModeManual      FixMode = "manual"
)

type Mode string

const (
	ModeAdvisory          Mode = "advisory"
	ModeStrict            Mode = "strict"
	ModeStartupChat       Mode = "startup-chat"
	ModeStartupServe      Mode = "startup-serve"
	ModeStartupService    Mode = "startup-service"
	ModeConfigurePostSave Mode = "configure-post-save"
)

type Finding struct {
	ID       string            `json:"id"`
	Area     string            `json:"area"`
	Severity Severity          `json:"severity"`
	Summary  string            `json:"summary"`
	Detail   string            `json:"detail,omitempty"`
	Evidence []string          `json:"evidence,omitempty"`
	FixMode  FixMode           `json:"fixMode,omitempty"`
	FixHint  string            `json:"fixHint,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Summary struct {
	Status       string `json:"status"`
	InfoCount    int    `json:"infoCount"`
	WarnCount    int    `json:"warnCount"`
	ErrorCount   int    `json:"errorCount"`
	BlockCount   int    `json:"blockCount"`
	FixableCount int    `json:"fixableCount"`
}

type AppliedFix struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type Report struct {
	Mode         Mode         `json:"mode"`
	Summary      Summary      `json:"summary"`
	Findings     []Finding    `json:"findings"`
	FixesApplied []AppliedFix `json:"fixesApplied,omitempty"`
}

type FilterOptions struct {
	Areas       []string
	MinSeverity Severity
	FixableOnly bool
}

func NormalizeSeverity(raw string) Severity {
	switch Severity(strings.ToLower(strings.TrimSpace(raw))) {
	case SeverityInfo, SeverityWarn, SeverityError, SeverityBlock:
		return Severity(strings.ToLower(strings.TrimSpace(raw)))
	default:
		return ""
	}
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityBlock:
		return 4
	case SeverityError:
		return 3
	case SeverityWarn:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

func HasFix(mode FixMode) bool {
	return mode == FixModeAutomatic || mode == FixModeInteractive
}

func CountFixMode(findings []Finding, mode FixMode) int {
	count := 0
	for _, finding := range findings {
		if finding.FixMode == mode {
			count++
		}
	}
	return count
}

func NewReport(mode Mode, findings []Finding) Report {
	items := append([]Finding{}, findings...)
	sort.SliceStable(items, func(i, j int) bool {
		li := severityRank(items[i].Severity)
		lj := severityRank(items[j].Severity)
		if li != lj {
			return li > lj
		}
		if items[i].Area != items[j].Area {
			return items[i].Area < items[j].Area
		}
		if items[i].ID != items[j].ID {
			return items[i].ID < items[j].ID
		}
		return items[i].Summary < items[j].Summary
	})
	report := Report{Mode: mode, Findings: items}
	report.recomputeSummary()
	return report
}

func (r Report) Filter(opts FilterOptions) Report {
	if len(opts.Areas) == 0 && opts.MinSeverity == "" && !opts.FixableOnly {
		return r
	}
	areaSet := map[string]struct{}{}
	for _, area := range opts.Areas {
		area = strings.ToLower(strings.TrimSpace(area))
		if area != "" {
			areaSet[area] = struct{}{}
		}
	}
	filtered := make([]Finding, 0, len(r.Findings))
	for _, finding := range r.Findings {
		if len(areaSet) > 0 {
			if _, ok := areaSet[strings.ToLower(strings.TrimSpace(finding.Area))]; !ok {
				continue
			}
		}
		if opts.MinSeverity != "" && severityRank(finding.Severity) < severityRank(opts.MinSeverity) {
			continue
		}
		if opts.FixableOnly && !HasFix(finding.FixMode) {
			continue
		}
		filtered = append(filtered, finding)
	}
	out := Report{
		Mode:         r.Mode,
		Findings:     filtered,
		FixesApplied: append([]AppliedFix{}, r.FixesApplied...),
	}
	out.recomputeSummary()
	return out
}

func (r Report) HasBlockingFindings() bool {
	for _, finding := range r.Findings {
		if finding.Severity == SeverityBlock {
			return true
		}
	}
	return false
}

func (r Report) HasStrictFailures() bool {
	for _, finding := range r.Findings {
		if severityRank(finding.Severity) >= severityRank(SeverityWarn) {
			return true
		}
	}
	return false
}

func (r Report) BlockingFindings() []Finding {
	items := make([]Finding, 0, len(r.Findings))
	for _, finding := range r.Findings {
		if finding.Severity == SeverityBlock {
			items = append(items, finding)
		}
	}
	return items
}

func (r Report) WarningsAndErrors() []Finding {
	items := make([]Finding, 0, len(r.Findings))
	for _, finding := range r.Findings {
		if severityRank(finding.Severity) >= severityRank(SeverityWarn) {
			items = append(items, finding)
		}
	}
	return items
}

func (r *Report) recomputeSummary() {
	summary := Summary{}
	for _, finding := range r.Findings {
		switch finding.Severity {
		case SeverityInfo:
			summary.InfoCount++
		case SeverityWarn:
			summary.WarnCount++
		case SeverityError:
			summary.ErrorCount++
		case SeverityBlock:
			summary.BlockCount++
		}
		if HasFix(finding.FixMode) {
			summary.FixableCount++
		}
	}
	switch {
	case summary.BlockCount > 0:
		summary.Status = "not ready"
	case summary.ErrorCount > 0:
		summary.Status = "needs attention"
	case summary.WarnCount > 0:
		summary.Status = "ready with warnings"
	default:
		summary.Status = "ok"
	}
	r.Summary = summary
}

func (r Report) MarshalJSON() ([]byte, error) {
	type alias Report
	return json.Marshal(alias(r))
}
