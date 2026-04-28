# or3-intern Security Code Review

**Date:** 2026-04-28
**Reviewer:** Susan Q
**Scope:** `or3-intern` Go backend (218 source files, 29 internal packages)
**Method:** Static analysis of auth, service API, tool execution, file access, sandbox, secret storage, and approval components

---

## Executive Summary

`or3-intern` is a well-architected Go runtime with strong security foundations, layered access control, and thoughtful defensive design. Expected vulnerabilities (SQL injection, unauthenticated data exposure, weak password storage) are either absent or properly mitigated.

However, four HIGH-severity issues and five MEDIUM issues weaken the security posture. The most serious is a path traversal in the file upload handler that allows authenticated users to create files outside the target directory.

**Severity summary:**
- CRITICAL: 0
- HIGH: 4
- MEDIUM: 5
- LOW: 3

---

## Critical (0)

No critical vulnerabilities were identified.

---

## HIGH

### H1: File Upload Path Traversal via `..` Filename

**Location:** `cmd/or3-intern/service.go` (`handleFileUpload`)

**Finding:** The upload handler validates the filename:
```go
name := filepath.Base(header.Filename)
if name == "." || name == string(filepath.Separator) || name == "" {
    // rejected
}
```

It does NOT check for `".."`. `filepath.Base("..")` returns `".."`, which passes the check.

**Impact:** An authenticated attacker could upload with `Filename: ".."`, causing `filepath.Join(dirPath, "..")` to target the parent directory. `os.O_CREATE | os.O_EXCL` prevents overwriting, but new files can be created in the parent directory if they don't already exist. An attacker could create files at the root level.

**Fix:** Add `".."` to the rejection check:
```go
if name == "." || name == ".." || name == string(filepath.Separator) || name == "" {
```

---

### H2: Symlink TOCTOU in File Tools

**Location:** `internal/tools/files.go` (`canonicalizePath`, `safePath`)

**Finding:** File tools resolve symlinks before checking the root boundary, then perform the actual file operation afterward. Between resolution and operation, a symlink could be swapped to point outside the root (Time-of-Check-Time-of-Use race).

**Impact:** Limited (requires local filesystem access or precise timing), but represents a genuine bypass of the root containment.

**Fix:** Stat-after-open pattern or use `openat` with `AT_SYMLINK_NOFOLLOW` where available. Alternatively, verify the opened file's resolved path after opening via `/proc/self/fd`.

---

### H3: Terminal Session ID Predictability

**Location:** `cmd/or3-intern/service.go` (`allocateTerminalSessionID`)

**Finding:** Session IDs are deterministic:
```go
return fmt.Sprintf("term_%d_%d", time.Now().UTC().Unix(), s.terminalSeq)
```

**Impact:** If auth is bypassed or stolen, terminal sessions become easy to enumerate and hijack.

**Fix:** Use cryptographically random IDs:
```go
b := make([]byte, 12)
crypto/rand.Read(b)
return "term_" + hex.EncodeToString(b)
```

---

### H4: No Rate Limiting on Authentication Failures

**Location:** `cmd/or3-intern/service_auth.go`, `cmd/or3-intern/service.go`

**Finding:** No rate limiting observed in the auth middleware. The server struct has `rateMu`/`rateWindow`/`rateCounts` fields (suggesting partial implementation), but they are not enforced during auth validation.

**Impact:** Brute-force risk for bearer tokens, session tokens, and pairing codes.

**Fix:** Implement per-IP and per-account rate limiting with exponential backoff on auth endpoints.

---

## MEDIUM

### M1: SHA1 Used for Skill Fingerprinting

**Location:** `internal/skills/skills.go:1014`

**Finding:** `crypto/sha1` is used for skill content hashing. SHA1 is cryptographically broken.

**Impact:** Low for this use case, but broken algorithms should not be used for integrity verification.

**Fix:** Replace `sha1.Sum` with `sha256.Sum256`. SHA-256 is already imported in the `security` package.

---

### M2: Sandbox Lacks Resource Limits

**Location:** `internal/tools/sandbox.go` (`commandWithSandbox`)

**Finding:** Bubblewrap sets up namespace isolation but does not enforce cgroup or rlimit resource controls.

**Impact:** Malicious commands could exhaust host resources (fork bombs, memory exhaustion).

**Fix:** Add `prlimit` or `cgexec` integration, or document that external resource limits are required.

---

### M3: Exec Blocked Patterns Are Easily Bypassed

**Location:** `internal/tools/exec.go` (`defaultBlockedPatterns`)

**Finding:** Pattern matching uses simple `strings.Contains`. These commands bypass every block:
```bash
python3 -c 'import shutil; shutil.rmtree("/")'
find . -delete
yes | xargs rm -rf
```

**Impact:** Minimal since the blocklist is defense-in-depth behind the approval system. But it gives a false sense of safety.

**Fix:** Document clearly that the blocklist is a safety net. Consider unifying exec controls under the approval broker completely rather than maintaining a weak blocklist.

---

### M4: Large File Scanner Buffer (1MB per line)

**Location:** `internal/tools/files.go` (all read functions)

**Finding:** `sc.Buffer(..., 1024*1024)` allows lines up to 1MB. Combined with bounded file reading, this is manageable but suboptimal.

**Fix:** Lower max token to 256KB. Most files won't have single lines that long.

---

### M5: EditFile Permission Handling Gap

**Location:** `internal/tools/files.go` (`EditFile.Execute`)

**Finding:** `EditFile` preserves file permissions using `existingFileMode(p, 0)`, which returns `0o600` if the file doesn't exist or `info.Mode().Perm()` if it does. However, `os.WriteFile` applies umask after the mode. Under restrictive umask (e.g., 077), execute or read bits can be silently stripped. The failing test `TestEditFile_PreservesExistingMode` already confirms this.

**Impact:** File permissions may be silently changed, potentially breaking executable scripts or making files unreadable in multi-user contexts.

**Fix:** Add `os.Chmod` after `os.WriteFile` to restore the original mode bits explicitly.

---

## LOW

### L1: Terminal Session Cleanup Is Lazy

**Location:** `cmd/or3-intern/service.go`

**Finding:** `cleanupTerminalSessions` runs only when terminal endpoints are hit. Expired sessions could remain in memory indefinitely if the terminal feature is unused.

**Fix:** Run cleanup on a background timer or register `time.AfterFunc` per session.

---

### L2: CORS Max-Age Is Long

**Location:** `cmd/or3-intern/service_auth.go`

**Finding:** `Access-Control-Max-Age: 600` seconds. Since origins are already strictly limited to loopback from loopback remotes, this is low impact.

**Fix:** Reduce to 60 seconds for a local-only service.

---

### L3: Secret Key Raw Byte Fallback

**Location:** `internal/security/store.go` (`LoadExistingKey`)

**Finding:** Falls back to raw key bytes if base64 decode returns an error: `return raw[:32], nil`. If a key file is partially corrupted, it silently uses the corrupted bytes.

**Fix:** Validate proper key format (base64-encoded, exact length) and return an error for non-base64 data. Or at minimum, verify decoded length = 32.

---

## Positive Security Observations

These are practices the codebase does well and should be preserved:

1. **All SQL is parameterized.** Every query in `internal/db/` uses `?` placeholders. No SQL injection.

2. **Service secrets use HMAC with time bounds.** Shared-secret tokens expire in 5 minutes and include a random nonce.

3. **Bearers tokens verified with constant-time comparison.** `hmac.Equal` used throughout.

4. **Role-based access control.** Clear role hierarchy (viewer, operator, admin, service-client) with route-level sensitivity requirements.

5. **Path traversal properly mitigated.** `filepath.Abs` + `EvalSymlinks` + root boundary check via `filepath.Rel`. Only the TOCTOU race remains unaddressed.

6. **File uploads use `O_EXCL`.** Cannot overwrite existing files via upload.

7. **Secret storage uses AES-GCM.** Proper authenticated encryption with random nonces per entry.

8. **Audit chain is tamper-evident.** Records chained with HMAC-SHA256; `Strict` mode fails closed if audit is unavailable.

9. **CORS is strictly scoped.** Browser origins are validated as loopback, from loopback remotes, via `Origin` header check.

10. **Service fails closed.** Startup validation requires `cfg.Service.Secret`; blank secrets are rejected.

11. **Approval system with policy modes.** `auto`/`ask`/`block` modes for exec and skill execution. Subject hashing to detect changes.

12. **Step-up auth for sensitive routes.** Device revocation, config changes, terminal access, and file writes require recent passkey verification.

13. **Exec tool uses flexible sandboxing.** Bubblewrap provides namespace isolation (PID, network, filesystem mounts). Works as intended.

14. **RestrictDir enforced for working directories.** The exec tool validates `cwd` is inside the allowed directory tree.

15. **AllowedPrograms whitelist.** Exec tool can be configured to only run a specific set of approved executables.

---

## Recommendations (Priority Order)

1. **Fix H1 immediately** (file upload `..` check) -- one-line fix, high exploitability
2. **Fix H3** (random terminal IDs) -- security-critical if auth ever leaks
3. **Implement rate limiting** (H4) -- important for brute-force protection
4. **Address TOCTOU** (H2) -- harder to exploit but architecturally important
5. **Replace SHA1** (M1) -- trivial fix, eliminate broken algorithm
6. **Fix EditFile permissions** (M5) -- existing test already failing
7. **Add resource limits** (M2) -- long-term hardening goal
