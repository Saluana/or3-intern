# Production service mode

Running OR3 Intern in production requires some extra steps.

## Process manager

Use a process manager to keep the service running:

- **systemd** — for Linux servers
- **supervisord** — works on any platform
- **Docker** — with restart policies

## Reverse proxy

Put OR3 Intern behind a reverse proxy if needed:

- **nginx** — add TLS, rate limiting, and routing
- **Caddy** — auto HTTPS, simple config

## Authentication

- Use passkeys for device pairing
- Set a strong service secret
- Enable approval mode for sensitive tools

## Audit logging

Turn on audit logging to track all tool calls and approval decisions. This helps with security reviews and debugging.

## Docker in production

Use Docker Compose with proper secrets. Don't use the default service secret. Set secrets in a `.env` file or a secrets manager.

## Secure remote access

Consider Tailscale or a similar VPN for secure remote access. This avoids exposing the service port to the internet.
