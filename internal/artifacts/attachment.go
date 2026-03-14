package artifacts

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

const (
	KindImage = "image"
	KindAudio = "audio"
	KindVideo = "video"
	KindFile  = "file"
)

type Attachment struct {
	ArtifactID string `json:"artifact_id"`
	Filename   string `json:"filename"`
	Mime       string `json:"mime"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"size_bytes"`
}

type StoredArtifact struct {
	ID         string
	SessionKey string
	Mime       string
	Path       string
	SizeBytes  int64
}

func DetectKind(filename, mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mt, "image/"):
		return KindImage
	case strings.HasPrefix(mt, "audio/"):
		return KindAudio
	case strings.HasPrefix(mt, "video/"):
		return KindVideo
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".heic", ".heif":
		return KindImage
	case ".mp3", ".m4a", ".wav", ".ogg", ".oga", ".opus", ".aac", ".flac":
		return KindAudio
	case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".m4v":
		return KindVideo
	default:
		return KindFile
	}
}

func NormalizeFilename(name, mimeType string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	if filepath.Ext(name) == "" {
		if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
			name += exts[0]
		}
	}
	return name
}

func Marker(att Attachment) string {
	name := strings.TrimSpace(att.Filename)
	if name == "" {
		name = "attachment"
	}
	kind := strings.TrimSpace(att.Kind)
	if kind == "" {
		kind = DetectKind(name, att.Mime)
	}
	return fmt.Sprintf("[%s: %s]", kind, name)
}

func FailureMarker(kind, name, reason string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = KindFile
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "attachment"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Sprintf("[%s: %s - unavailable]", kind, name)
	}
	return fmt.Sprintf("[%s: %s - %s]", kind, name, reason)
}
