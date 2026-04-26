package memory

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"

	"or3-intern/internal/db"
)

func PackFloat32(vec []float32) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, vec)
	return b.Bytes()
}

func UnpackFloat32(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 {
		return nil, errors.New("invalid float32 blob")
	}
	out := make([]float32, len(blob)/4)
	if err := binary.Read(bytes.NewReader(blob), binary.LittleEndian, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func Cosine(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

type VecCandidate struct {
	ID         int64
	Text       string
	Score      float64
	CreatedAt  int64
	Kind       string
	Status     string
	Importance float64
	Confidence float64
	ExpiresAt  int64
	UseCount   int
	LastUsedAt int64
}

type candMinHeap []VecCandidate

func (h candMinHeap) Len() int           { return len(h) }
func (h candMinHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h candMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candMinHeap) Push(x any)        { *h = append(*h, x.(VecCandidate)) }
func (h *candMinHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func VectorSearch(ctx context.Context, d *db.DB, sessionKey string, queryVec []float32, k int, scanLimit int) ([]VecCandidate, error) {
	_ = scanLimit
	queryBlob := PackFloat32(queryVec)
	rows, err := d.SearchMemoryVectors(ctx, sessionKey, queryBlob, k)
	if err != nil {
		return nil, err
	}
	out := make([]VecCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, VecCandidate{
			ID:         row.ID,
			Text:       row.Text,
			Score:      1.0 / (1.0 + row.Distance),
			CreatedAt:  row.CreatedAt,
			Kind:       row.Kind,
			Status:     row.Status,
			Importance: row.Importance,
			Confidence: row.Confidence,
			ExpiresAt:  row.ExpiresAt,
			UseCount:   row.UseCount,
			LastUsedAt: row.LastUsedAt,
		})
	}
	return out, nil
}
