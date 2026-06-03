# Auth Requirements

The service API is intended for trusted app and host-control clients. Most `/internal/v1/*` routes require an authenticated identity and a sufficient role.

## Public Discovery

`GET /internal/v1/auth/capabilities` is the public route that lets clients discover the current auth posture before prompting for pairing, passkeys, or session login.

Other routes should be treated as protected unless the route policy explicitly allows otherwise.

## Bearer Credentials

Service credentials are sent as:

```
Authorization: Bearer <token>
```

Accepted bearer identities include:

- **Shared-secret tokens** — signed service tokens derived from the configured service secret.
- **Paired-device tokens** — device tokens created by the pairing flow.
- **Legacy direct shared secret** — supported for compatible local clients when enabled by policy.

The auth layer detects missing, invalid, expired, unsupported, or replayed tokens and returns stable error codes such as `missing_token`, `invalid_token`, and `token_replay`.

## Auth Session Header

Passkey-backed app sessions use:

```
X-Or3-Session: <session_token>
```

`X-Auth-Session` and the `session_token` query parameter are also accepted aliases.

## Passkey (WebAuthn)

Passkey flows live under `/internal/v1/auth/passkeys/*`:

- registration begin/finish
- login begin/finish
- passkey list
- passkey rename
- passkey revoke

Step-up auth lives under `/internal/v1/auth/step-up/*` and is used when sensitive operations require a fresh passkey assertion.

## Pairing

Device pairing creates a trust relationship for a device, app, or channel identity. Pairing routes are under `/internal/v1/pairing/*`; paired device management is under `/internal/v1/devices/*`.

Pairing requests expire if not approved in time. Paired devices can be revoked or rotated.

## Role Gates

| Endpoint Group | Auth Type |
|---|---|
| `auth/capabilities` | Public |
| Turns, jobs, runner chat, chat sessions | Authenticated app/operator identity |
| MCP list, file reads, terminal reads | Authenticated operator session |
| Files, terminal, configure, MCP add/delete/test, skills, approvals, device management, service restart | Operator session with recent passkey step-up |
| Cron, audit, embeddings, scope | Operator role |

Route requirements are evaluated by `serviceRequestRouteRequirement` and enforced by `requireServiceRole` plus the auth policy layer.
