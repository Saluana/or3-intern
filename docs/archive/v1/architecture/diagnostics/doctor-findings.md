# Doctor Findings Format

Each finding produced by the doctor engine has a standard format.

## Finding struct

```go
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
```

Source: `internal/doctor/report.go:38-48`

## Normalization

Findings at severity warn or higher get normalized:
- If `Detail` is empty, it is set to match `Summary`
- If `FixHint` is empty, a default hint is provided
- If `FixMode` is empty, it is set to `manual`

Source: `internal/doctor/report.go:129-143` (normalizeFindingRepairFields)

## Fix modes

| Mode | Meaning |
|------|---------|
| `none` | Cannot be fixed |
| `automatic` | Can be fixed without user input (`or3-intern doctor --fix`) |
| `interactive` | Requires user choices (`or3-intern doctor --fix --interactive`) |
| `manual` | User must edit config manually |

Source: `internal/doctor/report.go:18-25`

## Report struct

```go
type Report struct {
    Mode         Mode         `json:"mode"`
    Summary      Summary      `json:"summary"`
    Findings     []Finding    `json:"findings"`
    FixesApplied []AppliedFix `json:"fixesApplied,omitempty"`
}
```

Source: `internal/doctor/report.go:64-69`

## Summary struct

```go
type Summary struct {
    Status       string `json:"status"`
    InfoCount    int    `json:"infoCount"`
    WarnCount    int    `json:"warnCount"`
    ErrorCount   int    `json:"errorCount"`
    BlockCount   int    `json:"blockCount"`
    FixableCount int    `json:"fixableCount"`
}
```

The `Status` field is derived from counts:
- Blockers → "not ready"
- Errors → "needs attention"
- Warnings → "ready with warnings"
- None → "ok"

Source: `internal/doctor/report.go:50-57` (Summary), `internal/doctor/report.go:218-246` (recomputeSummary)

## Rendering

Reports can be rendered as plain text or JSON. The text renderer groups findings by severity under section headers (Blockers, Errors, Warnings, Info) and shows fix availability.

Source: `internal/doctor/render.go:9-66` (RenderText), `internal/doctor/render.go:68-70` (RenderJSON)
