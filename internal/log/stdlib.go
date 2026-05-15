package log

import (
	"fmt"
	"io"
	stdlog "log"
	"os"
	"strings"
	"sync"
)

var (
	defaultBuffer   = NewBuffer(DefaultBufferSize)
	defaultBufferMu sync.RWMutex
	installOnce     sync.Once
	outputMu        sync.Mutex
	outputWriter    io.Writer = os.Stderr
)

func DefaultBuffer() *Buffer {
	defaultBufferMu.RLock()
	defer defaultBufferMu.RUnlock()
	return defaultBuffer
}

func ResetDefaultBufferForTest(max int) {
	defaultBufferMu.Lock()
	defer defaultBufferMu.Unlock()
	defaultBuffer = NewBuffer(max)
}

func InstallStdlibSink() {
	installOnce.Do(func() {
		writer := stdlog.Writer()
		if writer == nil {
			writer = os.Stderr
		}
		outputMu.Lock()
		outputWriter = writer
		outputMu.Unlock()
		stdlog.SetOutput(io.MultiWriter(writer, stdlibSink{}))
	})
}

func Debugf(component, format string, args ...any) {
	emit(LevelDebug, component, format, args...)
}

func Infof(component, format string, args ...any) {
	emit(LevelInfo, component, format, args...)
}

func Warnf(component, format string, args ...any) {
	emit(LevelWarn, component, format, args...)
}

func Errorf(component, format string, args ...any) {
	emit(LevelError, component, format, args...)
}

func emit(level Level, component, format string, args ...any) {
	if !Enabled(level) {
		return
	}
	message := fmt.Sprintf(format, args...)
	DefaultBuffer().Append(Entry{Level: level, Component: component, Message: message})
	outputMu.Lock()
	defer outputMu.Unlock()
	_, _ = fmt.Fprintf(outputWriter, "%s: %s\n", strings.TrimSpace(component), message)
}

type stdlibSink struct{}

func (w stdlibSink) Write(p []byte) (int, error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		message := normalizeStdlibLine(line)
		if message == "" {
			continue
		}
		level := inferLevel(message)
		if !Enabled(level) {
			continue
		}
		DefaultBuffer().Append(Entry{Level: level, Message: message})
	}
	return len(p), nil
}
