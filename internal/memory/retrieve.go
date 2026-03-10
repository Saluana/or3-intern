package memory

import (
	"context"
	"math"
	"sort"
	"strings"

	"or3-intern/internal/db"
)

type Retrieved struct {
	Source string // pinned|vector|fts
	ID int64
	Text string
	Score float64
}

type Retriever struct {
	DB *db.DB
	VectorWeight float64
	FTSWeight float64
	LexicalWeight float64
	RecencyWeight float64
	VectorScanLimit int
}

func NewRetriever(d *db.DB) *Retriever {
	return &Retriever{DB: d, VectorWeight: 0.55, FTSWeight: 0.25, LexicalWeight: 0.12, RecencyWeight: 0.08, VectorScanLimit: 2000}
}

func (r *Retriever) Retrieve(ctx context.Context, sessionKey, query string, queryVec []float32, vectorK, ftsK, topK int) ([]Retrieved, error) {
	vecs, err := VectorSearch(ctx, r.DB, sessionKey, queryVec, vectorK, r.VectorScanLimit)
	if err != nil { return nil, err }
	fts, _ := r.DB.SearchFTS(ctx, sessionKey, normalizeFTSQuery(query), ftsK)

	type agg struct {
		id int64
		text string
		v float64
		f float64
		createdAt int64
	}
	m := map[int64]*agg{}
	for _, c := range vecs {
		a := m[c.ID]
		if a == nil { a = &agg{id: c.ID, text: c.Text}; m[c.ID] = a }
		a.v = c.Score
		if c.CreatedAt > a.createdAt {
			a.createdAt = c.CreatedAt
		}
	}
	for _, f := range fts {
		a := m[f.ID]
		if a == nil { a = &agg{id: f.ID, text: f.Text}; m[f.ID] = a }
		// bm25 lower is better. Convert to a positive "higher is better".
		a.f = 1.0 / (1.0 + f.Rank)
		if f.CreatedAt > a.createdAt {
			a.createdAt = f.CreatedAt
		}
	}

	raw := make([]Retrieved, 0, len(m))
	tokens := retrievalTokens(query)
	newest := int64(0)
	for _, a := range m {
		if a.createdAt > newest {
			newest = a.createdAt
		}
	}
	for _, a := range m {
		lexical := lexicalOverlapScore(tokens, a.text)
		recency := recencyScore(a.createdAt, newest)
		score := (a.v * r.VectorWeight) + (a.f * r.FTSWeight) + (lexical * r.LexicalWeight) + (recency * r.RecencyWeight)
		src := "hybrid"
		if a.f > 0 && a.v == 0 { src = "fts" }
		if a.v > 0 && a.f == 0 { src = "vector" }
		raw = append(raw, Retrieved{Source: src, ID: a.id, Text: a.text, Score: score})
	}

	sort.Slice(raw, func(i, j int) bool {
		if raw[i].Score == raw[j].Score {
			return raw[i].ID > raw[j].ID
		}
		return raw[i].Score > raw[j].Score
	})
	return diversifyRetrieved(raw, topK), nil
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

func diversifyRetrieved(items []Retrieved, topK int) []Retrieved {
	if topK <= 0 || len(items) == 0 {
		return nil
	}
	selected := make([]Retrieved, 0, min(topK, len(items)))
	seenCanonical := map[string]struct{}{}
	sourceCounts := map[string]int{}
	for _, item := range items {
		canonical := canonicalRetrievedText(item.Text)
		if canonical != "" {
			if _, ok := seenCanonical[canonical]; ok {
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
	if q == "" { return "" }
	// simple: split on spaces, quote terms that contain punctuation
	parts := strings.Fields(q)
	for i, p := range parts {
		if strings.ContainsAny(p, `":*`) {
			parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		}
	}
	return strings.Join(parts, " ")
}
