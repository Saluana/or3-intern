package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"or3-intern/internal/db"
)

func migrateJSONL(ctx context.Context, d *db.DB, path, sessionKey string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024), 4<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			// tolerate non-json line
			if _, err := d.AppendMessage(ctx, sessionKey, "user", line, map[string]any{"migrated_line": lineNo}); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			continue
		}
		// detect metadata
		if lineNo == 1 {
			// store as session metadata_json if it looks like metadata
			if obj["role"] == nil && obj["content"] == nil {
				b, _ := json.Marshal(obj)
				if err := d.EnsureSession(ctx, sessionKey); err != nil {
					log.Printf("ensure session failed during migration: %v", err)
				}
				if _, err := d.SQL.ExecContext(ctx, `UPDATE sessions SET metadata_json=? WHERE key=?`, string(b), sessionKey); err != nil {
					log.Printf("session metadata update failed during migration: %v", err)
				}
				continue
			}
		}
		role := toStr(obj["role"])
		if role == "" {
			role = "user"
		}
		content := toStr(obj["content"])
		payload := obj
		delete(payload, "role")
		delete(payload, "content")
		_, err := d.AppendMessage(ctx, sessionKey, role, content, payload)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	return sc.Err()
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(v)
	}
}
