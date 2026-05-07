package approval

import (
	"context"
	"log"
	"strings"
)

type auditAuthKindKey struct{}

type auditActorKey struct{}

func ContextWithAuditAuthKind(ctx context.Context, kind string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ctx
	}
	return context.WithValue(ctx, auditAuthKindKey{}, kind)
}

func ContextWithAuditActor(ctx context.Context, actor string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, auditActorKey{}, actor)
}

func auditAuthKindFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	kind, _ := ctx.Value(auditAuthKindKey{}).(string)
	return kind
}

func auditActorFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	actor, _ := ctx.Value(auditActorKey{}).(string)
	return strings.TrimSpace(actor)
}

func (b *Broker) audit(ctx context.Context, eventType string, payload map[string]any) error {
	if b == nil || b.Audit == nil {
		return nil
	}
	if kind := auditAuthKindFromContext(ctx); kind != "" {
		merged := make(map[string]any, len(payload)+1)
		for k, v := range payload {
			merged[k] = v
		}
		merged["auth_kind"] = kind
		payload = merged
	}
	actor := auditActorFromContext(ctx)
	if payloadActor, ok := payload["actor"].(string); ok && strings.TrimSpace(payloadActor) != "" {
		actor = strings.TrimSpace(payloadActor)
	}
	if actor == "" {
		actor = "approval"
	}
	if err := b.Audit.Record(ctx, eventType, "", actor, payload); err != nil {
		log.Printf("approval audit %s: %v", eventType, err)
		return err
	}
	return nil
}

func (b *Broker) AuditExecEvent(ctx context.Context, eventType string, subjectHash string, extra map[string]any) {
	if b == nil {
		return
	}
	payload := map[string]any{
		"subject_hash": subjectHash,
		"host_id":      b.hostID(),
	}
	for k, v := range extra {
		payload[k] = v
	}
	_ = b.audit(ctx, eventType, payload)
}
