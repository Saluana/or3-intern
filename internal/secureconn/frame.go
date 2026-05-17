package secureconn

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type ReplayWindow struct {
	mu       sync.Mutex
	maxSeen  uint64
	seen     map[uint64]struct{}
	capacity int
}

func NewReplayWindow(capacity int) *ReplayWindow {
	if capacity <= 0 {
		capacity = 128
	}
	return &ReplayWindow{seen: map[uint64]struct{}{}, capacity: capacity}
}

func (w *ReplayWindow) Accept(sequence uint64) error {
	if sequence == 0 {
		return SecureConnectionError{Code: ErrorReplayDetected, SafeMessage: "Connection message order was invalid.", Retryable: true}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.seen[sequence]; exists {
		return SecureConnectionError{Code: ErrorReplayDetected, SafeMessage: "A repeated connection message was blocked.", Retryable: true}
	}
	if w.maxSeen > uint64(w.capacity) && sequence+uint64(w.capacity) <= w.maxSeen {
		return SecureConnectionError{Code: ErrorReplayDetected, SafeMessage: "An old connection message was blocked.", Retryable: true}
	}
	w.seen[sequence] = struct{}{}
	if sequence > w.maxSeen {
		w.maxSeen = sequence
	}
	cutoff := uint64(0)
	if w.maxSeen > uint64(w.capacity) {
		cutoff = w.maxSeen - uint64(w.capacity)
	}
	for item := range w.seen {
		if item < cutoff {
			delete(w.seen, item)
		}
	}
	return nil
}

func ValidateSecureFrame(frame SecureFrameV1, expectedSessionID string, window *ReplayWindow, now time.Time) error {
	if frame.Version != ProtocolVersion {
		return SecureConnectionError{Code: ErrorProtocolUnsupported, SafeMessage: "This app and computer use incompatible connection versions.", Retryable: false}
	}
	if strings.TrimSpace(frame.Kind) == "" || strings.TrimSpace(frame.SessionID) == "" || strings.TrimSpace(frame.CorrelationID) == "" {
		return SecureConnectionError{Code: ErrorMalformedPayload, SafeMessage: "A malformed connection message was blocked.", Retryable: false}
	}
	if expectedSessionID != "" && frame.SessionID != expectedSessionID {
		return SecureConnectionError{Code: ErrorMalformedPayload, SafeMessage: "A connection message was addressed to the wrong session.", Retryable: false}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	skew := now.UTC().UnixMilli() - frame.SentAtUnixMs
	if skew < -5*60*1000 || skew > 24*60*60*1000 {
		return fmt.Errorf("secure frame timestamp outside allowed window")
	}
	if window != nil {
		return window.Accept(frame.Sequence)
	}
	return nil
}
