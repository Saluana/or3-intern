# Consumer-grade stability

Consumer-grade stability means normal setup paths either become ready-to-use or save a clearly labeled draft, startup refuses unsafe states with repair guidance, optional integrations degrade instead of crashing the service, and public errors avoid exposing secrets or internal paths.

The release gate for this bar is:

- `go test ./...`
- targeted race coverage for service, terminal, job, and manager paths
- `staticcheck ./...`
- `gosec ./...`
- a fuzz smoke test for config readiness evaluation

CI enforces these checks in `.github/workflows/consumer-grade-stability.yml`.
