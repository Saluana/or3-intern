package agent

import "strings"

func renderActivePlanEnvelope(card TaskCard, meta ActivePlanMetadata, maxChars int) string {
	body := renderActivePlanCompact(card, meta, maxChars)
	if body == "" {
		return ""
	}
	return renderXMLEnvelope(xmlTagActiveTurnPlan, body, envelopeAttrs{
		"protected": "true",
		"volatile":  "true",
	})
}

func renderContextCompactionEnvelope(summary string, maxChars int) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	if maxChars > 0 && len(summary) > maxChars {
		summary = strings.TrimSpace(summary[:maxChars]) + "\n…[truncated]"
	}
	return renderXMLEnvelope(xmlTagContextCompaction, summary, envelopeAttrs{"volatile": "true"})
}

func renderEventContextEnvelope(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return renderXMLEnvelope(xmlTagEventContext, body, envelopeAttrs{"volatile": "true"})
}

func renderRuntimeContextEnvelope(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return renderXMLEnvelope(xmlTagRuntimeContext, body, envelopeAttrs{"volatile": "true"})
}
