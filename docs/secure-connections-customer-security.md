# Secure Connections Security

Secure Connections pairs a phone or browser to a specific desktop by scanning a short-lived QR code shown on that desktop. Cloud account sign-in can prove who you are, but it cannot silently make a computer trust a new device. The desktop must issue a host-signed enrollment certificate after local approval.

The relay carries routing metadata and encrypted frames only. It does not receive host private keys, device private keys, pairing secrets, command text, terminal output, file contents, or session keys.

Trusted devices can be revoked from the desktop. Revocation blocks new secure sessions and invalidates active sessions for that device. If the desktop host identity changes unexpectedly, remote sessions are blocked until local recovery or re-pairing.

Browser enrollments are intentionally lower trust than native app enrollments. They get shorter certificate lifetimes, fewer default capabilities, and more frequent step-up prompts.
