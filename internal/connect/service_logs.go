package connect

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	ManagedServiceLogsEnv = "OR3_CONNECT_MANAGED_LOGS"

	serviceLogMaxBytes int64 = 1 << 20
	serviceLogBackups        = 3
	serviceLogMaxLine        = 64 << 10
)

var (
	authorizationPattern    = regexp.MustCompile(`(?i)(\bauthorization[[:space:]]*[:=][[:space:]]*(?:bearer|basic)[[:space:]]+)[^[:space:]]+`)
	bearerCredentialPattern = regexp.MustCompile(`(?i)(\bbearer[[:space:]]+)[A-Za-z0-9._~+/=-]+`)
	namedCredentialPattern  = regexp.MustCompile(`(?i)\b(control[_-]?token|tunnel[_-]?(?:token|secret)|api[_-]?key|access[_-]?token|refresh[_-]?token|token|secret|password|credential)([[:space:]]*[:=][[:space:]]*)("[^"]*"|'[^']*'|[^[:space:],;]+)`)
	flagCredentialPattern   = regexp.MustCompile(`(?i)(--(?:token|api-key|secret)(?:=|[[:space:]]+))[^[:space:]]+`)
	jwtCredentialPattern    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
)

// ManagedServiceLogs owns the bounded stdout and stderr sinks used by the
// macOS launchd service. Both streams redact likely credentials before they
// reach disk.
type ManagedServiceLogs struct {
	Stdout io.Writer
	Stderr io.Writer

	stdout *redactingLineWriter
	stderr *redactingLineWriter
}

// OpenManagedServiceLogs opens owner-only, size-rotated service logs.
func OpenManagedServiceLogs(stateDir string) (*ManagedServiceLogs, error) {
	return openManagedServiceLogs(stateDir, serviceLogMaxBytes, serviceLogBackups)
}

func openManagedServiceLogs(stateDir string, maxBytes int64, backups int) (*ManagedServiceLogs, error) {
	if maxBytes <= 0 || backups < 0 {
		return nil, fmt.Errorf("invalid managed service log limits")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create service log directory: %w", err)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure service log directory: %w", err)
	}
	stdoutFile, err := openRotatingFile(filepath.Join(stateDir, "connect.log"), maxBytes, backups)
	if err != nil {
		return nil, err
	}
	stderrFile, err := openRotatingFile(filepath.Join(stateDir, "connect-error.log"), maxBytes, backups)
	if err != nil {
		_ = stdoutFile.Close()
		return nil, err
	}
	stdout := &redactingLineWriter{destination: stdoutFile, maxLine: serviceLogMaxLine}
	stderr := &redactingLineWriter{destination: stderrFile, maxLine: serviceLogMaxLine}
	return &ManagedServiceLogs{
		Stdout: stdout,
		Stderr: stderr,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

func (logs *ManagedServiceLogs) Close() error {
	if logs == nil {
		return nil
	}
	return errors.Join(logs.stdout.Close(), logs.stderr.Close())
}

// RecentServiceDiagnostics returns a redacted, bounded tail suitable for
// Doctor output. Failure to read a log is deliberately non-fatal.
func RecentServiceDiagnostics(stateDir string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	names := []string{"connect-error.log", "connect.log"}
	perLog := maxBytes / len(names)
	if perLog < 1 {
		perLog = 1
	}
	var output strings.Builder
	for _, name := range names {
		body, err := readFileTail(filepath.Join(stateDir, name), perLog+serviceLogMaxLine)
		if err != nil || len(body) == 0 {
			continue
		}
		body = redactCredentials(body)
		if len(body) > perLog {
			body = body[len(body)-perLog:]
		}
		heading := "\nRecent " + name + " (redacted):\n"
		if output.Len()+len(heading)+len(body) > maxBytes {
			remaining := maxBytes - output.Len() - len(heading)
			if remaining <= 0 {
				break
			}
			body = body[len(body)-remaining:]
		}
		output.WriteString(heading)
		output.Write(body)
		if body[len(body)-1] != '\n' {
			output.WriteByte('\n')
		}
	}
	result := output.String()
	if len(result) > maxBytes {
		return result[len(result)-maxBytes:]
	}
	return result
}

func readFileTail(path string, maxBytes int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	offset := info.Size() - int64(maxBytes)
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)))
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		newline := bytes.IndexByte(body, '\n')
		if newline < 0 {
			return nil, nil
		}
		body = body[newline+1:]
	}
	return body, nil
}

type rotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
}

func openRotatingFile(path string, maxBytes int64, backups int) (*rotatingFile, error) {
	writer := &rotatingFile{path: path, maxBytes: maxBytes, backups: backups}
	for index := 0; index <= backups; index++ {
		candidate := path
		if index > 0 {
			candidate = fmt.Sprintf("%s.%d", path, index)
		}
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > maxBytes {
			if err := os.Remove(candidate); err != nil {
				return nil, fmt.Errorf("remove unsafe service log %s: %w", filepath.Base(candidate), err)
			}
			continue
		}
		if err := os.Chmod(candidate, 0o600); err != nil {
			return nil, fmt.Errorf("secure service log %s: %w", filepath.Base(candidate), err)
		}
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups+1))
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (writer *rotatingFile) open() error {
	file, err := os.OpenFile(writer.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open service log %s: %w", filepath.Base(writer.path), err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure service log %s: %w", filepath.Base(writer.path), err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	writer.file = file
	writer.size = info.Size()
	return nil
}

func (writer *rotatingFile) Write(body []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	written := 0
	for len(body) > 0 {
		if writer.size >= writer.maxBytes {
			if err := writer.rotate(); err != nil {
				return written, err
			}
		}
		remaining := writer.maxBytes - writer.size
		chunkSize := int64(len(body))
		if chunkSize > remaining {
			chunkSize = remaining
		}
		count, err := writer.file.Write(body[:chunkSize])
		written += count
		writer.size += int64(count)
		body = body[count:]
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (writer *rotatingFile) rotate() error {
	if err := writer.file.Close(); err != nil {
		return err
	}
	writer.file = nil
	if writer.backups == 0 {
		if err := os.Remove(writer.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		_ = os.Remove(fmt.Sprintf("%s.%d", writer.path, writer.backups))
		for index := writer.backups - 1; index >= 1; index-- {
			older := fmt.Sprintf("%s.%d", writer.path, index)
			newer := fmt.Sprintf("%s.%d", writer.path, index+1)
			if err := os.Rename(older, newer); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := os.Rename(writer.path, writer.path+".1"); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return writer.open()
}

func (writer *rotatingFile) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

type redactingLineWriter struct {
	mu          sync.Mutex
	destination io.WriteCloser
	maxLine     int
	buffer      []byte
	discarding  bool
}

func (writer *redactingLineWriter) Write(body []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	consumed := len(body)
	for len(body) > 0 {
		newline := bytes.IndexByte(body, '\n')
		if newline < 0 {
			if writer.discarding {
				return consumed, nil
			}
			if len(writer.buffer)+len(body) > writer.maxLine {
				writer.buffer = nil
				writer.discarding = true
				_, err := io.WriteString(writer.destination, "[oversized log line omitted]\n")
				return consumed, err
			}
			writer.buffer = append(writer.buffer, body...)
			return consumed, nil
		}
		part := body[:newline+1]
		body = body[newline+1:]
		if writer.discarding {
			writer.discarding = false
			continue
		}
		if len(writer.buffer)+len(part) > writer.maxLine {
			writer.buffer = nil
			if _, err := io.WriteString(writer.destination, "[oversized log line omitted]\n"); err != nil {
				return consumed, err
			}
			continue
		}
		writer.buffer = append(writer.buffer, part...)
		if _, err := writer.destination.Write(redactCredentials(writer.buffer)); err != nil {
			return consumed, err
		}
		writer.buffer = writer.buffer[:0]
	}
	return consumed, nil
}

func (writer *redactingLineWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if !writer.discarding && len(writer.buffer) > 0 {
		if _, err := writer.destination.Write(redactCredentials(writer.buffer)); err != nil {
			_ = writer.destination.Close()
			return err
		}
	}
	writer.buffer = nil
	writer.discarding = false
	return writer.destination.Close()
}

func redactCredentials(body []byte) []byte {
	redacted := authorizationPattern.ReplaceAll(body, []byte(`${1}<redacted>`))
	redacted = bearerCredentialPattern.ReplaceAll(redacted, []byte(`${1}<redacted>`))
	redacted = namedCredentialPattern.ReplaceAll(redacted, []byte(`${1}${2}<redacted>`))
	redacted = flagCredentialPattern.ReplaceAll(redacted, []byte(`${1}<redacted>`))
	return jwtCredentialPattern.ReplaceAll(redacted, []byte(`<redacted-jwt>`))
}
