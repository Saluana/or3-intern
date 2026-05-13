# Auth Requirements

Different endpoints need different auth. Here is the breakdown.

## No Auth

- `GET /health` — health check
- `GET /ready` — readiness check

These endpoints are open because monitoring tools need to check them without credentials.

## Service Secret

Most endpoints need the service secret. The client sends it in a header:

```
Authorization: Bearer <service_secret>
```

The service secret is set in the config file. It acts like a master password for the API.

## Session Token

Some endpoints use session tokens. These are tied to a specific device or session. Tokens expire after a set time. They are created when a device authenticates.

## Passkey (WebAuthn)

Passkey auth uses WebAuthn. The device registers a passkey. Future requests use the passkey to authenticate. This is more secure than a shared secret.

## Pairing

Device pairing creates a trust relationship. One device approves another. Paired devices share access. Pairing requests expire if not approved in time.

## Auth Summary

| Endpoint Group | Auth Type |
|---|---|
| Health/Ready | None |
| Turns | Service secret or session token |
| Jobs | Service secret or session token |
| Approvals | Session token or passkey |
| Configure | Service secret |
| Files | Service secret or session token |
