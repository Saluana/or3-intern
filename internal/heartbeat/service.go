// Package heartbeat publishes recurring review events derived from a tasks file.
package heartbeat

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/triggers"
)

const (
	// DefaultChannel is the channel name used for emitted heartbeat events.
	DefaultChannel = "system"
	// DefaultFrom is the sender name used for emitted heartbeat events.
	DefaultFrom = "heartbeat"
	// SeedMessage is the default instruction sent with a heartbeat event.
	SeedMessage = "Review HEARTBEAT.md and execute any active recurring tasks."

	// MetaKeyHeartbeat marks an event as originating from the heartbeat service.
	MetaKeyHeartbeat = "heartbeat"
	// MetaKeyDone stores a completion callback that clears the in-flight flag.
	MetaKeyDone = "heartbeat_done"
)

// Service watches for recurring heartbeat work and publishes it onto the bus.
type Service struct {
	Config       config.HeartbeatConfig
	WorkspaceDir string
	Bus          *bus.Bus

	logf func(string, ...any)

	mu        sync.Mutex
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	tickQueue chan struct{}
	inFlight  atomic.Bool
	stopping  atomic.Bool
}

// New constructs a heartbeat service for cfg and workspaceDir.
func New(cfg config.HeartbeatConfig, workspaceDir string, eventBus *bus.Bus) *Service {
	return &Service{
		Config:       cfg,
		WorkspaceDir: workspaceDir,
		Bus:          eventBus,
		logf:         log.Printf,
	}
}

// Start launches the ticker and publisher goroutines when the service is enabled.
func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.Config.Enabled || s.Bus == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	s.stopping.Store(false)

	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.tickQueue = make(chan struct{}, 1)

	interval := time.Duration(normalizeIntervalMinutes(s.Config.IntervalMinutes)) * time.Minute
	s.wg.Add(2)
	go s.runTicker(childCtx, interval)
	go s.runPublisher(childCtx)
}

// Stop cancels background work and waits for the service to drain.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopping.Store(true)

	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.tickQueue = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	s.inFlight.Store(false)
}

func (s *Service) runTicker(ctx context.Context, interval time.Duration) {
	defer s.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.enqueueTick("timer")
		}
	}
}

func (s *Service) runPublisher(ctx context.Context) {
	defer s.wg.Done()

	for {
		if s.stopping.Load() || ctx.Err() != nil {
			return
		}

		s.mu.Lock()
		tickQueue := s.tickQueue
		s.mu.Unlock()
		if tickQueue == nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-tickQueue:
			if s.stopping.Load() || ctx.Err() != nil {
				return
			}
			s.processTick()
		}
	}
}

func (s *Service) enqueueTick(source string) bool {
	s.mu.Lock()
	tickQueue := s.tickQueue
	s.mu.Unlock()
	if tickQueue == nil {
		return false
	}

	select {
	case tickQueue <- struct{}{}:
		return true
	default:
		s.logf("heartbeat tick dropped: pending tick already queued source=%s", source)
		return false
	}
}

func (s *Service) processTick() {
	if s.inFlight.Load() {
		s.logf("heartbeat tick skipped: previous turn still in flight")
		return
	}

	path, text, err := LoadTasksFile(s.Config.TasksFile, s.WorkspaceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.logf("heartbeat tick skipped: tasks file not found")
			return
		}
		s.logf("heartbeat tick skipped: read failed path=%q err=%v", path, err)
		return
	}
	if !HasActiveInstructions(text) {
		return
	}

	s.inFlight.Store(true)
	meta := map[string]any{
		MetaKeyHeartbeat: true,
		MetaKeyDone: func() {
			s.inFlight.Store(false)
		},
		"tasks_path": path,
		triggers.MetaKeyStructuredEvent: triggers.StructuredEventMap(triggers.StructuredEvent{
			Type:    string(bus.EventHeartbeat),
			Source:  "heartbeat",
			Trusted: true,
			Details: map[string]any{"tasks_path": path, "session_key": normalizedSessionKey(s.Config.SessionKey)},
		}),
	}
	if structuredTasks, ok := triggers.ParseStructuredTasksText(text); ok {
		meta[triggers.MetaKeyStructuredTasks] = triggers.StructuredTasksMap(structuredTasks)
	}
	ev := bus.Event{
		Type:       bus.EventHeartbeat,
		SessionKey: normalizedSessionKey(s.Config.SessionKey),
		Channel:    DefaultChannel,
		From:       DefaultFrom,
		Message:    SeedMessage,
		Meta:       meta,
	}
	if ok := s.Bus.Publish(ev); !ok {
		s.inFlight.Store(false)
		s.logf("heartbeat tick dropped: event bus full")
	}
}

// LoadTasksFile resolves and reads the heartbeat tasks file.
// It returns the chosen path, trimmed contents, and any read error.
func LoadTasksFile(configPath, workspaceDir string) (string, string, error) {
	var firstErr error
	for _, path := range candidatePaths(configPath, workspaceDir) {
		data, err := os.ReadFile(path)
		if err == nil {
			return path, strings.TrimSpace(string(data)), nil
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return strings.TrimSpace(configPath), "", firstErr
	}
	return strings.TrimSpace(configPath), "", os.ErrNotExist
}

// HasActiveInstructions reports whether text contains actionable non-comment content.
func HasActiveInstructions(text string) bool {
	inComment := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		return true
	}
	return false
}

func candidatePaths(configPath, workspaceDir string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	if strings.TrimSpace(workspaceDir) != "" {
		add(filepath.Join(workspaceDir, "HEARTBEAT.md"))
		add(filepath.Join(workspaceDir, "heartbeat.md"))
	}
	add(configPath)
	return out
}

func normalizeIntervalMinutes(v int) int {
	if v <= 0 {
		return 30
	}
	if v < 1 {
		return 1
	}
	return v
}

func normalizedSessionKey(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return config.DefaultHeartbeatSessionKey
	}
	return v
}
