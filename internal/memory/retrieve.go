// Package memory retrieves and consolidates long-lived memory entries.
package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

// Retrieved is one memory hit returned from hybrid retrieval.
type Retrieved struct {
	Source     string // pinned|vector|fts|hybrid
	ID         int64
	Text       string
	Score      float64
	Kind       string
	Status     string
	Importance float64
	Confidence float64
	ExpiresAt  int64
	UseCount   int
	CreatedAt  int64
	LastUsedAt int64
	Ref        string
	Reason     string
}

// Retriever ranks vector, FTS, lexical, and recency signals into a single result set.
type Retriever struct {
	DB               *db.DB
	EmbedFingerprint string
	VectorWeight     float64
	FTSWeight        float64
	LexicalWeight    float64
	RecencyWeight    float64
	TaskWeight       float64
	VectorScanLimit  int
	TaskContext      string
	LastRejected     []string
}

const (
	unsupportedDistinctiveVectorPenalty = 0.12
	minUnsupportedVectorScore           = 0.35
)

// NewRetriever constructs a Retriever with default ranking weights.
func NewRetriever(d *db.DB) *Retriever {
	return &Retriever{DB: d, VectorWeight: 0.50, FTSWeight: 0.22, LexicalWeight: 0.12, RecencyWeight: 0.08, TaskWeight: 0.08, VectorScanLimit: 2000}
}

// Retrieve runs hybrid retrieval and returns diversified top-k memory results.
func (r *Retriever) Retrieve(ctx context.Context, sessionKey, query string, queryVec []float32, vectorK, ftsK, topK int) ([]Retrieved, error) {
	raw, err := r.retrieveCandidates(ctx, sessionKey, query, queryVec, vectorK, ftsK)
	if err != nil {
		return nil, err
	}
	return r.packToBudget(raw, topK), nil
}

func (r *Retriever) retrieveCandidates(ctx context.Context, sessionKey, query string, queryVec []float32, vectorK, ftsK int) ([]Retrieved, error) {
	r.LastRejected = nil
	var vecs []VecCandidate
	var err error
	if strings.TrimSpace(r.EmbedFingerprint) == "" {
		vecs, err = VectorSearch(ctx, r.DB, sessionKey, queryVec, vectorK, r.VectorScanLimit)
		if err != nil {
			return nil, err
		}
	} else {
		fingerprint, err := r.DB.MemoryVectorFingerprint(ctx)
		if err != nil {
			return nil, err
		}
		if fingerprint == strings.TrimSpace(r.EmbedFingerprint) {
			vecs, err = VectorSearch(ctx, r.DB, sessionKey, queryVec, vectorK, r.VectorScanLimit)
			if err != nil {
				return nil, err
			}
		}
	}
	fts, err := searchFTSWithFallback(ctx, r.DB, sessionKey, query, ftsK)
	if err != nil {
		return nil, err
	}

	type agg struct {
		id         int64
		text       string
		v          float64
		f          float64
		doc        float64
		createdAt  int64
		kind       string
		status     string
		importance float64
		confidence float64
		expiresAt  int64
		useCount   int
		lastUsedAt int64
		ref        string
	}
	m := map[int64]*agg{}
	for _, c := range vecs {
		a := m[c.ID]
		if a == nil {
			a = &agg{
				id:         c.ID,
				text:       c.Text,
				kind:       c.Kind,
				status:     c.Status,
				importance: c.Importance,
				confidence: c.Confidence,
				expiresAt:  c.ExpiresAt,
				useCount:   c.UseCount,
				lastUsedAt: c.LastUsedAt,
				ref:        fmt.Sprintf("memory:%d", c.ID),
			}
			m[c.ID] = a
		}
		a.v = c.Score
		if c.CreatedAt > a.createdAt {
			a.createdAt = c.CreatedAt
		}
	}
	for _, f := range fts {
		a := m[f.ID]
		if a == nil {
			a = &agg{
				id:         f.ID,
				text:       f.Text,
				kind:       f.Kind,
				status:     f.Status,
				importance: f.Importance,
				confidence: f.Confidence,
				expiresAt:  f.ExpiresAt,
				useCount:   f.UseCount,
				lastUsedAt: f.LastUsedAt,
				ref:        fmt.Sprintf("memory:%d", f.ID),
			}
			m[f.ID] = a
		} else {
			// Prefer the metadata from FTS if vector didn't have it (both are the same row).
			if a.kind == "" {
				a.kind = f.Kind
			}
		}
		// bm25 lower is better. Convert to a positive "higher is better".
		a.f = 1.0 / (1.0 + f.Rank)
		if f.CreatedAt > a.createdAt {
			a.createdAt = f.CreatedAt
		}
	}
	if ftsK > 0 {
		docs, _ := retrieveDocCandidates(ctx, r.DB, sessionKey, query, ftsK)
		for i, doc := range docs {
			id := -int64(i + 1)
			m[id] = &agg{
				id:        id,
				text:      doc.Excerpt,
				doc:       doc.Score,
				createdAt: db.NowMS(),
				kind:      db.MemoryKindFile,
				status:    db.MemoryStatusActive,
				ref:       "file:" + doc.Path,
			}
		}
	}

	raw := make([]Retrieved, 0, len(m))
	tokens := retrievalTokens(query)
	distinctiveTokens := distinctiveRetrievalTokens(query)
	taskTokens := retrievalTokens(r.TaskContext)
	newest := int64(0)
	for _, a := range m {
		if a.createdAt > newest {
			newest = a.createdAt
		}
	}
	for _, a := range m {
		lexical := lexicalOverlapScore(tokens, a.text)
		distinctiveLexical := lexicalOverlapScore(distinctiveTokens, a.text)
		task := lexicalOverlapScore(taskTokens, a.text)
		recency := recencyScore(a.createdAt, newest)
		missingDistinctiveSupport := len(distinctiveTokens) > 0 && a.f == 0 && a.doc == 0 && distinctiveLexical == 0 && task == 0
		if missingDistinctiveSupport && a.v < minUnsupportedVectorScore {
			r.LastRejected = append(r.LastRejected, fmt.Sprintf("%s %s: weak vector support and missing distinctive query term", a.ref, oneLineForReject(a.text)))
			continue
		}
		score := (a.v * r.VectorWeight) + (a.f * r.FTSWeight) + (a.doc * r.FTSWeight) + (lexical * r.LexicalWeight) + (task * r.TaskWeight) + (recency * r.RecencyWeight)
		if missingDistinctiveSupport {
			score -= unsupportedDistinctiveVectorPenalty
		}

		// Apply small bounded metadata adjustments so vector/FTS relevance
		// still dominates while durable/active notes get a slight preference.
		score += metadataScoreAdjust(a.kind, a.status, a.importance, a.useCount)
		if score < 0 {
			score = 0
		}

		src := "hybrid"
		if a.f > 0 && a.v == 0 {
			src = "fts"
		}
		if a.v > 0 && a.f == 0 {
			src = "vector"
		}
		if a.doc > 0 {
			src = "doc"
		}
		reason := "ranked by relevance"
		if task > 0 {
			reason = "matches current task"
		} else if lexical > 0 {
			reason = "matches query terms"
		} else if a.doc > 0 {
			reason = "indexed document match"
		} else if missingDistinctiveSupport {
			reason = "semantic match with weak lexical support"
		}
		raw = append(raw, Retrieved{
			Source:     src,
			ID:         a.id,
			Text:       a.text,
			Score:      score,
			Kind:       a.kind,
			Status:     a.status,
			Importance: a.importance,
			Confidence: a.confidence,
			ExpiresAt:  a.expiresAt,
			UseCount:   a.useCount,
			CreatedAt:  a.createdAt,
			LastUsedAt: a.lastUsedAt,
			Ref:        a.ref,
			Reason:     reason,
		})
	}

	sort.Slice(raw, func(i, j int) bool {
		if raw[i].Score == raw[j].Score {
			return raw[i].ID > raw[j].ID
		}
		return raw[i].Score > raw[j].Score
	})
	return raw, nil
}

func (r *Retriever) packToBudget(candidates []Retrieved, topK int) []Retrieved {
	return r.diversifyRetrieved(candidates, topK)
}

func retrieveDocCandidates(ctx context.Context, d *db.DB, sessionKey, query string, topK int) ([]RetrievedDoc, error) {
	if d == nil || strings.TrimSpace(query) == "" || topK <= 0 {
		return nil, nil
	}
	retriever := &DocRetriever{DB: d}
	scopes := []string{scope.GlobalMemoryScope}
	if trimmed := strings.TrimSpace(sessionKey); trimmed != "" && trimmed != scope.GlobalMemoryScope {
		scopes = append(scopes, trimmed)
	}
	seen := map[string]struct{}{}
	out := make([]RetrievedDoc, 0, topK*len(scopes))
	for _, docScope := range scopes {
		docs, err := retriever.RetrieveDocs(ctx, docScope, query, topK)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			key := doc.Path + "\x00" + doc.Excerpt
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, doc)
		}
	}
	return out, nil
}

func searchFTSWithFallback(ctx context.Context, d *db.DB, sessionKey, query string, k int) ([]db.FTSCandidate, error) {
	query = strings.TrimSpace(query)
	if d == nil || query == "" || k <= 0 {
		return nil, nil
	}
	ftsQuery := normalizeFTSQuery(query)
	results, err := d.SearchFTS(ctx, sessionKey, ftsQuery, k)
	if err == nil {
		return results, nil
	}
	quoted := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	return d.SearchFTS(ctx, sessionKey, quoted, k)
}

// metadataScoreAdjust returns a small additive score correction (bounded to
// the range [-0.10, +0.07]) based on note lifecycle metadata so that
// relevance signals (vector/FTS) continue to dominate while active durable
// notes are slightly preferred over stale rolling summaries.
func metadataScoreAdjust(kind, status string, importance float64, useCount int) float64 {
	adj := 0.0

	// Status: strongly demote stale and superseded notes.
	if status == db.MemoryStatusStale || status == db.MemoryStatusSuperseded {
		adj -= 0.10
	}

	// Kind: durable operational kinds outrank rolling summaries slightly.
	switch kind {
	case db.MemoryKindFact, db.MemoryKindProcedure:
		adj += 0.03
	case db.MemoryKindPreference, db.MemoryKindGoal:
		adj += 0.02
	case db.MemoryKindSummary, db.MemoryKindEpisode:
		adj -= 0.01
	}

	// Importance boost (bounded to [0,1] * 0.04 → [0, 0.04]).
	if importance > 0 {
		imp := importance
		if imp > 1.0 {
			imp = 1.0
		}
		adj += imp * 0.04
	}

	// Use-count boost: small signal, capped at 5 uses × 0.01 = 0.05 max.
	if useCount > 0 {
		uc := useCount
		if uc > 5 {
			uc = 5
		}
		adj += float64(uc) * 0.01
	}

	return adj
}

func retrievalTokens(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 3 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func distinctiveRetrievalTokens(query string) []string {
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 3 || !isDistinctiveRetrievalToken(part) {
			continue
		}
		token := strings.ToLower(part)
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func isDistinctiveRetrievalToken(token string) bool {
	letters := 0
	upper := 0
	lower := 0
	digits := 0
	for _, r := range token {
		switch {
		case r >= 'A' && r <= 'Z':
			letters++
			upper++
		case r >= 'a' && r <= 'z':
			letters++
			lower++
		case r >= '0' && r <= '9':
			digits++
		}
	}
	if digits > 0 && letters > 0 {
		return true
	}
	if upper >= 2 {
		return true
	}
	return upper > 0 && lower > 0 && token != strings.ToLower(token)
}

func lexicalOverlapScore(tokens []string, text string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	lower := strings.ToLower(text)
	hits := 0
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			hits++
		}
	}
	return float64(hits) / float64(len(tokens))
}

func recencyScore(createdAt, newest int64) float64 {
	if createdAt <= 0 || newest <= 0 || createdAt >= newest {
		return 1
	}
	ageHours := float64(newest-createdAt) / (1000 * 60 * 60)
	if ageHours <= 0 {
		return 1
	}
	return math.Exp(-ageHours / (24 * 14))
}

func (r *Retriever) diversifyRetrieved(items []Retrieved, topK int) []Retrieved {
	if topK <= 0 || len(items) == 0 {
		return nil
	}
	selected := make([]Retrieved, 0, min(topK, len(items)))
	seenCanonical := map[string]struct{}{}
	sourceCounts := map[string]int{}
	for _, item := range items {
		if reject := retrievalRejectReason(item); reject != "" {
			r.LastRejected = append(r.LastRejected, fmt.Sprintf("%s %s: %s", item.Ref, oneLineForReject(item.Text), reject))
			continue
		}
		canonical := canonicalRetrievedText(item.Text)
		if canonical != "" {
			if _, ok := seenCanonical[canonical]; ok {
				r.LastRejected = append(r.LastRejected, fmt.Sprintf("%s: duplicate", item.Ref))
				continue
			}
			duplicate := false
			for _, existing := range selected {
				if similarRetrievedText(existing.Text, item.Text) {
					duplicate = true
					break
				}
			}
			if duplicate {
				r.LastRejected = append(r.LastRejected, fmt.Sprintf("%s: near-duplicate", item.Ref))
				continue
			}
		}
		penalty := 1.0 / float64(sourceCounts[item.Source]+1)
		item.Score = item.Score * (0.85 + 0.15*penalty)
		selected = append(selected, item)
		if canonical != "" {
			seenCanonical[canonical] = struct{}{}
		}
		sourceCounts[item.Source]++
		if len(selected) >= topK {
			break
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Score == selected[j].Score {
			return selected[i].ID > selected[j].ID
		}
		return selected[i].Score > selected[j].Score
	})
	return selected
}

func retrievalRejectReason(item Retrieved) string {
	if item.Status == db.MemoryStatusStale || item.Status == db.MemoryStatusSuperseded {
		return "lifecycle status " + item.Status
	}
	if item.ExpiresAt > 0 && item.ExpiresAt <= db.NowMS() {
		return "expired"
	}
	if item.Confidence > 0 && item.Confidence < 0.2 {
		return "low confidence"
	}
	return ""
}

func oneLineForReject(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 80 {
		return text[:80] + "..."
	}
	return text
}

func canonicalRetrievedText(text string) string {
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	if len(text) > 180 {
		text = text[:180]
	}
	return text
}

func similarRetrievedText(a, b string) bool {
	ac := canonicalRetrievedText(a)
	bc := canonicalRetrievedText(b)
	if ac == "" || bc == "" {
		return false
	}
	if ac == bc {
		return true
	}
	at := retrievalTokens(ac)
	bt := retrievalTokens(bc)
	if len(at) == 0 || len(bt) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, token := range at {
		set[token] = struct{}{}
	}
	shared := 0
	union := len(set)
	for _, token := range bt {
		if _, ok := set[token]; ok {
			shared++
			continue
		}
		union++
	}
	if union == 0 {
		return false
	}
	return float64(shared)/float64(union) >= 0.8
}

func normalizeFTSQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	// simple: split on spaces, quote terms that contain punctuation
	parts := strings.Fields(q)
	for i, p := range parts {
		if strings.ContainsAny(p, `":*()-`) {
			parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		}
	}
	return strings.Join(parts, " ")
}
