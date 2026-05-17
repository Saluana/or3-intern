# Secure Connections Key Rotation

## Planned Rotation

1. Put the host into recovery-required mode.
2. Stop accepting remote secure sessions.
3. Generate a new host identity locally.
4. Show the new fingerprint on the desktop.
5. Require each device to re-pair and receive a new host-signed enrollment certificate.
6. Keep old audit records and fingerprints for investigation.

## Emergency Rotation

1. Revoke active sessions immediately.
2. Mark all enrolled devices stale.
3. Generate a new local host identity.
4. Require re-pairing from physical desktop access.
5. Publish a customer-facing recovery explanation.

Cloud account recovery can disable relay routing, but it cannot silently restore host-local trust. Re-pairing from the desktop is required after host identity replacement.
