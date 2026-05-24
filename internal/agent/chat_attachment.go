package agent

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"or3-intern/internal/artifacts"
)

const (
	attachmentSourceWorkspaceRef = "workspace_ref"
	attachmentSourceLocalArtifact = "local_artifact"
	attachmentSourceTextBlock    = "text_block"

	maxAttachmentCount       = 24
	maxAttachmentNameLen     = 240
	maxAttachmentPathLen     = 1024
	maxAttachmentPreviewLen  = 600
	maxAttachmentExcerptLen  = 1200
)

// ChatAttachment is the canonical turn attachment shape shared by app and service.
type ChatAttachment struct {
	ID             string `json:"id"`
	Source         string `json:"source"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	MimeType       string `json:"mime_type,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
	RootID         string `json:"root_id,omitempty"`
	Path           string `json:"path,omitempty"`
	ArtifactID     string `json:"artifact_id,omitempty"`
	Preview        string `json:"preview,omitempty"`
	ContentExcerpt string `json:"content_excerpt,omitempty"`
}

func DecodeChatAttachments(raw any) []ChatAttachment {
	switch typed := raw.(type) {
	case []ChatAttachment:
		return append([]ChatAttachment(nil), typed...)
	case []any:
		out := make([]ChatAttachment, 0, len(typed))
		for _, item := range typed {
			if att, ok := decodeChatAttachment(item); ok {
				out = append(out, att)
			}
		}
		return out
	case []map[string]any:
		out := make([]ChatAttachment, 0, len(typed))
		for _, item := range typed {
			if att, ok := decodeChatAttachment(item); ok {
				out = append(out, att)
			}
		}
		return out
	default:
		return nil
	}
}

func decodeChatAttachment(raw any) (ChatAttachment, bool) {
	switch typed := raw.(type) {
	case ChatAttachment:
		return normalizeChatAttachment(typed), true
	case map[string]any:
		att := ChatAttachment{
			ID:             firstString(typed, "id"),
			Source:         firstString(typed, "source"),
			Kind:           firstString(typed, "kind"),
			Name:           firstString(typed, "name", "filename"),
			MimeType:       firstString(typed, "mime_type", "mimeType", "mime"),
			SizeBytes:      payloadInt64Value(typed["size_bytes"]),
			RootID:         firstString(typed, "root_id", "rootId"),
			Path:           firstString(typed, "path"),
			ArtifactID:     firstString(typed, "artifact_id", "artifactId"),
			Preview:        firstString(typed, "preview"),
			ContentExcerpt: firstString(typed, "content_excerpt", "contentExcerpt", "content"),
		}
		if att.SizeBytes == 0 {
			att.SizeBytes = payloadInt64Value(typed["size"])
		}
		if att.Kind == "" && att.MimeType != "" {
			att.Kind = mimeToAttachmentKind(att.MimeType)
		}
		if att.Source == "" {
			if att.ArtifactID != "" {
				att.Source = attachmentSourceLocalArtifact
			} else if att.Path != "" {
				att.Source = attachmentSourceWorkspaceRef
			} else if att.ContentExcerpt != "" {
				att.Source = attachmentSourceTextBlock
			}
		}
		return normalizeChatAttachment(att), att.ID != "" || att.ArtifactID != "" || att.Path != "" || att.ContentExcerpt != ""
	default:
		return ChatAttachment{}, false
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if val := payloadStringValue(values[key]); val != "" {
			return val
		}
	}
	return ""
}

func normalizeChatAttachment(att ChatAttachment) ChatAttachment {
	att.ID = strings.TrimSpace(att.ID)
	att.Source = strings.TrimSpace(att.Source)
	att.Kind = strings.TrimSpace(att.Kind)
	att.Name = strings.TrimSpace(att.Name)
	att.MimeType = strings.TrimSpace(att.MimeType)
	att.RootID = strings.TrimSpace(att.RootID)
	att.Path = strings.TrimSpace(att.Path)
	att.ArtifactID = strings.TrimSpace(att.ArtifactID)
	att.Preview = oneLine(att.Preview, maxAttachmentPreviewLen)
	att.ContentExcerpt = oneLine(att.ContentExcerpt, maxAttachmentExcerptLen)
	if att.Name == "" {
		att.Name = "attachment"
	}
	if att.Kind == "" {
		att.Kind = "file"
	}
	return att
}

func mimeToAttachmentKind(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case mime == "text/plain" || strings.HasSuffix(mime, "/json"):
		return "text"
	default:
		return "file"
	}
}

func ValidateChatAttachments(atts []ChatAttachment) error {
	if len(atts) > maxAttachmentCount {
		return fmt.Errorf("too many attachments (max %d)", maxAttachmentCount)
	}
	for _, att := range atts {
		if len(att.Name) > maxAttachmentNameLen {
			return fmt.Errorf("attachment name too long")
		}
		if len(att.Path) > maxAttachmentPathLen {
			return fmt.Errorf("attachment path too long")
		}
		switch att.Source {
		case attachmentSourceWorkspaceRef:
			if att.Path == "" {
				return fmt.Errorf("workspace_ref attachment requires path")
			}
		case attachmentSourceLocalArtifact:
			if att.ArtifactID == "" {
				return fmt.Errorf("local_artifact attachment requires artifact_id")
			}
		case attachmentSourceTextBlock:
			if att.ContentExcerpt == "" && att.ArtifactID == "" {
				return fmt.Errorf("text_block attachment requires content_excerpt or artifact_id")
			}
		case "":
			return fmt.Errorf("attachment source required")
		default:
			return fmt.Errorf("unsupported attachment source: %s", att.Source)
		}
	}
	return nil
}

func chatAttachmentsFromMeta(meta map[string]any) []ChatAttachment {
	if len(meta) == 0 {
		return nil
	}
	if raw := meta["attachments"]; raw != nil {
		return DecodeChatAttachments(raw)
	}
	return nil
}

func mergeTurnAttachments(primary []ChatAttachment, meta map[string]any) []ChatAttachment {
	out := append([]ChatAttachment(nil), primary...)
	seen := map[string]struct{}{}
	for _, att := range out {
		seen[attachmentStableKey(att)] = struct{}{}
	}
	for _, att := range chatAttachmentsFromMeta(meta) {
		key := attachmentStableKey(att)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, att)
	}
	return out
}

func attachmentStableKey(att ChatAttachment) string {
	if id := strings.TrimSpace(att.ID); id != "" {
		return "id:" + id
	}
	if id := strings.TrimSpace(att.ArtifactID); id != "" {
		return "artifact:" + id
	}
	if path := strings.TrimSpace(att.Path); path != "" {
		return "path:" + path
	}
	return "name:" + strings.TrimSpace(att.Name)
}

func renderUserAttachmentsBody(atts []ChatAttachment) string {
	if len(atts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Use read_file for workspace_ref paths. Use read_artifact when artifact_id is present. Attachment bodies are not fully inlined.")
	for i, att := range atts {
		b.WriteString("\n")
		b.WriteString(renderChatAttachmentTag(att))
		_ = i
	}
	return strings.TrimSpace(b.String())
}

func renderUserAttachmentsEnvelope(atts []ChatAttachment) string {
	body := renderUserAttachmentsBody(atts)
	if body == "" {
		return ""
	}
	return renderXMLEnvelope(xmlTagUserAttachments, body, envelopeAttrs{
		"protected": "true",
		"volatile":  "true",
	})
}

func renderChatAttachmentTag(att ChatAttachment) string {
	att = normalizeChatAttachment(att)
	attrs := []string{
		fmt.Sprintf(`source="%s"`, htmlEscapeAttr(att.Source)),
		fmt.Sprintf(`kind="%s"`, htmlEscapeAttr(att.Kind)),
		fmt.Sprintf(`name="%s"`, htmlEscapeAttr(att.Name)),
	}
	if att.ID != "" {
		attrs = append(attrs, fmt.Sprintf(`id="%s"`, htmlEscapeAttr(att.ID)))
	}
	if att.MimeType != "" {
		attrs = append(attrs, fmt.Sprintf(`mime_type="%s"`, htmlEscapeAttr(att.MimeType)))
	}
	if att.SizeBytes > 0 {
		attrs = append(attrs, fmt.Sprintf(`size_bytes="%d"`, att.SizeBytes))
	}
	if att.RootID != "" {
		attrs = append(attrs, fmt.Sprintf(`root_id="%s"`, htmlEscapeAttr(att.RootID)))
	}
	if att.Path != "" {
		attrs = append(attrs, fmt.Sprintf(`path="%s"`, htmlEscapeAttr(att.Path)))
	}
	if att.ArtifactID != "" {
		attrs = append(attrs, fmt.Sprintf(`artifact_id="%s"`, htmlEscapeAttr(att.ArtifactID)))
	}
	body := strings.TrimSpace(att.Preview)
	if body == "" {
		body = strings.TrimSpace(att.ContentExcerpt)
	}
	if body != "" {
		body = oneLine(body, maxAttachmentPreviewLen)
	}
	return fmt.Sprintf("<attachment %s>%s</attachment>", strings.Join(attrs, " "), body)
}

func htmlEscapeAttr(value string) string {
	return html.EscapeString(value)
}

func chatAttachmentsToArtifactAttachments(atts []ChatAttachment) []artifacts.Attachment {
	out := make([]artifacts.Attachment, 0, len(atts))
	for _, att := range atts {
		if art, ok := att.ToArtifactAttachment(); ok {
			out = append(out, art)
		}
	}
	return out
}

func (att ChatAttachment) ToArtifactAttachment() (artifacts.Attachment, bool) {
	if strings.TrimSpace(att.ArtifactID) == "" {
		return artifacts.Attachment{}, false
	}
	return artifacts.Attachment{
		ArtifactID: att.ArtifactID,
		Filename:   att.Name,
		Mime:       att.MimeType,
		Kind:       artifacts.DetectKind(att.Name, att.MimeType),
		SizeBytes:  att.SizeBytes,
	}, true
}

// ChatAttachmentsForMeta serializes turn attachments for message meta persistence.
func ChatAttachmentsForMeta(atts []ChatAttachment) []map[string]any {
	return chatAttachmentsToMeta(atts)
}

func chatAttachmentsToMeta(atts []ChatAttachment) []map[string]any {
	if len(atts) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(atts))
	for _, att := range atts {
		raw, _ := json.Marshal(att)
		var item map[string]any
		_ = json.Unmarshal(raw, &item)
		if len(item) > 0 {
			out = append(out, item)
		}
	}
	return out
}

func attachmentMessageRefs(atts []ChatAttachment) []string {
	out := make([]string, 0, len(atts))
	for _, att := range atts {
		switch att.Source {
		case attachmentSourceWorkspaceRef:
			if att.Path != "" {
				out = append(out, "file:"+att.Path)
			}
		case attachmentSourceLocalArtifact:
			if att.ArtifactID != "" {
				out = append(out, "artifact:"+att.ArtifactID)
			}
		case attachmentSourceTextBlock:
			if att.ID != "" {
				out = append(out, "text:"+att.ID)
			}
		}
	}
	return out
}
