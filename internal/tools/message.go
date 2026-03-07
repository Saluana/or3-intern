package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DeliverFunc func(ctx context.Context, channel, to, text string, meta map[string]any) error

type SendMessage struct {
	Base
	Deliver        DeliverFunc
	DefaultChannel string
	DefaultTo      string
	AllowedRoot    string
	ArtifactsDir   string
	MaxMediaBytes  int
}

func (t *SendMessage) Name() string { return "send_message" }
func (t *SendMessage) Description() string {
	return "Send a message via a configured channel (for reminders/cron or proactive messages)."
}
func (t *SendMessage) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"channel": map[string]any{"type": "string"},
		"to":      map[string]any{"type": "string"},
		"text":    map[string]any{"type": "string"},
		"media": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional local file paths to send as attachments.",
		},
	}, "required": []string{}}
}
func (t *SendMessage) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *SendMessage) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Deliver == nil {
		return "", fmt.Errorf("deliver not configured")
	}
	ctxChannel, ctxTo := DeliveryFromContext(ctx)
	ch := readOptionalString(params, "channel")
	to := readOptionalString(params, "to")
	text := readOptionalString(params, "text")
	if ch == "" {
		ch = strings.TrimSpace(t.DefaultChannel)
	}
	if ch == "" {
		ch = strings.TrimSpace(ctxChannel)
	}
	if to == "" {
		to = strings.TrimSpace(t.DefaultTo)
	}
	if to == "" {
		to = strings.TrimSpace(ctxTo)
	}
	mediaPaths, err := t.validateMediaPaths(params["media"])
	if err != nil {
		return "", err
	}
	if text == "" && len(mediaPaths) == 0 {
		return "", fmt.Errorf("message requires text or media")
	}
	var meta map[string]any
	if len(mediaPaths) > 0 {
		meta = map[string]any{"media_paths": mediaPaths}
	}
	if err := t.Deliver(ctx, ch, to, text, meta); err != nil {
		return "", err
	}
	return "ok", nil
}

func (t *SendMessage) validateMediaPaths(raw any) ([]string, error) {
	items, err := stringSlice(raw)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	roots := make([]string, 0, 2)
	if strings.TrimSpace(t.AllowedRoot) != "" {
		roots = append(roots, strings.TrimSpace(t.AllowedRoot))
	}
	if strings.TrimSpace(t.ArtifactsDir) != "" {
		roots = append(roots, strings.TrimSpace(t.ArtifactsDir))
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		p, err := filepath.Abs(strings.TrimSpace(item))
		if err != nil {
			return nil, err
		}
		p, err = canonicalizePath(p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("media path is a directory: %s", item)
		}
		if t.MaxMediaBytes == 0 {
			return nil, fmt.Errorf("media attachments disabled by config")
		}
		if t.MaxMediaBytes > 0 && info.Size() > int64(t.MaxMediaBytes) {
			return nil, fmt.Errorf("media path exceeds maxMediaBytes: %s", item)
		}
		if len(roots) > 0 {
			allowed := false
			for _, root := range roots {
				ok, err := pathWithinRoot(p, root)
				if err != nil {
					return nil, err
				}
				if ok {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, fmt.Errorf("media path outside allowed roots: %s", item)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func pathWithinRoot(absPath, root string) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	root, err = canonicalizeRoot(root)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func stringSlice(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				continue
			}
			out = append(out, item)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("media must be an array of strings")
	}
}
