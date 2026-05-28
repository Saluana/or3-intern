package main

import (
	"context"
	"net/http"

	"or3-intern/internal/approval"
)

func servicePublicAuthIdentity() serviceAuthIdentity {
	return serviceAuthIdentity{Kind: "public", Actor: "anonymous"}
}

func serviceRequestWithAuthIdentity(r *http.Request, identity serviceAuthIdentity) *http.Request {
	return r.WithContext(serviceContextWithAuthIdentity(r.Context(), identity))
}

func serviceContextWithAuthIdentity(ctx context.Context, identity serviceAuthIdentity) context.Context {
	ctx = context.WithValue(ctx, serviceAuthContextKey{}, identity)
	ctx = context.WithValue(ctx, serviceAuthKindContextKey{}, identity.Kind)
	ctx = approval.ContextWithAuditAuthKind(ctx, identity.Kind)
	ctx = approval.ContextWithAuditActor(ctx, identity.Actor)
	return ctx
}

func serviceUnauthenticatedPairingRequest(r *http.Request) *http.Request {
	return serviceRequestWithAuthIdentity(r, serviceAuthIdentity{
		Kind:  "unauthenticated",
		Actor: "anonymous",
	})
}
