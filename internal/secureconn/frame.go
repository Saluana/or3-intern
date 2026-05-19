package secureconn

import (
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
		capacity = DefaultReplayWindowCap
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
	if len(frame.Body) > MaxSecureFrameBodyBytes {
		return SecureConnectionError{Code: ErrorMalformedPayload, SafeMessage: "A connection message was too large.", Retryable: false}
	}
	if expectedSessionID != "" && frame.SessionID != expectedSessionID {
		return SecureConnectionError{Code: ErrorMalformedPayload, SafeMessage: "A connection message was addressed to the wrong session.", Retryable: false}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	skew := now.UTC().UnixMilli() - frame.SentAtUnixMs
	if skew < -60*1000 || skew > 24*60*60*1000 {
		return SecureConnectionError{Code: ErrorMalformedPayload, SafeMessage: "A connection message had an invalid timestamp.", Retryable: false}
	}
	if window != nil {
		return window.Accept(frame.Sequence)
	}
	return nil
}

func EncodeSecureFrame(frame SecureFrameV1, now time.Time) ([]byte, error) {
	if frame.Version == 0 {
		frame.Version = ProtocolVersion
	}
	if frame.SentAtUnixMs == 0 {
		if now.IsZero() {
			now = time.Now().UTC()
		}
		frame.SentAtUnixMs = now.UTC().UnixMilli()
	}
	if err := ValidateSecureFrame(frame, frame.SessionID, nil, now); err != nil {
		return nil, err
	}
	return CanonicalBytes(frame)
}

func DecodeSecureFrame(raw []byte, expectedSessionID string, window *ReplayWindow, now time.Time) (SecureFrameV1, error) {
	var frame SecureFrameV1
	if err := DecodeCanonical(raw, &frame); err != nil {
		return SecureFrameV1{}, SecureConnectionError{Code: ErrorMalformedPayload, SafeMessage: "A malformed connection message was blocked.", Retryable: false}
	}
	if err := ValidateSecureFrame(frame, expectedSessionID, window, now); err != nil {
		return SecureFrameV1{}, err
	}
	return frame, nil
}
