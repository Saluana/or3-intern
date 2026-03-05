package memory

import (
	"context"
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
}

func NewRetriever(d *db.DB) *Retriever {
	return &Retriever{DB: d, VectorWeight: 0.7, FTSWeight: 0.3}
}

func (r *Retriever) Retrieve(ctx context.Context, query string, queryVec []float32, vectorK, ftsK, topK int) ([]Retrieved, error) {
	vecs, err := VectorSearch(ctx, r.DB, queryVec, vectorK)
	if err != nil { return nil, err }
	fts, _ := r.DB.SearchFTS(ctx, normalizeFTSQuery(query), ftsK)

	type agg struct {
		id int64
		text string
		v float64
		f float64
	}
	m := map[int64]*agg{}
	for _, c := range vecs {
		a := m[c.ID]
		if a == nil { a = &agg{id: c.ID, text: c.Text}; m[c.ID] = a }
		a.v = c.Score
	}
	for _, f := range fts {
		a := m[f.ID]
		if a == nil { a = &agg{id: f.ID, text: f.Text}; m[f.ID] = a }
		// bm25 lower is better. Convert to a positive "higher is better".
		a.f = 1.0 / (1.0 + f.Rank)
	}

	out := make([]Retrieved, 0, len(m))
	for _, a := range m {
		score := (a.v * r.VectorWeight) + (a.f * r.FTSWeight)
		src := "hybrid"
		if a.f > 0 && a.v == 0 { src = "fts" }
		if a.v > 0 && a.f == 0 { src = "vector" }
		out = append(out, Retrieved{Source: src, ID: a.id, Text: a.text, Score: score})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID > out[j].ID // stable-ish
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > topK { out = out[:topK] }
	return out, nil
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
