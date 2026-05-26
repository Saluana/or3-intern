package agent

import (
	"html"
	"strings"
)

const (
	CacheClassStatic  = "static"
	CacheClassSession = "session"
	CacheClassTurn    = "turn"
)

const (
	xmlTagAssistantIdentity       = "assistant_identity"
	xmlTagCodingAgentRules        = "coding_agent_rules"
	xmlTagToolPolicy              = "tool_policy"
	xmlTagPinnedMemory            = "pinned_memory"
	xmlTagRetrievedMemory         = "retrieved_memory"
	xmlTagWorkspaceContext        = "workspace_context"
	xmlTagContextCompaction       = "context_compaction_summary"
	xmlTagRuntimeContext          = "runtime_context"
	xmlTagRecentExecution         = "recent_execution_state"
	xmlTagCurrentUserRequest      = "current_user_request"
	xmlTagActiveTurnPlan          = "active_turn_plan"
	xmlTagUserAttachments         = "user_attachments"
	xmlTagEventContext            = "event_context"
	xmlTagSkillsInventory         = "skills_inventory"
)

type envelopeAttrs map[string]string

func renderXMLEnvelope(tag, body string, attrs envelopeAttrs) string {
	tag = strings.TrimSpace(tag)
	body = strings.TrimSpace(body)
	if tag == "" || body == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<")
	b.WriteString(tag)
	for _, key := range sortedAttrKeys(attrs) {
		val := strings.TrimSpace(attrs[key])
		if val == "" {
			continue
		}
		b.WriteString(" ")
		b.WriteString(key)
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(val))
		b.WriteString(`"`)
	}
	b.WriteString(">\n")
	b.WriteString(body)
	b.WriteString("\n</")
	b.WriteString(tag)
	b.WriteString(">")
	return b.String()
}

func sortedAttrKeys(attrs envelopeAttrs) []string {
	if len(attrs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func envelopeAttrsForSection(protected bool, cacheClass string, extra envelopeAttrs) envelopeAttrs {
	attrs := envelopeAttrs{}
	if protected {
		attrs["protected"] = "true"
	}
	switch cacheClass {
	case CacheClassStatic, CacheClassSession:
		attrs["cacheable"] = "true"
	case CacheClassTurn:
		attrs["volatile"] = "true"
	}
	for key, val := range extra {
		attrs[key] = val
	}
	return attrs
}

func renderTierSections(sections []systemPromptSection, maxEach int) string {
	var out strings.Builder
	for _, section := range sections {
		text := strings.TrimSpace(section.Text)
		if text == "" {
			continue
		}
		if maxEach > 0 {
			text = strings.TrimSpace(truncateText(text, maxEach))
		}
		tag := strings.TrimSpace(section.XMLTag)
		if tag == "" {
			tag = xmlTagFromLegacyTitle(section.Title)
		}
		body := renderXMLEnvelope(tag, text, envelopeAttrsForSection(section.Protected, section.CacheClass, section.Attrs))
		if body == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(body)
	}
	return strings.TrimSpace(out.String())
}

func xmlTagFromLegacyTitle(title string) string {
	switch strings.TrimSpace(title) {
	case "SOUL.md", "Identity":
		return xmlTagAssistantIdentity
	case "AGENTS.md":
		return xmlTagCodingAgentRules
	case "TOOLS.md":
		return xmlTagToolPolicy
	case "Pinned Memory":
		return xmlTagPinnedMemory
	case "Memory Digest":
		return xmlTagRetrievedMemory
	case "Retrieved Memory":
		return xmlTagRetrievedMemory
	case "Workspace Context", "Indexed File Context":
		return xmlTagWorkspaceContext
	case "Skills Inventory":
		return xmlTagSkillsInventory
	case "Runtime Context":
		return xmlTagRuntimeContext
	case "Current Turn":
		return xmlTagCurrentUserRequest
	case "Heartbeat":
		return xmlTagEventContext
	case "Structured Trigger Context":
		return xmlTagActiveTurnPlan
	default:
		slug := strings.ToLower(strings.TrimSpace(title))
		slug = strings.ReplaceAll(slug, " ", "_")
		return slug
	}
}

func renderCurrentUserRequestBody(userMessage string, messageID int64) string {
	return renderCurrentTurn(userMessage, messageID)
}

func renderCurrentUserRequestEnvelope(userMessage string, messageID int64) string {
	body := renderCurrentUserRequestBody(userMessage, messageID)
	if body == "" {
		return ""
	}
	return renderXMLEnvelope(xmlTagCurrentUserRequest, body, envelopeAttrs{
		"protected": "true",
		"volatile":  "true",
	})
}
