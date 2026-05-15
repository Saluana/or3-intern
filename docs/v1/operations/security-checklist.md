# Security checklist

Keep your OR3 Intern agent safe with these steps.

## Must do

- [ ] Change the default service secret
- [ ] Enable approval mode for sensitive tools
- [ ] Set up device pairing for service API access
- [ ] Use a strong API key for your AI provider
- [ ] Keep API keys out of version control

## Should do

- [ ] Use encrypted secret storage instead of plain env vars
- [ ] Enable audit logging
- [ ] Review network policies (who can reach the service port)
- [ ] Set up regular backups
- [ ] Run behind a reverse proxy with TLS

## Nice to do

- [ ] Run `or3-intern doctor --strict` to check your setup
- [ ] Use a VPN for remote access (Tailscale, WireGuard)
- [ ] Set up monitoring and alerts
- [ ] Restrict tool access with allow/block lists

## Check your setup

```bash
or3-intern doctor --strict
```

This runs a thorough security check and reports any problems.
