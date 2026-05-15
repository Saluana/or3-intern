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

const maxStructuredBufferBytes = DefaultPreviewMaxBytes

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
	if stream == "stdout" && isStructuredOutputMode(outputMode) {
		readStructuredStream(r, stream, chunkMaxBytes, seq, collector, onEvent)
		return
	}

	reader := bufio.NewReader(r)
	maxBufferedLineBytes := max(chunkMaxBytes, DefaultEventChunkMaxBytes)
	var pending strings.Builder

	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			pending.Write(fragment)
		}

		if err == nil {
			emitStreamLine(pending.String(), stream, chunkMaxBytes, seq, collector, onEvent, outputMode)
			pending.Reset()
			continue
		}
		if pending.Len() > maxBufferedLineBytes {
			emitStreamOutputChunks(strings.TrimRight(pending.String(), "\r\n"), stream, chunkMaxBytes, seq, collector, onEvent)
			pending.Reset()
		}
		if errors.Is(err, io.EOF) {
			if pending.Len() > 0 {
				emitStreamLine(pending.String(), stream, chunkMaxBytes, seq, collector, onEvent, outputMode)
			}
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if onEvent != nil {
			onEvent(AgentRunEvent{
				Type:    "error",
				Seq:     atomic.AddInt64(seq, 1),
				TS:      time.Now().UTC().Format(time.RFC3339Nano),
				Message: fmt.Sprintf("%s stream read: %v", stream, err),
			})
		}
		break
	}
}

func readStructuredStream(r io.Reader, stream string, chunkMaxBytes int, seq *int64, collector *outputCollector, onEvent func(AgentRunEvent)) {
	reader := bufio.NewReader(r)
	var pending strings.Builder

	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			pending.Write(fragment)
			if collector != nil {
				collector.stdout.Write(fragment)
			}
			if drainStructuredBuffer(&pending, seq, collector, onEvent) {
				continue
			}
		}

		if pending.Len() > maxStructuredBufferBytes {
			emitStreamOutputChunks(strings.TrimRight(pending.String(), "\r\n"), stream, chunkMaxBytes, seq, nil, onEvent)
			pending.Reset()
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			if pending.Len() > 0 && !drainStructuredBuffer(&pending, seq, collector, onEvent) {
				emitStreamOutputChunks(strings.TrimRight(pending.String(), "\r\n"), stream, chunkMaxBytes, seq, nil, onEvent)
				pending.Reset()
			}
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if onEvent != nil {
			onEvent(AgentRunEvent{
				Type:    "error",
				Seq:     atomic.AddInt64(seq, 1),
				TS:      time.Now().UTC().Format(time.RFC3339Nano),
				Message: fmt.Sprintf("%s stream read: %v", stream, err),
			})
		}
		break
	}
}

func emitStreamLine(rawLine, stream string, chunkMaxBytes int, seq *int64, collector *outputCollector, onEvent func(AgentRunEvent), outputMode OutputMode) {
	line := strings.TrimRight(rawLine, "\r\n")

	switch stream {
	case "stdout":
		collector.stdout.Write([]byte(line))
		collector.stdout.Write([]byte{'\n'})
	case "stderr":
		collector.stderr.Write([]byte(line))
		collector.stderr.Write([]byte{'\n'})
	}

	emittedStructured := false
	if stream == "stdout" && (outputMode == OutputJSONL || outputMode == OutputJSON) {
		emittedStructured = emitStructuredIfValid(onEvent, seq, collector, line)
	}
	if !emittedStructured {
		emitStreamOutputChunks(line, stream, chunkMaxBytes, seq, nil, onEvent)
	}
}

func emitStreamOutputChunks(line, stream string, chunkMaxBytes int, seq *int64, collector *outputCollector, onEvent func(AgentRunEvent)) {
	if collector != nil {
		switch stream {
		case "stdout":
			collector.stdout.Write([]byte(line))
		case "stderr":
			collector.stderr.Write([]byte(line))
		}
	}
	for _, chunk := range splitChunks(line, chunkMaxBytes) {
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
}

func emitStructuredIfValid(onEvent func(AgentRunEvent), seq *int64, collector *outputCollector, raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	payloads, _ := decodeStructuredPayloads(raw)
	if len(payloads) == 0 {
		return false
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
	return true
}

func decodeStructuredPayloads(raw string) ([]json.RawMessage, int) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	payloads := make([]json.RawMessage, 0, 1)
	consumed := 0
	for {
		var payload json.RawMessage
		if err := decoder.Decode(&payload); err != nil {
			if errors.Is(err, io.EOF) {
				return payloads, len(raw)
			}
			return payloads, consumed
		}
		consumed = int(decoder.InputOffset())
		payloads = append(payloads, append(json.RawMessage(nil), payload...))
	}
}

func drainStructuredBuffer(pending *strings.Builder, seq *int64, collector *outputCollector, onEvent func(AgentRunEvent)) bool {
	raw := pending.String()
	payloads, consumed := decodeStructuredPayloads(raw)
	if len(payloads) == 0 {
		return false
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
	remaining := raw[consumed:]
	pending.Reset()
	if strings.TrimSpace(remaining) != "" {
		pending.WriteString(remaining)
	}
	return true
}

func isStructuredOutputMode(outputMode OutputMode) bool {
	return outputMode == OutputJSON || outputMode == OutputJSONL
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
