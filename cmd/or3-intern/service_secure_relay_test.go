package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"or3-intern/internal/secureconn"
)

func TestSecureRelayHubForwardsOpaqueFrames(t *testing.T) {
	hub := newSecureConnectionRelayHub()
	host := &secureRelayPeer{id: "host", send: make(chan secureRelayEnvelope, 1)}
	device := &secureRelayPeer{id: "device", send: make(chan secureRelayEnvelope, 1)}
	hub.registerHost("host-hash", host)
	hub.registerDevice("device-hash", device)
	hub.registerRoute(secureRelayRoute{
		routeID:      "route",
		hostIDHash:   "host-hash",
		deviceIDHash: "device-hash",
		expiresAt:    time.Now().Add(time.Minute).UnixMilli(),
	})
	if result := hub.forward(false, "device-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}); !result.Delivered {
		t.Fatal("expected device-to-host frame to forward")
	}
	select {
	case <-host.send:
	default:
		t.Fatal("expected host to receive forwarded frame")
	}
	if result := hub.forward(true, "host-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}); !result.Delivered {
		t.Fatal("expected host-to-device frame to forward")
	}
	select {
	case <-device.send:
	default:
		t.Fatal("expected device to receive forwarded frame")
	}
}

func TestSecureRelayHubRejectsExpiredRoutes(t *testing.T) {
	hub := newSecureConnectionRelayHub()
	hub.registerHost("host-hash", &secureRelayPeer{id: "host", send: make(chan secureRelayEnvelope, 1)})
	hub.registerRoute(secureRelayRoute{
		routeID:      "route",
		hostIDHash:   "host-hash",
		deviceIDHash: "device-hash",
		expiresAt:    time.Now().Add(-time.Second).UnixMilli(),
	})
	if result := hub.forward(false, "device-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}); result.Delivered || result.Code != "ROUTE_EXPIRED" {
		t.Fatal("expected expired route not to forward")
	}
	hub.mu.RLock()
	_, retained := hub.routes["route"]
	hub.mu.RUnlock()
	if retained {
		t.Fatal("expected an expired route lookup to remove the route")
	}
}

func TestSecureRelayHubPurgesExpiredRoutesAndBoundsLiveRoutes(t *testing.T) {
	hub := newSecureConnectionRelayHub()
	now := time.Now().UTC()
	hub.registerRoute(secureRelayRoute{routeID: "expired", expiresAt: now.Add(-time.Second).UnixMilli()})
	hub.registerRoute(secureRelayRoute{routeID: "live", expiresAt: now.Add(time.Hour).UnixMilli()})
	hub.mu.RLock()
	_, expiredRetained := hub.routes["expired"]
	hub.mu.RUnlock()
	if expiredRetained {
		t.Fatal("expected route insertion to purge expired routes")
	}

	for i := 0; i < secureRelayMaxRoutes+8; i++ {
		hub.registerRoute(secureRelayRoute{routeID: fmt.Sprintf("route-%d", i), expiresAt: now.Add(time.Duration(i+1) * time.Minute).UnixMilli()})
	}
	hub.mu.RLock()
	count := len(hub.routes)
	hub.mu.RUnlock()
	if count > secureRelayMaxRoutes {
		t.Fatalf("expected at most %d live routes, got %d", secureRelayMaxRoutes, count)
	}
}

func TestSecureRelayHubRejectsMismatchedSender(t *testing.T) {
	hub := newSecureConnectionRelayHub()
	hub.registerHost("host-hash", &secureRelayPeer{id: "host", send: make(chan secureRelayEnvelope, 1)})
	hub.registerDevice("device-hash", &secureRelayPeer{id: "device", send: make(chan secureRelayEnvelope, 1)})
	hub.registerRoute(secureRelayRoute{
		routeID:      "route",
		hostIDHash:   "host-hash",
		deviceIDHash: "device-hash",
		expiresAt:    time.Now().Add(time.Minute).UnixMilli(),
	})
	if result := hub.forward(false, "device-imposter", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}); result.Delivered || result.Code != "SENDER_MISMATCH" {
		t.Fatal("expected device sender mismatch to be rejected")
	}
	if result := hub.forward(true, "host-imposter", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}); result.Delivered || result.Code != "SENDER_MISMATCH" {
		t.Fatal("expected host sender mismatch to be rejected")
	}
}

func TestSecureRelayOriginAllowed(t *testing.T) {
	appReq := httptest.NewRequest("GET", "http://example.test", nil)
	appReq.Header.Set("Origin", "app://or3")
	if !secureRelayOriginAllowed(appReq) {
		t.Fatal("expected app origin to be allowed")
	}
	loopbackReq := httptest.NewRequest("GET", "http://example.test", nil)
	loopbackReq.Header.Set("Origin", "http://127.0.0.1:3060")
	loopbackReq.RemoteAddr = "127.0.0.1:56789"
	if !secureRelayOriginAllowed(loopbackReq) {
		t.Fatal("expected exact loopback origin to be allowed")
	}
	evilReq := httptest.NewRequest("GET", "http://example.test", nil)
	evilReq.Header.Set("Origin", "https://localhost.evil.example")
	evilReq.RemoteAddr = "127.0.0.1:56789"
	if secureRelayOriginAllowed(evilReq) {
		t.Fatal("expected substring localhost origin to be rejected")
	}
}

func TestSecureRelayHostWebSocketRejectsMismatchedHash(t *testing.T) {
	mismatchHash := secureconn.HashBase64URL([]byte("host-456"))
	req := httptest.NewRequest("GET", "/relay/host?host_id_hash="+url.QueryEscape(mismatchHash), nil)
	rec := httptest.NewRecorder()
	server := &serviceServer{}
	store := &secureconn.TrustStore{Identity: secureconn.HostIdentity{HostID: "host-123"}}

	server.handleSecureRelayHostWebSocket(rec, req, store)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for mismatched host hash, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSecureRelayHubClosedTargetReturnsFailure(t *testing.T) {
	hub := newSecureConnectionRelayHub()
	host := &secureRelayPeer{id: "host", send: make(chan secureRelayEnvelope, 1), closed: make(chan struct{})}
	close(host.closed)
	hub.registerHost("host-hash", host)
	hub.registerDevice("device-hash", &secureRelayPeer{id: "device", send: make(chan secureRelayEnvelope, 1)})
	hub.registerRoute(secureRelayRoute{
		routeID:      "route",
		hostIDHash:   "host-hash",
		deviceIDHash: "device-hash",
		expiresAt:    time.Now().Add(time.Minute).UnixMilli(),
	})
	if result := hub.forward(false, "device-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}); result.Delivered || result.Code != "TARGET_CLOSED" {
		t.Fatal("expected forward to closed host peer to fail")
	}
}

func TestSecureRelayHubBackpressureReturnsFailureReason(t *testing.T) {
	hub := newSecureConnectionRelayHub()
	host := &secureRelayPeer{id: "host", send: make(chan secureRelayEnvelope, 1), closed: make(chan struct{})}
	host.send <- secureRelayEnvelope{Type: "opaque_frame", RouteID: "existing"}
	hub.registerHost("host-hash", host)
	hub.registerDevice("device-hash", &secureRelayPeer{id: "device", send: make(chan secureRelayEnvelope, 1), closed: make(chan struct{})})
	hub.registerRoute(secureRelayRoute{
		routeID:      "route",
		hostIDHash:   "host-hash",
		deviceIDHash: "device-hash",
		expiresAt:    time.Now().Add(time.Minute).UnixMilli(),
	})
	result := hub.forward(false, "device-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route", CorrelationID: "corr-1"})
	if result.Delivered {
		t.Fatal("expected backpressure to prevent delivery")
	}
	if result.Code != "TARGET_BACKPRESSURE" {
		t.Fatalf("expected TARGET_BACKPRESSURE, got %q", result.Code)
	}
}
