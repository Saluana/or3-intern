package shared

import (
	"encoding/json"
	"fmt"
	"io"
)

const MaxAPIResponseBytes int64 = 4 << 20

func DecodeJSONLimited(reader io.Reader, out any) error {
	data, err := io.ReadAll(io.LimitReader(reader, MaxAPIResponseBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > MaxAPIResponseBytes {
		return fmt.Errorf("channel API response exceeds %d bytes", MaxAPIResponseBytes)
	}
	return json.Unmarshal(data, out)
}

func ReadErrorPreview(reader io.Reader) string {
	const limit int64 = 64 << 10
	data, _ := io.ReadAll(io.LimitReader(reader, limit+1))
	if int64(len(data)) > limit {
		data = append(data[:limit], []byte("...[truncated]")...)
	}
	return string(data)
}
