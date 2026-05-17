# Secure Connections Observability

Telemetry is privacy preserving by default. Events may include hashed host/device identifiers, route IDs, result classes, latency, disconnect reason, and revocation state. Events must not include commands, terminal output, tool arguments, file contents, frame bodies, private keys, session keys, pairing secrets, bearer tokens, or enrollment certificates.

## Events

- `secure_connection.pairing_started`
- `secure_connection.pairing_finished`
- `secure_connection.session_started`
- `secure_connection.action_authorized`
- `secure_connection.session_ended`
- `secure_connection.device_revoked`
- `secure_connection.relay_throttled`

## Dashboard Panels

- Relay health and route creation failures.
- QR expiration and rejection rates.
- Rendezvous join-limit events.
- Secure session establishment and reconnect latency.
- Revocation counts and authorization denials.
- Step-up required and completed counts.

## Customer Transparency

The app should show active session state, trusted devices, recent remote activity, revoked devices, and relay connectivity in plain language.
