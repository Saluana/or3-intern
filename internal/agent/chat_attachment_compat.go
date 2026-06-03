package agent

import (
	"or3-intern/internal/artifacts"
	"or3-intern/internal/turns"
)

type ChatAttachment = turns.Attachment

func DecodeChatAttachments(raw any) []ChatAttachment {
	return turns.DecodeAttachments(raw)
}

func ValidateChatAttachments(atts []ChatAttachment) error {
	return turns.ValidateAttachments(atts)
}

func chatAttachmentsFromMeta(meta map[string]any) []ChatAttachment {
	return turns.AttachmentsFromMeta(meta)
}

func mergeTurnAttachments(primary []ChatAttachment, meta map[string]any) []ChatAttachment {
	return turns.MergeAttachments(primary, meta)
}

func attachmentStableKey(att ChatAttachment) string {
	return turns.AttachmentStableKey(att)
}

func renderUserAttachmentsBody(atts []ChatAttachment) string {
	return turns.RenderUserAttachmentsBody(atts)
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
	return turns.RenderAttachmentTag(att)
}

func chatAttachmentsToArtifactAttachments(atts []ChatAttachment) []artifacts.Attachment {
	return turns.AttachmentsToArtifactAttachments(atts)
}

func ChatAttachmentsForMeta(atts []ChatAttachment) []map[string]any {
	return turns.AttachmentsForMeta(atts)
}

func chatAttachmentsToMeta(atts []ChatAttachment) []map[string]any {
	return turns.AttachmentsForMeta(atts)
}

func attachmentMessageRefs(atts []ChatAttachment) []string {
	return turns.AttachmentMessageRefs(atts)
}
