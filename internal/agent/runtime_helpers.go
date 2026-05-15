package agent

import (
	"context"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/channels"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

func summarizeToolParams(toolName string, params map[string]any) map[string]any {
	summary := map[string]any{"tool": toolName}
	switch toolName {
	case tools.ToolNameExec:
		summary["program"] = strings.TrimSpace(fmt.Sprint(params["program"]))
		summary["cwd"] = strings.TrimSpace(fmt.Sprint(params["cwd"]))
	case tools.ToolNameRunSkill, tools.ToolNameRunSkillScript:
		summary["skill"] = strings.TrimSpace(fmt.Sprint(params["skill"]))
		summary["entrypoint"] = strings.TrimSpace(fmt.Sprint(params["entrypoint"]))
		summary["plan_id"] = strings.TrimSpace(fmt.Sprint(params["plan_id"]))
	case tools.ToolNameSpawnSubagent:
		summary["task"] = previewText(strings.TrimSpace(fmt.Sprint(params["task"])), 120)
	case tools.ToolNameWebFetch, tools.ToolNameWebFetchMarkdown:
		summary["url"] = strings.TrimSpace(fmt.Sprint(params["url"]))
	}
	return summary
}

func formatToolExecutionError(toolName string, params map[string]any, out string, err error) string {
	if err == nil {
		return out
	}
	return tools.EncodeToolFailure(toolName, params, out, err)
}

func (r *Runtime) skillRunEnvFor(name string) map[string]string {
	if r.Builder == nil {
		return nil
	}
	return r.Builder.Skills.RunEnvForSkill(name)
}

func (r *Runtime) persistAssistantReply(ctx context.Context, sessionKey string, msgID int64, channel, replyTarget, finalText string, replyMeta map[string]any, streamed bool, autoDeliver bool) {
	if strings.TrimSpace(finalText) == "" {
		finalText = "(no response)"
	}
	assistantID, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", finalText, map[string]any{"in_reply_to": msgID})
	if err != nil {
		log.Printf("append assistant(final) failed: %v", err)
	} else if r.DB != nil {
		scopeKey := sessionKey
		if resolved, rerr := r.DB.ResolveScopeKey(ctx, sessionKey); rerr == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
		card, _, _ := loadTaskCard(ctx, r.DB, sessionKey)
		card.Status = "active"
		card.MessageRefs = appendBoundedInt64(card.MessageRefs, assistantID, 12)
		if err := saveTaskCard(ctx, r.DB, sessionKey, scopeKey, card); err != nil {
			log.Printf("save task card failed: %v", err)
		}
	}
	if autoDeliver && !streamed && r.Deliver != nil {
		if err := r.deliver(ctx, channel, replyTarget, finalText, replyMeta); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}
}

func (r *Runtime) deliver(ctx context.Context, channel, to, text string, meta map[string]any) error {
	if r.Deliver == nil {
		return nil
	}
	if withMeta, ok := r.Deliver.(MetaDeliverer); ok {
		return withMeta.DeliverWithMeta(ctx, channel, to, text, channels.ReplyMeta(meta))
	}
	return r.Deliver.Deliver(ctx, channel, to, text)
}

func (r *Runtime) boundTextResult(ctx context.Context, sessionKey string, text string) (stored string, preview string, artifactID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(no response)", "(no response)", ""
	}
	preview = previewText(text, r.toolPreviewBytes())
	shouldStoreArtifact := r.Artifacts != nil && (preview != text || (r.MaxToolBytes > 0 && len(text) > r.MaxToolBytes))
	if shouldStoreArtifact {
		id, err := r.Artifacts.Save(ctx, sessionKey, "text/plain", []byte(text))
		if err != nil {
			log.Printf("artifact save failed: %v", err)
			return text, preview, ""
		}
		if r.DB != nil {
			summary := buildArtifactSummary(id, "text/plain", preview, int64(len(text)))
			if _, err := r.DB.InsertMemoryNoteTyped(ctx, sessionKey, db.TypedNoteInput{
				Text:             summary,
				Summary:          compactSemanticJSON(db.MemoryKindArtifact, preview, []string{"artifact:" + id}),
				SourceArtifactID: id,
				Kind:             db.MemoryKindArtifact,
				Status:           db.MemoryStatusActive,
				Importance:       0.2,
				Confidence:       0.9,
			}); err != nil {
				log.Printf("artifact summary note save failed: %v", err)
			}
			scopeKey := sessionKey
			if resolved, rerr := r.DB.ResolveScopeKey(ctx, sessionKey); rerr == nil && strings.TrimSpace(resolved) != "" {
				scopeKey = resolved
			}
			card, _, _ := loadTaskCard(ctx, r.DB, sessionKey)
			card.Status = "active"
			card.ArtifactRefs = appendBoundedString(card.ArtifactRefs, id, 12)
			if err := saveTaskCard(ctx, r.DB, sessionKey, scopeKey, card); err != nil {
				log.Printf("save task card artifact ref failed: %v", err)
			}
		}
		return tools.EncodeToolResult(tools.ToolResult{
			Kind:       "large_tool_output",
			OK:         true,
			Summary:    fmt.Sprintf("Large tool output saved as artifact %s", id),
			Preview:    preview,
			ArtifactID: id,
			Stats: map[string]any{
				"bytes": len(text),
			},
		}), preview, id
	}
	return text, preview, ""
}
