# Codebase Audit — Internals + HTTP REST Service

Date: 2026-04-24
Scope: `internal/**`, `cmd/or3-intern/**` (with focus on service/control plane path)

## Method
- Ran package-level test sweep for internals + CLI service surfaces.
- Ran `go vet` across the same scope.
- Reviewed critical paths for network safety, scheduling reliability, and service request handling.

---

## Findings (sorted by impact)

## 1) [HIGH][Security] Host allowlist can be bypassed by using literal IPs
**Area:** outbound network policy enforcement (`web_fetch`, shared host policy)

### Evidence
- Failing test reproduces expected denial but currently fails:
  - `go test ./internal/tools -count=1`
  - `--- FAIL: TestWebFetch_HostPolicyDeniesUnknownHost ... expected host policy denial`
- Hostname default-deny check is skipped for literal IPs in policy resolution logic (`resolveHostWithPolicies`), so `DefaultDeny + AllowedHosts` can be bypassed with direct IP URLs.

### Why this matters
This weakens the intended network boundary model. If policy is configured to only allow explicit hostnames, direct IP access may still succeed, which is a policy bypass risk (especially in environments relying on outbound restrictions).

### Shortest clean fix
In `internal/security/network.go`, enforce default-deny for literal IP targets unless explicitly allowed by dedicated policy knobs (or explicit IP allowlist support).

**Practical patch direction (minimal):**
- In `resolveHostWithPolicies`, apply an explicit `DefaultDeny` guard for parsed IP literals too (not just hostnames).
- Keep existing private/loopback checks as-is.

---

## 2) [HIGH][Functional] Interval cron jobs (`KindEvery`) are not scheduled
**Area:** scheduler/service runtime behavior

### Evidence
- During tests, cron scheduler logs repeated parse failures:
  - `parser does not accept descriptors: @every ...`
- `internal/cron/cron.go` initializes parser without descriptor support, then later schedules interval jobs using `@every` syntax.

### Why this matters
Any persisted or runtime-created `KindEvery` jobs silently fail to schedule. This is user-visible non-functional behavior for recurring automation.

### Shortest clean fix
Enable descriptor parsing in cron parser setup.

**Practical patch direction (minimal):**
- Update parser flags in `Start()` to include descriptor support (or switch to parser config that supports `@every`), then keep existing `@every` generation logic.

---

## 3) [MEDIUM][DX/Quality] `internal/tools` test suite is red on default run
**Area:** CI/developer experience

### Evidence
- Full targeted sweep fails solely because of `internal/tools`:
  - `go test ./internal/... ./cmd/or3-intern/...` fails due to `TestWebFetch_HostPolicyDeniesUnknownHost`.

### Why this matters
A red package in regular test workflow slows merges, hides regressions, and reduces trust in release gates.

### Shortest clean fix
Fix Findings #1 and #2 (root causes), then keep this package as a mandatory check in CI if not already required.

---

## 4) [LOW][Perf/Cleanliness] HTTP handlers re-create controlplane wrapper per request
**Area:** REST service request path

### Evidence
- `cmd/or3-intern/service.go` uses `cp := s.control()` in handlers, and `control()` allocates a new `controlplane.Service` wrapper each time.

### Why this matters
Overhead is small, but unnecessary allocations occur on hot request paths; this is avoidable with a single cached instance since wrapper construction is deterministic from existing pointers/config.

### Shortest clean fix
Cache a `*controlplane.Service` on `serviceServer` (initialized once) and reuse it in handlers.

---

## What already looks good
- Broad coverage exists in tests across internals and service-contract paths.
- `go vet` is clean in audited scope.
- Request body parsing in service endpoints uses strict decoding (`DisallowUnknownFields`) and bounded body limits, which helps reduce malformed-request ambiguity and abuse surface.

---

## Commands run
- `go test ./internal/... ./cmd/or3-intern/...`
- `go test ./internal/tools -count=1`
- `go vet ./internal/... ./cmd/or3-intern/...`
- `rg -n "TODO|FIXME|panic\(|t\.Skip|not implemented|stub|XXX|HACK" internal cmd/or3-intern`

