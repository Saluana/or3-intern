package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"or3-intern/internal/db"
)

type semanticSummary struct {
	Kind       string   `json:"kind"`
	Summary    string   `json:"summary"`
	Refs       []string `json:"refs,omitempty"`
	Constraint string   `json:"constraint,omitempty"`
}

func compactSemanticJSON(kind, summary string, refs []string) string {
	item := semanticSummary{Kind: strings.TrimSpace(kind), Summary: oneLine(summary, 320), Refs: cleanRefs(refs)}
	if item.Kind == "" {
		item.Kind = "summary"
	}
	b, _ := json.Marshal(item)
	return string(b)
}

func renderSemanticMemoryDigestLine(m memoryLike) string {
	label := semanticLabel(m.memoryKind())
	ref := strings.TrimSpace(m.memoryRef())
	if ref == "" && m.memoryID() > 0 {
		ref = fmt.Sprintf("memory:%d", m.memoryID())
	}
	text := oneLine(m.memoryText(), defaultDigestOneLineMax)
	if ref != "" {
		return fmt.Sprintf("- %s: %s (Ref: %s)", label, text, ref)
	}
	return fmt.Sprintf("- %s: %s", label, text)
}

type memoryLike interface {
	memoryKind() string
	memoryID() int64
	memoryText() string
	memoryRef() string
}

func semanticLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case db.MemoryKindGoal:
		return "Goal"
	case db.MemoryKindProcedure:
		return "Procedure"
	case db.MemoryKindDecision:
		return "Decision"
	case db.MemoryKindWarning:
		return "Warning"
	case db.MemoryKindPreference:
		return "Preference"
	case db.MemoryKindFile:
		return "File"
	case db.MemoryKindArtifact:
		return "Artifact"
	default:
		return "Fact"
	}
}

func buildHistorySummary(rows []db.Message, maxItems int) string {
	if maxItems <= 0 {
		maxItems = 6
	}
	if len(rows) > maxItems {
		rows = rows[len(rows)-maxItems:]
	}
	var out strings.Builder
	for _, row := range rows {
		role := strings.TrimSpace(row.Role)
		if role == "" {
			role = "message"
		}
		out.WriteString(fmt.Sprintf("- %s Msg:%d %s\n", strings.Title(role), row.ID, oneLine(row.Content, 180)))
	}
	return strings.TrimSpace(out.String())
}

func buildToolOutputSummary(toolName, output, artifactID string, maxChars int) string {
	label := "Tool"
	if strings.TrimSpace(toolName) != "" {
		label = "Tool " + strings.TrimSpace(toolName)
	}
	parts := []string{fmt.Sprintf("%s: %s", label, oneLine(output, maxChars))}
	if strings.TrimSpace(artifactID) != "" {
		parts = append(parts, "Ref: artifact:"+strings.TrimSpace(artifactID))
	}
	return strings.Join(parts, " | ")
}

func buildArtifactSummary(artifactID, mime, preview string, sizeBytes int64) string {
	parts := []string{fmt.Sprintf("Artifact: %s", oneLine(preview, 220))}
	if artifactID != "" {
		parts = append(parts, "Ref: artifact:"+artifactID)
	}
	if mime != "" {
		parts = append(parts, "MIME: "+mime)
	}
	if sizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("Size: %d", sizeBytes))
	}
	return strings.Join(parts, " | ")
}

func buildFileSummary(path, summary string, sourceMessageID int64) string {
	parts := []string{fmt.Sprintf("File: %s", strings.TrimSpace(path)), oneLine(summary, 220)}
	if sourceMessageID > 0 {
		parts = append(parts, fmt.Sprintf("Ref: message:%d", sourceMessageID))
	}
	return strings.Join(parts, " | ")
}

func cleanRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}
