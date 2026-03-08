package memory

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/scope"
)

func PackFloat32(vec []float32) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, vec)
	return b.Bytes()
}

func UnpackFloat32(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 { return nil, errors.New("invalid float32 blob") }
	out := make([]float32, len(blob)/4)
	if err := binary.Read(bytes.NewReader(blob), binary.LittleEndian, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func Cosine(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n { n = len(b) }
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 { return 0 }
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

type VecCandidate struct {
	ID int64
	Text string
	Score float64
}

type candMinHeap []VecCandidate

func (h candMinHeap) Len() int { return len(h) }
func (h candMinHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h candMinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *candMinHeap) Push(x any) { *h = append(*h, x.(VecCandidate)) }
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
	scopes := []string{scope.GlobalMemoryScope}
	if trimmedSessionKey := strings.TrimSpace(sessionKey); trimmedSessionKey != "" && trimmedSessionKey != scope.GlobalMemoryScope {
		scopes = append(scopes, sessionKey)
	}
	seen := make(map[int64]struct{}, k*len(scopes))
	out := make([]VecCandidate, 0, k*len(scopes))
	for _, memoryScope := range scopes {
		rows, err := d.SearchVecScope(ctx, memoryScope, queryBlob, k)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			rows, err = d.SearchVecScopeFallback(ctx, memoryScope, queryBlob, k)
			if err != nil {
				return nil, err
			}
		}
		for _, row := range rows {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			out = append(out, VecCandidate{
				ID:    row.ID,
				Text:  row.Text,
				Score: 1.0 / (1.0 + row.Distance),
			})
		}
	}
	return out, nil
}

func addVectorCandidates(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, queryVec []float32, k int, h *candMinHeap) error {
	for rows.Next() {
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		if err := rows.Scan(&id, &text, &emb, &src, &tags, &created); err != nil {
			return err
		}
		v, err := UnpackFloat32(emb)
		if err != nil {
			continue
		}
		score := Cosine(queryVec, v)
		if h.Len() < k {
			heap.Push(h, VecCandidate{ID: id, Text: text, Score: score})
		} else if (*h)[0].Score < score {
			(*h)[0] = VecCandidate{ID: id, Text: text, Score: score}
			heap.Fix(h, 0)
		}
	}
	return rows.Err()
}
