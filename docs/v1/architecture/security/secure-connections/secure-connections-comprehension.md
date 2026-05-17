# Secure Connections Comprehension Results

## First-Glance Checks

The mainstream pairing flow uses three labels:

- Connect with QR
- Waiting for desktop approval
- Connected

The UI avoids protocol names, command-line instructions, Noise, CBOR, signatures, and relay details in the primary path. Manual fallback text is available when camera scanning is unavailable.

## Recovery Checks

Recovery copy uses direct user actions:

- This code expired.
- The computer rejected this request.
- Device revoked.
- Reconnect from the desktop.

The host identity recovery state blocks remote sessions and tells the user to recover locally rather than asking them to trust a changed key remotely.
