package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/db"
	"or3-intern/internal/secureconn"
)

const secureRelayMaxFrameBytes = secureconn.MaxSecureFrameBodyBytes + 4096

type secureRelayEnvelope struct {
	Type          string          `json:"type"`
	RouteID       string          `json:"route_id,omitempty"`
	HostIDHash    string          `json:"host_id_hash,omitempty"`
	DeviceIDHash  string          `json:"device_id_hash,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

type secureRelayPeer struct {
	id     string
	conn   *websocket.Conn
	send   chan secureRelayEnvelope
	closed chan struct{}
}

type secureConnectionRelayHub struct {
	mu      sync.RWMutex
	hosts   map[string]*secureRelayPeer
	devices map[string]*secureRelayPeer
	routes  map[string]secureRelayRoute
}

type secureRelayRoute struct {
	routeID      string
	hostIDHash   string
	deviceIDHash string
	expiresAt    int64
}

func newSecureConnectionRelayHub() *secureConnectionRelayHub {
	return &secureConnectionRelayHub{
		hosts:   map[string]*secureRelayPeer{},
		devices: map[string]*secureRelayPeer{},
		routes:  map[string]secureRelayRoute{},
	}
}

func (h *secureConnectionRelayHub) registerHost(hostIDHash string, peer *secureRelayPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hosts[hostIDHash] = peer
}

func (h *secureConnectionRelayHub) registerDevice(deviceIDHash string, peer *secureRelayPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.devices[deviceIDHash] = peer
}

func (h *secureConnectionRelayHub) unregisterHost(hostIDHash string, peer *secureRelayPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hosts[hostIDHash] == peer {
		delete(h.hosts, hostIDHash)
	}
}

func (h *secureConnectionRelayHub) unregisterDevice(deviceIDHash string, peer *secureRelayPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.devices[deviceIDHash] == peer {
		delete(h.devices, deviceIDHash)
	}
}

func (h *secureConnectionRelayHub) registerRoute(route secureRelayRoute) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.routes[route.routeID] = route
}

func (h *secureConnectionRelayHub) purgeExpiredRoutes() int {
	now := time.Now().UTC().UnixMilli()
	h.mu.Lock()
	defer h.mu.Unlock()
	purged := 0
	for id, route := range h.routes {
		if route.expiresAt > 0 && route.expiresAt <= now {
			delete(h.routes, id)
			purged++
		}
	}
	return purged
}

func (h *secureConnectionRelayHub) host(hostIDHash string) *secureRelayPeer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.hosts[hostIDHash]
}

func (h *secureConnectionRelayHub) forward(fromHost bool, peerID string, env secureRelayEnvelope) bool {
	h.mu.RLock()
	route, ok := h.routes[strings.TrimSpace(env.RouteID)]
	if !ok || (route.expiresAt > 0 && route.expiresAt <= time.Now().UTC().UnixMilli()) {
		h.mu.RUnlock()
		return false
	}
	peerID = strings.TrimSpace(peerID)
	if fromHost && peerID != route.hostIDHash {
		h.mu.RUnlock()
		return false
	}
	if !fromHost && peerID != route.deviceIDHash {
		h.mu.RUnlock()
		return false
	}
	var target *secureRelayPeer
	if fromHost {
		target = h.devices[route.deviceIDHash]
	} else {
		target = h.hosts[route.hostIDHash]
	}
	h.mu.RUnlock()
	if target == nil {
		return false
	}
	select {
	case <-target.closed:
		return false
	default:
	}
	select {
	case <-target.closed:
		return false
	case target.send <- env:
		return true
	default:
		return false
	}
}

func (s *serviceServer) handleSecureRelayRouteRequest(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	s.components()
	limitServiceRequestBody(w, r, servicePairingBodyLimit)
	var body struct {
		AccountID    string `json:"account_id"`
		HostIDHash   string `json:"host_id_hash"`
		DeviceIDHash string `json:"device_id_hash"`
		TTLSeconds   int    `json:"ttl_seconds"`
	}
	if err := decodeServiceRequestBody(r.Body, &body); err != nil {
		writeServiceRequestDecodeError(w, err)
		return
	}
	if strings.TrimSpace(body.HostIDHash) == "" {
		body.HostIDHash = secureconn.HashBase64URL([]byte(store.Identity.HostID))
	} else {
		expectedHash := secureconn.HashBase64URL([]byte(store.Identity.HostID))
		if strings.TrimSpace(body.HostIDHash) != expectedHash {
			writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "host_id_hash does not match authenticated host"})
			return
		}
	}
	if strings.TrimSpace(body.DeviceIDHash) == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "device_id_hash required"})
		return
	}
	ttl := time.Duration(body.TTLSeconds) * time.Second
	if ttl <= 0 || ttl > time.Hour {
		ttl = 10 * time.Minute
	}
	routeID, err := secureconn.RandomBase64URL(18)
	if err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "relay route creation failed", err)
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl).UnixMilli()
	hostIDHash := strings.TrimSpace(body.HostIDHash)
	deviceIDHash := strings.TrimSpace(body.DeviceIDHash)
	if err := store.DB.CreateRelayRoute(r.Context(), db.RelayRouteRecord{
		RouteID:      routeID,
		AccountID:    strings.TrimSpace(body.AccountID),
		HostIDHash:   hostIDHash,
		DeviceIDHash: deviceIDHash,
		Status:       secureconn.StatusCreated,
		CreatedAt:    now.UnixMilli(),
		ExpiresAt:    expiresAt,
		Metadata:     map[string]any{"kind": "opaque-secure-route"},
	}); err != nil {
		writeServiceError(w, r, http.StatusBadRequest, "relay route creation failed", err)
		return
	}
	s.secureRelayHub.registerRoute(secureRelayRoute{routeID: routeID, hostIDHash: hostIDHash, deviceIDHash: deviceIDHash, expiresAt: expiresAt})
	if host := s.secureRelayHub.host(hostIDHash); host != nil {
		select {
		case host.send <- secureRelayEnvelope{Type: "route_request", RouteID: routeID, HostIDHash: hostIDHash, DeviceIDHash: deviceIDHash}:
		default:
		}
	}
	writeServiceJSON(w, http.StatusCreated, map[string]any{"route_id": routeID, "expires_at": expiresAt})
}

func (s *serviceServer) handleSecureRelayHostWebSocket(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	expectedHash := secureconn.HashBase64URL([]byte(store.Identity.HostID))
	requestedHash := strings.TrimSpace(r.URL.Query().Get("host_id_hash"))
	// If a host_id_hash is provided, it must match the authenticated host.
	// Reject arbitrary hash registration.
	if requestedHash != "" && requestedHash != expectedHash {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "host_id_hash does not match authenticated host"})
		return
	}
	s.handleSecureRelayWebSocket(w, r, expectedHash, true)
}

func (s *serviceServer) handleSecureRelayDeviceWebSocket(w http.ResponseWriter, r *http.Request, store *secureconn.TrustStore) {
	deviceIDHash := strings.TrimSpace(r.URL.Query().Get("device_id_hash"))
	if deviceIDHash == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "device_id_hash required"})
		return
	}
	// Validate the device_id_hash corresponds to an enrolled device for this host.
	// Compute the hash of each active enrolled device ID and compare.
	deviceIDs, err := store.ListDeviceIDs(r.Context(), secureconn.StatusActive)
	if err != nil {
		writeServiceError(w, r, http.StatusInternalServerError, "device validation failed", err)
		return
	}
	matched := false
	for _, deviceID := range deviceIDs {
		if secureconn.HashBase64URL([]byte(deviceID)) == deviceIDHash {
			matched = true
			break
		}
	}
	if !matched {
		writeServiceJSON(w, http.StatusForbidden, map[string]any{"error": "device_id_hash does not match any enrolled device"})
		return
	}
	s.handleSecureRelayWebSocket(w, r, deviceIDHash, false)
}

func (s *serviceServer) handleSecureRelayWebSocket(w http.ResponseWriter, r *http.Request, id string, host bool) {
	s.components()
	upgrader := websocket.Upgrader{
		HandshakeTimeout: serviceTerminalWebSocketHandshakeTimeout,
		CheckOrigin: func(req *http.Request) bool {
			return secureRelayOriginAllowed(req)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(secureRelayMaxFrameBytes)
	peer := &secureRelayPeer{id: id, conn: conn, send: make(chan secureRelayEnvelope, 32), closed: make(chan struct{})}
	if host {
		s.secureRelayHub.registerHost(id, peer)
		defer s.secureRelayHub.unregisterHost(id, peer)
	} else {
		s.secureRelayHub.registerDevice(id, peer)
		defer s.secureRelayHub.unregisterDevice(id, peer)
	}
	go secureRelayWritePump(peer)
	secureRelayReadPump(s.secureRelayHub, peer, host)
}

func secureRelayReadPump(hub *secureConnectionRelayHub, peer *secureRelayPeer, host bool) {
	defer func() {
		close(peer.closed)
		_ = peer.conn.Close()
	}()
	for {
		var env secureRelayEnvelope
		if err := peer.conn.ReadJSON(&env); err != nil {
			return
		}
		switch env.Type {
		case "presence", "ping":
			select {
			case peer.send <- secureRelayEnvelope{Type: "ack", CorrelationID: env.CorrelationID}:
			default:
			}
		case "opaque_frame":
			if len(env.Payload) > secureRelayMaxFrameBytes {
				select {
				case peer.send <- secureRelayEnvelope{Type: "error", CorrelationID: env.CorrelationID, Payload: json.RawMessage(`{"code":"FRAME_TOO_LARGE"}`)}:
				default:
				}
				continue
			}
			hub.forward(host, peer.id, env)
		}
	}
}

func secureRelayOriginAllowed(req *http.Request) bool {
	if req == nil {
		return false
	}
	origin := strings.TrimSpace(req.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	switch scheme {
	case "app":
		return host == "or3"
	case "http", "https":
		return serviceOriginIsLoopback(parsed) && requestRemoteIsLoopback(req.RemoteAddr)
	default:
		return false
	}
}

func secureRelayWritePump(peer *secureRelayPeer) {
	ticker := time.NewTicker(serviceTerminalWebSocketPingInterval)
	defer ticker.Stop()
	for {
		select {
		case env := <-peer.send:
			_ = peer.conn.SetWriteDeadline(time.Now().Add(serviceTerminalWebSocketWriteTimeout))
			if err := peer.conn.WriteJSON(env); err != nil {
				_ = peer.conn.Close()
				return
			}
		case <-ticker.C:
			_ = peer.conn.SetWriteDeadline(time.Now().Add(serviceTerminalWebSocketWriteTimeout))
			if err := peer.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				_ = peer.conn.Close()
				return
			}
		case <-peer.closed:
			return
		}
	}
}
