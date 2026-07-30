// Package memorysvc provides bounded OR3 memory operations for runners and service APIs.
package memorysvc

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

const (
	defaultRecentLimit = 10
	defaultPinnedMax   = 400
	defaultRecentMax   = 240
	maxSearchTopK      = 25
	maxQueryLen        = 4000
	maxNoteLen         = 16000
)

// Service performs scoped memory reads and writes without the generic tool registry.
type Service struct {
	DB               *db.DB
	Provider         *providers.Client
	EmbedModel       string
	EmbedFingerprint string
	VectorK          int
	FTSK             int
	TopK             int
	VectorScanLimit  int
	RecentDefault    int
	RecentMax        int
	RecentMaxChars   int
	PinnedMaxChars   int
}

func New(cfg config.Config, database *db.DB, provider *providers.Client, embedFingerprint string) *Service {
	embedRole := cfg.ModelRole(config.ModelRoleEmbeddings)
	recentMax := cfg.HistoryMax
	if recentMax <= 0 {
		recentMax = 40
	}
	return &Service{
		DB:               database,
		Provider:         provider,
		EmbedModel:       embedRole.Primary.Model,
		EmbedFingerprint: embedFingerprint,
		VectorK:          cfg.VectorK,
		FTSK:             cfg.FTSK,
		TopK:             cfg.MemoryRetrieve,
		VectorScanLimit:  cfg.VectorScanLimit,
		RecentDefault:    defaultRecentLimit,
		RecentMax:        recentMax,
		RecentMaxChars:   defaultRecentMax,
		PinnedMaxChars:   defaultPinnedMax,
	}
}

type SearchRequest struct {
	SessionKey string
	Query      string
	TopK       int
	GlobalOnly bool
}

type SearchHit struct {
	Source string  `json:"source"`
	Score  float64 `json:"score"`
	Text   string  `json:"text"`
}

type SearchResponse struct {
	Warning string      `json:"warning,omitempty"`
	Hits    []SearchHit `json:"hits"`
}

func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if s == nil || s.DB == nil {
		return SearchResponse{}, fmt.Errorf("memory service unavailable")
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		return SearchResponse{}, fmt.Errorf("empty query")
	}
	if len(q) > maxQueryLen {
		q = q[:maxQueryLen]
	}
	topK := req.TopK
	if topK <= 0 {
		topK = s.TopK
	}
	if topK > maxSearchTopK {
		topK = maxSearchTopK
	}
	scopeKey := resolveScope(req.SessionKey, req.GlobalOnly)
	var warning string
	var queryVec []float32
	if s.Provider != nil {
		vec, err := s.Provider.Embed(ctx, s.EmbedModel, q)
		if err != nil {
			warning = "semantic search unavailable; keyword search used"
		} else {
			queryVec = vec
		}
	} else {
		warning = "semantic search unavailable; keyword search used"
	}
	r := memory.NewRetriever(s.DB)
	r.EmbedFingerprint = s.EmbedFingerprint
	r.VectorScanLimit = s.VectorScanLimit
	got, err := r.Retrieve(ctx, scopeKey, q, queryVec, s.VectorK, s.FTSK, topK)
	if err != nil {
		return SearchResponse{}, err
	}
	hits := make([]SearchHit, 0, len(got))
	for _, m := range got {
		hits = append(hits, SearchHit{Source: m.Source, Score: m.Score, Text: m.Text})
	}
	return SearchResponse{Warning: warning, Hits: hits}, nil
}

type AddNoteRequest struct {
	SessionKey      string
	Text            string
	Tags            string
	SourceMessageID int64
	GlobalOnly      bool
}

type AddNoteResponse struct {
	ID      int64  `json:"id"`
	Warning string `json:"warning,omitempty"`
}

func (s *Service) AddNote(ctx context.Context, req AddNoteRequest) (AddNoteResponse, error) {
	if s == nil || s.DB == nil {
		return AddNoteResponse{}, fmt.Errorf("memory service unavailable")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return AddNoteResponse{}, fmt.Errorf("empty text")
	}
	if len(text) > maxNoteLen {
		text = text[:maxNoteLen]
	}
	if err := memory.ValidateNoteText(text); err != nil {
		return AddNoteResponse{}, err
	}
	var embedding []byte
	var warning string
	if s.Provider != nil {
		vec, err := s.Provider.Embed(ctx, s.EmbedModel, text)
		if err != nil {
			warning = "note stored without embedding; semantic recall unavailable for this note"
		} else {
			embedding = memory.PackFloat32(vec)
		}
	} else {
		warning = "note stored without embedding; semantic recall unavailable until embeddings are configured"
	}
	var src sql.NullInt64
	if req.SourceMessageID > 0 {
		src = sql.NullInt64{Int64: req.SourceMessageID, Valid: true}
	}
	id, err := s.DB.InsertMemoryNoteTyped(ctx, resolveScope(req.SessionKey, req.GlobalOnly), db.TypedNoteInput{
		Text:             text,
		Embedding:        embedding,
		EmbedFingerprint: s.EmbedFingerprint,
		SourceMsgID:      src,
		Tags:             strings.TrimSpace(req.Tags),
	})
	if err != nil {
		return AddNoteResponse{}, err
	}
	return AddNoteResponse{ID: id, Warning: warning}, nil
}

type PinnedEntry struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}

func (s *Service) GetPinned(ctx context.Context, sessionKey, key string, globalOnly bool) ([]PinnedEntry, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("memory service unavailable")
	}
	pinned, err := s.DB.GetPinned(ctx, resolveScope(sessionKey, globalOnly))
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key != "" {
		value, ok := pinned[key]
		if !ok {
			return nil, nil
		}
		return []PinnedEntry{{Key: key, Content: compactText(value, s.PinnedMaxChars)}}, nil
	}
	keys := make([]string, 0, len(pinned))
	for k := range pinned {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]PinnedEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, PinnedEntry{Key: k, Content: compactText(pinned[k], s.PinnedMaxChars)})
	}
	return out, nil
}

type SetPinnedRequest struct {
	SessionKey string
	Key        string
	Content    string
	GlobalOnly bool
}

func (s *Service) SetPinned(ctx context.Context, req SetPinnedRequest) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("memory service unavailable")
	}
	key := strings.TrimSpace(req.Key)
	content := strings.TrimSpace(req.Content)
	if key == "" || content == "" {
		return fmt.Errorf("missing key/content")
	}
	if err := memory.ValidatePinKey(key); err != nil {
		return err
	}
	if err := memory.ValidatePinContent(content); err != nil {
		return err
	}
	return s.DB.UpsertPinned(ctx, resolveScope(req.SessionKey, req.GlobalOnly), key, content)
}

func resolveScope(sessionKey string, globalOnly bool) string {
	if globalOnly {
		return scope.GlobalMemoryScope
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return scope.GlobalMemoryScope
	}
	return sessionKey
}

func compactText(text string, maxChars int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return text[:maxChars]
	}
	return text[:maxChars-3] + "..."
}
