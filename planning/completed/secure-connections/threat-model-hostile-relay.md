# Hostile Relay Threat Model

## Security Claim

Compromising OR3 cloud infrastructure is not enough to control a customer's computer.

## Primary Attackers

- Relay database reader
- Relay frame injector/replayer
- Relay route swapper
- Cloud account takeover
- Stolen device storage attacker
- Malicious browser origin
- Local network observer
- Host malware, treated as partially out of scope

## Required Failures for Computer Control

An attacker must satisfy host-local trust, endpoint crypto, role/capability policy, and approval/step-up policy. Relay routing or account access alone is not a control grant.

## Relay Compromise

Allowed attacker outcomes:

- deny service
- delay frames
- observe limited timing/routing metadata

Blocked outcomes:

- derive pairing secret from database
- decrypt command/result frames
- create host-signed enrollments
- approve sensitive actions
- bypass host trust store

## Account Takeover

Cloud account access may request routing and recovery surfaces, but cannot enroll a new control device without the desktop QR secret and local host approval.

## Stolen Device Storage

Native builds must use platform secure storage where available. Browser or plugin-missing storage is lower assurance and receives shorter sessions and stricter step-up in later phases.

## Malicious Browser Origin

Browser-origin enrollment is lower trust. Passkey origin checks and host approval are separate controls. Iframe or untrusted-origin enrollment should remain blocked when web enrollment UI is added.

## Host Identity Changes

Endpoints never auto-accept host signing/noise key changes. Mismatch creates `HOST_IDENTITY_CHANGED` and requires local desktop recovery or re-pairing.
