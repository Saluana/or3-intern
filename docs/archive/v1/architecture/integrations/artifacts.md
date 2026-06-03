# Artifact Storage

Artifacts persist binary attachments (images, audio, video, and files) from conversations. The artifact system is in `internal/artifacts/store.go` and `internal/artifacts/attachment.go`.

## Store

```go
type Store struct {
    Dir string
    DB  *db.DB
}
```

- `Dir` — filesystem directory where artifact files are stored
- `DB` — database connection for tracking metadata

## Save

`Store.Save(ctx, sessionKey, mime, data)` writes binary data to disk and records it in the database:

1. Ensures the session exists in the sessions table
2. Creates the storage directory with mode `0700` if it does not exist
3. Generates a random 16-byte hex ID
4. Writes data to `<Dir>/<id>` with mode `0600`
5. Inserts a row into `artifacts` table (id, session_key, mime, path, size_bytes, created_at)
6. On insert failure, removes the file and returns the error

## SaveNamed

`Store.SaveNamed(ctx, sessionKey, filename, mimeType, data)` is like `Save` but also normalizes the filename and returns an `Attachment` record with file kind detection:

```go
type Attachment struct {
    ArtifactID string
    Filename   string
    Mime       string
    Kind       string
    SizeBytes  int64
}
```

## Lookup

`Store.Lookup(ctx, artifactID)` queries the database for a stored artifact. Returns `ErrNotFound` if the artifact ID does not exist.

## ReadCapped

`Store.ReadCapped(ctx, sessionKey, artifactID, maxBytes)` reads artifact content after checking access:

1. Looks up the artifact metadata
2. Verifies the requesting session can access the artifact's session
3. Resolves the stored path safely (must be within the store directory)
4. Opens and reads the file, capped at `maxBytes` (default 200000 bytes)
5. Returns a `ReadResult` with content, truncated flag, and read byte count

`ReadCappedFrom` adds an offset parameter for partial reads.

## Session Access Control

`sessionCanRead` (`internal/artifacts/store.go:148-165`) allows access when:

1. The requesting session key matches the artifact's session key, OR
2. The database resolves the requesting session's scope key to match the artifact's session key

This means artifacts can be shared between sessions that belong to the same scope (e.g., a chat session and its sub-sessions).

## Path Safety

`safeStoredPath` (`internal/artifacts/store.go:167-195`) prevents path traversal attacks:

1. Resolves the store directory to an absolute, canonical path
2. Resolves the stored path to absolute (following symlinks)
3. Computes the relative path — must not be `".."` or start with `"../"`

## Attachment Types

Defined in `internal/artifacts/attachment.go`:

| Kind | Detection |
|------|-----------|
| `image` | MIME starts with `image/` or extension is jpg/jpeg/png/gif/webp/bmp/heic/heif |
| `audio` | MIME starts with `audio/` or extension is mp3/m4a/wav/ogg/opus/aac/flac |
| `video` | MIME starts with `video/` or extension is mp4/mov/avi/mkv/webm/m4v |
| `file` | Everything else |

## Filename Normalization

`NormalizeFilename(name, mimeType)` (`internal/artifacts/attachment.go:57-68`):
- Takes `filepath.Base` to strip directory components
- Defaults to `"attachment"` if empty or `.`
- Appends a file extension from the MIME type if none is present

## Attachment Markers

`Marker(att)` produces a display string like `[image: screenshot.png]` or `[file: report.pdf]`. Used in chat messages to reference attached artifacts.

`FailureMarker(kind, name, reason)` produces a failure indicator like `[image: photo.jpg - unavailable]`.
