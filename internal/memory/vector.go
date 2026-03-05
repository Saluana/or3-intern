package memory

import (
	"bytes"
	"context"
	"container/heap"
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

func VectorSearch(ctx context.Context, d *db.DB, queryVec []float32, k int) ([]VecCandidate, error) {
	rows, err := d.StreamMemoryNotes(ctx)
	if err != nil { return nil, err }
	defer rows.Close()

	h := &candMinHeap{}
	heap.Init(h)

	for rows.Next() {
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		if err := rows.Scan(&id, &text, &emb, &src, &tags, &created); err != nil {
			return nil, err
		}
		v, err := UnpackFloat32(emb)
		if err != nil { continue }
		score := Cosine(queryVec, v)
		if h.Len() < k {
			heap.Push(h, VecCandidate{ID: id, Text: text, Score: score})
		} else if (*h)[0].Score < score {
			(*h)[0] = VecCandidate{ID: id, Text: text, Score: score}
			heap.Fix(h, 0)
		}
	}
	if err := rows.Err(); err != nil { return nil, err }

	// pop into descending slice
	out := make([]VecCandidate, h.Len())
	for i := len(out)-1; i >= 0; i-- {
		out[i] = heap.Pop(h).(VecCandidate)
	}
	// now out ascending (min->max). reverse to max->min
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 { out[i], out[j] = out[j], out[i] }
	return out, nil
}
