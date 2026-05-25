package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"or3-intern/internal/agent"
	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type channelApprovalHandler struct {
	Config   config.Config
	Runtime  *agent.Runtime
	Jobs     *agent.JobRegistry
	Broker   *approval.Broker
	Channels *rootchannels.Manager
}

type parsedApprovalCommand struct {
	Action    string
	RequestID int64
}

func (h *channelApprovalHandler) Handle(ctx context.Context, ev bus.Event) (bool, error) {
	if h == nil || h.Broker == nil || strings.EqualFold(ev.Channel, "cli") || ev.Type != bus.EventUserMessage {
		return false, nil
	}
	cmd, ok := parseChannelApprovalCommand(ev.Message)
	if !ok {
		return false, nil
	}
	req, ok, err := h.resolveApprovalCommandRequest(ctx, cmd, ev)
	if err != nil {
		h.deliver(ctx, ev, channelApprovalMessage("Approval update failed", err.Error()))
		return true, err
	}
	if !ok {
		h.deliver(ctx, ev, "I could not find a matching pending approval for this conversation. Reply `/approve <id>` or `/deny <id>` using the request number from the approval prompt.")
		return true, nil
	}
	if !channelApprovalRequestMatchesEvent(req, ev) {
		h.deliver(ctx, ev, "I cannot accept that approval from this conversation. Please approve it from the original chat or in the OR3 app.")
		return true, nil
	}
	actor := channelApprovalActor(ev)
	switch cmd.Action {
	case "deny":
		if err := h.Broker.DenyRequest(ctx, req.ID, actor, "denied from "+ev.Channel); err != nil {
			h.deliver(ctx, ev, channelApprovalMessage("Approval denial failed", err.Error()))
			return true, err
		}
		h.deliver(ctx, ev, fmt.Sprintf("Request #%d was denied. I will not continue that action.", req.ID))
		return true, nil
	case "approve":
		issued, err := h.Broker.ApproveRequest(ctx, req.ID, actor, false, "approved from "+ev.Channel)
		if err != nil {
			h.deliver(ctx, ev, channelApprovalMessage("Approval failed", err.Error()))
			return true, err
		}
		h.deliver(ctx, ev, fmt.Sprintf("Request #%d was approved. Continuing now.", req.ID))
		go h.resumeApprovedRequest(withDetachedContext(ctx), issued, actor)
		return true, nil
	default:
		return false, nil
	}
}

func (h *channelApprovalHandler) resolveApprovalCommandRequest(ctx context.Context, cmd parsedApprovalCommand, ev bus.Event) (db.ApprovalRequestRecord, bool, error) {
	if cmd.RequestID > 0 {
		req, err := h.Broker.DB.GetApprovalRequest(ctx, cmd.RequestID)
		if err != nil {
			return db.ApprovalRequestRecord{}, false, err
		}
		if req.Status != approval.StatusPending {
			return req, false, nil
		}
		return req, true, nil
	}
	records, err := h.Broker.DB.ListApprovalRequestsFiltered(ctx, approval.StatusPending, "", 100)
	if err != nil {
		return db.ApprovalRequestRecord{}, false, err
	}
	matches := make([]db.ApprovalRequestRecord, 0, 2)
	for _, rec := range records {
		if channelApprovalRequestMatchesEvent(rec, ev) {
			matches = append(matches, rec)
		}
	}
	if len(matches) != 1 {
		return db.ApprovalRequestRecord{}, false, nil
	}
	return matches[0], true, nil
}

func (h *channelApprovalHandler) resumeApprovedRequest(ctx context.Context, issued approval.IssuedApproval, actor string) {
	if h == nil || h.Runtime == nil {
		return
	}
	serviceApp := app.NewServiceApp(h.Config, h.Runtime, h.Jobs, nil, nil)
	finalText, err := serviceApp.ResumeApprovedRequest(ctx, app.ResumeApprovedRequest{
		IssuedApproval: issued,
		Capability:     tools.CapabilityGuarded,
		Actor:          actor,
		Role:           approval.RoleOperator,
	})
	requester := approval.RequesterContextFromJSON(issued.Request.RequesterContextJSON)
	if err != nil {
		var approvalErr *tools.ApprovalRequiredError
		if errors.As(err, &approvalErr) && h.deliverResumeApprovalRequired(ctx, issued.Request, approvalErr) {
			return
		}
		text := approvalResumeFailureMessage(err)
		if h.Channels != nil && isApprovalExternalChannel(requester.Channel) && strings.TrimSpace(requester.ReplyTarget) != "" {
			_ = h.Channels.DeliverWithMeta(ctx, requester.Channel, requester.ReplyTarget, text, approvalDeliveryMeta(requester))
		}
		return
	}
	if strings.TrimSpace(finalText) != "" && h.Channels != nil && isApprovalExternalChannel(requester.Channel) && strings.TrimSpace(requester.ReplyTarget) != "" {
		_ = h.Channels.DeliverWithMeta(ctx, requester.Channel, requester.ReplyTarget, finalText, approvalDeliveryMeta(requester))
	}
}

func (h *channelApprovalHandler) deliverResumeApprovalRequired(ctx context.Context, fallbackReq db.ApprovalRequestRecord, approvalErr *tools.ApprovalRequiredError) bool {
	if h == nil || h.Channels == nil || approvalErr == nil {
		return false
	}
	req, text := approvalRequiredContinuationPrompt(ctx, h.Broker, fallbackReq, approvalErr)
	requester := approval.RequesterContextFromJSON(req.RequesterContextJSON)
	if !isApprovalExternalChannel(requester.Channel) {
		return false
	}
	to := strings.TrimSpace(requester.ReplyTarget)
	if to == "" {
		to = strings.TrimSpace(requester.From)
	}
	if to == "" || strings.TrimSpace(text) == "" {
		return false
	}
	return h.Channels.DeliverWithMeta(ctx, requester.Channel, to, text, approvalDeliveryMeta(requester)) == nil
}

func parseChannelApprovalCommand(message string) (parsedApprovalCommand, bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return parsedApprovalCommand{}, false
	}
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return parsedApprovalCommand{}, false
	}
	action := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if action != "approve" && action != "deny" {
		return parsedApprovalCommand{}, false
	}
	cmd := parsedApprovalCommand{Action: action}
	if len(fields) >= 2 {
		idText := strings.Trim(strings.TrimSpace(fields[1]), "#.,:;")
		id, err := strconv.ParseInt(idText, 10, 64)
		if err == nil && id > 0 {
			cmd.RequestID = id
		}
	}
	return cmd, true
}

func channelApprovalRequestMatchesEvent(req db.ApprovalRequestRecord, ev bus.Event) bool {
	requester := approval.RequesterContextFromJSON(req.RequesterContextJSON)
	if requester.IsZero() {
		return false
	}
	if !strings.EqualFold(requester.Channel, ev.Channel) {
		return false
	}
	if requester.From != "" && requester.From != ev.From {
		return false
	}
	if requester.ReplyTarget != "" && channelEventTarget(ev) != "" && requester.ReplyTarget != channelEventTarget(ev) {
		return false
	}
	if requester.SessionKey != "" && requester.SessionKey != ev.SessionKey && !channelApprovalRequesterTargetMatchesEvent(requester, ev) {
		return false
	}
	return true
}

func channelApprovalRequesterTargetMatchesEvent(requester approval.RequesterContext, ev bus.Event) bool {
	target := channelEventTarget(ev)
	if target == "" {
		return false
	}
	if requester.ReplyTarget != "" && requester.ReplyTarget == target {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(ev.Channel)) {
	case "telegram", "whatsapp":
		return requester.From != "" && requester.From == target
	default:
		return false
	}
}

func channelApprovalActor(ev bus.Event) string {
	channel := strings.ToLower(strings.TrimSpace(ev.Channel))
	from := strings.TrimSpace(ev.From)
	if from == "" {
		from = "unknown"
	}
	return "channel:" + channel + ":" + from
}

func (h *channelApprovalHandler) deliver(ctx context.Context, ev bus.Event, text string) {
	if h == nil || h.Channels == nil || strings.TrimSpace(text) == "" {
		return
	}
	_ = h.Channels.DeliverWithMeta(ctx, ev.Channel, channelEventTarget(ev), text, rootchannels.ReplyMeta(ev.Meta))
}

func channelApprovalMessage(title string, detail string) string {
	title = strings.TrimSpace(title)
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return title
	}
	return title + ": " + detail
}

func channelEventTarget(ev bus.Event) string {
	if len(ev.Meta) > 0 {
		for _, key := range []string{"chat_id", "channel_id"} {
			if target := strings.TrimSpace(fmt.Sprint(ev.Meta[key])); target != "" && target != "<nil>" {
				return target
			}
		}
	}
	return strings.TrimSpace(ev.From)
}
