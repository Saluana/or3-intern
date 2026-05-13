# Working Directory and Environment

Runner processes run with a controlled working directory and a sanitized environment. These are defined in `internal/agentcli/cwd.go` and `internal/agentcli/env.go`.

## Working Directory Resolution

`resolveAgentCLICwd` (`internal/agentcli/cwd.go:19-49`) follows these rules:

1. **Empty requested, restricted** → use the `RestrictDir` as default
2. **Empty requested, unrestricted** → use the service process's working directory
3. **Relative requested** → resolve against `RestrictDir` (or os.Getwd if unrestricted)
4. **Absolute requested, restricted** → validate it is inside `RestrictDir`
5. **Absolute requested, unrestricted** → use as-is

### Validation

`validateCwdWithinRoot` (`internal/agentcli/cwd.go:51-69`) ensures the working directory is within the allowed root:

1. Resolve to absolute path
2. Canonicalize via `tools.CanonicalizePath`
3. Canonicalize the root via `tools.CanonicalizeRoot`
4. Compute relative path — must not be `".."` or start with `"../"`

## Environment Building

`BuildAgentCLIEnv` (`internal/agentcli/env.go:15-60`) builds the child process environment:

### Always Set
- `NO_COLOR=1` — disable ANSI color codes in runner output
- `TERM=dumb` — prevent terminal control sequences

### Allowlist Filtering

Only environment variables in the configured `ChildEnvAllowlist` are passed through. The allowlist uses `tools.BuildChildEnv` which handles standard patterns like `PATH`, `HOME`, `USER`, etc.

### PATH Extension

When `PATH` is in the allowlist, additional directories are appended:
- `~/.opencode/bin`
- `~/.bun/bin`
- `~/.npm-global/bin`
- `~/.local/bin`
- `~/go/bin`
- `~/.cargo/bin`
- `/opt/homebrew/bin`
- `/opt/homebrew/sbin`
- `/usr/local/bin`

Only directories that exist on the system are included.

### Secret Blocking

These environment variables are always stripped, even if the allowlist is broad:

- `OR3_INTERNAL_TOKEN`
- `OR3_PAIRING_SECRET`
- `OR3_NODE_SECRET`
- `OR3_SERVICE_SECRET`
- `OR3_API_KEY`
- `OPENAI_API_KEY`

## Binary Resolution

`ResolveExecutable` (`internal/agentcli/executable.go:14-42`) resolves the runner binary against the child process environment's PATH, not the service process PATH.

On Windows, it tries each PATHEXT extension (`.COM`, `.EXE`, `.BAT`, `.CMD`) in addition to the bare filename.

The resolved path must point to a regular file with the execute permission bit set (or on Windows, a file with a recognized executable extension).
