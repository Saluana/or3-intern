package channels

import (
	"fmt"
	"strings"
)

func ComposeMessageText(text string, markers []string) string {
	parts := make([]string, 0, len(markers)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, strings.TrimSpace(text))
	}
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker == "" {
			continue
		}
		parts = append(parts, marker)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func MediaPaths(meta map[string]any) []string {
	if len(meta) == 0 {
		return nil
	}
	raw := meta["media_paths"]
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				continue
			}
			out = append(out, item)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
