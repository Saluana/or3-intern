package log

import (
	"os"
	"strings"
)

type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

func ParseLevel(value string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return LevelDebug, true
	case "", "info":
		return LevelInfo, true
	case "warn", "warning":
		return LevelWarn, true
	case "error":
		return LevelError, true
	default:
		return LevelInfo, false
	}
}

func ResolveLevel() Level {
	level, ok := ParseLevel(os.Getenv("LOG_LEVEL"))
	if !ok {
		return LevelInfo
	}
	return level
}

func CurrentLevel() Level {
	return ResolveLevel()
}

func Enabled(level Level) bool {
	return levelRank(level) >= levelRank(CurrentLevel())
}

func levelRank(level Level) int {
	switch level {
	case LevelDebug:
		return 10
	case LevelInfo:
		return 20
	case LevelWarn:
		return 30
	case LevelError:
		return 40
	default:
		return 20
	}
}
