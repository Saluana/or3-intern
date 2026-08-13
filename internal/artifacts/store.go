// Package artifacts persists binary attachments referenced from conversations.
package artifacts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/db"
)

var (
	ErrNotFound     = errors.New("artifact not found")
	ErrNotAvailable = errors.New("artifact not available for session")
)

// Store saves artifact bytes on disk and tracks them in the database.
type Store struct {
	Dir string
	DB  *db.DB
}

// Reconcile removes old regular files that have no metadata row. The grace
// period avoids racing a currently executing Save between file creation and
// its database insert.
func (s *Store) Reconcile(ctx context.Context, grace time.Duration) error {
	if s == nil || s.DB == nil || strings.TrimSpace(s.Dir) == "" {
		return nil
	}
	if grace <= 0 {
		grace = time.Hour
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `SELECT path FROM artifacts`)
	if err != nil {
		return err
	}
	referenced := map[string]struct{}{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			_ = rows.Close()
			return err
		}
		if abs, err := filepath.Abs(path); err == nil {
			referenced[filepath.Clean(abs)] = struct{}{}
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-grace)
	var reconcileErr error
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path, err := filepath.Abs(filepath.Join(s.Dir, entry.Name()))
		if err != nil {
			continue
		}
		if _, ok := referenced[filepath.Clean(path)]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("remove orphan artifact %s: %w", entry.Name(), err))
		}
	}
	return reconcileErr
}

// ReadResult contains a bounded artifact read authorized for a session.
type ReadResult struct {
	Artifact  StoredArtifact
	Content   string
	Truncated bool
	ReadBytes int
}

// Save writes data to disk and returns the stored artifact ID.
func (s *Store) Save(ctx context.Context, sessionKey, mime string, data []byte) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("artifacts dir not set")
	}
	if s.DB == nil {
		return "", fmt.Errorf("artifacts db not set")
	}
	if err := s.DB.EnsureSession(ctx, strings.TrimSpace(sessionKey)); err != nil {
		return "", err
	}
	_ = os.MkdirAll(s.Dir, 0o700)
	id := randID()
	path := filepath.Join(s.Dir, id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	_, err := s.DB.SQL.ExecContext(ctx,
		`INSERT INTO artifacts(id, session_key, mime, path, size_bytes, created_at) VALUES(?,?,?,?,?,?)`,
		id, sessionKey, mime, path, len(data), time.Now().UnixMilli())
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return id, nil
}

// SaveNamed stores data and returns an attachment record with a normalized filename.
func (s *Store) SaveNamed(ctx context.Context, sessionKey, filename, mimeType string, data []byte) (Attachment, error) {
	filename = NormalizeFilename(filename, mimeType)
	id, err := s.Save(ctx, sessionKey, mimeType, data)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{
		ArtifactID: id,
		Filename:   filename,
		Mime:       strings.TrimSpace(mimeType),
		Kind:       DetectKind(filename, mimeType),
		SizeBytes:  int64(len(data)),
	}, nil
}

// Lookup returns the stored artifact metadata for artifactID.
func (s *Store) Lookup(ctx context.Context, artifactID string) (StoredArtifact, error) {
	if s.DB == nil {
		return StoredArtifact{}, fmt.Errorf("artifacts db not set")
	}
	row := s.DB.SQL.QueryRowContext(ctx,
		`SELECT id, session_key, mime, path, size_bytes FROM artifacts WHERE id=?`,
		strings.TrimSpace(artifactID),
	)
	var stored StoredArtifact
	if err := row.Scan(&stored.ID, &stored.SessionKey, &stored.Mime, &stored.Path, &stored.SizeBytes); err != nil {
		if err == sql.ErrNoRows {
			return StoredArtifact{}, fmt.Errorf("%w: %s", ErrNotFound, artifactID)
		}
		return StoredArtifact{}, err
	}
	return stored, nil
}

// ReadCapped reads at most maxBytes from artifactID after checking that the
// artifact belongs to the caller's session or resolved scope.
func (s *Store) ReadCapped(ctx context.Context, sessionKey, artifactID string, maxBytes int64) (ReadResult, error) {
	return s.ReadCappedFrom(ctx, sessionKey, artifactID, 0, maxBytes)
}

// ReadCappedFrom reads at most maxBytes from artifactID starting at offset
// after checking that the artifact belongs to the caller's session or resolved scope.
func (s *Store) ReadCappedFrom(ctx context.Context, sessionKey, artifactID string, offset, maxBytes int64) (ReadResult, error) {
	if maxBytes <= 0 {
		maxBytes = 200000
	}
	if offset < 0 {
		offset = 0
	}
	stored, err := s.Lookup(ctx, artifactID)
	if err != nil {
		return ReadResult{}, err
	}
	if !s.sessionCanRead(ctx, sessionKey, stored.SessionKey) {
		return ReadResult{}, fmt.Errorf("%w", ErrNotAvailable)
	}
	path, err := s.safeStoredPath(stored.Path)
	if err != nil {
		return ReadResult{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return ReadResult{}, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return ReadResult{}, err
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	return ReadResult{Artifact: stored, Content: string(data), Truncated: truncated, ReadBytes: len(data)}, nil
}

func (s *Store) sessionCanRead(ctx context.Context, requestSession, artifactSession string) bool {
	requestSession = strings.TrimSpace(requestSession)
	artifactSession = strings.TrimSpace(artifactSession)
	if requestSession == "" || artifactSession == "" {
		return false
	}
	if requestSession == artifactSession {
		return true
	}
	if s.DB == nil {
		return false
	}
	resolved, err := s.DB.ResolveScopeKey(ctx, requestSession)
	if err != nil || strings.TrimSpace(resolved) == "" {
		return false
	}
	return strings.TrimSpace(resolved) == artifactSession
}

func (s *Store) safeStoredPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("artifact path missing")
	}
	if strings.TrimSpace(s.Dir) == "" {
		return "", fmt.Errorf("artifacts dir not set")
	}
	root, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if evaluated, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		abs = evaluated
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path outside store")
	}
	return abs, nil
}

func randID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
