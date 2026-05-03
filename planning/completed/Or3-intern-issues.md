# Dumb Issues - or3-intern Review

## 1) This "sandbox" is basically a tourist brochure for the host filesystem

**Where:** `internal/tools/sandbox.go:18-50`

**Code snippet:**
```go
args := []string{"--die-with-parent", "--new-session", "--proc", "/proc", "--dev", "/dev", "--ro-bind", "/", "/"}
if !cfg.AllowNetwork {
    args = append(args, "--unshare-net")
}
for _, path := range cfg.WritablePaths {
    ...
    args = append(args, "--bind", clean, clean)
}
if strings.TrimSpace(cwd) != "" {
    cleanCWD := filepath.Clean(cwd)
    args = append(args, "--bind", cleanCWD, cleanCWD, "--chdir", cleanCWD)
}
```

**Why this is bad:**
This is not meaningful filesystem isolation. You are literally mounting the entire host root into the sandbox with `--ro-bind / /`, then selectively re-binding writable paths on top. That means any allowed binary can still read basically the whole machine: config files, source trees, SSH config, cloud creds in badly-permissioned homes, service metadata files, whatever is readable by the parent user. Calling this "sandboxing" is marketing, not engineering.

It also skips the other isolation primitives that make Bubblewrap useful in the first place: no private tmpfs root, no explicit minimal bind set, no user namespace setup here, no pid isolation, no home sanitization. You kept the scary part and threw away the point.

**Real-world consequences if left unfixed:**
A tool execution path that is supposed to be constrained can still exfiltrate host-readable data. In a hosted or multi-user setup, this becomes a cross-tenant data exposure machine wearing a fake mustache.

**Concrete fix:**
Start from an empty filesystem view, not the host root. Bind only what is needed.

```go
args := []string{
    "--die-with-parent",
    "--new-session",
    "--unshare-net",
    "--proc", "/proc",
    "--dev", "/dev",
    "--tmpfs", "/",
    "--ro-bind", "/usr", "/usr",
    "--ro-bind", "/bin", "/bin",
    "--ro-bind", "/lib", "/lib",
    "--ro-bind", "/lib64", "/lib64",
    "--dir", "/tmp",
}

for _, path := range cfg.WritablePaths {
    clean := filepath.Clean(path)
    args = append(args, "--bind", clean, clean)
}
```

Also validate that writable paths stay under an approved root instead of trusting whatever config hands you.

## 2) Your exec allowlist can be bypassed by anyone who knows what `filepath.Base` does

**Where:** `internal/tools/exec.go:110-112` and `internal/tools/exec.go:161-176`

**Code snippet:**
```go
if program != "" && len(t.AllowedPrograms) > 0 && !allowedProgram(program, t.AllowedPrograms) {
    return "", fmt.Errorf("program not allowed: %s", program)
}
...
func allowedProgram(program string, allowed []string) bool {
    program = strings.TrimSpace(program)
    if program == "" {
        return false
    }
    base := filepath.Base(program)
    for _, candidate := range allowed {
        ...
        if candidate == program || candidate == base {
            return true
        }
    }
    return false
}
```

**Why this is bad:**
This blesses any path whose basename matches an allowed program. If the allowlist contains `git`, then `/tmp/evil/git`, `/home/user/bin/git`, and `./definitely-not-git` all pass. Then `exec.CommandContext` runs that exact path. Congratulations, your allowlist is actually a basename cosplay contest.

**Real-world consequences if left unfixed:**
Any caller who can place or reference a binary in a writable location can run arbitrary code while pretending to be an allowed program. That is a straight bypass of the main control you are relying on to keep exec sane.

**Concrete fix:**
Resolve the executable with `exec.LookPath`, canonicalize it, and compare against an allowlist of absolute approved paths or exact bare names resolved from trusted PATH directories.

```go
resolved, err := exec.LookPath(program)
if err != nil {
    return "", err
}
resolved, err = filepath.EvalSymlinks(resolved)
if err != nil {
    return "", err
}
if !allowedResolvedPath(resolved, t.AllowedPrograms) {
    return "", fmt.Errorf("program not allowed: %s", resolved)
}
c = exec.CommandContext(cctx, resolved, stringArgs(params["args"])...)
```

At minimum, reject any program containing a path separator unless the full canonical path is explicitly allowlisted.

## 3) Non-zero exit is being reported as success because apparently lying to yourself is a feature now

**Where:** `internal/tools/exec.go:139-158`

**Code snippet:**
```go
err = c.Run()
...
if err != nil {
    return fmt.Sprintf("exit error: %v\n\nstdout:\n%s\n\nstderr:\n%s", err, out, er), nil
}
```

**Why this is bad:**
This returns a formatted failure string with a `nil` error. So every caller that uses the error channel for control flow gets fake success. That is not just sloppy, it is actively misleading.

One example of the fallout: structured autonomous execution increments the success counter when `err == nil`. So failed commands get counted as successful jobs. That makes telemetry wrong, summaries wrong, and tool orchestration logic dumb in exactly the way you deserve if you do this.

**Real-world consequences if left unfixed:**
Failed commands look successful to the runtime. Retries may not happen. Safety or audit logic keyed off actual tool failure will misfire. Operators get garbage signals and agents make decisions on bad state.

**Concrete fix:**
Return a real error on command failure.

```go
if err != nil {
    return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", out, er), fmt.Errorf("exec failed: %w", err)
}
```

If you want the output preserved, return it alongside the error. Do not smuggle failure through the success path like a goblin.

## 4) The network policy has a DNS rebinding hole because validation and dialing are divorced

**Where:** `internal/security/network.go:55-82` and `internal/security/network.go:124-149`

**Code snippet:**
```go
func (p HostPolicy) ValidateHost(ctx context.Context, hostname string) error {
    ...
    addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
    ...
    for _, addr := range addrs {
        ...
        if err := p.validateAddr(ip.Unmap()); err != nil {
            return err
        }
    }
    return nil
}
...
cloned.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
    if err := policy.ValidateURL(req.Context(), req.URL); err != nil {
        return nil, err
    }
    return base.RoundTrip(req)
})
```

**Why this is bad:**
You validate the hostname by resolving it once, then let the underlying transport resolve it again later during the actual connection. That is classic time-of-check/time-of-use garbage. If DNS changes between validation and connect, or the resolver returns different answers on subsequent lookups, the request can still land on a blocked address.

So the code gets to feel very secure while still being vulnerable to rebinding. Amazing. A security boundary made of vibes.

**Real-world consequences if left unfixed:**
SSRF protections can be bypassed against internal services, loopback, or metadata endpoints if an attacker controls DNS or can influence resolution timing.

**Concrete fix:**
Pin the validated IP into the actual dial path. Wrap the transport's `DialContext` so the connection only goes to the approved address set, or resolve once and replace the request host with the validated IP while preserving `Host` / TLS SNI carefully.

```go
transport := &http.Transport{
    DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
        host, port, _ := net.SplitHostPort(addr)
        ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
        if err != nil {
            return nil, err
        }
        approved := pickApprovedIP(ips)
        return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(approved.String(), port))
    },
}
```

Security checks that do not bind the actual socket destination are decorative.

## 5) `read_file` ignores `maxBytes` until after it has already inhaled the whole file

**Where:** `internal/tools/files.go:86-94`

**Code snippet:**
```go
max := defaultReadFileMaxBytes
if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 { max = int(v) }
b, err := os.ReadFile(p)
if err != nil { return "", err }
if len(b) > max { b = b[:max] }
return string(b), nil
```

**Why this is bad:**
The byte limit is fake. `os.ReadFile` reads the entire file into memory first, and only then do you slice it. So a caller asking for 4 KB from a 2 GB file still gets the process to attempt reading 2 GB. That is not a limit. That is a lie with extra allocations.

**Real-world consequences if left unfixed:**
Memory spikes, GC churn, potential OOM, and easy denial-of-service against any environment where the tool can touch large files.

**Concrete fix:**
Use a bounded reader.

```go
f, err := os.Open(p)
if err != nil {
    return "", err
}
defer f.Close()
body, err := io.ReadAll(io.LimitReader(f, int64(max)))
if err != nil {
    return "", err
}
return string(body), nil
```

Do the obvious thing instead of pretending slicing after the damage counts as resource control.

## 6) The email parser defeats its own body size limit for base64 messages

**Where:** `internal/channels/email/email.go:558-575`

**Code snippet:**
```go
func decodeTransferEncoding(body io.Reader, encoding string) io.Reader {
    switch strings.ToLower(strings.TrimSpace(encoding)) {
    case "base64":
        return base64.NewDecoder(base64.StdEncoding, strings.NewReader(readAllString(body)))
    case "quoted-printable":
        return quotedprintable.NewReader(body)
    default:
        return body
    }
}

func readAllString(reader io.Reader) string {
    data, err := io.ReadAll(reader)
    if err != nil {
        return ""
    }
    return string(data)
}
```

**Why this is bad:**
The surrounding code tries to limit body reads with `io.LimitReader`, then this function comes along and slurps the entire encoded body into memory before the limit applies. So the limit works for quoted-printable and plain text, but base64 gets a special VIP lane straight to memory abuse.

**Real-world consequences if left unfixed:**
A large base64 email can blow memory or at minimum trigger nasty allocation spikes. That is a denial-of-service vector in the channel parser.

**Concrete fix:**
Stream decode directly from the original reader.

```go
func decodeTransferEncoding(body io.Reader, encoding string) io.Reader {
    switch strings.ToLower(strings.TrimSpace(encoding)) {
    case "base64":
        return base64.NewDecoder(base64.StdEncoding, body)
    case "quoted-printable":
        return quotedprintable.NewReader(body)
    default:
        return body
    }
}
```

Then keep the `io.LimitReader` around the decoded stream. No eager `ReadAll`, no garbage fire.

## 7) The append-only audit chain is not actually safe under concurrency

**Where:** `internal/db/db.go:24-40` and `internal/db/security.go:84-103`

**Code snippet:**
```go
s.SetMaxOpenConns(4)
...
func (d *DB) AppendAuditEvent(ctx context.Context, input AuditEventInput, key []byte) error {
    ...
    row := d.SQL.QueryRowContext(ctx, `SELECT record_hash FROM audit_events ORDER BY id DESC LIMIT 1`)
    ...
    _, err = d.SQL.ExecContext(ctx,
        `INSERT INTO audit_events(event_type, session_key, actor, payload_json, prev_hash, record_hash, created_at) VALUES(?,?,?,?,?,?,?)`,
        ...)
    return err
}
```

**Why this is bad:**
This is a read-last-hash then insert-new-row sequence with no transaction, no table lock, and a pool allowing multiple concurrent connections. Two goroutines can read the same last hash and both append children of the same parent. Then `VerifyAuditChain` quite correctly screams that the chain is broken. You built a self-corrupting tamper log.

And no, saying "SQLite" in a serious voice does not fix race conditions you wrote yourself.

**Real-world consequences if left unfixed:**
Under concurrent audited operations, the audit chain can fork and fail verification even without any attacker. That turns the integrity mechanism into its own source of false alarms and operational pain.

**Concrete fix:**
Serialize append operations inside a transaction that takes the write lock before reading the previous hash.

```go
func (d *DB) AppendAuditEvent(ctx context.Context, input AuditEventInput, key []byte) error {
    tx, err := d.SQL.BeginTx(ctx, &sql.TxOptions{})
    if err != nil {
        return err
    }
    defer tx.Rollback()

    var prevHash []byte
    row := tx.QueryRowContext(ctx, `SELECT record_hash FROM audit_events ORDER BY id DESC LIMIT 1`)
    if err := row.Scan(&prevHash); err != nil && err != sql.ErrNoRows {
        return err
    }

    ...
    if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(...) VALUES(...)`, ...); err != nil {
        return err
    }
    return tx.Commit()
}
```

Or set the primary DB to one writer connection if you want deterministic behavior instead of writing fan fiction about it.

## 8) The filesystem tools and artifact store create world-readable files like it is still 2004

**Where:** `internal/tools/files.go:109-117`, `internal/tools/files.go:142-161`, and `internal/artifacts/store.go:32-35`

**Code snippet:**
```go
if mkdirs {
    if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { return "", err }
}
if err := os.WriteFile(p, []byte(content), 0o644); err != nil { return "", err }
...
if err := os.WriteFile(p, []byte(s), 0o644); err != nil { return "", err }
...
_ = os.MkdirAll(s.Dir, 0o755)
if err := os.WriteFile(path, data, 0o644); err != nil {
    return "", err
}
```

**Why this is bad:**
These permissions are too broad for a system that handles prompts, memories, artifacts, and potentially credentials-adjacent content. Group/world-readable files and traversable directories are a terrible default in shared hosts, CI runners, cheap VPS setups, or any multi-user box.

You were careful enough to use `0600` for secret keys in one place, then immediately went back to spraying `0644` and `0755` everywhere else. Consistency clearly died tired.

**Real-world consequences if left unfixed:**
Local users or neighboring processes on the same machine can read generated files, chat artifacts, and modified workspace files that were never meant to be public. In hosted setups that is the kind of bug that turns into incident reports and legal sadness.

**Concrete fix:**
Default to owner-only permissions unless there is a strong reason not to.

```go
if mkdirs {
    if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil { return "", err }
}
if err := os.WriteFile(p, []byte(content), 0o600); err != nil { return "", err }
```

Apply the same treatment to artifact directories and files.

## 9) ClawHub install happily reads arbitrary-sized ZIPs into RAM and then does it again for each entry

**Where:** `internal/clawhub/client.go:227-249` and `internal/clawhub/client.go:346-385`

**Code snippet:**
```go
func (c *Client) Download(ctx context.Context, slug, version string) ([]byte, error) {
    ...
    return io.ReadAll(resp.Body)
}
...
func extractZipToDir(zipBytes []byte, target string) error {
    reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
    ...
    for _, file := range reader.File {
        ...
        rc, err := file.Open()
        ...
        data, readErr := io.ReadAll(rc)
        ...
        if err := os.WriteFile(full, data, mode); err != nil {
            return err
        }
    }
}
```

**Why this is bad:**
No download size limit. No max entry count. No max uncompressed bytes. No per-file size cap. No compression ratio cap. And every file is read fully into memory before being written. This is how you invite zip bombs and memory exhaustion into your supply chain path.

The only thing missing is a handwritten note saying "please DOS me." 

**Real-world consequences if left unfixed:**
A malicious or compromised registry response can exhaust memory, disk, or both during skill install/update. That is especially stupid because the whole point of a registry client is to consume untrusted remote content.

**Concrete fix:**
Stream the response to a temp file with a hard cap, then enforce archive limits during extraction.

```go
const maxZipBytes = 32 << 20
const maxFileBytes = 4 << 20
const maxTotalUncompressed = 64 << 20
const maxEntries = 512

lr := &io.LimitedReader{R: resp.Body, N: maxZipBytes + 1}
_, err := io.Copy(tmpFile, lr)
if lr.N <= 0 {
    return fmt.Errorf("zip too large")
}
```

Then reject archives with too many entries, too-large entries, or excessive total expanded size, and stream each entry to disk with `io.Copy` instead of `io.ReadAll`.

## 10) The README says "single-connection" while the code opens six. Pick one reality and stick to it

**Where:** `README.md:38` and `internal/db/db.go:24-40`

**Code snippet:**
```md
- Uses SQLite with WAL + single-connection for deterministic low-RAM operation.
```

```go
s.SetMaxOpenConns(4)
s.SetMaxIdleConns(4)
...
vec.SetMaxOpenConns(2)
vec.SetMaxIdleConns(2)
```

**Why this is bad:**
This is not a tiny wording issue. The docs claim deterministic single-connection behavior while the implementation explicitly allows four connections on one handle and two on the vector handle. That mismatch matters because concurrency assumptions drive correctness, performance, and safety. The broken audit chain logic above is exactly the kind of thing this lie helps hide.

**Real-world consequences if left unfixed:**
Operators and future maintainers will reason about the system incorrectly. They will trust invariants that do not exist, and then spend their evenings debugging bugs created by those fake invariants.

**Concrete fix:**
Either make the code match the claim:

```go
s.SetMaxOpenConns(1)
s.SetMaxIdleConns(1)
vec.SetMaxOpenConns(1)
vec.SetMaxIdleConns(1)
```

or stop claiming single-connection behavior in the README. Right now the documentation is just confidently wrong.

