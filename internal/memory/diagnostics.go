package memory

import (
	"sync"
	"sync/atomic"
	"time"
)

var (
	lastDocRetrievalErr atomic.Value
	lastVectorIndexErr  atomic.Value
)

func init() {
	lastDocRetrievalErr.Store("")
	lastVectorIndexErr.Store("")
}

// SetLastDocRetrievalError records the most recent document retrieval failure for status reporting.
func SetLastDocRetrievalError(err error) {
	lastDocRetrievalErr.Store(formatDiagnosticError(err))
}

// LastDocRetrievalError returns the most recent document retrieval error message.
func LastDocRetrievalError() string {
	if v, ok := lastDocRetrievalErr.Load().(string); ok {
		return v
	}
	return ""
}

// SetLastVectorIndexError records the most recent vector index failure for status reporting.
func SetLastVectorIndexError(err error) {
	lastVectorIndexErr.Store(formatDiagnosticError(err))
}

// LastVectorIndexError returns the most recent vector index error message.
func LastVectorIndexError() string {
	if v, ok := lastVectorIndexErr.Load().(string); ok {
		return v
	}
	return ""
}

func formatDiagnosticError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// DocSyncState tracks the latest document sync outcome for status reporting.
type DocSyncState struct {
	LastSyncAtMS int64
	PartialScan  bool
	Warning      string
}

var docSyncState struct {
	mu    sync.RWMutex
	state DocSyncState
}

// RecordDocSyncState updates global doc sync diagnostics.
func RecordDocSyncState(result DocSyncResult) {
	docSyncState.mu.Lock()
	defer docSyncState.mu.Unlock()
	docSyncState.state = DocSyncState{
		LastSyncAtMS: time.Now().UnixMilli(),
		PartialScan:  result.PartialScan,
		Warning:      result.Warning,
	}
}

// LatestDocSyncState returns the latest recorded doc sync state.
func LatestDocSyncState() DocSyncState {
	docSyncState.mu.RLock()
	defer docSyncState.mu.RUnlock()
	return docSyncState.state
}
