package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func terminalWebSocketRequestedProtocols(r *http.Request) []string {
	if r == nil {
		return nil
	}
	raw := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Protocol"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	protocols := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			protocols = append(protocols, part)
		}
	}
	return protocols
}

func terminalWebSocketProtocolRequested(r *http.Request) bool {
	for _, protocol := range terminalWebSocketRequestedProtocols(r) {
		if protocol == serviceTerminalWebSocketProtocol {
			return true
		}
	}
	return false
}

func terminalWebSocketTicketFromRequest(r *http.Request) (string, bool) {
	for _, protocol := range terminalWebSocketRequestedProtocols(r) {
		if strings.HasPrefix(protocol, serviceTerminalWebSocketTicketPrefix) {
			ticket := strings.TrimSpace(strings.TrimPrefix(protocol, serviceTerminalWebSocketTicketPrefix))
			return ticket, ticket != ""
		}
	}
	return "", false
}

func (s *serviceServer) handleTerminalWebSocket(w http.ResponseWriter, r *http.Request, sessionID string) {
	if !terminalWebSocketProtocolRequested(r) {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "terminal websocket protocol is required"})
		return
	}
	session, ok := s.getTerminalSessionByID(sessionID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "terminal session not found"})
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: serviceTerminalWebSocketHandshakeTimeout,
		Subprotocols:     []string{serviceTerminalWebSocketProtocol},
		CheckOrigin:      s.terminalWebSocketOriginAllowed,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(serviceTerminalBodyLimit)

	history, events, unsubscribe := session.subscribe()
	defer unsubscribe()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		s.readTerminalWebSocket(conn, sessionID)
	}()

	pings := time.NewTicker(serviceTerminalWebSocketPingInterval)
	defer pings.Stop()

	writeEvent := func(event serviceTerminalEvent) error {
		if err := conn.SetWriteDeadline(time.Now().Add(serviceTerminalWebSocketWriteTimeout)); err != nil {
			return err
		}
		return conn.WriteJSON(event)
	}
	closeNormally := func(reason string) {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason), time.Now().Add(serviceTerminalWebSocketWriteTimeout))
	}
	for _, event := range history {
		if err := writeEvent(event); err != nil {
			return
		}
		if terminalSessionEventIsTerminal(event) {
			closeNormally("terminal session ended")
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-readDone:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeEvent(event); err != nil {
				return
			}
			if terminalSessionEventIsTerminal(event) {
				closeNormally("terminal session ended")
				return
			}
		case <-pings.C:
			deadline := time.Now().Add(serviceTerminalWebSocketWriteTimeout)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				return
			}
		}
	}
}

func (s *serviceServer) terminalWebSocketOriginAllowed(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("Origin")) == "" {
		return true
	}
	_, ok := serviceAllowedBrowserOrigin(s.config, r)
	return ok
}

type serviceTerminalWebSocketClientMessage struct {
	Type  string `json:"type"`
	Input string `json:"input"`
	Rows  int    `json:"rows"`
	Cols  int    `json:"cols"`
}

func (s *serviceServer) readTerminalWebSocket(conn *websocket.Conn, sessionID string) {
	pongWait := serviceTerminalWebSocketPingInterval * 2
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		var msg serviceTerminalWebSocketClientMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				terminalWebSocketClose(conn, websocket.CloseUnsupportedData, "invalid terminal websocket message")
			}
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		switch strings.TrimSpace(msg.Type) {
		case "input":
			if msg.Input == "" {
				terminalWebSocketClose(conn, websocket.CloseUnsupportedData, "input is required")
				return
			}
			if err := s.terminalWriteInput(sessionID, msg.Input); err != nil {
				terminalWebSocketClose(conn, terminalWebSocketCloseCodeForError(err), err.Error())
				return
			}
		case "resize":
			if msg.Rows <= 0 && msg.Cols <= 0 {
				terminalWebSocketClose(conn, websocket.CloseUnsupportedData, errTerminalResizeRequired.Error())
				return
			}
			if _, _, err := s.terminalResize(sessionID, msg.Rows, msg.Cols); err != nil {
				terminalWebSocketClose(conn, terminalWebSocketCloseCodeForError(err), err.Error())
				return
			}
		case "close":
			if err := s.terminalClose(sessionID, "closed"); err != nil {
				terminalWebSocketClose(conn, terminalWebSocketCloseCodeForError(err), err.Error())
				return
			}
		default:
			terminalWebSocketClose(conn, websocket.CloseUnsupportedData, "unknown terminal websocket message type")
			return
		}
	}
}

func terminalWebSocketClose(conn *websocket.Conn, code int, reason string) {
	if conn == nil {
		return
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(serviceTerminalWebSocketWriteTimeout))
}

func terminalWebSocketCloseCodeForError(err error) int {
	switch {
	case errors.Is(err, errTerminalSessionNotFound):
		return websocket.ClosePolicyViolation
	case errors.Is(err, errTerminalInputRequired), errors.Is(err, errTerminalResizeRequired):
		return websocket.CloseUnsupportedData
	case errors.Is(err, errTerminalSessionNotWritable):
		return websocket.ClosePolicyViolation
	default:
		return websocket.CloseInternalServerErr
	}
}

func terminalSessionEventIsTerminal(event serviceTerminalEvent) bool {
	if event.Type != "status" {
		return false
	}
	status, _ := event.Data["status"].(string)
	return isTerminalSessionStatus(status)
}

func isTerminalSessionStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "closed", "failed", "exited", "expired":
		return true
	default:
		return false
	}
}
