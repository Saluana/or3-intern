package triggers

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type FileWatcher struct {
	Config     config.FileWatchConfig
	Bus        *bus.Bus
	SessionKey string

	mu     sync.Mutex
	last   map[string]fileState
	cancel context.CancelFunc
}

type fileState struct {
	mtime  time.Time
	size   int64
	lastEv time.Time // last time we published an event for this path
}

func NewFileWatcher(cfg config.FileWatchConfig, b *bus.Bus, sessionKey string) *FileWatcher {
	return &FileWatcher{
		Config:     cfg,
		Bus:        b,
		SessionKey: sessionKey,
		last:       map[string]fileState{},
	}
}

func (fw *FileWatcher) Start(ctx context.Context) {
	if !fw.Config.Enabled || len(fw.Config.Paths) == 0 {
		return
	}
	pollInterval := time.Duration(fw.Config.PollSeconds) * time.Second
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	ctx, fw.cancel = context.WithCancel(ctx)
	go fw.loop(ctx, pollInterval)
}

func (fw *FileWatcher) Stop() {
	if fw.cancel != nil {
		fw.cancel()
	}
}

func (fw *FileWatcher) loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fw.poll(ctx)
		}
	}
}

func (fw *FileWatcher) poll(ctx context.Context) {
	debounce := time.Duration(fw.Config.DebounceSeconds) * time.Second
	if debounce <= 0 {
		debounce = 2 * time.Second
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	now := time.Now()
	for _, p := range fw.Config.Paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// Don't follow symlinks
		info, err := os.Lstat(absPath)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		prev, seen := fw.last[absPath]
		cur := fileState{mtime: info.ModTime(), size: info.Size()}
		if seen {
			// Check if changed
			if cur.mtime == prev.mtime && cur.size == prev.size {
				continue
			}
			// Debounce: don't republish if we published recently
			if now.Sub(prev.lastEv) < debounce {
				// update state but don't publish yet
				fw.last[absPath] = fileState{mtime: cur.mtime, size: cur.size, lastEv: prev.lastEv}
				continue
			}
		}
		cur.lastEv = now
		fw.last[absPath] = cur
		if !seen {
			// First observation - record baseline with zero lastEv so debounce
			// does not prevent the first change event from being published.
			fw.last[absPath] = fileState{mtime: cur.mtime, size: cur.size}
			continue
		}
		// Publish event
		ev := bus.Event{
			Type:       bus.EventFileChange,
			SessionKey: fw.SessionKey,
			Channel:    "filewatch",
			From:       absPath,
			Message:    "file changed: " + absPath,
			Meta: map[string]any{
				"path":  absPath,
				"size":  info.Size(),
				"mtime": info.ModTime().UnixMilli(),
			},
		}
		if ok := fw.Bus.Publish(ev); !ok {
			log.Printf("filewatch: bus full, dropping event for %s", absPath)
		}
	}
}
