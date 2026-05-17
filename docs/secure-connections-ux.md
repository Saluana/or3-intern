# Secure Connections UX

## Consumer Pairing Prototype

1. Desktop shows a QR code with a short expiry and refresh action.
2. Mobile scans the QR code and shows the desktop name and requested capability summary.
3. Desktop shows a local approval prompt with device name, platform, account, role, and capabilities.

## States

- Waiting: QR visible and still valid.
- Pending approval: scanned device is waiting for desktop approval.
- Approved: enrollment certificate stored.
- Rejected: desktop denied the request.
- Expired: QR or rendezvous timed out.

## Accessibility

- All actions must be reachable by keyboard.
- QR fallback text must be available without exposing secrets in logs.
- Status changes must be announced to assistive technology.
- Color cannot be the only signal for approved, rejected, or expired states.

## Copy Rules

Use "Connect this device", "Approve on your computer", "This code expired", and "Device revoked". Avoid protocol names, Noise, CBOR, signatures, relay internals, and setup commands in mainstream UI.
