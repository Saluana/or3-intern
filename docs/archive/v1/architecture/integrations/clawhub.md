# ClawHub Client

ClawHub is a remote registry for managed skills. The client is in `internal/clawhub/client.go`.

## Client

```go
type Client struct {
    SiteURL     string
    RegistryURL string
    HTTP        *http.Client
}
```

Created with `New(siteURL, registryURL)`.

## API Endpoints

The client talks to four API endpoints:

| Endpoint | Purpose |
|----------|---------|
| `/api/v1/search` | Search for skills by name |
| `/api/v1/skills/<slug>` | Inspect skill metadata |
| `/api/v1/resolve` | Match a local fingerprint to a published version |
| `/api/v1/download` | Download a skill bundle as a zip file |

## Search

`Client.Search(ctx, query, limit)` returns matching skills with display name, summary, version, and relevance score.

## Inspect

`Client.Inspect(ctx, slug, version)` returns metadata for a specific skill: display name, summary, latest version, and owner. If no version is specified, the latest version is selected.

## Download

`Client.Download(ctx, slug, version)` fetches the skill bundle as a zip file. The download is capped at 32 MB (`maxDownloadZipBytes`).

## Install

`Client.Install(ctx, slug, version, destDir, opts)` performs a full install:

1. Inspects the skill to resolve the version
2. Downloads the zip bundle
3. Extracts to a temporary directory
4. Computes a content fingerprint (SHA-256 hash of all regular files, sorted by path)
5. Scans the extracted files for security issues
6. Writes `.clawhub/origin.json` with provenance metadata
7. Moves the extracted directory to the final destination (with backup/rollback support)

### Fingerprint

`FingerprintDir` (`internal/clawhub/client.go:449-496`) hashes all regular files in the skill directory (excluding `.clawhub` metadata). Each file's content is SHA-256 hashed, then a combined hash is computed from `path:hash\n` lines sorted by path. This fingerprint is used to detect local modifications.

### Security Scanning

`scanInstalledSkill` (`internal/clawhub/client.go:573-621`) checks installed files:

**Path checks**: Flags credential-related files (`.env`, `.netrc`, `.npmrc`, `.pypirc`, SSH keys, AWS credentials).

**Content checks**: Flags suspicious patterns in executable scripts:
- `curl ... | sh` or `wget ... | sh` — pipe-to-shell patterns
- `Invoke-WebRequest ... iex` — PowerShell download-and-execute
- `/dev/tcp/` or `nc -e` — reverse shell patterns
- `osascript` — system automation outside tool model

**Severity levels**:
- `high` → skill is blocked from installation entirely
- `medium` → skill is flagged as `quarantined`
- No findings → status is `clean`

## Local Modification Detection

`LocalEdits` (`internal/clawhub/client.go:499-509`) compares the current directory fingerprint against the recorded fingerprint in `origin.json`. If they differ, the skill has been modified locally.

## Origin Metadata

Each installed skill has `.clawhub/origin.json`:

```json
{
  "version": 2,
  "registry": "https://registry.clawhub.io",
  "slug": "my-skill",
  "owner": "publisher-handle",
  "installedVersion": "1.0.0",
  "installedAt": 1700000000000,
  "fingerprint": "abc123...",
  "scanStatus": "clean",
  "scanFindings": []
}
```

This metadata is read by the skills system to determine publisher, registry, version, and trust status.

## Version Resolution

`Client.Resolve(ctx, slug, fingerprint)` checks whether a locally installed fingerprint matches a known published version. This is used to detect when an installed skill is outdated or modified.

## Safety Limits

- Max download size: 32 MB
- Max archive entries: 512
- Max archive file size: 4 MB per file
- Max archive total extracted size: 64 MB
- HTTP timeout: 15 seconds
