package agentcli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
)

type outputCollector struct {
	stdout    *ringBuffer
	stderr    *ringBuffer
	extractor *finalTextExtractor
}

func newOutputCollector(maxBytes int, runnerID RunnerID) *outputCollector {
	return &outputCollector{
		stdout:    newRingBuffer(maxBytes),
		stderr:    newRingBuffer(maxBytes),
		extractor: newFinalTextExtractor(runnerID),
	}
}

type ringBuffer struct {
	buf  []byte
	pos  int
	full bool
}

func newRingBuffer(size int) *ringBuffer {
	if size <= 0 {
		size = DefaultPreviewMaxBytes
	}
	return &ringBuffer{buf: make([]byte, size)}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	for _, b := range p {
		r.buf[r.pos] = b
		r.pos++
		if r.pos >= len(r.buf) {
			r.pos = 0
			r.full = true
		}
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	if !r.full {
		return string(r.buf[:r.pos])
	}
	return string(r.buf[r.pos:]) + string(r.buf[:r.pos])
}

func readStream(r io.Reader, stream string, chunkMaxBytes int, seq *int64, collector *outputCollector, onEvent func(AgentRunEvent), outputMode OutputMode) {
	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)
	scanner.Buffer(make([]byte, chunkMaxBytes+1), chunkMaxBytes+1)

	for scanner.Scan() {
		line := scanner.Bytes()
		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)

		switch stream {
		case "stdout":
			collector.stdout.Write(lineCopy)
			collector.stdout.Write([]byte{'\n'})
		case "stderr":
			collector.stderr.Write(lineCopy)
			collector.stderr.Write([]byte{'\n'})
		}

		chunks := splitChunks(string(line), chunkMaxBytes)
		for _, chunk := range chunks {
			seqNum := atomic.AddInt64(seq, 1)
			if onEvent != nil {
				onEvent(AgentRunEvent{
					Type:   "output",
					Seq:    seqNum,
					TS:     time.Now().UTC().Format(time.RFC3339Nano),
					Stream: stream,
					Chunk:  chunk,
				})
			}
		}

		if stream == "stdout" && (outputMode == OutputJSONL || outputMode == OutputJSON) {
			emitStructuredIfValid(onEvent, seq, collector, string(lineCopy))
		}
	}

	if err := scanner.Err(); err != nil && onEvent != nil {
		onEvent(AgentRunEvent{
			Type:    "error",
			Seq:     atomic.AddInt64(seq, 1),
			TS:      time.Now().UTC().Format(time.RFC3339Nano),
			Message: fmt.Sprintf("%s stream read: %v", stream, err),
		})
	}
}

func emitStructuredIfValid(onEvent func(AgentRunEvent), seq *int64, collector *outputCollector, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	payloads := decodeStructuredPayloads(raw)
	if len(payloads) == 0 {
		return
	}
	for _, payload := range payloads {
		if collector != nil && collector.extractor != nil {
			collector.extractor.Consider(payload)
		}
		if onEvent != nil {
			onEvent(AgentRunEvent{
				Type:    "structured",
				Seq:     atomic.AddInt64(seq, 1),
				TS:      time.Now().UTC().Format(time.RFC3339Nano),
				Payload: payload,
			})
		}
	}
}

func decodeStructuredPayloads(raw string) []json.RawMessage {
	decoder := json.NewDecoder(strings.NewReader(raw))
	payloads := make([]json.RawMessage, 0, 1)
	for {
		var payload json.RawMessage
		if err := decoder.Decode(&payload); err != nil {
			if errors.Is(err, io.EOF) {
				return payloads
			}
			return nil
		}
		payloads = append(payloads, append(json.RawMessage(nil), payload...))
	}
}

func splitChunks(data string, maxBytes int) []string {
	if maxBytes <= 0 {
		maxBytes = DefaultEventChunkMaxBytes
	}
	if len(data) <= maxBytes {
		return []string{data}
	}
	var chunks []string
	for len(data) > maxBytes {
		chunks = append(chunks, data[:maxBytes])
		data = data[maxBytes:]
	}
	if len(data) > 0 {
		chunks = append(chunks, data)
	}
	return chunks
}
