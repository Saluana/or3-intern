# Secure Connections Runbooks

## Relay Outage

1. Confirm local desktop control still works over the local service API.
2. Pause new relay route creation.
3. Expire stale relay rendezvous records.
4. Keep enrolled device trust unchanged; the relay is not trust authority.
5. Publish an incident update that names affected remote connectivity only.

## Suspicious Device

1. Revoke the device from the secure-connections device list.
2. Verify active sessions for that device are revoked.
3. Rotate bearer compatibility tokens if the device also used legacy pairing.
4. Review audit events for `secure_connection.action_authorized`.

## Host Identity Change

1. Treat unexpected host identity replacement as recovery-required.
2. Block remote secure sessions until the operator confirms recovery locally.
3. Ask all devices to re-pair after emergency rotation.
4. Preserve old audit records and old host fingerprints for investigation.

## Pairing Abuse

1. Expire visible QR codes and relay rendezvous records.
2. Check join counts and source rate-limit activity.
3. Keep QR secrets out of logs and support artifacts.
4. Prefer re-pairing over manually editing trust records.
