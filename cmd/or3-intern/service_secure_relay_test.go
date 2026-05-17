package main

import (
	"net/http/httptest"
	"testing"
	"time"
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
	if !hub.forward(false, "device-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}) {
		t.Fatal("expected device-to-host frame to forward")
	}
	select {
	case <-host.send:
	default:
		t.Fatal("expected host to receive forwarded frame")
	}
	if !hub.forward(true, "host-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}) {
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
	if hub.forward(false, "device-hash", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}) {
		t.Fatal("expected expired route not to forward")
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
	if hub.forward(false, "device-imposter", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}) {
		t.Fatal("expected device sender mismatch to be rejected")
	}
	if hub.forward(true, "host-imposter", secureRelayEnvelope{Type: "opaque_frame", RouteID: "route"}) {
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
