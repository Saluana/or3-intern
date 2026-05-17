# Secure Connections Legacy Deprecation

Six-digit bearer-token pairing remains a local compatibility path only. It must not authorize relay-mediated remote computer control.

## Timeline

1. Capability discovery advertises secure-connections v2 and remote legacy pairing disabled.
2. Existing paired devices are prompted to upgrade to device identity plus host-signed enrollment.
3. Bearer token authority is restricted to local compatibility.
4. Remote control requires secure session claims backed by enrolled device keys.
5. Legacy pairing is removed after migration metrics show safe adoption.

## Rollback

Rollback may re-enable local legacy pairing for recovery, but must not restore relay-mediated bearer-token control.
